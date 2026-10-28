import XCTest
@testable import Zakki

// 写回管道测试：模拟编辑后 saveTabText 路径 encode→落盘→重新 decode 一致；
// 未知字段/桌面专属 settings 保真；writable=false 拒绝写入；write_protected 兜底。
// 全部在临时沙盒进行，不触碰真实用户目录。
final class EditorWritebackTests: XCTestCase {
    private let v3Sample = """
    {"version":3,"active":0,"tabs":[
      {"id":1,"name":"Inbox","text":"first line\\nsecond line","color":"#d4253a"}
    ]}
    """

    private let v4Sample = """
    {"version":4,"active":0,"tabs":[{"id":1,"name":"Future","text":"newer app","color":"#0f75e9"}]}
    """

    @MainActor
    private func makeModel() throws -> (NotesModel, URL, URL) {
        let dir = FileManager.default.temporaryDirectory
            .appendingPathComponent("zakki-writeback-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let sandbox = dir.appendingPathComponent("zakki.json")
        return (NotesModel(sandboxURL: sandbox), sandbox, dir)
    }

    @MainActor
    private func importSample(_ json: String, into dir: URL, model: NotesModel) throws {
        let src = dir.appendingPathComponent("import-\(UUID().uuidString).json")
        try json.write(to: src, atomically: true, encoding: .utf8)
        XCTAssertTrue(model.importFile(at: src))
    }

    @MainActor
    private func readSandbox(_ sandbox: URL) throws -> NotesSnapshot {
        try NotesCore.decode(try String(contentsOf: sandbox, encoding: .utf8))
    }

    // 编辑 → 防抖落盘 → 重新 decode 的 text 一致；raw 基线更新为 encode 输出。
    @MainActor
    func testEditedTextRoundTripsThroughDisk() throws {
        let (model, sandbox, dir) = try makeModel()
        try importSample(v3Sample, into: dir, model: model)
        let edited = "edited @2026-01-01 10:00 ~10m ^1d\nsecond line"
        model.updateTabText(edited, tabID: 1)
        model.flushPendingSave()
        XCTAssertNil(model.saveError)

        let onDisk = try String(contentsOf: sandbox, encoding: .utf8)
        let snap = try readSandbox(sandbox)
        XCTAssertEqual(snap.tabs?[0].text, edited)
        XCTAssertEqual(model.raw, onDisk, "encode 输出应成为新 raw 基线")

        // 幂等：无新增改动时 flush 不再写。
        model.flushPendingSave()
        XCTAssertEqual(try String(contentsOf: sandbox, encoding: .utf8), onDisk)
    }

    // 防抖：updateTabText 后未到期限不落盘，期限到点自动写入。
    @MainActor
    func testDebounceHoldsWriteUntilDeadline() async throws {
        let (model, sandbox, dir) = try makeModel()
        try importSample(v3Sample, into: dir, model: model)
        model.updateTabText("pending edit", tabID: 1)
        XCTAssertEqual(try readSandbox(sandbox).tabs?[0].text, "first line\nsecond line", "防抖期内磁盘仍是旧文")
        try await Task.sleep(nanoseconds: UInt64((NotesModel.saveDebounceInterval + 0.5) * 1_000_000_000))
        XCTAssertEqual(try readSandbox(sandbox).tabs?[0].text, "pending edit", "防抖到点应自动落盘")
    }

    // 三层未知字段（顶层/tab/settings）与桌面专属 settings 经编辑写回不丢。
    @MainActor
    func testUnknownFieldsAndDesktopSettingsSurviveWriteback() throws {
        let (model, sandbox, dir) = try makeModel()
        let raw = """
        {"version":3,"active":0,"topFuture":{"a":1},
         "tabs":[{"id":1,"name":"N","text":"T","color":"#000000","tabFuture":{"k":true}}],
         "settings":{"alwaysOnTop":true,"hideOnEscape":false,"settingsFuture":{"deep":{"x":1}}}}
        """
        try importSample(raw, into: dir, model: model)
        model.updateTabText("T2 @2026-01-01 10:00", tabID: 1)
        model.flushPendingSave()
        XCTAssertNil(model.saveError)

        let obj = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(try String(contentsOf: sandbox, encoding: .utf8).utf8)) as? [String: Any])
        XCTAssertEqual((obj["topFuture"] as? [String: Any])?["a"] as? Int, 1)
        let tabs = try XCTUnwrap(obj["tabs"] as? [[String: Any]])
        XCTAssertEqual(tabs[0]["text"] as? String, "T2 @2026-01-01 10:00")
        XCTAssertEqual((tabs[0]["tabFuture"] as? [String: Any])?["k"] as? Bool, true)
        let settings = try XCTUnwrap(obj["settings"] as? [String: Any])
        XCTAssertEqual(settings["alwaysOnTop"] as? Bool, true)
        XCTAssertEqual(settings["hideOnEscape"] as? Bool, false)
        let deep = try XCTUnwrap(((settings["settingsFuture"] as? [String: Any])?["deep"] as? [String: Any]))
        XCTAssertEqual(deep["x"] as? Int, 1)
    }

    // writable=false：编辑入口拒绝、flush 无写、磁盘字节不动。
    @MainActor
    func testWriteProtectedRefusesEditAndWrite() throws {
        let (model, sandbox, dir) = try makeModel()
        try importSample(v4Sample, into: dir, model: model)
        XCTAssertFalse(model.snapshot?.writable ?? true)

        model.updateTabText("hacked", tabID: 1)
        model.flushPendingSave()
        XCTAssertNil(model.saveError)

        let onDisk = try String(contentsOf: sandbox, encoding: .utf8)
        XCTAssertEqual(onDisk, v4Sample, "写保护文件字节不动")
        // v4 仍按只读语义解码（tabs 不外露），reason 保持 versionTooNew。
        XCTAssertEqual(try readSandbox(sandbox).reason, WriteProtectReason.versionTooNew.rawValue)
    }

    // write_protected 兜底：落盘时基线被污染（如外部并发改写）→ encode 拒绝且
    // saveError 如实上报，待写集合保留供重试。
    @MainActor
    func testEncodeRejectSurfacesSaveError() throws {
        let (model, sandbox, dir) = try makeModel()
        try importSample(v3Sample, into: dir, model: model)
        model.updateTabText("t2", tabID: 1)
        model.debugReplaceRawForTesting(v4Sample)
        model.flushPendingSave()
        XCTAssertNotNil(model.saveError, "写回失败必须如实上报")
        let onDisk = try String(contentsOf: sandbox, encoding: .utf8)
        XCTAssertFalse(onDisk.contains("t2"), "失败的写回不落盘")
    }
}

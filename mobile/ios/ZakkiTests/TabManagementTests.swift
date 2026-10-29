import XCTest
@testable import Zakki

// 标签管理（M2-4）模型层测试：增删改色排序即时落盘 → 重新 decode 往返一致；
// 9 上限、写保护拒绝、active 三种桌面语义、未知字段三层保真、unusedTabColor。
// 全部在临时沙盒进行，不触碰真实用户目录。
final class TabManagementTests: XCTestCase {
    // 两标签 v3 文件（id 1/2，active=1 指向第二个）。
    private let v3Sample = """
    {"version":3,"active":1,"tabs":[
      {"id":1,"name":"Inbox","text":"first","color":"#d4253a"},
      {"id":2,"name":"Ideas","text":"second","color":"#65c802"}
    ]}
    """

    private let v4Sample = """
    {"version":4,"active":0,"tabs":[{"id":1,"name":"Future","text":"newer app","color":"#0f75e9"}]}
    """

    private let v2Sample = """
    {"version":2,"active":0,"tabs":[{"id":1,"name":"Legacy","text":"old","color":"#65c802"}]}
    """

    @MainActor
    private func makeModel() throws -> (NotesModel, URL, URL) {
        let dir = FileManager.default.temporaryDirectory
            .appendingPathComponent("zakki-tabmgmt-\(UUID().uuidString)")
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

    // 新增：追加到末尾、id=max+1、active 落末尾、即时落盘、raw 基线同步。
    @MainActor
    func testAddTabAppendsAndRoundTripsThroughDisk() throws {
        let (model, sandbox, dir) = try makeModel()
        try importSample(v3Sample, into: dir, model: model)

        XCTAssertTrue(model.addTab(name: "", color: "#00a3b4"))
        XCTAssertNil(model.saveError)

        let onDisk = try String(contentsOf: sandbox, encoding: .utf8)
        XCTAssertEqual(model.raw, onDisk, "落盘 JSON 应成为新 raw 基线")
        let snap = try readSandbox(sandbox)
        XCTAssertEqual(snap.tabs?.count, 3)
        XCTAssertEqual(snap.tabs?[2].id, 3, "新 id 应为现有最大 id+1")
        XCTAssertEqual(snap.tabs?[2].color, "#00a3b4")
        XCTAssertEqual(snap.tabs?[2].name, "")
        XCTAssertEqual(snap.active, 2, "active 应为末尾新索引")
        XCTAssertEqual(model.snapshot, snap, "快照应与磁盘重解码一致")
    }

    // 第 10 个标签拒绝：状态与磁盘字节都不动，错误如实上报。
    @MainActor
    func testTenthTabRefused() throws {
        let (model, sandbox, dir) = try makeModel()
        let nine = self.nineTabsSample()
        try importSample(nine, into: dir, model: model)

        XCTAssertFalse(model.addTab(name: "ten", color: "#98989d"))
        XCTAssertNotNil(model.saveError)
        XCTAssertEqual(model.snapshot?.tabs?.count, 9)
        XCTAssertEqual(try String(contentsOf: sandbox, encoding: .utf8), nine, "拒绝时磁盘字节不动")
    }

    // 无标签极端文件：新 id 从 1 起，不凭空补默认标签。
    @MainActor
    func testAddTabOnEmptyTabsStartsFromOne() throws {
        let (model, sandbox, dir) = try makeModel()
        try importSample(#"{"version":3,"active":0,"tabs":[]}"#, into: dir, model: model)

        XCTAssertTrue(model.addTab(name: "", color: "#d4253a"))
        let snap = try readSandbox(sandbox)
        XCTAssertEqual(snap.tabs?.count, 1)
        XCTAssertEqual(snap.tabs?.first?.id, 1)
        XCTAssertEqual(snap.active, 0)
    }

    // 删除的 active 语义（桌面 parity）：删 active → min(i, len-1)；删其他 → 按 id 跟随。
    @MainActor
    func testDeleteTabActiveSemantics() throws {
        let (model, sandbox, dir) = try makeModel()
        try importSample(v3Sample, into: dir, model: model)

        // 补两个到 [1,2,3,4]，active=3（末尾 id 4）。
        XCTAssertTrue(model.addTab(name: "A", color: "#0f75e9"))
        XCTAssertTrue(model.addTab(name: "B", color: "#a40bd8"))

        // 删 active 后面的标签 → active 按 id 跟随不动。
        XCTAssertTrue(model.deleteTab(id: 2))
        var snap = try readSandbox(sandbox)
        XCTAssertEqual(snap.tabs?.map(\.id), [1, 3, 4])
        XCTAssertEqual(snap.active, 2)

        // 删 active 前面的标签 → active 仍按 id 跟随（下标减一）。
        XCTAssertTrue(model.deleteTab(id: 3))
        snap = try readSandbox(sandbox)
        XCTAssertEqual(snap.tabs?.map(\.id), [1, 4])
        XCTAssertEqual(snap.active, 1, "active 应按 id 跟随")

        // 删 active 自己 → min(i, len-1)。
        XCTAssertTrue(model.deleteTab(id: 4))
        snap = try readSandbox(sandbox)
        XCTAssertEqual(snap.tabs?.map(\.id), [1])
        XCTAssertEqual(snap.active, 0)
    }

    // 新 id 语义：按“现有最大 id+1”就地计算（绑定无状态），删掉最大 id 后
    // 新增会复用该号段（与桌面常驻计数器不同，桌面语义由规格定案为落盘口径）。
    @MainActor
    func testAddTabIDReusesMaxAfterDelete() throws {
        let (model, sandbox, dir) = try makeModel()
        try importSample(v3Sample, into: dir, model: model)
        XCTAssertTrue(model.deleteTab(id: 2))
        XCTAssertTrue(model.addTab(name: "A", color: "#0f75e9"))
        let snap = try readSandbox(sandbox)
        XCTAssertEqual(snap.tabs?.map(\.id), [1, 2])
        XCTAssertEqual(snap.tabs?[1].name, "A")
    }

    // 只剩一个标签时拒绝删除；id 不存在拒绝；磁盘与快照都不动。
    @MainActor
    func testDeleteTabRefusals() throws {
        let (model, sandbox, dir) = try makeModel()
        try importSample(v3Sample, into: dir, model: model)
        XCTAssertTrue(model.deleteTab(id: 2))
        let onDisk = try String(contentsOf: sandbox, encoding: .utf8)

        XCTAssertFalse(model.deleteTab(id: 1), "只剩一个标签必须拒绝")
        XCTAssertNotNil(model.saveError)
        XCTAssertFalse(model.deleteTab(id: 99), "不存在的 id 必须拒绝")
        XCTAssertEqual(try String(contentsOf: sandbox, encoding: .utf8), onDisk)
        XCTAssertEqual(model.snapshot?.tabs?.count, 1)
    }

    // 重命名与换色：即时落盘、去首尾空白（桌面 parity）、未知字段不丢。
    @MainActor
    func testRenameAndColorRoundTrip() throws {
        let (model, sandbox, dir) = try makeModel()
        try importSample(v3Sample, into: dir, model: model)

        XCTAssertTrue(model.renameTab(id: 1, name: "  工作笔记  "))
        var snap = try readSandbox(sandbox)
        XCTAssertEqual(snap.tabs?[0].name, "工作笔记", "重命名应去首尾空白")

        XCTAssertTrue(model.setTabColor(id: 1, color: "#0f75e9"))
        snap = try readSandbox(sandbox)
        XCTAssertEqual(snap.tabs?[0].color, "#0f75e9")
        XCTAssertEqual(snap.tabs?[0].name, "工作笔记", "换色不得影响其他字段")
        XCTAssertEqual(model.snapshot, snap)
    }

    // 排序：磁盘顺序按 ids 排列，active 按 id 跟随（桌面 parity）。
    @MainActor
    func testReorderFollowsActiveByID() throws {
        let (model, sandbox, dir) = try makeModel()
        try importSample(v3Sample, into: dir, model: model)

        XCTAssertTrue(model.reorderTabs(ids: [2, 1]))
        let snap = try readSandbox(sandbox)
        XCTAssertEqual(snap.tabs?.map(\.id), [2, 1])
        XCTAssertEqual(snap.active, 0, "active 指向的 id=2 应跟随到新位置 0")
        XCTAssertEqual(model.snapshot, snap)
    }

    // 非完整排列拒绝（长度/未知 id/重复），错误上报且磁盘不动。
    @MainActor
    func testReorderRefusesNonPermutation() throws {
        let (model, sandbox, dir) = try makeModel()
        try importSample(v3Sample, into: dir, model: model)
        let onDisk = try String(contentsOf: sandbox, encoding: .utf8)

        XCTAssertFalse(model.reorderTabs(ids: [1]), "长度不一致必须拒绝")
        XCTAssertFalse(model.reorderTabs(ids: [1, 3]), "未知 id 必须拒绝")
        XCTAssertFalse(model.reorderTabs(ids: [1, 1]), "重复 id 必须拒绝")
        XCTAssertEqual(try String(contentsOf: sandbox, encoding: .utf8), onDisk)
        XCTAssertEqual(model.snapshot?.tabs?.map(\.id), [1, 2])
    }

    // 写保护文件拒绝一切标签写入；磁盘字节不动。
    @MainActor
    func testWriteProtectedRefusesAllManagementOps() throws {
        for sample in [v4Sample, v2Sample] {
            let (model, sandbox, dir) = try makeModel()
            try importSample(sample, into: dir, model: model)
            let onDisk = try String(contentsOf: sandbox, encoding: .utf8)

            XCTAssertFalse(model.addTab(name: "x", color: "#d4253a"))
            XCTAssertFalse(model.deleteTab(id: 1))
            XCTAssertFalse(model.renameTab(id: 1, name: "hacked"))
            XCTAssertFalse(model.setTabColor(id: 1, color: "#000000"))
            XCTAssertFalse(model.reorderTabs(ids: [1]))
            XCTAssertNotNil(model.saveError, "每次拒绝都应如实上报")
            XCTAssertEqual(try String(contentsOf: sandbox, encoding: .utf8), onDisk, "写保护文件字节不动")
        }
    }

    // 三层未知字段与桌面专属 settings 经增/删/重命名保真。
    @MainActor
    func testUnknownFieldsSurviveManagementOps() throws {
        let (model, sandbox, dir) = try makeModel()
        let raw = """
        {"version":3,"active":0,"topFuture":{"a":1},
         "tabs":[{"id":1,"name":"N","text":"T","color":"#000000","tabFuture":{"k":true}},
                 {"id":2,"name":"M","text":"U","color":"#111111"}],
         "settings":{"alwaysOnTop":true,"hideOnEscape":false,"settingsFuture":{"deep":{"x":1}}}}
        """
        try importSample(raw, into: dir, model: model)

        XCTAssertTrue(model.addTab(name: "added", color: "#98989d"))
        XCTAssertTrue(model.renameTab(id: 1, name: "renamed"))
        XCTAssertTrue(model.deleteTab(id: 2))

        let obj = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(try String(contentsOf: sandbox, encoding: .utf8).utf8)) as? [String: Any])
        XCTAssertEqual((obj["topFuture"] as? [String: Any])?["a"] as? Int, 1)
        let tabs = try XCTUnwrap(obj["tabs"] as? [[String: Any]])
        XCTAssertEqual(tabs.count, 2, "应剩 id=1 与新增 id=3")
        XCTAssertEqual((tabs[0]["tabFuture"] as? [String: Any])?["k"] as? Bool, true)
        XCTAssertEqual(tabs[0]["name"] as? String, "renamed")
        let settings = try XCTUnwrap(obj["settings"] as? [String: Any])
        XCTAssertEqual(settings["alwaysOnTop"] as? Bool, true)
        XCTAssertEqual(settings["hideOnEscape"] as? Bool, false)
        XCTAssertEqual(((settings["settingsFuture"] as? [String: Any])?["deep"] as? [String: Any])?["x"] as? Int, 1)
    }

    // unusedTabColor：未用色优先（大小写不敏感、空/非法色不占位），全占用回退 palette[count%12]。
    @MainActor
    func testUnusedTabColorSemantics() {
        XCTAssertEqual(NotesModel.unusedTabColor(for: []), ThemeTagPalette.colors[0])
        func tabs(colors: [String]) -> [NotesTab] {
            colors.enumerated().map { i, c in
                NotesTab(id: UInt64(i + 1), name: "", text: "", color: c)
            }
        }
        XCTAssertEqual(NotesModel.unusedTabColor(for: tabs(colors: ["#d4253a", "#f67500"])), "#ffd01c")
        XCTAssertEqual(
            NotesModel.unusedTabColor(for: tabs(colors: ["#D4253A"])),
            "#f67500",
            "大小写不敏感"
        )
        XCTAssertEqual(
            NotesModel.unusedTabColor(for: tabs(colors: ["", "bogus"])),
            "#d4253a",
            "空/非法色不占位"
        )
        let all = ThemeTagPalette.colors
        XCTAssertEqual(
            NotesModel.unusedTabColor(for: tabs(colors: all)),
            ThemeTagPalette.hex(at: all.count),
            "全占用回退 palette[count%12]"
        )
    }

    private func nineTabsSample() -> String {
        let tabs = (1...9).map { i in
            "{\"id\":\(i),\"name\":\"Tab \(i)\",\"text\":\"t\(i)\",\"color\":\"\(ThemeTagPalette.colors[(i - 1) % 12])\"}"
        }.joined(separator: ",")
        return "{\"version\":3,\"active\":0,\"tabs\":[\(tabs)]}"
    }
}

import XCTest
@testable import Zakki

// 直接对绑定封装层喂合成 zakki.json 字符串；不起 UI 自动化，
// 模拟器上的真实导入实测见 docs/reports/2026-10-28-m1-scaffold-verification.md。
final class NotesBindingTests: XCTestCase {
    private let v3Sample = """
    {"version":3,"active":1,"tabs":[
      {"id":1,"name":"Inbox","text":"first line\\nsecond line","color":"#d4253a"},
      {"id":2,"name":"Ideas","text":"","color":""}
    ]}
    """

    private let v4Sample = """
    {"version":4,"active":0,"tabs":[{"id":1,"name":"Future","text":"written by a newer app","color":"#0f75e9"}]}
    """

    private func tenTabsSample() -> String {
        // 颜色值里的 "# 若放进 #"..."# 原始字符串会提前终止分隔符，用普通转义构造。
        let tabs = (1...10).map { i in
            "{\"id\":\(i),\"name\":\"Tab \(i)\",\"text\":\"t\(i)\",\"color\":\"#98989d\"}"
        }.joined(separator: ",")
        return "{\"version\":3,\"active\":0,\"tabs\":[\(tabs)]}"
    }

    private let v2Sample = """
    {"version":2,"active":0,"tabs":[
      {"id":1,"name":"Legacy","text":"old version","color":"#65c802"},
      {"id":2,"name":"Second","text":"","color":""}
    ]}
    """

    // v3 正常：reason=ok、writable、tabs 解析。
    func testV3NormalDecodesWritableWithTabs() throws {
        let snapshot = try NotesCore.decode(v3Sample)
        XCTAssertTrue(snapshot.writable)
        XCTAssertEqual(snapshot.reason, "ok")
        XCTAssertEqual(snapshot.version, 3)
        XCTAssertEqual(snapshot.active, 1)
        XCTAssertEqual(snapshot.tabs?.count, 2)
        XCTAssertEqual(snapshot.tabs?[0].id, 1)
        XCTAssertEqual(snapshot.tabs?[0].name, "Inbox")
        XCTAssertEqual(snapshot.tabs?[0].text, "first line\nsecond line")
        XCTAssertEqual(snapshot.tabs?[0].color, "#d4253a")
        XCTAssertEqual(snapshot.tabs?[1].color, "")
    }

    // v4 版本过高：versionTooNew、无 tabs、保留磁盘版本号。
    func testV4TooNewIsWriteProtectedWithoutTabs() throws {
        let snapshot = try NotesCore.decode(v4Sample)
        XCTAssertFalse(snapshot.writable)
        XCTAssertEqual(snapshot.reason, WriteProtectReason.versionTooNew.rawValue)
        XCTAssertEqual(snapshot.version, 4)
        XCTAssertNil(snapshot.tabs)
    }

    // 10 个标签超过上限：tooManyTabs、无 tabs，不得截断。
    func testTenTabsIsWriteProtectedAsTooManyTabs() throws {
        let snapshot = try NotesCore.decode(tenTabsSample())
        XCTAssertFalse(snapshot.writable)
        XCTAssertEqual(snapshot.reason, WriteProtectReason.tooManyTabs.rawValue)
        XCTAssertEqual(snapshot.version, 3)
        XCTAssertNil(snapshot.tabs)
    }

    // v2 旧版本：legacyVersion、只读展示且 tabs 原样返回。
    func testV2LegacyKeepsTabsForReadOnlyDisplay() throws {
        let snapshot = try NotesCore.decode(v2Sample)
        XCTAssertFalse(snapshot.writable)
        XCTAssertEqual(snapshot.reason, WriteProtectReason.legacyVersion.rawValue)
        XCTAssertEqual(snapshot.version, 2)
        XCTAssertEqual(snapshot.tabs?.count, 2)
        XCTAssertEqual(snapshot.tabs?[0].name, "Legacy")
        XCTAssertEqual(snapshot.tabs?[1].id, 2)
    }

    // 非法 JSON：invalid。
    func testBrokenJSONReportsInvalid() throws {
        let snapshot = try NotesCore.decode("{not json")
        XCTAssertFalse(snapshot.writable)
        XCTAssertEqual(snapshot.reason, WriteProtectReason.invalid.rawValue)
        XCTAssertNil(snapshot.tabs)
    }

    // 三层未知字段（顶层/tab/settings）经 EncodeNotes 修改已知字段后原样保留。
    func testEncodePreservesUnknownFieldsAtThreeLevels() throws {
        let raw = """
        {"version":3,"active":0,"topFuture":{"a":1},
         "tabs":[{"id":1,"name":"N","text":"T","color":"#000000","tabFuture":{"k":true}}],
         "settings":{"alwaysOnTop":true,"hideOnEscape":false,"settingsFuture":{"deep":{"x":1}}}}
        """
        let snapshot = try NotesCore.decode(raw)
        XCTAssertTrue(snapshot.writable)
        var renamed = snapshot.tabs!
        renamed[0].name = "Renamed"
        renamed[0].text = "T2"
        renamed[0].color = "#111111"

        let out = try NotesCore.encode(raw, tabs: renamed)
        let obj = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(out.utf8)) as? [String: Any])
        XCTAssertEqual(obj["version"] as? Int, 3)
        XCTAssertEqual((obj["topFuture"] as? [String: Any])?["a"] as? Int, 1)

        let tabs = try XCTUnwrap(obj["tabs"] as? [[String: Any]])
        XCTAssertEqual(tabs.count, 1)
        XCTAssertEqual(tabs[0]["name"] as? String, "Renamed")
        XCTAssertEqual(tabs[0]["text"] as? String, "T2")
        XCTAssertEqual(tabs[0]["color"] as? String, "#111111")
        XCTAssertEqual((tabs[0]["tabFuture"] as? [String: Any])?["k"] as? Bool, true)

        let settings = try XCTUnwrap(obj["settings"] as? [String: Any])
        let deep = try XCTUnwrap(settings["settingsFuture"] as? [String: Any])
        let deepInner = try XCTUnwrap(deep["deep"] as? [String: Any])
        XCTAssertEqual(deepInner["x"] as? Int, 1)
    }

    // 桌面专属设置 alwaysOnTop/hideOnEscape 经 EncodeNotes 后原值不变。
    func testEncodeKeepsDesktopOnlySettings() throws {
        let raw = """
        {"version":3,"active":0,
         "tabs":[{"id":1,"name":"N","text":"T","color":"#000000"}],
         "settings":{"alwaysOnTop":true,"hideOnEscape":false}}
        """
        let snapshot = try NotesCore.decode(raw)
        var renamed = snapshot.tabs!
        renamed[0].name = "Changed"

        let out = try NotesCore.encode(raw, tabs: renamed)
        let obj = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(out.utf8)) as? [String: Any])
        let settings = try XCTUnwrap(obj["settings"] as? [String: Any])
        XCTAssertEqual(settings["alwaysOnTop"] as? Bool, true)
        XCTAssertEqual(settings["hideOnEscape"] as? Bool, false)
        let tabs = try XCTUnwrap(obj["tabs"] as? [[String: Any]])
        XCTAssertEqual(tabs[0]["name"] as? String, "Changed")
    }

    // EncodeNotes 的写保护：高版本与低版本都拒绝，错误串按 `code: 说明` 格式给出前缀码。
    func testEncodeRejectsWriteProtectedVersions() throws {
        XCTAssertThrowsError(try NotesCore.encode(v4Sample, tabs: [])) { error in
            XCTAssertEqual((error as? NotesCoreError)?.code, "write_protected")
        }
        XCTAssertThrowsError(try NotesCore.encode(v2Sample, tabs: [])) { error in
            XCTAssertEqual((error as? NotesCoreError)?.code, "write_protected")
        }
    }
}

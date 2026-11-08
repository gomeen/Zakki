import XCTest
@testable import Zakki

// 冲突留档（D13）：命名、唯一性、列表、读取、删除与设备名净化。
// 全部走临时目录，绝不触真实用户目录。
final class ConflictArchiveTests: XCTestCase {
    private var dir: URL!
    private var store: ConflictArchiveStore!

    override func setUp() {
        super.setUp()
        dir = FileManager.default.temporaryDirectory
            .appendingPathComponent("zakki-archive-tests-\(UUID().uuidString)", isDirectory: true)
        store = ConflictArchiveStore(dir: dir,
                                     clock: { 1_700_000_000_000 }, // 2023-11-14 22:13:20 UTC，本地时区命名
                                     deviceName: { "Test Device" })
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: dir)
        super.tearDown()
    }

    // 命名格式：zakki-YYYYMMDD-HHmmss-<设备名>.json（本地墙上时刻）。
    func test_write_usesTimestampedDeviceName() throws {
        let name = try store.write(content: #"{"version":3}"#)
        XCTAssertTrue(name.hasPrefix("zakki-"), name)
        let stem = (name as NSString).deletingPathExtension
        let parts = stem.split(separator: "-", maxSplits: 3).map(String.init)
        XCTAssertEqual(parts.count, 4, stem)
        XCTAssertEqual(parts[0], "zakki")
        XCTAssertNotNil(ConflictArchiveStore.parseTimestamp(parts[1] + "-" + parts[2]), "时间戳段可解析")
        XCTAssertEqual(parts[3], "Test-Device", "设备名空白折叠为连字符")
        XCTAssertTrue(name.hasSuffix(".json"))
        // 文件真实写入且内容完整。
        let content = try String(contentsOf: dir.appendingPathComponent(name), encoding: .utf8)
        XCTAssertEqual(content, #"{"version":3}"#)
    }

    // 同秒同名（极小概率）不覆盖既有留档：递增序号后缀。
    func test_write_sameSecondCollision_getsUniqueName() throws {
        let first = try store.write(content: "a")
        let second = try store.write(content: "b")
        XCTAssertNotEqual(first, second)
        XCTAssertTrue(second.hasPrefix("zakki-"), second)
        XCTAssertTrue(second.contains("-2.json"), second)
        XCTAssertEqual(try String(contentsOf: dir.appendingPathComponent(first), encoding: .utf8), "a")
        XCTAssertEqual(try String(contentsOf: dir.appendingPathComponent(second), encoding: .utf8), "b")
    }

    // 列表按时间倒序；读取返回完整内容；删除后不再出现。
    func test_listReadDelete_roundTrip() throws {
        _ = try store.write(content: "older")
        // 第二条用更晚时钟写入。
        let later = ConflictArchiveStore(dir: dir,
                                         clock: { 1_700_060_000_000 },
                                         deviceName: { "Test Device" })
        _ = try later.write(content: "newer")
        let entries = store.list()
        XCTAssertEqual(entries.count, 2)
        XCTAssertEqual(entries[0].createdAtMs, 1_700_060_000_000, "新留档排前")
        XCTAssertEqual(entries[1].createdAtMs, 1_700_000_000_000)
        XCTAssertEqual(entries[0].deviceName, "Test-Device")
        XCTAssertEqual(store.content(of: entries[0]), "newer")
        store.delete(entries[0])
        XCTAssertEqual(store.list().count, 1)
        XCTAssertNil(store.content(of: entries[0]))
    }

    // 设备名净化：路径分隔符/控制字符折叠为连字符，空名回落 device；长度截断。
    func test_deviceName_sanitized() throws {
        let hostile = ConflictArchiveStore(dir: dir, clock: { 1_700_000_000_000 },
                                           deviceName: { "../evil:name with/slash" })
        let name = try hostile.write(content: "x")
        let stem = (name as NSString).deletingPathExtension
        let device = stem.split(separator: "-", maxSplits: 3).map(String.init).last ?? ""
        XCTAssertFalse(device.contains("/") || device.contains(":"), device)
        let second = try hostile.write(content: "x")
        XCTAssertNotEqual(name, second, "同秒同名不覆盖，序号后缀")
    }

    // 目录不存在时自动创建（与 zakki.json 同目录下的 conflicts/）。
    func test_directoryCreatedOnDemand() throws {
        XCTAssertFalse(FileManager.default.fileExists(atPath: dir.path))
        _ = try store.write(content: "x")
        var isDir: ObjCBool = false
        XCTAssertTrue(FileManager.default.fileExists(atPath: dir.path, isDirectory: &isDir))
        XCTAssertTrue(isDir.boolValue)
    }

    // 留档目录定位：zakki.json 同目录下的 conflicts/（§2.2）。
    func test_directory_forZakkiURL() {
        let zakki = URL(fileURLWithPath: "/tmp/app").appendingPathComponent("zakki.json")
        XCTAssertEqual(ConflictArchiveStore.directory(forZakkiURL: zakki).path,
                       URL(fileURLWithPath: "/tmp/app/conflicts").path)
    }
}

import XCTest
@testable import Zakki

// MARK: - 云端冲突副本双通道枚举（命名扫描 + NSFileVersion）

// 真实 iCloud 冲突版本需真机（探针 §3）；模拟器可验证命名通道、双通道合并、
// 容错边界（非 iCloud 文件的 NSFileVersion 报错按空集容错）。
@MainActor
final class CloudConflictEnumeratorTests: XCTestCase {
    private var root: URL!
    private var bookmark: Data!

    override func setUpWithError() throws {
        root = FileManager.default.temporaryDirectory
            .appendingPathComponent("zakki-copies-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        try Data("{\"main\":true}".utf8).write(to: root.appendingPathComponent("zakki.json"))
        bookmark = try CloudFolderAccess.makeBookmarkData(for: root)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: root)
    }

    private func touch(_ name: String, content: String) throws {
        try Data(content.utf8).write(to: root.appendingPathComponent(name))
    }

    // 命名通道：编号副本 + conflicted 命名都进列表，非副本文件不误伤；
    // 每条带文件名、内容与来源标记。
    func testEnumeratesNamedCopies() async throws {
        try touch("zakki (1).json", content: "{\"copy\":1}")
        try touch("zakki (2).json", content: "{\"copy\":2}")
        try touch("zakki (Ringo's conflicted copy 2026-11-05).json", content: "{\"conflicted\":true}")
        try touch("无关.txt", content: "noise")
        try touch("zakki2 (1).json", content: "noise")

        let outcome = await CloudConflictEnumerator.enumerate(bookmarkData: bookmark)
        guard case .success(let copies) = outcome else {
            return XCTFail("枚举应成功：\(outcome)")
        }
        XCTAssertEqual(copies.count, 3)
        XCTAssertEqual(copies.filter { $0.source == .namedCopy }.count, 3)
        XCTAssertTrue(copies.contains { $0.fileName == "zakki (1).json" && $0.content == "{\"copy\":1}" })
        XCTAssertTrue(copies.contains { $0.fileName.contains("conflicted copy") })
    }

    // 无副本时返回空列表（主文件本身与无关文件不算副本）。
    func testEmptyWhenNoCopies() async throws {
        try touch("notes.txt", content: "plain")
        let outcome = await CloudConflictEnumerator.enumerate(bookmarkData: bookmark)
        guard case .success(let copies) = outcome else { return XCTFail("枚举应成功") }
        XCTAssertTrue(copies.isEmpty, "普通本地文件的 NSFileVersion 通道应按空集容错（探针 §3）")
    }

    // 仅文件授权：列不了父目录，命名通道跳过，整体仍成功（空列表）。
    func testFileScopeSkipsNamedChannel() async throws {
        let fileURL = root.appendingPathComponent("zakki.json")
        let fileBookmark = try CloudFolderAccess.makeBookmarkData(for: fileURL)
        try touch("zakki (1).json", content: "{\"copy\":1}")
        let outcome = await CloudConflictEnumerator.enumerate(bookmarkData: fileBookmark)
        guard case .success(let copies) = outcome else { return XCTFail("枚举应成功") }
        XCTAssertTrue(copies.isEmpty)
    }

    // 书签失效：整体按 failure 报告（引擎如实呈现错误码，不清空旧列表语义在引擎层）。
    func testInvalidBookmarkFailsGracefully() async throws {
        try FileManager.default.removeItem(at: root)
        let outcome = await CloudConflictEnumerator.enumerate(bookmarkData: bookmark)
        guard case .failure(let error) = outcome else { return XCTFail("应报 failure") }
        XCTAssertEqual(error, .bookmarkInvalid)
    }

    // 冲突标记清理：普通本地文件无冲突版本，清理空转不崩溃（真机真冲突行为待验）。
    func testResolveVersionMarkersToleratesPlainFiles() async throws {
        CloudConflictEnumerator.resolveVersionMarkers(bookmarkData: bookmark)
        // 无异常即通过。
    }
}

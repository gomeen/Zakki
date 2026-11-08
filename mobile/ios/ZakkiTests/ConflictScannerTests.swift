import XCTest
@testable import Zakki

// MARK: - 冲突副本命名扫描（探针 §3 移植边界 + 数值排序修正）

final class ConflictScannerTests: XCTestCase {
    private var dir: URL!

    override func setUpWithError() throws {
        dir = FileManager.default.temporaryDirectory
            .appendingPathComponent("zakki-scanner-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: dir)
    }

    private func touch(_ name: String) throws -> URL {
        let url = dir.appendingPathComponent(name, isDirectory: false)
        try Data("{}".utf8).write(to: url)
        return url
    }

    private func scan(targetName: String, files: [String]) throws -> ConflictScanner.Result {
        for name in files { _ = try touch(name) }
        let target = dir.appendingPathComponent(targetName, isDirectory: false)
        return ConflictScanner.scan(target: target, in: dir)
    }

    // (10) 必须排在 (2) 后面：字典序会让 (10) 在前（探针记录的注意点）。
    func testNumberedCopiesSortedNumerically() throws {
        let result = try scan(targetName: "zakki.json", files: [
            "zakki (10).json", "zakki (2).json", "zakki (1).json", "zakki.json",
        ])
        XCTAssertEqual(result.numbered.map(\.lastPathComponent),
                       ["zakki (1).json", "zakki (2).json", "zakki (10).json"])
        XCTAssertEqual(result.all.map(\.lastPathComponent),
                       ["zakki (1).json", "zakki (2).json", "zakki (10).json"])
    }

    // 误伤边界（与探针单测一致）：无空格、不同扩展名、不同 base 都不识别。
    func testFalsePositiveBoundaries() throws {
        let result = try scan(targetName: "zakki.json", files: [
            "zakki(1).json", // 无空格
            "zakki (1).txt", // 不同扩展名
            "zakki2 (1).json", // 不同 base
            "zakki (a).json", // 非数字编号
            "zakki (1) (2).json", // 编号后还有尾缀，stem 不等于 "zakki (N)"
        ])
        XCTAssertTrue(result.numbered.isEmpty)
        XCTAssertTrue(result.all.isEmpty)
    }

    // iCloud conflicted copy 命名（含大小写变体）识别为 conflicted 副本。
    func testConflictedCopyNaming() throws {
        let result = try scan(targetName: "zakki.json", files: [
            "zakki (Ringo's conflicted copy 2026-11-05).json",
            "Zakki (Conflicted Copy 2026-11-06).json",
            "zakki (1).json",
        ])
        XCTAssertEqual(result.conflicted.map(\.lastPathComponent).sorted(), [
            "Zakki (Conflicted Copy 2026-11-06).json",
            "zakki (Ringo's conflicted copy 2026-11-05).json",
        ])
        // 双通道合并：编号副本在前，conflicted 在后，共 3 条。
        XCTAssertEqual(result.all.count, 3)
    }

    // 合并视图：编号副本与 conflicted 命名都进列表（anchored 正则下两者同名不可能，
    // 去重分支是防御性的；这里验证两类都不丢）。
    func testMergedContainsBothChannels() throws {
        let result = try scan(targetName: "zakki.json", files: [
            "zakki (1).json", "zakki (xxx conflicted copy yyy).json", "zakki (1).json.bak",
        ])
        XCTAssertEqual(result.all.map(\.lastPathComponent).sorted(),
                       ["zakki (1).json", "zakki (xxx conflicted copy yyy).json"].sorted())
    }

    // 目录缺失或不可读时返回空结果，不抛错。
    func testMissingDirectoryYieldsEmpty() throws {
        let target = dir.appendingPathComponent("zakki.json")
        let missing = dir.appendingPathComponent("nope", isDirectory: true)
        let result = ConflictScanner.scan(target: target, in: missing)
        XCTAssertTrue(result.all.isEmpty)
    }
}

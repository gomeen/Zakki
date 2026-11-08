import XCTest
@testable import Zakki

// MARK: - 占位文件安全读取（探针 §2 边界的本地可验部分）

// 模拟器未登录 iCloud，无法构造真实占位文件（真机项）；本地可验证的是
// 「不误判本地文件」与读取路径本身，与探针单测同边界。
@MainActor
final class PlaceholderReaderTests: XCTestCase {
    private var dir: URL!

    override func setUpWithError() throws {
        dir = FileManager.default.temporaryDirectory
            .appendingPathComponent("zakki-placeholder-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: dir)
    }

    // 本地文件 isUbiquitousItem 键缺失（非 false）：verdict 必须 localFile，
    // 不误判为占位（探针：test_non_ubiquitous_file_has_no_downloading_status）。
    func testLocalFileNotMisjudgedAsPlaceholder() throws {
        let url = dir.appendingPathComponent("local.json")
        try Data("{}".utf8).write(to: url)
        let diagnosis = PlaceholderReader.diagnose(url: url)
        XCTAssertFalse(diagnosis.isUbiquitous)
        XCTAssertNil(diagnosis.statusRaw)
        XCTAssertEqual(diagnosis.verdict, .localFile)
    }

    // 本地快速路径直读：内容原样返回，不触发下载轮询。
    func testReadLocalFastPath() async throws {
        let url = dir.appendingPathComponent("local.json")
        try Data("{\"k\":1}".utf8).write(to: url)
        let data = try await PlaceholderReader.read(url: url)
        XCTAssertEqual(String(data: data, encoding: .utf8), "{\"k\":1}")
    }

    // 目标不存在：协调读失败按语义化 readFailed 报告（不崩溃、不误报占位）。
    func testReadMissingFileFailsWithSemanticError() async throws {
        let url = dir.appendingPathComponent("gone.json")
        do {
            _ = try await PlaceholderReader.read(url: url)
            XCTFail("缺失文件应抛错")
        } catch let error as CloudFolderError {
            guard case .readFailed = error else {
                XCTFail("应报 readFailed，实际 \(error)")
                return
            }
        }
    }

    // 版本号 = 文件身份 + 修改时间毫秒（后端 B 基线，修改时间只作变化提示）：
    // 就地覆写不改身份，但 mtime 变化足以让版本号前进（「变化提示」语义）。
    func testVersionTracksIdentityAndMtime() throws {
        let url = dir.appendingPathComponent("f.json")
        try Data("a".utf8).write(to: url)
        guard let v1 = CloudFolderAccess.version(of: url) else {
            return XCTFail("版本号应可读")
        }
        Thread.sleep(forTimeInterval: 1.1) // 毫秒精度跨过 1 秒
        try Data("b".utf8).write(to: url)
        guard let v2 = CloudFolderAccess.version(of: url) else {
            return XCTFail("版本号应可读")
        }
        XCTAssertNotEqual(v1, v2, "内容变化后版本号必须前进")
        XCTAssertEqual(v2.split(separator: "-").count, 2, "形状：<identifier>-<mtimeMs>")
    }

    // 原子替换（write .atomic）换 inode：文件身份变化同样让版本号前进。
    func testVersionChangesAfterAtomicReplace() throws {
        let url = dir.appendingPathComponent("f.json")
        try Data("a".utf8).write(to: url)
        let v1 = CloudFolderAccess.version(of: url)
        try Data("b".utf8).write(to: url, options: .atomic)
        let v2 = CloudFolderAccess.version(of: url)
        XCTAssertNotEqual(v1, v2)
    }
}

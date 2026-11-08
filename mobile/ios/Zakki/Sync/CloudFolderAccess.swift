import Foundation

// MARK: - 后端 B 授权基础件（文件选择器 + security-scoped bookmark，探针 §1 路线）

// 授权形态：用户手动一次性授文件/文件夹；每次使用 = 解析书签 → startAccessing →
// 用完 stopAccessing。用户可在系统设置撤销授权 → 一切路径按可失败设计（探针 §5）。

// 已授权目标的范围：文件夹授权可写同目录 conflicts/（留档随同步保存）；
// 仅文件授权只能读写 zakki.json 本身，留档保留在设备本地。
enum CloudGrantScope: String, Codable {
    case folder
    case file
}

// 书签失效类错误（探针 §5：目标被删时解析抛错而非 stale 标志；撤销授权则 startAccessing 失败）。
// 引擎按错误码呈现并引导重选位置。
enum CloudFolderError: Error, Equatable {
    case bookmarkInvalid // 书签解析失败：目标被删/移动/系统清理
    case accessDenied // startAccessing 失败：授权被撤销
    case readFailed(code: String)
    case writeFailed(code: String)
    case downloadTimeout // 占位文件下载轮询超时（真机项）
}

// 书签访问会话：resolve → startAccessing，作用域结束时 stopAccessing。
// 一个实例只代表一次会话；所有后端操作都经它进入文件提供方。
struct CloudFolderAccess {
    let rootURL: URL
    let scope: CloudGrantScope

    private var accessing = false

    // security-scoped bookmark 的范围与解析选项按目标种类区分：目录用 .withoutUI？
    // 不——探针验证的最小可用组合是 .minimalBookmark 创建 + 空选项解析。
    static func makeBookmarkData(for url: URL) throws -> Data {
        try url.bookmarkData(options: .minimalBookmark)
    }

    // 解析书签；目标不存在抛 CloudFolderError.bookmarkInvalid（探针实测：解析抛错而非 stale）。
    static func resolve(bookmarkData: Data) throws -> (rootURL: URL, scope: CloudGrantScope) {
        var stale = false
        let url: URL
        do {
            url = try URL(resolvingBookmarkData: bookmarkData,
                          options: [],
                          relativeTo: nil,
                          bookmarkDataIsStale: &stale)
        } catch {
            throw CloudFolderError.bookmarkInvalid
        }
        let scope: CloudGrantScope = url.hasDirectoryPath ? .folder : .file
        return (url, scope)
    }

    // 开始访问；失败按 accessDenied 报告（撤销授权路径）。幂等：已开会话直接成功。
    mutating func startAccessing() throws {
        guard !accessing else { return }
        guard rootURL.startAccessingSecurityScopedResource() else {
            throw CloudFolderError.accessDenied
        }
        accessing = true
    }

    mutating func stopAccessing() {
        guard accessing else { return }
        accessing = false
        rootURL.stopAccessingSecurityScopedResource()
    }

    // 会话内执行；异常路径也保证 stopAccessing。
    static func withAccess<T>(bookmarkData: Data, _ body: (CloudFolderAccess) throws -> T) throws -> T {
        var access = try resolve(bookmarkData: bookmarkData)
        try access.startAccessing()
        defer { access.stopAccessing() }
        return try body(access)
    }

    // zakki.json 的目标 URL：文件夹授权指向文件夹内固定名；仅文件授权即授权文件本身。
    var mainURL: URL {
        switch scope {
        case .folder:
            return rootURL.appendingPathComponent("zakki.json", isDirectory: false)
        case .file:
            return rootURL
        }
    }

    // 留档目录（文件夹授权才有意义）：与 zakki.json 同目录的 conflicts/（§2.2 D13）。
    var archiveDirectory: URL? {
        guard scope == .folder else { return nil }
        return rootURL.appendingPathComponent("conflicts", isDirectory: true)
    }

    // MARK: 文件版本号（后端 B 基线：文件身份 + 修改时间；修改时间只作变化提示——
    // 判定表的「远端已变」由它驱动，内容是否真的不同由内容 sha256 决定，见 SyncModels）。

    static func version(of url: URL) -> String? {
        let values = try? url.resourceValues(forKeys: [.fileIdentifierKey, .contentModificationDateKey])
        guard let identifier = values?.fileIdentifier else { return nil }
        let ms = values?.contentModificationDate.map { Int64(($0.timeIntervalSince1970 * 1000).rounded()) } ?? 0
        return "\(identifier)-\(ms)"
    }

    static func modificationMs(of url: URL) -> Int64 {
        let values = try? url.resourceValues(forKeys: [.contentModificationDateKey])
        return values?.contentModificationDate.map { Int64(($0.timeIntervalSince1970 * 1000).rounded()) } ?? 0
    }

    // 文件系统错误 → 语义化错误码（short code，不带路径/域名，不匹配文案）。
    static func mapIOError(_ error: Error, reading: Bool) -> CloudFolderError {
        let code = (error as NSError).code
        return reading ? .readFailed(code: "io-\(code)") : .writeFailed(code: "io-\(code)")
    }
}

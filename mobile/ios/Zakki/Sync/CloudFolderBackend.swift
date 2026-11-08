import Foundation

// MARK: - 后端 B：云盘文件夹（iCloud Drive 文件选择器 + security-scoped bookmark）

// §2.2「打开对账 + 冲突副本」模型：前台/聚焦时读远端文件 → D13 判定 → 写回/回补/冲突。
// 没有 sha CAS——用「文件身份+修改时间」版本号做写前比对，不一致抛 casConflict，
// 引擎重拉重判定；写前后仍有固有小窗口，云盘自身的冲突副本机制 + 双通道检测是兜底
//（这正是 iCloud conflicted copy 的工作方式，属已接受的后端形态约束）。
final class CloudFolderBackend: SyncBackend {
    let kind: SyncBackendKind = .cloudFolder

    // 书签数据由引擎在每轮装配时提供（连接/重选后即时生效）。
    private let bookmarkData: Data
    // 占位下载轮询参数（模拟器/单测注入；真机占位路径待验，探针 §2）。
    private let downloadTimeout: TimeInterval
    private let pollInterval: TimeInterval

    init(bookmarkData: Data,
         downloadTimeout: TimeInterval = 30,
         pollInterval: TimeInterval = 0.5)
    {
        self.bookmarkData = bookmarkData
        self.downloadTimeout = downloadTimeout
        self.pollInterval = pollInterval
    }

    // MARK: pull（读远端 zakki.json）

    func pullMain() async throws -> RemoteFile {
        try await withSession { access in
            let mainURL = access.mainURL
            guard FileManager.default.fileExists(atPath: mainURL.path) else {
                // 远端缺失：交由 D13 判定走回补（与后端 A 404 同语义）。
                return .missing
            }
            // 占位文件走下载轮询路径（本地文件快速直读，探针 §2 边界已验证）。
            let data: Data
            do {
                data = try await PlaceholderReader.read(
                    url: mainURL, timeout: downloadTimeout, pollInterval: pollInterval)
            } catch let error as CloudFolderError {
                throw Self.map(error)
            } catch {
                throw SyncError.api("cloud-read:\((error as NSError).code)")
            }
            guard let content = String(data: data, encoding: .utf8) else {
                throw SyncError.api("cloud-encoding")
            }
            guard let version = CloudFolderAccess.version(of: mainURL) else {
                throw SyncError.api("cloud-version")
            }
            return RemoteFile(exists: true, content: content, version: version)
        }
    }

    // MARK: push（写前版本比对 ≈ CAS；远端已前进抛 casConflict，不盲写）

    @discardableResult
    func pushMain(content: String, expectedVersion: String?) async throws -> String {
        try await withSession { access in
            let mainURL = access.mainURL
            let currentVersion = CloudFolderAccess.version(of: mainURL)
            let exists = FileManager.default.fileExists(atPath: mainURL.path)
            // CAS 比对：新建语义（expected=nil）要求文件确实不存在；
            // 更新语义要求版本号一致（身份+修改时间未变）。不一致 → 重拉重判定。
            if expectedVersion == nil {
                if exists { throw SyncError.casConflict }
            } else {
                guard exists, currentVersion == expectedVersion else { throw SyncError.casConflict }
            }
            try Self.write(content: content, to: mainURL)
            // 写后重读版本号返回：原子替换会换文件身份，基线必须记录写后的稳定值。
            guard let newVersion = CloudFolderAccess.version(of: mainURL) else {
                throw SyncError.api("cloud-version")
            }
            return newVersion
        }
    }

    // MARK: 留档（conflicts/<name>，文件夹授权写入云盘同目录；仅文件授权留档留在设备本地）

    func pushArchive(name: String, content: String) async throws {
        try await withSession { access in
            guard let archiveDir = access.archiveDirectory else {
                // 仅文件授权：写不进 zakki.json 同目录。引擎把该错误视作
                // 「留档已保存在设备本地、无需上传」，保持 D13 完整留档语义。
                throw SyncError.archiveNotSupported
            }
            do {
                try FileManager.default.createDirectory(at: archiveDir, withIntermediateDirectories: true)
                try Self.write(content: content, to: archiveDir.appendingPathComponent(name))
            } catch let error as CloudFolderError {
                throw Self.map(error)
            }
        }
    }

    // MARK: 公共

    // 会话包装：解析书签 → startAccessing → 执行 → stopAccessing；
    // 书签/授权错误统一映射为 SyncError（引擎按码呈现 + 引导重选）。
    // 解析在后台执行（书签解析涉及文件提供方，不占主线程）。
    private func withSession<T>(_ body: (CloudFolderAccess) async throws -> T) async throws -> T {
        let resolved: (rootURL: URL, scope: CloudGrantScope)
        do {
            resolved = try CloudFolderAccess.resolve(bookmarkData: bookmarkData)
        } catch let error as CloudFolderError {
            throw Self.map(error)
        }
        var access = CloudFolderAccess(rootURL: resolved.rootURL, scope: resolved.scope)
        do {
            try access.startAccessing()
        } catch let error as CloudFolderError {
            throw Self.map(error)
        }
        defer { access.stopAccessing() }
        return try await body(access)
    }

    // 文件写入：iCloud 项目经 NSFileCoordinator 协调写（避免静默覆盖并发修改；
    // 系统在真竞争时生成 conflicted copy，进入双通道检测）。新建文件（留档）直接写。
    private static func write(content: String, to url: URL) throws {
        let data = Data(content.utf8)
        do {
            if FileManager.default.fileExists(atPath: url.path) {
                var coordError: NSError?
                NSFileCoordinator().coordinate(writingItemAt: url, options: [], error: &coordError) { target in
                    do {
                        try data.write(to: target)
                    } catch {
                        coordError = error as NSError
                    }
                }
                if let coordError { throw coordError }
            } else {
                try data.write(to: url)
            }
        } catch let error as NSError {
            throw CloudFolderError.writeFailed(code: "io-\(error.code)")
        }
    }

    // CloudFolderError → SyncError（引擎统一按 SyncError.code 呈现）。
    static func map(_ error: CloudFolderError) -> SyncError {
        switch error {
        case .bookmarkInvalid: return .bookmarkInvalid
        case .accessDenied: return .accessDenied
        case .downloadTimeout: return .api("cloud-download-timeout")
        case .readFailed(let code): return .api("cloud-read:\(code)")
        case .writeFailed(let code): return .api("cloud-write:\(code)")
        }
    }
}

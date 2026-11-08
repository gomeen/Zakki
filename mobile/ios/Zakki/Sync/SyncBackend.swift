import Foundation

// MARK: - 同步后端契约（§2.2：双后端任一时刻只激活一个）

// 后端 B（云盘文件夹，#53）实现同一契约接入：pullMain/pushMain/pushArchive。
// version 的语义随后端而定（A=blob sha；B=文件身份+修改时间），引擎不解释其内容。
protocol SyncBackend: AnyObject {
    var kind: SyncBackendKind { get }
    // 读取远端 zakki.json；文件不存在返回 .missing（404/410）。
    func pullMain() async throws -> RemoteFile
    // CAS 上传：expectedVersion 传当前已知远端版本号（新建传 nil）。
    // 远端已前进抛 SyncError.casConflict——调用方重拉重判定，不盲重试旧 sha（D12）。
    @discardableResult
    func pushMain(content: String, expectedVersion: String?) async throws -> String
    // 留档上传（conflicts/<name>）：按新文件创建，失败如实抛错（调用方保留 pending 重试）。
    func pushArchive(name: String, content: String) async throws
}

// MARK: - 后端 A：GitHub Contents API

// GET /repos/{owner}/{repo}/contents/<path> → content(base64)+sha；
// PUT 带 sha 上传（CAS）；409/422 → casConflict；401 → 走认证服务刷新后重试一次。
final class GitHubContentsBackend: SyncBackend {
    let kind: SyncBackendKind = .github

    private let transport: GitHubTransporting
    private let auth: GitHubAuthService
    // 远端文件路径常量：主文件在仓库根，留档在 conflicts/ 子目录（§2.2）。
    static let mainPath = "zakki.json"
    static let archiveDir = "conflicts"
    private let apiBase: URL
    private let owner: () -> String?
    private let repo: () -> String?

    init(transport: GitHubTransporting,
         auth: GitHubAuthService,
         apiBase: URL = URL(string: "https://api.github.com")!,
         owner: @escaping () -> String?,
         repo: @escaping () -> String?)
    {
        self.transport = transport
        self.auth = auth
        self.apiBase = apiBase
        self.owner = owner
        self.repo = repo
    }

    // MARK: pull

    func pullMain() async throws -> RemoteFile {
        try await get(path: Self.mainPath)
    }

    private func get(path: String) async throws -> RemoteFile {
        try await get(path: path, allowAuthRetry: true)
    }

    private func get(path: String, allowAuthRetry: Bool) async throws -> RemoteFile {
        guard let url = contentsURL(path: path) else { throw SyncError.notConfigured }
        let token = try await auth.authorizedToken()
        let (data, http) = try await request(url: url, method: "GET", token: token, body: nil)
        switch http.statusCode {
        case 200:
            struct Payload: Decodable {
                struct Content: Decodable { var sha: String? }
                var content: String?
                var encoding: String?
                var sha: String?
            }
            guard let payload = try? JSONDecoder().decode(Payload.self, from: data) else {
                throw SyncError.api("contents-decode")
            }
            // encoding=base64（GitHub 可能折行插入换行，先剥空白再解）。
            guard payload.encoding == "base64", let b64 = payload.content else {
                throw SyncError.api("contents-encoding")
            }
            let cleaned = b64.filter { !$0.isWhitespace }
            guard let decoded = Data(base64Encoded: cleaned) else {
                throw SyncError.api("contents-base64")
            }
            return RemoteFile(exists: true,
                              content: String(decoding: decoded, as: UTF8.self),
                              version: payload.sha)
        case 404, 410:
            // 文件不存在（或已被删除）：远端缺失语义，交由 D13 判定走回补。
            return .missing
        case 401:
            // token 撤销/提前失效：强刷一次重试；重试仍 401 即 needsReauth 断连。
            guard allowAuthRetry else { throw SyncError.needsReauth }
            return try await withRefreshedToken { try await self.get(path: path, allowAuthRetry: false) }
        default:
            throw SyncError.api("contents-get:\(http.statusCode)")
        }
    }

    // MARK: push（sha CAS）

    @discardableResult
    func pushMain(content: String, expectedVersion: String?) async throws -> String {
        try await put(path: Self.mainPath, content: content, expectedVersion: expectedVersion)
    }

    func pushArchive(name: String, content: String) async throws {
        let path = "\(Self.archiveDir)/\(name)"
        _ = try await put(path: path, content: content, expectedVersion: nil)
    }

    private func put(path: String, content: String, expectedVersion: String?) async throws -> String {
        try await put(path: path, content: content, expectedVersion: expectedVersion, allowAuthRetry: true)
    }

    private func put(path: String, content: String, expectedVersion: String?, allowAuthRetry: Bool) async throws -> String {
        guard let url = contentsURL(path: path) else { throw SyncError.notConfigured }
        let token = try await auth.authorizedToken()
        struct Body: Encodable {
            var message: String
            var content: String
            var sha: String?
        }
        let body = Body(message: commitMessage(path: path),
                        content: Data(content.utf8).base64EncodedString(),
                        sha: expectedVersion)
        let payloadData = try JSONEncoder().encode(body)
        let (data, http) = try await request(url: url, method: "PUT", token: token, body: payloadData)
        switch http.statusCode {
        case 200, 201:
            struct Payload: Decodable {
                struct Content: Decodable { var sha: String? }
                var content: Content?
            }
            guard let payload = try? JSONDecoder().decode(Payload.self, from: data),
                  let sha = payload.content?.sha else {
                throw SyncError.api("contents-put-decode")
            }
            return sha
        case 409, 422:
            // CAS 拒绝：远端已前进（或新建冲突）。语义化上抛 casConflict，
            // 由引擎重新 pull 重判定——绝不带着旧 sha 盲重试（D12）。
            throw SyncError.casConflict
        case 401:
            guard allowAuthRetry else { throw SyncError.needsReauth }
            return try await withRefreshedToken {
                try await self.put(path: path, content: content, expectedVersion: expectedVersion, allowAuthRetry: false)
            }
        default:
            throw SyncError.api("contents-put:\(http.statusCode)")
        }
    }

    // MARK: 公共

    // 401 兜底：刷新成功后用同一闭包重试一次；刷新失败向上抛 needsReauth。
    private func withRefreshedToken<T>(_ retry: () async throws -> T) async throws -> T {
        do {
            _ = try await auth.forceRefreshAfterUnauthorized()
            return try await retry()
        } catch let error as SyncError {
            throw error
        } catch {
            throw SyncError.api("retry:\(String(describing: error))")
        }
    }

    private func contentsURL(path: String) -> URL? {
        guard let owner = owner(), let repo = repo(), !owner.isEmpty, !repo.isEmpty else { return nil }
        // path 已是 URL 安全的固定结构（zakki.json / conflicts/<生成名>）。
        return URL(string: "repos/\(owner)/\(repo)/contents/\(path)", relativeTo: apiBase)
    }

    private func request(url: URL, method: String, token: String, body: Data?) async throws -> (Data, HTTPURLResponse) {
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.setValue("application/vnd.github+json", forHTTPHeaderField: "Accept")
        request.setValue("2022-11-28", forHTTPHeaderField: "X-GitHub-Api-Version")
        if let body {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = body
        }
        request.timeoutInterval = 30
        return try await transport.data(for: request)
    }

    // 提交信息（仓库历史可见，非 UI 文案；设备名进 message 便于追溯）。
    private func commitMessage(path: String) -> String {
        if path == Self.mainPath {
            return "Zakki sync: update zakki.json"
        }
        return "Zakki sync: archive conflict copy"
    }
}

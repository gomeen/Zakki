import Foundation

// MARK: - GitHub Device Flow 认证（GitHub App user token 语义）

// Device Flow 起始返回：展示 user_code，后台按 interval 轮询直到授权或过期。
struct DeviceFlowStart: Equatable {
    var deviceCode: String
    var userCode: String
    var verificationURL: URL
    var intervalSeconds: Int
    var expiresAt: Date
}

// 轮询单步结果；error body 按 RFC 8628 / GitHub 文档语义分类。
enum DeviceFlowPoll: Equatable {
    case pending // authorization_pending：继续按间隔轮询
    case slowDown(intervalSeconds: Int) // slow_down：间隔 +5s（RFC 8628）
    case authorized // 换来 token，已写入 Keychain
    case expired // expired_token：device_code 过期，需重新发起
    case denied // access_denied：用户拒绝
}

// 一对 user token：GitHub App 的 access_token 会过期（默认 8h），
// refresh_token 用于刷新且会轮换（旧值作废），两者都只进 Keychain（R7）。
struct TokenPair {
    var accessToken: String
    var refreshToken: String?
    // access_token 过期时刻；nil 表示后端未给 expires_in（不可过期语义）。
    var expiresAt: Date?
}

// 传输抽象：URLProtocol mock 与进程内 stub 共用（单测不依赖真实网络）。
protocol GitHubTransporting: AnyObject {
    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse)
}

// URLSession 实现；测试注入 ephemeral session + stub URLProtocol。
final class URLSessionTransport: GitHubTransporting {
    private let session: URLSession

    init(session: URLSession = .shared) {
        self.session = session
    }

    func data(for request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw SyncError.api("non-http-response")
        }
        return (data, http)
    }
}

// Keychain 键名常量在 KeychainStore.swift（含 access_token 过期时刻，R7）。

// Device Flow 客户端：发起授权、轮询、刷新、撤销兜底。全程不落日志；
// token 只经 TokenStoring 进 Keychain（R7）。
final class GitHubAuthService {
    private let transport: GitHubTransporting
    private let tokens: TokenStoring
    private let now: () -> Date
    // base 可替换：单测/端到端 stub 指向本地 stub；生产为 github.com。
    private let base: URL
    // 刷新需要 client_id，但 service 不持有配置——由引擎注入 provider，
    // 保持认证逻辑与配置来源解耦（测试可直接给定值）。
    var clientIDProvider: (() -> String?)?

    init(transport: GitHubTransporting,
         tokens: TokenStoring,
         base: URL = URL(string: "https://github.com")!,
         now: @escaping () -> Date = Date.init)
    {
        self.transport = transport
        self.tokens = tokens
        self.base = base
        self.now = now
    }

    // 当前是否有 token（不看过期；过期由 authorizedToken 兜）。
    var hasToken: Bool { tokens.token(forKey: TokenKey.accessToken) != nil }

    // 断开连接：清空全部凭据（用户主动断开或撤销兜底）。
    func disconnect() {
        tokens.setToken(nil, forKey: TokenKey.accessToken)
        tokens.setToken(nil, forKey: TokenKey.refreshToken)
        tokens.setToken(nil, forKey: TokenKey.accessTokenExpiry)
    }

    // MARK: Device Flow

    // 第一步：申请 device code。GitHub App 的 user token 权限来自 App 安装授权，
    // 不传 scope（scope 是 OAuth App 语义；规格 §2.2 明确不用全仓库 OAuth token）。
    func startDeviceFlow(clientID: String) async throws -> DeviceFlowStart {
        let request = try formRequest(path: "login/device/code", fields: ["client_id": clientID])
        let (data, http) = try await transport.data(for: request)
        guard http.statusCode == 200 else {
            throw SyncError.deviceFlow("start:\(http.statusCode)")
        }
        struct Payload: Decodable {
            var device_code: String
            var user_code: String
            var verification_uri: String
            var expires_in: Int
            var interval: Int?
        }
        guard let payload = try? JSONDecoder().decode(Payload.self, from: data),
              let url = URL(string: payload.verification_uri) else {
            throw SyncError.deviceFlow("start-decode")
        }
        return DeviceFlowStart(
            deviceCode: payload.device_code,
            userCode: payload.user_code,
            verificationURL: url,
            intervalSeconds: max(1, payload.interval ?? 5),
            expiresAt: now().addingTimeInterval(TimeInterval(payload.expires_in)))
    }

    // 第二步：单次轮询。调用方按 start.interval 与 slowDown 反馈控制节奏与循环。
    func poll(clientID: String, deviceCode: String) async throws -> DeviceFlowPoll {
        let request = try formRequest(path: "login/oauth/access_token", fields: [
            "client_id": clientID,
            "device_code": deviceCode,
            "grant_type": "urn:ietf:params:oauth:grant-type:device_code",
        ])
        let (data, _) = try await transport.data(for: request)
        struct Payload: Decodable {
            var access_token: String?
            var refresh_token: String?
            var expires_in: Int?
            var error: String?
        }
        guard let payload = try? JSONDecoder().decode(Payload.self, from: data) else {
            throw SyncError.deviceFlow("poll-decode")
        }
        if let token = payload.access_token {
            store(TokenPair(accessToken: token,
                            refreshToken: payload.refresh_token,
                            expiresAt: payload.expires_in.map { now().addingTimeInterval(TimeInterval($0)) }))
            return .authorized
        }
        switch payload.error {
        case "authorization_pending":
            return .pending
        case "slow_down":
            return .slowDown(intervalSeconds: 5)
        case "expired_token":
            return .expired
        case "access_denied":
            return .denied
        case .some(let other):
            throw SyncError.deviceFlow(other)
        case nil:
            throw SyncError.deviceFlow("poll-no-token-no-error")
        }
    }

    // MARK: token 过期/刷新/撤销（§8 M4）

    // 取可用 token：余量充足直接用；临期（<120s）用 refresh_token 换新。
    // GitHub App user token 刷新时 refresh_token 轮换，旧值立即作废，必须同写。
    // 过期记录从未写入＝不可过期 token（后端未给 expires_in），直接用。
    func authorizedToken() async throws -> String {
        guard let access = tokens.token(forKey: TokenKey.accessToken) else {
            throw SyncError.notConfigured
        }
        guard let stored = tokens.token(forKey: TokenKey.accessTokenExpiry) else { return access }
        guard let expiry = Self.iso8601.date(from: stored) else {
            // 过期记录存在但解析失败：按临期处理走刷新，不赌服务端容忍。
            return try await refreshOrThrow()
        }
        if expiry > now().addingTimeInterval(120) { return access }
        return try await refreshOrThrow()
    }

    // 401 兜底：token 被服务端撤销/提前失效时强刷一次；失败即断连报 needsReauth。
    func forceRefreshAfterUnauthorized() async throws -> String {
        try await refreshOrThrow()
    }

    private func refreshOrThrow() async throws -> String {
        do {
            return try await refresh().accessToken
        } catch {
            // 刷新失败＝refresh token 过期或授权被撤销：清凭据断连，
            // 下次同步按 needsReauth 提示重新走 Device Flow。
            disconnect()
            throw SyncError.needsReauth
        }
    }

    private func refresh() async throws -> TokenPair {
        guard let refresh = tokens.token(forKey: TokenKey.refreshToken), !refresh.isEmpty else {
            throw SyncError.needsReauth
        }
        guard let clientID = clientIDProvider?(), !clientID.isEmpty else {
            throw SyncError.notConfigured
        }
        let request = try formRequest(path: "login/oauth/access_token", fields: [
            "client_id": clientID,
            "refresh_token": refresh,
            "grant_type": "refresh_token",
        ])
        let (data, _) = try await transport.data(for: request)
        struct Payload: Decodable {
            var access_token: String?
            var refresh_token: String?
            var expires_in: Int?
            var error: String?
        }
        guard let payload = try? JSONDecoder().decode(Payload.self, from: data),
              let access = payload.access_token else {
            throw SyncError.needsReauth
        }
        let pair = TokenPair(accessToken: access,
                             refreshToken: payload.refresh_token ?? refresh,
                             expiresAt: payload.expires_in.map { now().addingTimeInterval(TimeInterval($0)) })
        store(pair)
        return pair
    }

    // MARK: 存储（全部进 Keychain）

    private func store(_ pair: TokenPair) {
        tokens.setToken(pair.accessToken, forKey: TokenKey.accessToken)
        tokens.setToken(pair.refreshToken, forKey: TokenKey.refreshToken)
        if let expiry = pair.expiresAt {
            tokens.setToken(Self.iso8601.string(from: expiry), forKey: TokenKey.accessTokenExpiry)
        } else {
            tokens.setToken(nil, forKey: TokenKey.accessTokenExpiry)
        }
    }


    // MARK: 请求组装

    private func formRequest(path: String, fields: [String: String]) throws -> URLRequest {
        guard let url = URL(string: path, relativeTo: base) else {
            throw SyncError.deviceFlow("bad-url")
        }
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        request.setValue("application/x-www-form-urlencoded", forHTTPHeaderField: "Content-Type")
        let pairs = fields.sorted { $0.key < $1.key }.map { key, value in
            "\(key)=\(Self.formEscape(value))"
        }
        request.httpBody = Data(pairs.joined(separator: "&").utf8)
        return request
    }

    // RFC 3986 unreserved 集合（grant_type 含冒号与斜杠，必须转义）。
    private static func formEscape(_ value: String) -> String {
        value.addingPercentEncoding(withAllowedCharacters: .urlQueryValueAllowed) ?? value
    }

    private static let iso8601: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return f
    }()
}

extension CharacterSet {
    static let urlQueryValueAllowed: CharacterSet = {
        var set = CharacterSet.alphanumerics
        set.insert(charactersIn: "-._~")
        return set
    }()
}

import XCTest
@testable import Zakki

// MARK: - URLProtocol mock（记录请求、按脚本返回响应；单测不依赖真实网络）

final class ScriptedURLProtocol: URLProtocol {
    // (request) → (status, body)；每用例重置。
    nonisolated(unsafe) static var handler: ((URLRequest) -> (Int, Data, [String: String]))?
    nonisolated(unsafe) static var requests: [URLRequest] = []

    static func reset(_ handler: @escaping (URLRequest) -> (Int, Data, [String: String])) {
        self.handler = handler
        requests = []
    }

    static func session() -> URLSession {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [ScriptedURLProtocol.self]
        return URLSession(configuration: config)
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let body = request.httpBody ?? request.bodyStreamData ?? Data()
        var requestWithBody = request
        requestWithBody.httpBody = body
        Self.requests.append(requestWithBody)
        let (status, data, headers) = (Self.handler?(requestWithBody) ?? (500, Data(), [:]))
        let response = HTTPURLResponse(url: request.url!, statusCode: status,
                                       httpVersion: "HTTP/1.1", headerFields: headers)!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: data)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}

// 请求体读取辅助（URLSession 内部会把 httpBody 转成 stream）。
extension URLRequest {
    var bodyStreamData: Data? {
        guard let stream = httpBodyStream else { return nil }
        stream.open()
        defer { stream.close() }
        var data = Data()
        let size = 4096
        let buffer = UnsafeMutablePointer<UInt8>.allocate(capacity: size)
        defer { buffer.deallocate() }
        while stream.hasBytesAvailable {
            let read = stream.read(buffer, maxLength: size)
            if read <= 0 { break }
            data.append(buffer, count: read)
        }
        return data
    }
}

// MARK: - Contents API 客户端（后端 A）

final class GitHubContentsBackendTests: XCTestCase {
    private var tokens: InMemoryTokenStore!
    private var auth: GitHubAuthService!
    private var backend: GitHubContentsBackend!

    override func setUp() {
        super.setUp()
        tokens = InMemoryTokenStore()
        tokens.setToken("access-1", forKey: TokenKey.accessToken)
        auth = GitHubAuthService(transport: URLSessionTransport(session: ScriptedURLProtocol.session()),
                                 tokens: tokens,
                                 base: URL(string: "https://github.com")!)
        auth.clientIDProvider = { "client-id" }
        backend = GitHubContentsBackend(
            transport: URLSessionTransport(session: ScriptedURLProtocol.session()),
            auth: auth,
            apiBase: URL(string: "https://api.github.com")!,
            owner: { "owner" },
            repo: { "repo" })
    }

    private func json(_ obj: [String: Any]) -> Data {
        (try? JSONSerialization.data(withJSONObject: obj)) ?? Data()
    }

    // GET 200：base64 内容解码 + sha 提取（内容含 GitHub 折行）。
    func test_pull_decodesBase64ContentAndSha() async throws {
        let content = #"{"version":3,"active":1,"tabs":[]}"#
        var b64 = Data(content.utf8).base64EncodedString()
        b64 = b64.insertNewlines(every: 60) // GitHub 折行形态
        ScriptedURLProtocol.reset { _ in
            (200, self.json(["sha": "abc123", "encoding": "base64", "content": b64]), [:])
        }
        let remote = try await backend.pullMain()
        XCTAssertTrue(remote.exists)
        XCTAssertEqual(remote.content, content)
        XCTAssertEqual(remote.version, "abc123")
    }

    // GET 404/410 → 远端缺失语义。
    func test_pull_missingOn404and410() async throws {
        for status in [404, 410] {
            ScriptedURLProtocol.reset { _ in (status, Data(), [:]) }
            let remote = try await backend.pullMain()
            XCTAssertFalse(remote.exists)
            XCTAssertNil(remote.content)
        }
    }

    // PUT 200：带 sha 上传（CAS 基准），返回新版本号；请求形状核对。
    func test_put_sendsShaAndReturnsNewVersion() async throws {
        ScriptedURLProtocol.reset { _ in
            (200, self.json(["content": ["sha": "newsha"]]), [:])
        }
        let newSha = try await backend.pushMain(content: #"{"version":3}"#, expectedVersion: "oldsha")
        XCTAssertEqual(newSha, "newsha")
        let request = ScriptedURLProtocol.requests.last!
        XCTAssertEqual(request.url?.path, "/repos/owner/repo/contents/zakki.json")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer access-1")
        XCTAssertEqual(request.httpMethod, "PUT")
        let body = try XCTUnwrap(request.httpBody)
        let fields = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: Any])
        XCTAssertEqual(fields["sha"] as? String, "oldsha")
        let payload = try XCTUnwrap(JSONDecoder().decode(PUTBody.self, from: body))
        XCTAssertEqual(payload.decodedContent, #"{"version":3}"#)
        XCTAssertFalse(payload.message.isEmpty)
    }

    struct PUTBody: Decodable {
        let message: String
        let content: String
        var decodedContent: String { Data(base64Encoded: content).flatMap { String(data: $0, encoding: .utf8) } ?? "" }
    }

    // CAS 拒绝：409/422 都语义化为 casConflict——引擎据此重拉重判定，不盲重试旧 sha。
    func test_put_casRejection_maps409and422ToCasConflict() async {
        for status in [409, 422] {
            ScriptedURLProtocol.reset { _ in (status, Data(), [:]) }
            do {
                _ = try await backend.pushMain(content: "x", expectedVersion: "stale")
                XCTFail("expected casConflict")
            } catch {
                XCTAssertEqual(error as? SyncError, .casConflict, "status \(status)")
            }
        }
    }

    // 留档上传：路径进 conflicts/ 子目录，新建不带 sha。
    func test_pushArchive_usesConflictsPath() async throws {
        ScriptedURLProtocol.reset { _ in
            (201, self.json(["content": ["sha": "archivesha"]]), [:])
        }
        try await backend.pushArchive(name: "zakki-20261108-101010-Test.json", content: "{}")
        let request = ScriptedURLProtocol.requests.last!
        XCTAssertEqual(request.url?.path, "/repos/owner/repo/contents/conflicts/zakki-20261108-101010-Test.json")
        let body = try XCTUnwrap(request.httpBody)
        let fields = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: Any])
        XCTAssertNil(fields["sha"])
    }

    // 401：强刷一次后重试成功（token 撤销兜底路径）。
    func test_get_unauthorized_refreshesThenRetries() async throws {
        tokens.setToken("refresh-1", forKey: TokenKey.refreshToken)
        // token 本体未过期：401 来自服务端撤销，走 401 兜底强刷路径。
        tokens.setToken(Date().addingTimeInterval(28800).iso8601String, forKey: TokenKey.accessTokenExpiry)
        ScriptedURLProtocol.reset { request in
            if request.url?.path == "/repos/owner/repo/contents/zakki.json" {
                if request.value(forHTTPHeaderField: "Authorization") == "Bearer access-1" {
                    return (401, Data(), [:])
                }
                return (200, self.json(["sha": "s", "encoding": "base64",
                                        "content": Data("{}".utf8).base64EncodedString()]), [:])
            }
            return (200, self.json(["access_token": "access-2", "refresh_token": "refresh-2",
                                    "expires_in": 28800]), [:])
        }
        let remote = try await backend.pullMain()
        XCTAssertTrue(remote.exists)
        XCTAssertEqual(tokens.token(forKey: TokenKey.accessToken), "access-2")
        XCTAssertEqual(tokens.token(forKey: TokenKey.refreshToken), "refresh-2")
    }
}

// MARK: - Device Flow 认证（过期/刷新/撤销路径）

final class GitHubAuthServiceTests: XCTestCase {
    private var tokens: InMemoryTokenStore!
    private var auth: GitHubAuthService!
    private var httpCalls = 0

    override func setUp() {
        super.setUp()
        tokens = InMemoryTokenStore()
        httpCalls = 0
        auth = GitHubAuthService(transport: URLSessionTransport(session: ScriptedURLProtocol.session()),
                                 tokens: tokens,
                                 base: URL(string: "https://github.com")!)
        auth.clientIDProvider = { "client-id" }
    }

    private func json(_ obj: [String: Any]) -> Data {
        (try? JSONSerialization.data(withJSONObject: obj)) ?? Data()
    }

    // 起始：解析 device_code/user_code/interval。
    func test_startDeviceFlow_parsesResponse() async throws {
        ScriptedURLProtocol.reset { _ in
            (200, self.json(["device_code": "dc", "user_code": "ABCD-1234",
                             "verification_uri": "https://github.com/login/device",
                             "expires_in": 900, "interval": 5]), [:])
        }
        let start = try await auth.startDeviceFlow(clientID: "client-id")
        XCTAssertEqual(start.deviceCode, "dc")
        XCTAssertEqual(start.userCode, "ABCD-1234")
        XCTAssertEqual(start.intervalSeconds, 5)
        XCTAssertEqual(start.verificationURL.absoluteString, "https://github.com/login/device")
    }

    // 轮询状态机：pending → slow_down(+5s) → authorized（token 只进 TokenStoring）。
    func test_poll_pendingSlowDownAuthorized_storesTokens() async throws {
        func pollOnce() async throws -> DeviceFlowPoll {
            try await auth.poll(clientID: "client-id", deviceCode: "dc")
        }
        ScriptedURLProtocol.reset { request in
            self.httpCalls += 1
            switch self.httpCalls {
            case 1: return (200, self.json(["error": "authorization_pending"]), [:])
            case 2: return (200, self.json(["error": "slow_down", "error_description": "slow"]), [:])
            default: return (200, self.json(["access_token": "at", "refresh_token": "rt",
                                             "expires_in": 28800]), [:])
            }
        }
        let step1 = try await pollOnce()
        XCTAssertEqual(step1, DeviceFlowPoll.pending)
        let step2 = try await pollOnce()
        XCTAssertEqual(step2, DeviceFlowPoll.slowDown(intervalSeconds: 5))
        let step3 = try await pollOnce()
        XCTAssertEqual(step3, DeviceFlowPoll.authorized)
        XCTAssertEqual(tokens.token(forKey: TokenKey.accessToken), "at")
        XCTAssertEqual(tokens.token(forKey: TokenKey.refreshToken), "rt")
        // token 不进 UserDefaults（R7：prefs 只存非凭据配置）。
        XCTAssertNil(UserContextProbe.defaultsValue(forKey: TokenKey.accessToken))
    }

    // 轮询终态：expired_token / access_denied。
    func test_poll_terminalStates() async throws {
        func pollOnce() async throws -> DeviceFlowPoll {
            try await auth.poll(clientID: "client-id", deviceCode: "dc")
        }
        for (errorBody, expected) in [("expired_token", DeviceFlowPoll.expired),
                                      ("access_denied", DeviceFlowPoll.denied)] {
            ScriptedURLProtocol.reset { _ in
                (200, self.json(["error": errorBody]), [:])
            }
            let actual = try await pollOnce()
            XCTAssertEqual(actual, expected)
        }
    }

    // token 未过期：直接复用，不发任何请求。
    func test_authorizedToken_farExpiry_noNetwork() async throws {
        tokens.setToken("access-1", forKey: TokenKey.accessToken)
        tokens.setToken(Date().addingTimeInterval(28800).iso8601String, forKey: TokenKey.accessTokenExpiry)
        ScriptedURLProtocol.reset { _ in
            XCTFail("不应发请求")
            return (500, Data(), [:])
        }
        let token = try await auth.authorizedToken()
        XCTAssertEqual(token, "access-1")
    }

    // 临期：refresh_token 换新并轮换（旧 refresh 作废必须同写）。
    func test_authorizedToken_nearExpiry_refreshesAndRotates() async throws {
        tokens.setToken("access-old", forKey: TokenKey.accessToken)
        tokens.setToken("refresh-old", forKey: TokenKey.refreshToken)
        tokens.setToken(Date().addingTimeInterval(60).iso8601String, forKey: TokenKey.accessTokenExpiry)
        ScriptedURLProtocol.reset { request in
            let body = String(decoding: request.httpBody ?? Data(), as: UTF8.self)
            XCTAssertTrue(body.contains("grant_type=refresh_token"))
            XCTAssertTrue(body.contains("refresh_token=refresh-old"))
            return (200, self.json(["access_token": "access-new", "refresh_token": "refresh-new",
                                    "expires_in": 28800]), [:])
        }
        let token = try await auth.authorizedToken()
        XCTAssertEqual(token, "access-new")
        XCTAssertEqual(tokens.token(forKey: TokenKey.refreshToken), "refresh-new")
    }

    // 刷新失败（refresh token 过期/授权被撤销）：断连清凭据 + needsReauth。
    func test_authorizedToken_refreshRejected_disconnects() async {
        tokens.setToken("access-old", forKey: TokenKey.accessToken)
        tokens.setToken("refresh-old", forKey: TokenKey.refreshToken)
        tokens.setToken(Date().addingTimeInterval(10).iso8601String, forKey: TokenKey.accessTokenExpiry)
        ScriptedURLProtocol.reset { _ in
            (200, self.json(["error": "bad_refresh_token"]), [:])
        }
        do {
            _ = try await auth.authorizedToken()
            XCTFail("expected needsReauth")
        } catch {
            XCTAssertEqual(error as? SyncError, .needsReauth)
        }
        XCTAssertNil(tokens.token(forKey: TokenKey.accessToken))
        XCTAssertNil(tokens.token(forKey: TokenKey.refreshToken))
        XCTAssertFalse(auth.hasToken)
    }

    // 无 refresh token 时临期 → needsReauth（撤销兜底）。
    func test_authorizedToken_expiredNoRefresh_needsReauth() async {
        tokens.setToken("access-old", forKey: TokenKey.accessToken)
        tokens.setToken(Date.distantPast.iso8601String, forKey: TokenKey.accessTokenExpiry)
        ScriptedURLProtocol.reset { _ in
            XCTFail("无 refresh token 不应发刷新请求")
            return (500, Data(), [:])
        }
        do {
            _ = try await auth.authorizedToken()
            XCTFail("expected needsReauth")
        } catch {
            XCTAssertEqual(error as? SyncError, .needsReauth)
        }
    }

    // 断开连接：清空全部凭据键。
    func test_disconnect_clearsAllCredentials() {
        tokens.setToken("a", forKey: TokenKey.accessToken)
        tokens.setToken("r", forKey: TokenKey.refreshToken)
        tokens.setToken("x", forKey: TokenKey.accessTokenExpiry)
        auth.disconnect()
        XCTAssertFalse(auth.hasToken)
        XCTAssertNil(tokens.token(forKey: TokenKey.refreshToken))
        XCTAssertNil(tokens.token(forKey: TokenKey.accessTokenExpiry))
    }
}

// MARK: - Keychain 存取（mock 存储；R7 断言凭据不入 prefs）

// UserDefaults 值探测（测试辅助；不落任何真实值）。
enum UserContextProbe {
    static func defaultsValue(forKey key: String) -> String? {
        UserDefaults.standard.string(forKey: key)
    }
}

final class TokenStoreTests: XCTestCase {
    // mock Keychain 存取往返 + 删除。
    func test_inMemoryStore_roundTripAndDelete() {
        let store = InMemoryTokenStore()
        XCTAssertNil(store.token(forKey: TokenKey.accessToken))
        store.setToken("abc", forKey: TokenKey.accessToken)
        XCTAssertEqual(store.token(forKey: TokenKey.accessToken), "abc")
        store.setToken("", forKey: TokenKey.accessToken)
        XCTAssertNil(store.token(forKey: TokenKey.accessToken))
        store.setToken(nil, forKey: TokenKey.refreshToken)
        XCTAssertNil(store.token(forKey: TokenKey.refreshToken))
    }

    // 真实 Keychain 实现的存取冒烟（模拟器 Keychain；唯一 service，测试后清理）。
    func test_keychainStore_roundTripAndCleanup() {
        let service = "com.gomeen.zakki.ios.tests-\(UUID().uuidString)"
        let store = KeychainStore(service: service)
        defer {
            store.setToken(nil, forKey: TokenKey.accessToken)
            store.setToken(nil, forKey: TokenKey.refreshToken)
        }
        store.setToken("secret-token", forKey: TokenKey.accessToken)
        XCTAssertEqual(store.token(forKey: TokenKey.accessToken), "secret-token")
        store.setToken("rotated", forKey: TokenKey.accessToken)
        XCTAssertEqual(store.token(forKey: TokenKey.accessToken), "rotated")
        store.setToken(nil, forKey: TokenKey.accessToken)
        XCTAssertNil(store.token(forKey: TokenKey.accessToken))
    }
}

extension Date {
    var iso8601String: String {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return f.string(from: self)
    }
}

private extension String {
    func insertNewlines(every n: Int) -> String {
        var out = ""
        var i = startIndex
        while i < endIndex {
            let j = index(i, offsetBy: n, limitedBy: endIndex) ?? endIndex
            out += self[i..<j] + "\n"
            i = j
        }
        return out
    }
}

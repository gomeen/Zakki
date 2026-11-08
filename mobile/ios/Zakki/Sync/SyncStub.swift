import Foundation

// MARK: - 端到端 stub（仅启动参数 -ZakkiSyncStub 时生效）

// 进程内 URLProtocol 模拟 GitHub 端点：连接（Device Flow）→ 拉推（Contents API）
// → 人为制造远端变化（脚本化注入）→ 冲突 → 留档上传，全程不出进程、不碰真实网络。
// 仅供模拟器端到端实测与 UI 测试使用；单测走独立 URLProtocol mock。
enum SyncStubMode {
    static let launchArg = "-ZakkiSyncStub"
    // 预授权路径启动参数（仅 stub 模式生效）：模拟器端到端实测后端 B，见 SyncEngine.forLaunch。
    static let cloudGrantArg = "-ZakkiCloudGrant"
    static var isActive: Bool { ProcessInfo.processInfo.arguments.contains(launchArg) }
    static let host = "stub.zakki.local"
    // Device Flow 端点挂在 /github 下，Contents API 挂在 /api 下（单域名拦截）。
    static let githubBase = URL(string: "https://\(host)/github")!
    static let apiBase = URL(string: "https://\(host)/api")!
    static let defaultsSuite = "zakki-sync-stub"

    static func stubbedSession() -> URLSession {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [SyncStubURLProtocol.self]
        return URLSession(configuration: config)
    }
}

// PUT zakki.json 的脚本化结果：成功返回新版本号；CAS 不匹配返回冲突语义。
enum StubPutOutcome {
    case updated(newVersion: String)
    case conflict
}

// stub 世界状态：远端 zakki.json、版本号、脚本化注入开关与调用记录。
final class SyncStubState {
    static let shared = SyncStubState()

    private let lock = NSLock()
    private(set) var remoteContent: String?
    private(set) var remoteVersion = ""
    private var versionCounter = 0
    private var pollCount = 0
    // 注入开关：置位后的下一次 GET zakki.json 会先给远端追加一个标签（人为远端变化），
    // 然后自动复位。首个 PUT 成功后引擎内部无需感知——由端到端脚本控制时序。
    var injectRemoteMutationOnNextGet = false
    private(set) var archivedPaths: [String] = []
    private(set) var putBodies: [String: String] = [:]

    func reset(remoteContent: String?) {
        lock.lock()
        defer { lock.unlock() }
        self.remoteContent = remoteContent
        remoteVersion = ""
        versionCounter = 0
        pollCount = 0
        injectRemoteMutationOnNextGet = false
        archivedPaths = []
        putBodies = [:]
    }

    // GET zakki.json：先按需注入远端变化。
    func currentRemote() -> (content: String, sha: String)? {
        lock.lock()
        defer { lock.unlock() }
        if injectRemoteMutationOnNextGet {
            injectRemoteMutationOnNextGet = false
            remoteContent = Self.mutated(remoteContent)
            bumpVersionLocked()
        }
        guard let content = remoteContent else { return nil }
        return (content, remoteVersion)
    }

    // PUT zakki.json：CAS 校验（sha 不匹配 → 409）；成功记录新版本并按脚本武装注入。
    func putMain(content: String, sha: String?) -> StubPutOutcome {
        lock.lock()
        defer { lock.unlock() }
        if remoteContent?.isEmpty == false || !remoteVersion.isEmpty {
            guard sha == remoteVersion else { return .conflict }
        }
        remoteContent = content
        bumpVersionLocked()
        // 端到端脚本：首个成功上传后武装「下一次 GET 注入远端变化」。
        injectRemoteMutationOnNextGet = true
        return .updated(newVersion: remoteVersion)
    }

    func putArchive(path: String, content: String) {
        lock.lock()
        defer { lock.unlock() }
        archivedPaths.append(path)
        putBodies[path] = content
    }

    func pollDeviceFlow() -> String {
        lock.lock()
        defer { lock.unlock() }
        pollCount += 1
        return pollCount <= 1 ? "authorization_pending" : "ok"
    }

    private func bumpVersionLocked() {
        versionCounter += 1
        remoteVersion = String(format: "stubsha%08d", versionCounter)
    }

    // 人为远端变化：给首个标签追加一行（字节必变，JSON 仍合法）。
    static func mutated(_ raw: String?) -> String? {
        guard var obj = raw.flatMap({ try? JSONSerialization.jsonObject(with: Data($0.utf8)) }) as? [String: Any],
              var tabs = obj["tabs"] as? [[String: Any]], !tabs.isEmpty else { return raw }
        var first = tabs[0]
        first["text"] = ((first["text"] as? String) ?? "") + "\n- [ ] 远端新增的改动行"
        tabs[0] = first
        obj["tabs"] = tabs
        guard let data = try? JSONSerialization.data(withJSONObject: obj) else { return raw }
        return String(decoding: data, as: UTF8.self)
    }
}

// URLProtocol 拦截实现：同步处理（stub 无 IO），按 host + path 分发。
final class SyncStubURLProtocol: URLProtocol {
    override class func canInit(with request: URLRequest) -> Bool {
        request.url?.host == SyncStubMode.host
    }

    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        guard let url = request.url else { return }
        let path = url.path
        let state = SyncStubState.shared
        let body = request.httpBody ?? request.bodyStreamData ?? Data()
        let (status, payload): (Int, Data)

        switch (request.httpMethod ?? "GET", true) {
        case (_, path.hasSuffix("/login/device/code")):
            payload = stubJSON([
                "device_code": "stub-device-code",
                "user_code": "TEST-CODE",
                "verification_uri": "https://github.com/login/device",
                "expires_in": 900,
                "interval": 1,
            ])
            status = 200
        case (_, path.hasSuffix("/login/oauth/access_token")):
            if let fields = parseForm(body), fields["grant_type"] == "refresh_token" {
                payload = stubJSON([
                    "access_token": "stub-access-\(Int.random(in: 1000...9999))",
                    "refresh_token": "stub-refresh-\(Int.random(in: 1000...9999))",
                    "expires_in": 28800,
                ])
                status = 200
            } else if state.pollDeviceFlow() == "authorization_pending" {
                payload = stubJSON(["error": "authorization_pending"])
                status = 200
            } else {
                payload = stubJSON([
                    "access_token": "stub-access-token",
                    "refresh_token": "stub-refresh-token",
                    "expires_in": 28800,
                ])
                status = 200
            }
        case ("GET", true):
            if path.hasSuffix("/contents/zakki.json") {
                if let remote = state.currentRemote() {
                    payload = stubJSON(["sha": remote.sha, "encoding": "base64",
                                        "content": Data(remote.content.utf8).base64EncodedString()])
                    status = 200
                } else {
                    payload = stubJSON(["message": "Not Found"])
                    status = 404
                }
            } else {
                payload = stubJSON(["message": "Not Found"])
                status = 404
            }
        case ("PUT", true):
            if let fields = try? JSONSerialization.jsonObject(with: body) as? [String: Any],
               let content = fields["content"] as? String,
               let decoded = Data(base64Encoded: content),
               let text = String(data: decoded, encoding: .utf8) {
                let cleanPath = path.replacingOccurrences(of: "/repos/stub-owner/stub-repo/contents/", with: "")
                if cleanPath == "zakki.json" {
                    switch state.putMain(content: text, sha: fields["sha"] as? String) {
                    case .updated(let newSha):
                        payload = stubJSON(["content": ["sha": newSha]])
                        status = 200
                    case .conflict:
                        payload = stubJSON(["message": "conflict"])
                        status = 409
                    }
                } else {
                    state.putArchive(path: cleanPath, content: text)
                    payload = stubJSON(["content": ["sha": "stub-archive-sha"]])
                    status = 201
                }
            } else {
                payload = stubJSON(["message": "bad request"])
                status = 400
            }
        default:
            payload = stubJSON(["message": "unsupported"])
            status = 405
        }

        let response = HTTPURLResponse(url: url, statusCode: status,
                                       httpVersion: "HTTP/1.1",
                                       headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: payload)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}

    private func stubJSON(_ obj: [String: Any]) -> Data {
        (try? JSONSerialization.data(withJSONObject: obj)) ?? Data("{}".utf8)
    }

    private func parseForm(_ body: Data) -> [String: String]? {
        guard let raw = String(data: body, encoding: .utf8) else { return nil }
        var out: [String: String] = [:]
        for pair in raw.split(separator: "&") {
            let kv = pair.split(separator: "=", maxSplits: 1)
            guard let key = kv.first.map(String.init) else { continue }
            out[key.removingPercentEncoding ?? key] = kv.count > 1 ? (kv[1].removingPercentEncoding ?? String(kv[1])) : ""
        }
        return out.isEmpty ? nil : out
    }
}

extension URLRequest {
    // httpBody 为空时（URLSession 内部转换）从 bodyStream 兜底读取。
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

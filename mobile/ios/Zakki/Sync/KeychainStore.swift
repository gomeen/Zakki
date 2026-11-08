import Foundation
import Security

// MARK: - Token 存储（R7：token/refresh_token 只进 Keychain，不进 zakki.json/prefs/日志）

// 注入口：生产用 Keychain，测试/端到端 stub 用内存实现。
protocol TokenStoring: AnyObject {
    func token(forKey key: String) -> String?
    func setToken(_ value: String?, forKey key: String)
}

// Keychain 键名常量（不携带任何凭据值，可安全出现在代码与日志里）。
// 除 token 对外还存 access_token 过期时刻（ISO8601 字符串），仍是凭据元数据，
// 只进 Keychain，不进 prefs/日志（R7）。
enum TokenKey {
    static let accessToken = "github.access-token"
    static let refreshToken = "github.refresh-token"
    static let accessTokenExpiry = "github.access-token-expiry"
    // 后端 B（#53）：security-scoped bookmark（base64）。书签不含敏感数据，
    // 但属授权凭据，与 token 同规则进 Keychain，不进 prefs/zakki.json（R7）。
    static let cloudFolderBookmark = "cloud.folder.bookmark"
}

// 系统 Keychain 实现：kSecClassGenericPassword，service 固定、account 即键名。
final class KeychainStore: TokenStoring {
    private let service: String

    init(service: String = "com.gomeen.zakki.ios.sync") {
        self.service = service
    }

    func token(forKey key: String) -> String? {
        var query = baseQuery(key: key)
        query[kSecReturnData as String] = kCFBooleanTrue
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: AnyObject?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        guard status == errSecSuccess, let data = result as? Data else { return nil }
        return String(decoding: data, as: UTF8.self)
    }

    func setToken(_ value: String?, forKey key: String) {
        // 空值即删除；有值则先删后写，避免 update 的属性差异分支。
        guard let value, !value.isEmpty else {
            delete(key: key)
            return
        }
        delete(key: key)
        var query = baseQuery(key: key)
        query[kSecValueData as String] = Data(value.utf8)
        query[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        SecItemAdd(query as CFDictionary, nil)
    }

    private func delete(key: String) {
        SecItemDelete(baseQuery(key: key) as CFDictionary)
    }

    private func baseQuery(key: String) -> [String: Any] {
        [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
        ]
    }
}

// 内存实现：单元测试与端到端 stub 用，绝不触真实 Keychain。
final class InMemoryTokenStore: TokenStoring {
    private var storage: [String: String] = [:]

    func token(forKey key: String) -> String? { storage[key] }

    func setToken(_ value: String?, forKey key: String) {
        if let value, !value.isEmpty {
            storage[key] = value
        } else {
            storage.removeValue(forKey: key)
        }
    }
}

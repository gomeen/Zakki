import Foundation
import Zakkicore

// DecodeNotes 返回 JSON 的已知字段；各层未知字段不跨绑定边界，由 Go 侧
// model.Decode/Encode 在原文件里原样保留（M1 只读，M2 编辑写回时消费）。
struct NotesTab: Codable, Equatable, Sendable {
    var id: UInt64
    var name: String
    var text: String
    var color: String
}

struct NotesSnapshot: Codable, Equatable, Sendable {
    var writable: Bool
    var reason: String
    var version: Int
    var active: Int
    // 只有 reason=ok 才非 nil；legacy 的 v1 文件是空数组。
    var tabs: [NotesTab]?
}

// 写保护原因，取值与 core/zakkicore notes.go 的 reason 一致。
enum WriteProtectReason: String {
    case versionTooNew
    case tooManyTabs
    case legacyVersion
    case invalid
}

// 绑定封装层：gomobile 生成的自由函数返回非可选 String，Go error 走显式
// NSErrorPointer（不套 Swift throws 约定，因返回值标注了 _Nonnull）；
// 错误串是 `code: 说明` 格式，客户端按错误码处理、不匹配中文文案。
enum NotesCore {
    static func decode(_ raw: String) throws -> NotesSnapshot {
        var err: NSError?
        let out = ZakkicoreDecodeNotes(raw, &err)
        if let err {
            throw NotesCoreError(core: err)
        }
        return try JSONDecoder().decode(NotesSnapshot.self, from: Data(out.utf8))
    }

    static func encode(_ raw: String, tabs: [NotesTab]) throws -> String {
        let tabsJSON = try JSONEncoder().encode(tabs)
        var err: NSError?
        let out = ZakkicoreEncodeNotes(raw, String(decoding: tabsJSON, as: UTF8.self), &err)
        if let err {
            throw NotesCoreError(core: err)
        }
        return out
    }
}

// 绑定层返回的 Go 错误（`code: 说明` 格式），上层按前缀码分类。
struct NotesCoreError: Error {
    let nsError: NSError

    init(core nsError: NSError) {
        self.nsError = nsError
    }

    var code: String { nsError.localizedDescription.split(separator: ":").first.map(String.init) ?? "" }
}

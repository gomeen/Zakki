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

    // 时区同步（规格 §3.2）：启动与时区变化时调用；空串恢复 time.Local。
    static func setLocation(_ name: String) {
        var err: NSError?
        _ = ZakkicoreSetLocation(name, &err)
        if let err {
            // 时区名来自系统（IANA 标识），理论不会失败；防御性忽略并留调试断言。
            assertionFailure("SetLocation 失败: \(err)")
        }
    }
}

// AddNoteTab 的返回形状：新文件 JSON 与新标签 id（新 id 从这里取，客户端不自行计算）。
struct AddNoteTabResult: Codable, Equatable, Sendable {
    var raw: String
    var id: UInt64
}

// 写路径三函数（M2-4）与 encode 同口径：返回整个新文件 JSON，错误串 `code: 说明`。
extension NotesCore {
    // 末尾追加标签；满 9 个或写保护文件报错，新标签的 id 在返回值里。
    static func addTab(_ raw: String, name: String, color: String) throws -> AddNoteTabResult {
        var err: NSError?
        let out = ZakkicoreAddNoteTab(raw, name, color, &err)
        if let err {
            throw NotesCoreError(core: err)
        }
        return try JSONDecoder().decode(AddNoteTabResult.self, from: Data(out.utf8))
    }

    // 按 id 删除标签；只剩一个或 id 不存在报错（invalid_args），写保护报错。
    static func deleteTab(_ raw: String, id: UInt64) throws -> String {
        var err: NSError?
        // 越界 id 截断后必不匹配任何标签，由 Go 侧按 id 不存在拒绝。
        let out = ZakkicoreDeleteNoteTab(raw, Int64(clamping: id), &err)
        if let err {
            throw NotesCoreError(core: err)
        }
        return out
    }

    // 按完整 id 排列重排；ids 经 JSON 数组过边界（gomobile 不绑定 uint64 切片）。
    static func reorderTabs(_ raw: String, ids: [UInt64]) throws -> String {
        let idsJSON = try JSONEncoder().encode(ids)
        var err: NSError?
        let out = ZakkicoreReorderNoteTabs(raw, String(decoding: idsJSON, as: UTF8.self), &err)
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

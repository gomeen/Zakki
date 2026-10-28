import Foundation

// M1 的笔记状态：磁盘 zakki.json 的解码快照与原始字节。只读展示，编辑属 M2。
@MainActor
final class NotesModel: ObservableObject {
    @Published private(set) var snapshot: NotesSnapshot?
    // 文件读取/复制/内部解码失败的原因（reason=invalid 的解码失败不算，走横幅）。
    @Published var importError: String?
    // EncodeNotes 的输入原始字节；M1 只读暂不写回，M2 编辑保存要靠它。
    private(set) var raw: String?

    static let launchArgImportPath = "ZakkiImportPath"

    static var sandboxURL: URL {
        let dir = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        return dir.appendingPathComponent("zakki.json", isDirectory: false)
    }

    init() {
        loadOnLaunch()
    }

    // 启动加载顺序：有启动参数先走导入管道（覆盖沙盒副本），否则沙盒已有文件直接加载。
    private func loadOnLaunch() {
        let args = ProcessInfo.processInfo.arguments
        if let i = args.firstIndex(of: "-\(Self.launchArgImportPath)"), i + 1 < args.count {
            importFile(at: URL(fileURLWithPath: args[i + 1]))
            return
        }
        let url = Self.sandboxURL
        guard FileManager.default.fileExists(atPath: url.path) else { return }
        loadSandbox(at: url)
    }

    // 导入管道：读字节 → 原子复制到沙盒 Application Support/zakki.json → DecodeNotes。
    // 文件选择器与启动参数 -ZakkiImportPath 都走这一条路；选择器 asCopy 给的是
    // 临时副本，安全作用域调用对其返回 false，两条来源统一包裹无副作用。
    @discardableResult
    func importFile(at source: URL) -> Bool {
        let scoped = source.startAccessingSecurityScopedResource()
        defer { if scoped { source.stopAccessingSecurityScopedResource() } }
        let data: Data
        do {
            data = try Data(contentsOf: source)
        } catch {
            importError = error.localizedDescription
            return false
        }
        return install(data)
    }

    private func loadSandbox(at url: URL) {
        let data: Data
        do {
            data = try Data(contentsOf: url)
        } catch {
            importError = error.localizedDescription
            return
        }
        apply(data)
    }

    // 解码失败（reason=invalid）也保留副本：写保护横幅要能解释磁盘现状。
    private func install(_ data: Data) -> Bool {
        do {
            // 全新沙盒里 Application Support 不存在，原子写会因父目录缺失失败。
            let dir = Self.sandboxURL.deletingLastPathComponent()
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            try data.write(to: Self.sandboxURL, options: .atomic)
        } catch {
            importError = error.localizedDescription
            return false
        }
        apply(data)
        return true
    }

    private func apply(_ data: Data) {
        let text = String(decoding: data, as: UTF8.self)
        do {
            snapshot = try NotesCore.decode(text)
            raw = text
        } catch {
            // DecodeNotes 只在内部异常时报错；到这里说明绑定层坏了，如实上报不吞。
            importError = error.localizedDescription
        }
    }
}

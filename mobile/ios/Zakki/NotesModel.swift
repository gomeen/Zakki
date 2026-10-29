import Foundation

// M2 的笔记状态：磁盘 zakki.json 的解码快照与原始字节；正文编辑经防抖写回沙盒副本。
@MainActor
final class NotesModel: ObservableObject {
    @Published private(set) var snapshot: NotesSnapshot?
    // 文件读取/复制/内部解码失败的原因（reason=invalid 的解码失败不算，走横幅）。
    @Published var importError: String?
    // 写回失败如实上报（含 write_protected 兜底）；nil 表示无待呈现错误。
    @Published private(set) var saveError: String?
    // EncodeNotes 的输入原始字节；每次成功写回后以 encode 输出为新基线。
    private(set) var raw: String?
    private let sandboxURL: URL

    static let launchArgImportPath = "ZakkiImportPath"
    // 正文防抖写回间隔。
    static let saveDebounceInterval: TimeInterval = 1.0

    nonisolated static var defaultSandboxURL: URL {
        let dir = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        return dir.appendingPathComponent("zakki.json", isDirectory: false)
    }

    // 待写回的 tab 集合；防抖定时器到点或编辑器关闭时统一落盘。
    private var pendingTabIDs: Set<UInt64> = []
    private var saveWorkItem: DispatchWorkItem?
    // 测试注入沙盒路径用；生产路径由 defaultSandboxURL 提供。
    // swiftlint:disable:next private_init
    init(sandboxURL: URL = NotesModel.defaultSandboxURL) {
        self.sandboxURL = sandboxURL
        loadOnLaunch()
    }

    // 启动加载顺序：有启动参数先走导入管道（覆盖沙盒副本），否则沙盒已有文件直接加载。
    private func loadOnLaunch() {
        let args = ProcessInfo.processInfo.arguments
        if let i = args.firstIndex(of: "-\(Self.launchArgImportPath)"), i + 1 < args.count {
            importFile(at: URL(fileURLWithPath: args[i + 1]))
            return
        }
        guard FileManager.default.fileExists(atPath: sandboxURL.path) else { return }
        loadSandbox(at: sandboxURL)
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
            let dir = sandboxURL.deletingLastPathComponent()
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            try data.write(to: sandboxURL, options: .atomic)
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

    // MARK: 编辑写回（D8）

    // 编辑器写回入口：更新快照并调度防抖保存；writable=false 拒绝（编辑器
    // isEditable=false 是第一道门，这里兜底）。
    func updateTabText(_ text: String, tabID: UInt64) {
        guard var snap = snapshot, snap.writable, var tabs = snap.tabs,
              let i = tabs.firstIndex(where: { $0.id == tabID }), tabs[i].text != text else { return }
        tabs[i].text = text
        snap.tabs = tabs
        snapshot = snap
        pendingTabIDs.insert(tabID)
        scheduleSave()
    }

    private func scheduleSave() {
        saveWorkItem?.cancel()
        let item = DispatchWorkItem { [weak self] in self?.flushPendingSave() }
        saveWorkItem = item
        DispatchQueue.main.asyncAfter(deadline: .now() + Self.saveDebounceInterval, execute: item)
    }

    // 用户确认保存失败提示后清除。
    func clearSaveError() {
        saveError = nil
    }

    // 仅供测试注入被污染的基线以触发 write_protected 兑底路径；生产流程不调用。
    func debugReplaceRawForTesting(_ newRaw: String) {
        raw = newRaw
    }

    // 导出/分享入口交出的沙盒文件 URL（无文件时 UI 隐藏分享按钮）。
    var fileURL: URL { sandboxURL }

    // MARK: 标签管理（M2-4）

    // 管理操作不走防抖：变更经绑定即时原子落盘，成功后以新 JSON 为 raw 基线并
    // 重解码快照（active 等磁盘语义以落盘文件为准）。写失败如实上报且状态不动。
    @discardableResult
    private func commitTabChange(_ makeNewRaw: (String) throws -> String) -> Bool {
        guard let snap = snapshot, snap.writable, snap.tabs != nil else {
            saveError = Strings.tabChangeFailedMessage(code: "write_protected")
            return false
        }
        // 先冲刷防抖中的正文改动：结构变更基于过期基线会把待写正文冲掉。
        flushPendingSave()
        guard pendingTabIDs.isEmpty, let raw else { return false } // flush 失败已上报
        do {
            let out = try makeNewRaw(raw)
            try FileManager.default.createDirectory(at: sandboxURL.deletingLastPathComponent(), withIntermediateDirectories: true)
            try Data(out.utf8).write(to: sandboxURL, options: .atomic)
            self.raw = out
            do {
                snapshot = try NotesCore.decode(out)
            } catch {
                // 文件已落盘但快照重解码失败：与启动解码同渠道上报，不吞。
                importError = error.localizedDescription
                return false
            }
            saveError = nil
            return true
        } catch {
            saveError = Strings.tabChangeFailedMessage(code: (error as? NotesCoreError)?.code ?? "io")
            return false
        }
    }

    // 结构性变更（增/删/排序）：绑定直接产出新文件 JSON。
    @discardableResult
    private func performStructuralChange(_ change: (String) throws -> String) -> Bool {
        commitTabChange(change)
    }

    // 新增标签到末尾并切为磁盘 active（末尾）；颜色由调用方用 unusedTabColor 选，
    // Name 传空串由 UI 显示 Untitled。返回新 id 便于调用方导航。
    @discardableResult
    func addTab(name: String, color: String) -> Bool {
        performStructuralChange { raw in
            try NotesCore.addTab(raw, name: name, color: color).raw
        }
    }

    @discardableResult
    func deleteTab(id: UInt64) -> Bool {
        performStructuralChange { try NotesCore.deleteTab($0, id: id) }
    }

    @discardableResult
    func reorderTabs(ids: [UInt64]) -> Bool {
        performStructuralChange { try NotesCore.reorderTabs($0, ids: ids) }
    }

    // 已知字段覆盖类变更（重命名/换色）：与 updateTabText 同用 EncodeNotes 管道，
    // 但即时落盘。桌面 renameTab parity：名称去首尾空白，空串=未命名。
    @discardableResult
    func renameTab(id: UInt64, name: String) -> Bool {
        overlayTab(id: id) { $0.name = name.trimmingCharacters(in: .whitespacesAndNewlines) }
    }

    @discardableResult
    func setTabColor(id: UInt64, color: String) -> Bool {
        overlayTab(id: id) { $0.color = color }
    }

    private func overlayTab(id: UInt64, mutate: (inout NotesTab) -> Void) -> Bool {
        guard let snap = snapshot, snap.writable, var tabs = snap.tabs,
              let i = tabs.firstIndex(where: { $0.id == id }) else {
            saveError = Strings.tabChangeFailedMessage(code: "write_protected")
            return false
        }
        let before = tabs[i]
        mutate(&tabs[i])
        guard tabs[i] != before else { return true } // 无变化不落盘
        return commitTabChange { try NotesCore.encode($0, tabs: tabs) }
    }

    // 桌面 unusedTabColor 移植：优先未被现有标签占用的调色板色（大小写不敏感，
    // 空/非法色不占位）；全部占用时回退 palette[标签数 % 12]。
    static func unusedTabColor(for tabs: [NotesTab]) -> String {
        for cand in ThemeTagPalette.colors {
            if !tabs.contains(where: { $0.color.caseInsensitiveCompare(cand) == .orderedSame }) {
                return cand
            }
        }
        return ThemeTagPalette.hex(at: tabs.count)
    }

    // 立即落盘全部待写改动（编辑器关闭/退后台时也调用，幂等）。
    func flushPendingSave() {
        saveWorkItem?.cancel()
        saveWorkItem = nil
        guard !pendingTabIDs.isEmpty else { return }
        guard let raw, let snap = snapshot, snap.writable, let tabs = snap.tabs else {
            pendingTabIDs = []
            return
        }
        do {
            let out = try NotesCore.encode(raw, tabs: tabs)
            try FileManager.default.createDirectory(at: sandboxURL.deletingLastPathComponent(), withIntermediateDirectories: true)
            try Data(out.utf8).write(to: sandboxURL, options: .atomic)
            // encode 输出成为新 raw 基线：后续 encode 都基于它（保持绑定往返稳定）。
            self.raw = out
            pendingTabIDs = []
            saveError = nil
        } catch {
            // 写回失败如实上报；保留待写集合，下次 flush 重试。
            let code = (error as? NotesCoreError)?.code
            saveError = Strings.saveFailedMessage(code: code ?? "io")
        }
    }
}

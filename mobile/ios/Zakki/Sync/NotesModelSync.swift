import Foundation

// MARK: - NotesModel 同步接入（M4-1）

// 同步引擎需要的安装路径；扩展保持 NotesModel 主体不涉同步语义。
extension NotesModel {
    // 以远端内容整体替换本地文件并刷新快照（§2.2 采用远端/保留远端路径）。
    // writable 前置检查由引擎完成；待写集合与防抖定时器作废（远端字节是新的基线）。
    // zakki.json 的 active 字段随远端字节原样往返，本方法不做任何字段改写。
    @discardableResult
    func installSyncedContent(_ data: Data) -> Bool {
        discardPendingWrites()
        return install(data)
    }

    // 同步前读取的原始字节（防抖冲刷后调用）。
    var syncRawContent: String? { raw }
}

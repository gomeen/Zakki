import Foundation
import UIKit

// MARK: - 冲突留档（D13）：conflicts/zakki-YYYYMMDD-HHmmss-<设备名>.json

// 落选版本完整写入与 zakki.json 同目录的 conflicts/ 子目录，随同步一起保存
//（后端 A 上传到仓库 conflicts/ 路径）；留档不进标签、不参与调度、不计入 9 个
// 标签上限；设置里可查看和删除，不自动清理。
struct ConflictArchiveEntry: Identifiable, Equatable {
    var fileName: String
    var createdAtMs: Int64
    var deviceName: String
    var id: String { fileName }
}

final class ConflictArchiveStore {
    // 目录注入：生产为 zakki.json 同目录下的 conflicts/；测试/端到端用临时目录。
    private let dir: URL
    private let clock: () -> Int64
    private let deviceName: () -> String

    static let fileExtension = "json"

    init(dir: URL, clock: @escaping () -> Int64 = { Int64(Date().timeIntervalSince1970 * 1000) },
         deviceName: (() -> String)? = nil)
    {
        self.dir = dir
        self.clock = clock
        // 设备名由装配层（引擎，主线程）提供；独立使用回落固定名，不在此处读 UIDevice。
        self.deviceName = deviceName ?? { "" }
    }

    // zakki.json 同目录（Application Support）下的 conflicts/。
    static func directory(forZakkiURL zakkiURL: URL) -> URL {
        zakkiURL.deletingLastPathComponent().appendingPathComponent("conflicts", isDirectory: true)
    }

    // MARK: 写入

    // 落选版本完整落盘；返回文件名（供 pending 上传清单登记）。
    // 同一秒同名冲突（极小概率）不覆盖既有留档——递增序号后缀保证完整留档。    @discardableResult
    func write(content: String) throws -> String {
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let base = "zakki-\(timestamp())-\(sanitizedDeviceName())"
        var name = "\(base).\(Self.fileExtension)"
        var suffix = 2
        while FileManager.default.fileExists(atPath: dir.appendingPathComponent(name).path) {
            name = "\(base)-\(suffix).\(Self.fileExtension)"
            suffix += 1
        }
        let url = dir.appendingPathComponent(name)
        try Data(content.utf8).write(to: url, options: .atomic)
        return name
    }

    // MARK: 列表与删除

    func list() -> [ConflictArchiveEntry] {
        let files = (try? FileManager.default.contentsOfDirectory(at: dir, includingPropertiesForKeys: [.creationDateKey, .contentModificationDateKey])) ?? []
        let entries: [ConflictArchiveEntry] = files.compactMap { url in
            guard url.pathExtension == Self.fileExtension, url.lastPathComponent.hasPrefix("zakki-") else { return nil }
            return parse(fileName: url.lastPathComponent, url: url)
        }
        return entries.sorted { $0.createdAtMs > $1.createdAtMs }
    }

    // 读取留档全文（设置页查看入口）；按文件名或条目皆可。
    func content(ofName fileName: String) -> String? {
        let url = dir.appendingPathComponent(fileName)
        return try? String(contentsOf: url, encoding: .utf8)
    }

    func content(of entry: ConflictArchiveEntry) -> String? {
        content(ofName: entry.fileName)
    }

    func delete(_ entry: ConflictArchiveEntry) {
        try? FileManager.default.removeItem(at: dir.appendingPathComponent(entry.fileName))
    }

    // MARK: 解析

    // 文件名 → 展示字段；解析失败按修改时间与原始名兜底（外部手工放置的文件也能列出）。
    private func parse(fileName: String, url: URL) -> ConflictArchiveEntry {
        // zakki-YYYYMMDD-HHmmss-<设备名>.json（设备名可含连字符，取最右两段之外的主体）。
        let stem = url.deletingPathExtension().lastPathComponent
        let parts = stem.split(separator: "-", maxSplits: 3).map(String.init)
        if parts.count >= 3, parts[0] == "zakki",
           let ms = Self.parseTimestamp(parts[1] + "-" + parts[2]) {
            let device = parts.count >= 4 ? parts[3] : ""
            return ConflictArchiveEntry(fileName: fileName, createdAtMs: ms, deviceName: device)
        }
        let mtime = (try? url.resourceValues(forKeys: [.contentModificationDateKey]))?
            .contentModificationDate?.timeIntervalSince1970 ?? 0
        return ConflictArchiveEntry(fileName: fileName, createdAtMs: Int64(mtime * 1000), deviceName: "")
    }

    // 时间戳：本地墙上时刻 yyyyMMdd-HHmmss（仅命名用，不参与冲突判定——D13 判定不依赖时钟）。
    private func timestamp() -> String {
        let f = DateFormatter()
        f.dateFormat = "yyyyMMdd-HHmmss"
        f.locale = Locale(identifier: "en_US_POSIX")
        return f.string(from: Date(timeIntervalSince1970: TimeInterval(clock()) / 1000))
    }

    static func parseTimestamp(_ s: String) -> Int64? {
        let f = DateFormatter()
        f.dateFormat = "yyyyMMdd-HHmmss"
        f.locale = Locale(identifier: "en_US_POSIX")
        f.timeZone = TimeZone.current
        guard let d = f.date(from: s) else { return nil }
        return Int64(d.timeIntervalSince1970 * 1000)
    }

    // 设备名进文件名与 GitHub path：剔除路径分隔符与控制字符，空白折叠为连字符，
    // 空名回落 "device"。
    private func sanitizedDeviceName() -> String {
        let raw = deviceName()
        var allowed = CharacterSet.alphanumerics
        allowed.insert(charactersIn: "-_")
        let mapped = raw.unicodeScalars.map { scalar -> Character in
            allowed.contains(scalar) ? Character(scalar) : "-"
        }
        let cleaned = String(mapped).trimmingCharacters(in: CharacterSet(charactersIn: "-"))
        return cleaned.isEmpty ? "device" : String(cleaned.prefix(40))
    }
}

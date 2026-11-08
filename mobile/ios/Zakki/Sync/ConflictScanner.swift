import Foundation

// MARK: - 冲突副本命名扫描（探针 §3 ConflictScanner 移植 + 数值排序修正）

// 两种模式：<base> (N).<ext>（编号副本）与含 "conflicted copy" 的云盘冲突副本命名
//（iCloud 风格，不区分大小写）。误伤边界与探针单测一致：不匹配无空格、不同扩展名、
// 不同 base 的文件。
enum ConflictScanner {
    struct Result: Equatable {
        var numbered: [URL] = []
        var conflicted: [URL] = []
        var all: [URL] = []

        // 双通道合并视图：编号副本 + conflicted 命名（去重，保持各自排序）。
        var merged: [URL] {
            var seen = Set<String>()
            return (numbered + conflicted).filter { seen.insert($0.lastPathComponent).inserted }
        }
    }

    // 在 dir 里找 target 的疑似冲突副本。numbered 按编号数值排序——
    // 字典序会让 (10) 排在 (2) 前面（探针记录的注意点），必须按数值比。
    static func scan(target: URL, in dir: URL, fm: FileManager = .default) -> Result {
        let base = target.deletingPathExtension().lastPathComponent
        let ext = target.pathExtension
        var result = Result()
        let names = (try? fm.contentsOfDirectory(atPath: dir.path)) ?? []
        let pattern = "^\(NSRegularExpression.escapedPattern(for: base)) \\((\\d+)\\)$"
        let numberRegex = try? NSRegularExpression(pattern: pattern)
        struct NumberedEntry {
            var url: URL
            var number: Int
        }
        var numberedEntries: [NumberedEntry] = []
        var conflictedNames: [String] = []
        for name in names where (name as NSString).pathExtension == ext {
            let stem = (name as NSString).deletingPathExtension
            let range = NSRange(stem.startIndex..., in: stem)
            if let numberRegex, let match = numberRegex.firstMatch(in: stem, range: range),
               let r = Range(match.range(at: 1), in: stem), let n = Int(stem[r]) {
                numberedEntries.append(NumberedEntry(url: dir.appendingPathComponent(name), number: n))
            }
            if stem.lowercased().contains("conflicted copy") {
                conflictedNames.append(name)
            }
        }
        result.numbered = numberedEntries.sorted { a, b in
            a.number != b.number ? a.number < b.number : a.url.lastPathComponent < b.url.lastPathComponent
        }.map(\.url)
        result.conflicted = conflictedNames.sorted().map { dir.appendingPathComponent($0) }
        // all 去重合并（同一文件既是编号副本又含 conflicted 命名时只出现一次）。
        result.all = result.merged
        return result
    }
}

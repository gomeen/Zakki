import Foundation

// 实时预览模型·分块解析（LiveBlocks）：文档 raw 按桌面 parseLines 规则切块，
// 附并块/续行/裸标记等结构编辑原语。坐标系约定见 LiveBlockModel.swift。

// MARK: 分块（对齐 live_blocks.go parseLines）

enum LiveBlocks {
    // 文档 → 块数组。规则（便签子集，与桌面一致）：
    //   - ``` 围栏：从开栏行到闭栏行（未闭合则到文档尾）为一个块；
    //   - 空白行：独立块（可编辑、可点击）；
    //   - 标题/引用/分隔线/列表项/待办：单行块；
    //   - 其余连续行：合并为一个段落块（软换行段）。
    //   - 表格行不特判（移动端无表格块），按普通文本并入段落。
    static func split(_ raw: String) -> LiveBlockList {
        let lines = raw.components(separatedBy: "\n")
        var lineStarts: [Int] = []
        lineStarts.reserveCapacity(lines.count)
        var offset = 0
        for line in lines {
            lineStarts.append(offset)
            offset += LiveSyntax.length(line) + 1
        }
        return LiveBlockList(blocks: parseLines(lines, 0, lines.count), lineStarts: lineStarts, raw: raw)
    }

    // mdLineSpecial：一行是否天然开启新块（段落不能吸收它）。
    static func mdLineSpecial(_ line: String) -> Bool {
        let trimmed = LiveSyntax.trimmed(line)
        let s = LiveSyntax.trimLeftSpace(line)
        return trimmed.isEmpty
            || MarkdownProtection.parseFenceOpeningLine(line) != nil
            || trimmed == "---" || trimmed == "***"
            || LiveSyntax.isHeading(trimmed)
            || trimmed.hasPrefix(">")
            || LiveSyntax.taskPrefixLen(s) > 0
            || LiveSyntax.listMarkerLen(s) > 0
    }

    // parseLines 把 lines[from:stop] 切成块（stop 为排他行界）。
    static func parseLines(_ lines: [String], _ from: Int, _ stop: Int) -> [LiveBlock] {
        var out: [LiveBlock] = []
        out.reserveCapacity(max(0, stop - from))
        var i = from
        while i < stop {
            let line = lines[i]
            let trimmed = LiveSyntax.trimmed(line)
            let s = LiveSyntax.trimLeftSpace(line)
            if let opening = MarkdownProtection.parseFenceOpeningLine(line) {
                var j = i + 1
                while j < stop, !MarkdownProtection.isFenceClosingLine(lines[j], opening: opening) {
                    j += 1
                }
                let closed = j < stop
                let end = closed ? j : stop - 1
                var block = LiveBlock(kind: .code, startLine: i, endLine: end, raw: joinLines(lines, i, end))
                block.fenceInfo = LiveSyntax.fenceInfo(line)
                if closed {
                    block.codeLines = Array(lines[(i + 1)..<j])
                } else if i + 1 < stop {
                    block.codeLines = Array(lines[(i + 1)..<stop])
                }
                out.append(block)
                i = end + 1
            } else if trimmed.isEmpty {
                out.append(LiveBlock(kind: .blank, startLine: i, endLine: i, raw: line))
                i += 1
            } else if trimmed == "---" || trimmed == "***" {
                out.append(LiveBlock(kind: .rule, startLine: i, endLine: i, raw: line))
                i += 1
            } else if LiveSyntax.isHeading(trimmed) {
                let content = LiveSyntax.trimmed(line)
                let u = Array(content.utf16)
                var level = 1
                while level < u.count, u[level] == 0x23 { level += 1 }
                if level > 4 { level = 4 }
                var block = LiveBlock(kind: .heading, startLine: i, endLine: i, raw: line, level: level)
                let body = String(decoding: u[level...], as: UTF16.self)
                let stripped = LiveSyntax.splitTimeSuffix(LiveSyntax.trimmed(body))
                block.timeTokenRaw = stripped.tokenRaw
                out.append(block)
                i += 1
            } else if trimmed.hasPrefix(">") {
                var block = LiveBlock(kind: .quote, startLine: i, endLine: i, raw: line)
                // 桌面口径：去 "> " 串后的 body 若带任务前缀且前缀含 [x] 即勾选。
                let body = LiveSyntax.trimmed(quoteBody(trimmed))
                if LiveSyntax.taskPrefixLen(body) > 0, body.contains("[x]") {
                    block.checked = true
                }
                let afterMarker = String(trimmed.dropFirst())
                let stripped = LiveSyntax.splitTimeSuffix(LiveSyntax.trimmed(afterMarker))
                block.timeTokenRaw = stripped.tokenRaw
                out.append(block)
                i += 1
            } else if LiveSyntax.taskPrefixLen(s) > 0 {
                let markLen = LiveSyntax.taskPrefixLen(s)
                let mark = (s as NSString).substring(to: markLen)
                var block = LiveBlock(kind: .task, startLine: i, endLine: i, raw: line, checked: mark.contains("[x]"))
                let body = (s as NSString).substring(from: markLen)
                let stripped = LiveSyntax.splitTimeSuffix(LiveSyntax.trimmed(body))
                block.timeTokenRaw = stripped.tokenRaw
                out.append(block)
                i += 1
            } else if LiveSyntax.listMarkerLen(s) > 0 {
                let markerLen = LiveSyntax.listMarkerLen(s)
                var block = LiveBlock(kind: .list, startLine: i, endLine: i, raw: line)
                block.number = LiveSyntax.orderedNumber(s)
                let body = (s as NSString).substring(from: markerLen)
                let stripped = LiveSyntax.splitTimeSuffix(LiveSyntax.trimmed(body))
                block.timeTokenRaw = stripped.tokenRaw
                out.append(block)
                i += 1
            } else {
                // 段落：吸收连续普通行。
                var j = i
                while j + 1 < stop, !mdLineSpecial(lines[j + 1]) {
                    j += 1
                }
                var block = LiveBlock(kind: .para, startLine: i, endLine: j, raw: joinLines(lines, i, j))
                // 段落令牌按源码行识别（首个含令牌的行），防止软换行拼出不存在的令牌。
                if block.timeTokenRaw == nil {
                    for k in i...j {
                        let stripped = LiveSyntax.splitTimeSuffix(LiveSyntax.trimmed(lines[k]))
                        if let token = stripped.tokenRaw {
                            block.timeTokenRaw = token
                            break
                        }
                    }
                }
                out.append(block)
                i = j + 1
            }
        }
        return out
    }

    private static func joinLines(_ lines: [String], _ from: Int, _ to: Int) -> String {
        guard from <= to, from < lines.count else { return "" }
        return lines[from...min(to, lines.count - 1)].joined(separator: "\n")
    }

    // isLiveSeparator：区分源码分隔符与真正的空段落（对齐桌面同名函数）。
    static func isLiveSeparator(_ blocks: [LiveBlock], _ i: Int) -> Bool {
        guard i > 0, i < blocks.count - 1 else { return false }
        let block = blocks[i]
        guard block.kind == .blank, block.raw == "" else { return false }
        if blocks[i - 1].kind != .blank { return true }
        if blocks[i + 1].kind == .blank { return false }
        for b in blocks[..<i] where b.kind != .blank { return false }
        return true
    }

    // bareListMarker：整行恰好是列表/待办标记时的标记文本。
    static func bareListMarker(_ line: String) -> String? {
        let s = LiveSyntax.trimLeftSpace(line)
        for m in ["- [ ] ", "- [x] ", "* [ ] ", "* [x] ", "- ", "* "] where s == m {
            return m
        }
        let n = LiveSyntax.listMarkerLen(s)
        if n > 0, n == LiveSyntax.length(s) { return s }
        return nil
    }

    // listContinuePrefix：列表/待办块的续行标记（回车续项用，任务前缀 [x] 归一为 [ ]）。
    static func listContinuePrefix(_ block: LiveBlock) -> String {
        let t = LiveSyntax.trimmed(block.raw)
        switch block.kind {
        case .task:
            let n = LiveSyntax.taskPrefixLen(t)
            if n > 0 {
                let prefix = (t as NSString).substring(to: n)
                // 桌面 strings.Replace(prefix, "[x]", "[ ]", 1)：前缀只含一处 [x]。
                return prefix.replacingOccurrences(of: "[x]", with: "[ ]")
            }
        case .list:
            let n = LiveSyntax.listMarkerLen(t)
            if n > 0 {
                let prefix = (t as NSString).substring(to: n)
                if prefix.hasSuffix(". ") {
                    let value = LiveSyntax.orderedNumber(t)
                    if value > 0 { return "\(value + 1). " }
                }
                return prefix
            }
        default:
            break
        }
        return ""
    }

    // joinBlockRaw：块首退格并块后的合并 raw（对齐桌面 joinBlockRaw：
    // 段落直接相连、列表并项去标记，其余删除换行符式直连）。
    static func joinBlockRaw(prev: LiveBlock, cur: LiveBlock) -> String {
        switch (prev.kind, cur.kind) {
        case (.para, .para):
            return prev.raw + cur.raw
        case (.list, .list):
            let t = LiveSyntax.trimmed(cur.raw)
            let n = LiveSyntax.listMarkerLen(t)
            let body = n > 0 ? LiveSyntax.trimmed((t as NSString).substring(from: n)) : t
            return prev.raw + "\n" + body
        default:
            return prev.raw + cur.raw
        }
    }

    // joinCaret：并块后光标（块内 UTF-16 偏移，对齐桌面 joinCaret）。
    static func joinCaret(prev: LiveBlock, cur: LiveBlock) -> Int {
        switch (prev.kind, cur.kind) {
        case (.para, .para):
            return LiveSyntax.length(prev.raw)
        case (.list, .list):
            return LiveSyntax.length(prev.raw) + 1
        default:
            return LiveSyntax.length(prev.raw)
        }
    }
}

// TrimLeft(s, characters)（仅分块解析用）。
private func trimLeft(_ s: String, characters: String) -> String {
    var result = s
    while let first = result.utf16.first, characters.utf16.contains(first) {
        result = String(result.dropFirst())
    }
    return result
}

// 引用 body：去行首的 ">" 与空格串（桌面 strings.TrimLeft(t, "> ")）。
private func quoteBody(_ trimmedLine: String) -> String {
    trimLeft(trimmedLine, characters: "> ")
}

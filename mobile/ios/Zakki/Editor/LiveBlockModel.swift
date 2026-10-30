import Foundation
import Zakkicore

// MARK: - 实时预览块模型（M2-6，同构桌面 live_blocks.go / live_projection.go）

// 桌面实时预览的 Typora 式投影在移动端的移植：raw 按桌面 parseLines 规则切块，
// 每块再投影出「可见文字」（块级/行内标记剥离），并给出可见↔raw 双向映射与
// 编辑写回（replacing/fragment）。全文件偏移一律为 UTF-16 code unit（与
// NSString/NSRange、FindTimeToken 绑定、现有 EditorDocument 管线同一坐标系）；
// 桌面源码是 rune 坐标，标记字符均为 ASCII，语义逐条对齐，坐标宽度按 UTF-16 解释。
// 本文件纯 Foundation + core 绑定，不依赖 UIKit。

// MARK: 块类型与块

enum LiveBlockKind: Equatable {
    case para
    case heading
    case quote
    case code
    case rule
    case list
    case task
    case blank
}

// 行内标记 span：raw 内 UTF-16 区间，含标记符本身（对齐桌面 mSpan）。
enum LiveSpanKind: Equatable {
    case bold
    case italic
    case code
    case link
}

struct LiveSpan: Equatable {
    var start: Int
    var end: Int
    var kind: LiveSpanKind
}

// 从文档切出的一个可编辑块（对齐桌面 liveBlock）。
struct LiveBlock: Equatable {
    var kind: LiveBlockKind
    var startLine: Int // 文档起始行（含）
    var endLine: Int // 文档结束行（含）
    var raw: String // 原始 markdown（行间以 \n 相连）
    var level: Int = 0 // 标题级别（1-4，桌面 parseLines 同款钳制）
    var number: Int = 0 // 有序列表序号（0 = 无序）
    var checked: Bool = false // 待办勾选
    var codeLines: [String] = [] // 代码块内容行（不含围栏行）
    var fenceInfo: String = "" // 代码块开栏 info 串
    var timeTokenRaw: String? // 正文首个时间令牌原文（识别只走 core 绑定）

    // 视觉身份：集合视图 diff 只重载真正变化的块（行号平移不算变化）。
    var visualIdentity: String {
        "\(kind)|\(level)|\(number)|\(checked)|\(raw)|\(fenceInfo)"
    }
}

// MARK: 语法辅助（UTF-16 移植 internal/markdown/helpers.go）

enum LiveSyntax {
    static let objectRune: unichar = 0xFFFC // 可见文字中的时间组件占位符

    static func length(_ s: String) -> Int { (s as NSString).length }

    // Go unicode.IsSpace 的常用子集（含 U+3000 全角空格）。
    static func isSpaceLike(_ c: unichar) -> Bool {
        switch c {
        case 0x09...0x0D, 0x20, 0x85, 0xA0, 0x1680,
             0x2000...0x200A, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000:
            return true
        default:
            return false
        }
    }

    // strings.TrimSpace 近似移植：返回去首尾空白后的内容。
    static func trimmed(_ s: String) -> String {
        let u = Array(s.utf16)
        var lo = 0
        var hi = u.count
        while lo < hi, isSpaceLike(u[lo]) { lo += 1 }
        while hi > lo, isSpaceLike(u[hi - 1]) { hi -= 1 }
        guard lo < hi else { return "" }
        return String(decoding: u[lo..<hi], as: UTF16.self)
    }

    // TrimLeft(line, " \t") 之后的子串。
    static func trimLeftSpace(_ s: String) -> String {
        let u = Array(s.utf16)
        var i = 0
        while i < u.count, u[i] == 0x20 || u[i] == 0x09 { i += 1 }
        return String(decoding: u[i...], as: UTF16.self)
    }

    // isHeading：#{1,6} + 空格（对齐 helpers.go）。
    static func isHeading(_ trimmed: String) -> Bool {
        let u = Array(trimmed.utf16)
        guard let first = u.first, first == 0x23 else { return false }
        var n = 0
        while n < u.count, u[n] == 0x23 { n += 1 }
        return n <= 6 && n < u.count && u[n] == 0x20
    }

    // taskPrefixLen：四种任务前缀（含尾空格）。
    static func taskPrefixLen(_ trimmed: String) -> Int {
        for mark in ["- [ ] ", "* [ ] ", "- [x] ", "* [x] "] where trimmed.hasPrefix(mark) {
            return length(mark)
        }
        return 0
    }

    // listMarkerLen："- "/"* " 或 "数字. "（数字段全为 0-9）。
    static func listMarkerLen(_ trimmed: String) -> Int {
        let u = Array(trimmed.utf16)
        if u.count >= 2, (u[0] == 0x2D || u[0] == 0x2A), u[1] == 0x20 { return 2 }
        var i = 0
        while i < u.count {
            let c = u[i]
            if c == 0x2E, i + 1 < u.count, u[i + 1] == 0x20, i > 0 {
                for k in 0..<i where u[k] < 0x30 || u[k] > 0x39 { return 0 }
                return i + 2
            }
            if c < 0x30 || c > 0x39 { return 0 }
            i += 1
        }
        return 0
    }

    // orderedNumber："1. " → 1；非有序返回 0。
    static func orderedNumber(_ trimmed: String) -> Int {
        let u = Array(trimmed.utf16)
        let n = listMarkerLen(trimmed)
        guard n >= 3, n <= u.count, u[n - 2] == 0x2E else { return 0 }
        var value = 0
        for k in 0..<(n - 2) {
            guard u[k] >= 0x30, u[k] <= 0x39 else { return 0 }
            value = value * 10 + Int(u[k] - 0x30)
        }
        return value
    }

    // fenceInfo：开栏行 info 串（标记后的剩余部分，去首尾空白；对齐 helpers.go）。
    static func fenceInfo(_ line: String) -> String {
        var content = line
        if content.hasSuffix("\r") { content.removeLast() }
        let u = Array(content.utf16)
        var indent = 0
        while indent < u.count, u[indent] == 0x20 {
            indent += 1
            if indent > 3 { return "" }
        }
        guard u.count - indent >= 3 else { return "" }
        let first = u[indent]
        guard first == 0x60 || first == 0x7E else { return "" }
        var size = 0
        while indent + size < u.count, u[indent + size] == first { size += 1 }
        guard size >= 3 else { return "" }
        return trimmed(String(decoding: u[(indent + size)...], as: UTF16.self))
    }

    // splitKeepTimeSuffix（对齐 live_input.go）：拆分点之后除空白外不带任何可见文本时，
    // 把行尾时间令牌留在左侧；右侧还有正文时不动令牌。
    static func splitKeepTimeSuffix(_ left: String, _ right: String) -> (String, String) {
        guard let hit = EditorDocument.findTimeToken(in: right, protectedJSON: "") else {
            return (left, right)
        }
        let ns = right as NSString
        let rest = ns.substring(to: hit.range.location)
        if !trimmedRight(rest, characters: " \t").isEmpty {
            return (left, right)
        }
        return (trimRightSpaceTab(left) + " " + ns.substring(from: hit.range.location), "")
    }

    private static func trimmedRight(_ s: String, characters: String) -> String {
        var result = s
        while let last = result.utf16.last, characters.utf16.contains(last) {
            result = String(result.dropLast())
        }
        return result
    }

    static func trimRightSpaceTab(_ s: String) -> String {
        trimmedRight(s, characters: " \t")
    }

    // isFenceDraftLine（对齐桌面同名函数）：单行、缩进 ≤3 空格、整行只有围栏
    // 标记（或标记+info 串）。处于此态的块以字面文本投影（桌面 fenceDraft：
    // 键入 ``` 后回车前保持普通文本层级，标记不隐藏，避免可编辑区凭空消失）。
    static func isFenceDraftLine(_ raw: String) -> Bool {
        if raw.isEmpty || raw.contains("\n") || raw.contains("\r") { return false }
        let u = Array(raw.utf16)
        var indent = 0
        while indent < u.count, u[indent] == 0x20 {
            indent += 1
            if indent > 3 { return false }
        }
        if indent == u.count { return false }
        let marker = u[indent]
        guard marker == 0x60 || marker == 0x7E else { return false }
        var run = 1
        while indent + run < u.count, u[indent + run] == marker { run += 1 }
        if run < 3 {
            return indent + run == u.count
        }
        return MarkdownProtection.parseFenceOpeningLine(raw) != nil
    }

    // fenceClosingMarker：与开栏标记同字符等长的闭栏串。
    static func fenceClosingMarker(character: unichar, size: Int) -> String {
        String(repeating: String(UnicodeScalar(character)!), count: size)
    }

    // stripTimeSuffix（对齐 live_blocks.go）：识别正文中的首个时间令牌，返回剥离
    // 令牌后的文本与令牌原文。识别只走 core 绑定（红线），无保护区参数
    //（桌面 stripTimeSuffix 同样裸调 FindTimeToken）。
    static func splitTimeSuffix(_ text: String) -> (text: String, tokenRaw: String?) {
        guard let hit = EditorDocument.findTimeToken(in: text, protectedJSON: "") else {
            return (text, nil)
        }
        let ns = text as NSString
        let joined = ns.substring(to: hit.range.location)
            + ns.substring(from: hit.range.location + hit.range.length)
        return (trimmed(joined), hit.raw)
    }
}

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

// MARK: 结构化编辑（纯函数移植 live_input.go 的跨块语义）

// 块级结构编辑的结果：新文档 raw 与新光标（全文 UTF-16 偏移）。nil 表示不动。
struct LiveDocEdit {
    var doc: String
    var caret: Int
}

enum LiveDocEditing {
    // 块尾回车：拆块/续行/空列表退出/围栏补全等。fenceDraft 由容器传入
    //（键入 ``` 后回车前该块以字面文本投影，回车时自动补闭栏）。
    // native 为 true 表示回车交给原生路径（代码块内单换行）。
    static func enter(_ doc: String, list: LiveBlockList, index: Int, visibleCaret: Int,
                      projection: LiveProjection) -> Result {
        let blocks = list.blocks
        guard blocks.indices.contains(index) else { return .unchanged }
        let block = blocks[index]
        if block.kind == .code {
            // 退化单行围栏（刚键入的 ```）+ 光标在末尾 → 自动补闭栏（桌面 completeFenceDraft）。
            // 无论容器用哪种投影（字面草稿投影或零宽自然投影），末尾判据都是
            // visibleCaret == units.count。
            if LiveSyntax.isFenceDraftLine(block.raw),
               visibleCaret == projection.units.count,
               let opening = MarkdownProtection.parseFenceOpeningLine(block.raw) {
                let closing = LiveSyntax.fenceClosingMarker(character: opening.character, size: opening.size)
                let newRaw = block.raw + "\n\n" + closing
                let caret = list.startOffset(of: index) + LiveSyntax.length(block.raw) + 1
                let newDoc = LiveDocEditing.spliceLines(doc, block.startLine, block.endLine, newRaw).joined(separator: "\n")
                return .edited(LiveDocEdit(doc: newDoc, caret: caret))
            }
            return .native // 代码块内单换行走原生路径（replace 写回）
        }
        if !projection.units.isEmpty,
           block.kind == .para || block.kind == .heading || block.kind == .list || block.kind == .task {
            return splitProjected(doc, list: list, index: index, visibleCaret: visibleCaret, projection: projection)
        }
        return enterRaw(doc, list: list, index: index, sourceCaret: projection.sourceOffset(visibleCaret))
    }

    enum Result {
        case edited(LiveDocEdit)
        case native
        case unchanged
    }

    // 焦点块可见文字非空时的回车：fragment 保包裹拆分（对齐桌面 applyProjectedHint）。
    private static func splitProjected(_ doc: String, list: LiveBlockList, index: Int,
                                       visibleCaret: Int, projection: LiveProjection) -> Result {
        let block = list.blocks[index]
        let unitsEnd = projection.units.count
        let caret = min(max(visibleCaret, 0), unitsEnd)
        let left = projection.fragment(0, caret)
        let right = projection.fragment(caret, unitsEnd)
        let rawUnits = Array(block.raw.utf16)
        let prefix = String(decoding: rawUnits[0..<min(projection.start, rawUnits.count)], as: UTF16.self)
        var newRaw: String
        var caretInRaw: Int
        if block.kind == .list || block.kind == .task {
            if caret == 0, caret == unitsEnd {
                // 空项上方再开一项：复用本块的完整前缀。
                newRaw = prefix + left + "\n" + prefix + right
                caretInRaw = LiveSyntax.length(prefix + left)
            } else {
                let continuation = LiveBlocks.listContinuePrefix(block)
                newRaw = prefix + left + "\n" + continuation + right
                caretInRaw = LiveSyntax.length(prefix + left) + 1 + LiveSyntax.length(continuation)
            }
        } else {
            newRaw = prefix + left + "\n\n" + right
            caretInRaw = LiveSyntax.length(prefix + left) + 2
        }
        let newDoc = LiveDocEditing.spliceLines(doc, block.startLine, block.endLine, newRaw).joined(separator: "\n")
        return .edited(LiveDocEdit(doc: newDoc, caret: list.startOffset(of: index) + caretInRaw))
    }

    // 焦点块可见文字为空（或引用/分隔线）时的回车：raw 空间操作（对齐桌面 afterEnter）。
    private static func enterRaw(_ doc: String, list: LiveBlockList, index: Int, sourceCaret: Int) -> Result {
        let blocks = list.blocks
        let block = blocks[index]
        let raw = block.raw
        let n = LiveSyntax.length(raw)
        let s = min(max(sourceCaret, 0), n)
        let blockStart = list.startOffset(of: index)
        let trimmed = LiveSyntax.trimmed(raw)
        switch block.kind {
        case .list, .task:
            let line = LiveSyntax.trimLeftSpace(raw)
            if let bare = LiveBlocks.bareListMarker(line), s == 0 || s == n {
                // 空列表项退出：从源码行剥掉标记，光标落行首。
                var lines = doc.components(separatedBy: "\n")
                guard block.startLine < lines.count else { return .unchanged }
                lines[block.startLine] = dropRawPrefix(lines[block.startLine], bare)
                return .edited(LiveDocEdit(doc: lines.joined(separator: "\n"), caret: blockStart))
            }
            if s == 0 {
                let marker = LiveBlocks.listContinuePrefix(block)
                let newDoc = LiveDocEditing.spliceLines(doc, block.startLine, block.endLine, marker + "\n" + raw).joined(separator: "\n")
                return .edited(LiveDocEdit(doc: newDoc, caret: blockStart + LiveSyntax.length(marker)))
            }
            let (left, right) = LiveSyntax.splitKeepTimeSuffix(
                slice(raw, 0, s), slice(raw, s, n))
            let prefix = LiveBlocks.listContinuePrefix(block)
            let newRaw = left + "\n" + prefix + right
            let newDoc = LiveDocEditing.spliceLines(doc, block.startLine, block.endLine, newRaw).joined(separator: "\n")
            return .edited(LiveDocEdit(doc: newDoc, caret: blockStart + LiveSyntax.length(left) + 1 + LiveSyntax.length(prefix)))
        case .blank:
            if s == 0, trimmed.isEmpty {
                // 空段落内回车：在其后再插一个空段落，光标留在本块。
                var lines = doc.components(separatedBy: "\n")
                let at = min(block.endLine + 1, lines.count)
                lines.insert("", at: at)
                return .edited(LiveDocEdit(doc: lines.joined(separator: "\n"), caret: blockStart))
            }
        case .para, .heading:
            if s == 0 {
                if block.kind == .para, trimmed.isEmpty {
                    var lines = doc.components(separatedBy: "\n")
                    let at = min(block.endLine + 1, lines.count)
                    lines.insert("", at: at)
                    return .edited(LiveDocEdit(doc: lines.joined(separator: "\n"), caret: blockStart))
                }
                // 可见开头回车：在上方开一个空段（前缀仍在后面的块上）。
                let newDoc = LiveDocEditing.spliceLines(doc, block.startLine, block.endLine, "\n\n" + raw).joined(separator: "\n")
                return .edited(LiveDocEdit(doc: newDoc, caret: blockStart + 2))
            }
        case .code:
            return .native
        case .quote, .rule:
            break
        }
        let (left, right) = LiveSyntax.splitKeepTimeSuffix(slice(raw, 0, s), slice(raw, s, n))
        let newRaw = left + "\n\n" + right
        let newDoc = LiveDocEditing.spliceLines(doc, block.startLine, block.endLine, newRaw).joined(separator: "\n")
        return .edited(LiveDocEdit(doc: newDoc, caret: blockStart + LiveSyntax.length(left) + 2))
    }

    // 块首退格：并块/空行段收敛/裸标记退出（对齐桌面 backspaceAtBlockStart）。
    static func backspaceAtStart(_ doc: String, list: LiveBlockList, index: Int) -> LiveDocEdit? {
        let blocks = list.blocks
        guard blocks.indices.contains(index) else { return nil }
        let cur = blocks[index]
        if cur.kind == .blank {
            let (lo, hi) = blankRun(blocks, index)
            let run = hi - lo + 1
            if hi + 1 < blocks.count, run >= 3 {
                // 两段正文之间多出来的空行：一次退格去掉整段分段。
                return deleteBlankBreak(doc, line: blocks[lo].startLine)
            }
            if hi + 1 >= blocks.count, run > 2 {
                // 文末连续空段一次只删一行。
                return deleteDocLine(doc, line: cur.startLine)
            }
        }
        if let marker = LiveBlocks.bareListMarker(LiveSyntax.trimLeftSpace(cur.raw)) {
            var lines = doc.components(separatedBy: "\n")
            guard cur.startLine < lines.count else { return nil }
            lines[cur.startLine] = dropRawPrefix(lines[cur.startLine], marker)
            return LiveDocEdit(doc: lines.joined(separator: "\n"), caret: list.startOffset(of: index))
        }
        var prevIdx = index - 1
        while prevIdx >= 0, blocks[prevIdx].kind == .blank, LiveBlocks.isLiveSeparator(blocks, prevIdx) {
            prevIdx -= 1
        }
        guard prevIdx >= 0 else { return nil }
        let prev = blocks[prevIdx]
        if prev.kind == .code || cur.kind == .code { return nil }
        let joined = LiveBlocks.joinBlockRaw(prev: prev, cur: cur)
        var lines = doc.components(separatedBy: "\n")
        guard prev.startLine < lines.count, cur.endLine < lines.count, prev.startLine <= cur.endLine else { return nil }
        lines.replaceSubrange(prev.startLine...cur.endLine, with: [joined])
        let caret = list.startOffset(of: prevIdx) + LiveBlocks.joinCaret(prev: prev, cur: cur)
        return LiveDocEdit(doc: lines.joined(separator: "\n"), caret: caret)
    }

    // 方向键越界跨块：跳过分隔空行，返回目标块与光标（对齐桌面 crossBlock）。
    static func crossBlock(_ blocks: [LiveBlock], from index: Int, direction: Int) -> (index: Int, atEnd: Bool)? {
        var i = index + direction
        while i >= 0, i < blocks.count, blocks[i].kind == .blank, LiveBlocks.isLiveSeparator(blocks, i) {
            i += direction
        }
        guard i >= 0, i < blocks.count else { return nil }
        return (i, direction < 0)
    }

    // MARK: 内部工具

    private static func blankRun(_ blocks: [LiveBlock], _ i: Int) -> (lo: Int, hi: Int) {
        var lo = i
        var hi = i
        while lo > 0, blocks[lo - 1].kind == .blank { lo -= 1 }
        while hi + 1 < blocks.count, blocks[hi + 1].kind == .blank { hi += 1 }
        return (lo, hi)
    }

    // 删掉从 line 起的两行空行，光标落在上一段末尾（对齐桌面 deleteBlankBreak）。
    private static func deleteBlankBreak(_ doc: String, line: Int) -> LiveDocEdit? {
        var lines = doc.components(separatedBy: "\n")
        guard line >= 0, line + 1 < lines.count else { return nil }
        lines.removeSubrange(line...(line + 1))
        var caret = 0
        if line > 0 {
            for i in 0..<(line - 1) {
                caret += LiveSyntax.length(lines[i]) + 1
            }
            caret += LiveSyntax.length(lines[line - 1])
        }
        return LiveDocEdit(doc: lines.joined(separator: "\n"), caret: caret)
    }

    // 删除一行，光标落在原行首位置（对齐桌面 deleteDocLine）。
    private static func deleteDocLine(_ doc: String, line: Int) -> LiveDocEdit? {
        var lines = doc.components(separatedBy: "\n")
        guard line >= 0, line < lines.count else { return nil }
        lines.remove(at: line)
        let kept = lines
        var caret = 0
        if line >= kept.count {
            caret = LiveSyntax.length(kept.joined(separator: "\n"))
        } else {
            for i in 0..<line {
                caret += LiveSyntax.length(kept[i]) + 1
            }
        }
        return LiveDocEdit(doc: kept.joined(separator: "\n"), caret: caret)
    }

    // 行切片替换（逐字对齐桌面 spliceLines：start > end 时为在 start 处纯插入）。
    static func spliceLines(_ doc: String, _ startLine: Int, _ endLine: Int, _ insert: String) -> [String] {
        let lines = doc.components(separatedBy: "\n")
        let start = max(0, startLine)
        var end = endLine >= lines.count ? lines.count - 1 : endLine
        if start > end { end = start - 1 }
        let ins = insert.components(separatedBy: "\n")
        var out: [String] = []
        out.reserveCapacity(lines.count + ins.count)
        out.append(contentsOf: lines[0..<min(start, lines.count)])
        out.append(contentsOf: ins)
        if end + 1 < lines.count {
            out.append(contentsOf: lines[(end + 1)...])
        }
        return out
    }

    private static func dropRawPrefix(_ line: String, _ prefix: String) -> String {
        line.hasPrefix(prefix) ? String(line.dropFirst(prefix.count)) : line
    }

    private static func slice(_ s: String, _ start: Int, _ end: Int) -> String {
        let u = Array(s.utf16)
        let lo = min(max(start, 0), u.count)
        let hi = min(max(end, lo), u.count)
        return String(decoding: u[lo..<hi], as: UTF16.self)
    }
}

// MARK: 块列表与坐标

// 分块结果：blocks + 每行在全文中的 UTF-16 起点（行间以 \n 相连，末尾无换行）。
struct LiveBlockList {
    let blocks: [LiveBlock]
    let lineStarts: [Int]
    let raw: String

    var count: Int { blocks.count }

    // 块 i 的全文 UTF-16 起点。
    func startOffset(of i: Int) -> Int {
        guard blocks.indices.contains(i) else { return (raw as NSString).length }
        return lineStarts[blocks[i].startLine]
    }

    // 全文偏移 → 所在块索引与块起点（对齐桌面 blockOfRuneOffset：
    // 文档 = 各块 raw 依次相连，块间由换行分隔，末尾无换行）。
    func blockOf(_ offset: Int) -> (index: Int, blockStart: Int) {
        for (i, block) in blocks.enumerated() {
            let start = startOffset(of: i)
            if offset < start + LiveSyntax.length(block.raw) + 1 {
                return (i, start)
            }
        }
        if blocks.isEmpty { return (0, 0) }
        let last = blocks.count - 1
        return (last, startOffset(of: last))
    }
}

// MARK: 行内标记（UTF-16 移植 internal/markdown/inline_block.go inlineSpans）

// 找出一段文字里的 **粗体**、*斜体*、`代码` 和 [文本](链接)。
// 匹配不成功时标记保持字面文本——Typora 式「输入中即时成型」来源于每次
// 重切后的重新解析：补全闭合标记后下一投影即渲染态。
enum LiveInline {
    static func spans(in text: String) -> [LiveSpan] {
        let rs = Array(text.utf16)
        let n = rs.count
        // next[target][i]：i 起（含）最近一次出现 target 的下标，无则 n。
        func next(_ target: unichar) -> [Int] {
            var out = [Int](repeating: n, count: n + 1)
            var at = n
            var i = n
            while i >= 0 {
                if i < n, rs[i] == target { at = i }
                out[i] = at
                i -= 1
            }
            return out
        }
        let nextTick = next(0x60)
        let nextStar = next(0x2A)
        let nextUnder = next(0x5F)
        let nextRight = next(0x5D)
        let nextParen = next(0x29)
        var nextDoubleStar = [Int](repeating: n, count: n + 1)
        var doubleAt = n
        var scan = n
        while scan >= 0 {
            if scan + 1 < n, rs[scan] == 0x2A, rs[scan + 1] == 0x2A { doubleAt = scan }
            nextDoubleStar[scan] = doubleAt
            scan -= 1
        }

        var out: [LiveSpan] = []
        var i = 0
        while i < n {
            switch rs[i] {
            case 0x60: // `
                let j = nextTick[i + 1]
                if j < n {
                    out.append(LiveSpan(start: i, end: j + 1, kind: .code))
                    i = j + 1
                    continue
                }
            case 0x2A: // *
                if i + 1 < n, rs[i + 1] == 0x2A {
                    let j = nextDoubleStar[min(i + 2, n)]
                    if j < n {
                        out.append(LiveSpan(start: i, end: j + 2, kind: .bold))
                        i = j + 2
                        continue
                    }
                } else {
                    let j = nextStar[i + 1]
                    if j < n, j > i + 1 {
                        out.append(LiveSpan(start: i, end: j + 1, kind: .italic))
                        i = j + 1
                        continue
                    }
                }
            case 0x5F: // _
                let j = nextUnder[i + 1]
                if j < n, j > i + 1 {
                    out.append(LiveSpan(start: i, end: j + 1, kind: .italic))
                    i = j + 1
                    continue
                }
            case 0x5B: // [
                let cb = nextRight[i + 1]
                if cb < n, cb + 1 < n, rs[cb] == 0x5D, rs[cb + 1] == 0x28 {
                    let pe = nextParen[cb + 2]
                    if pe < n {
                        out.append(LiveSpan(start: i, end: pe + 1, kind: .link))
                        i = pe + 1
                        continue
                    }
                }
            default:
                break
            }
            i += 1
        }
        return out
    }
}

// MARK: 投影（UTF-16 移植 live_projection.go）

// 一个可见字符单元：可见下标 ↔ raw UTF-16 区间。
struct LiveProjectedRune: Equatable {
    var start: Int
    var end: Int
}

// 一个行内样式：可见 UTF-16 区间 + raw 内开标记/内容/闭标记/尾偏移。
struct LiveStyleRange: Equatable {
    var start: Int // 可见区间（含）
    var end: Int // 可见区间（排他）
    var open: Int // raw 内开标记起点
    var content: Int // raw 内内容起点
    var close: Int // raw 内闭标记起点
    var tail: Int // raw 内尾终点（闭标记之后）
    var kind: LiveSpanKind
}

// 块的可见文字投影：raw 中的标记被剥离，时间令牌收成单个 U+FFFC 占位；
// units/styles 承担可见↔raw 的双向映射与编辑写回。
struct LiveProjection: Equatable {
    var raw: String
    var text: String
    var start: Int // 可编辑源区起点（前缀标记/围栏行在区外）
    var end: Int // 可编辑源区终点
    var units: [LiveProjectedRune]
    var styles: [LiveStyleRange]
    var objects: [Int] // 时间组件在可见文字中的单字符位置
    var isInline: Bool

    static func == (lhs: LiveProjection, rhs: LiveProjection) -> Bool {
        lhs.raw == rhs.raw && lhs.text == rhs.text && lhs.start == rhs.start && lhs.end == rhs.end
    }

    // 源偏移 → 可见偏移（区外钳到两端）。
    func sourceOffset(_ visible: Int) -> Int {
        let v = min(max(visible, 0), units.count)
        if units.isEmpty { return start }
        if v == units.count { return units[v - 1].end }
        return units[v].start
    }

    // 可见偏移 → 源偏移（对齐桌面 visibleOffset：首个 unit.start >= source 的下标）。
    func visibleOffset(_ source: Int) -> Int {
        var n = 0
        for unit in units where unit.start < source {
            n += 1
        }
        return n
    }

    // 源选区 → 可见选区（选区压到组件上时扩到整组件，对齐桌面 visibleSelection）。
    func visibleSelection(_ start: Int, _ end: Int) -> (start: Int, end: Int) {
        var s = visibleOffset(start)
        var e = visibleOffset(end)
        if start < end {
            for at in objects {
                let u = units[at]
                if start < u.end, end > u.start {
                    s = min(s, at)
                    e = max(e, at + 1)
                }
            }
        }
        return (s, e)
    }

    // 用新的可见文字（时间令牌已展开为其 raw 文本）替换可编辑区内容，保留幸存
    // 字符的包裹；整段样式内容被删空时移除包裹。返回新块 raw 与新光标
    //（块内 UTF-16 偏移）。selectedStart/End 为编辑前的可见选区（替换选区时
    // 保留包裹的消歧提示，对齐桌面 replaceSelection）。
    func replacing(_ next: String, caret: Int, selectedStart: Int = -1, selectedEnd: Int = -1) -> (raw: String, caret: Int) {
        let old = Array(text.utf16)
        let neu = Array(next.utf16)
        var caret = min(max(caret, 0), neu.count)
        var prefix = 0
        while prefix < old.count, prefix < neu.count, old[prefix] == neu[prefix] {
            prefix += 1
        }
        // 重复字符存在多个等价 diff；结果光标标识真实插入/删除边界。
        let delta = neu.count - old.count
        prefix = min(prefix, max(0, caret - max(0, delta)))
        var suffix = 0
        while suffix < old.count - prefix, suffix < neu.count - caret,
              old[old.count - 1 - suffix] == neu[neu.count - 1 - suffix] {
            suffix += 1
        }
        if selectedStart >= 0, selectedEnd >= selectedStart, selectedEnd <= old.count,
           selectedStart <= caret, caret <= neu.count,
           Self.matches(old, 0, neu, 0, selectedStart),
           Self.matches(old, selectedEnd, neu, caret, old.count - selectedEnd) {
            prefix = selectedStart
            suffix = old.count - selectedEnd
        }
        // UTF-16 修正：diff 边界不得切进代理对（桌面 rune diff 无此问题）。
        prefix = Self.alignPrefix(old, prefix)
        suffix = Self.alignSuffix(old, suffix, limit: old.count - prefix)
        let end = old.count - suffix
        var inserted = neu.count - suffix > prefix
            ? String(decoding: neu[(neu.startIndex + prefix)..<(neu.startIndex + neu.count - suffix)], as: UTF16.self)
            : ""

        // 时间令牌必须以空白收尾；组件后相邻输入补一个空格（对齐桌面 replace）。
        if !inserted.isEmpty, let first = inserted.utf16.first, !" \t\r\n".utf16.contains(first) {
            for object in objects where prefix == object + 1 {
                inserted = " " + inserted
                caret += 1
                break
            }
        }
        let at = sourceOffset(prefix)
        let rawUnits = Array(raw.utf16)
        var remove = [Bool](repeating: false, count: rawUnits.count)
        func mark(_ start: Int, _ stop: Int) {
            guard start >= 0, stop <= rawUnits.count else { return }
            for i in start..<max(start, stop) { remove[i] = true }
        }
        for i in prefix..<end {
            let u = units[i]
            mark(u.start, u.end)
        }
        for style in styles where prefix <= style.start && end >= style.end
            && (inserted.isEmpty || at < style.content || at > style.close) {
            mark(style.open, style.content)
            mark(style.close, style.tail)
        }
        var out: [unichar] = []
        out.reserveCapacity(rawUnits.count + inserted.utf16.count)
        let insertedUnits = Array(inserted.utf16)
        var i = 0
        while i <= rawUnits.count {
            if i == at {
                out.append(contentsOf: insertedUnits)
            }
            if i < rawUnits.count, !remove[i] {
                out.append(rawUnits[i])
            }
            i += 1
        }
        let newRaw = String(decoding: out, as: UTF16.self)
        var projected = LiveProjection.region(
            newRaw, lo: start,
            hi: end + (out.count - rawUnits.count),
            inline: isInline
        )
        if !objects.isEmpty {
            projected = projected.withTimeObject()
        }
        return (newRaw, projected.sourceOffset(caret))
    }

    // 序列化可见区间 [start, end) 为带完整包裹的源码片段（跨块拆分时左侧闭样式、
    // 右侧重开样式，链接目标保留；对齐桌面 fragment）。
    func fragment(_ start: Int, _ end: Int) -> String {
        let s = min(max(start, 0), units.count)
        let e = min(max(end, s), units.count)
        let rs = Array(raw.utf16)
        var out = ""
        func appendUnits(_ lo: Int, _ hi: Int) {
            guard lo < hi else { return }
            for u in units[lo..<hi] {
                out += String(decoding: rs[u.start..<u.end], as: UTF16.self)
            }
        }
        var pos = s
        for style in styles {
            let lo = max(s, style.start)
            let hi = min(e, style.end)
            guard lo < hi else { continue }
            appendUnits(pos, lo)
            out += String(decoding: rs[style.open..<style.content], as: UTF16.self)
            appendUnits(lo, hi)
            out += String(decoding: rs[style.close..<style.tail], as: UTF16.self)
            pos = hi
        }
        appendUnits(pos, e)
        return out
    }

    // 时间令牌在可见文字中收成单个 U+FFFC（对齐桌面 withTimeObject）。
    func withTimeObject() -> LiveProjection {
        guard let token = EditorDocument.findTimeToken(in: raw, protectedJSON: "") else { return self }
        let start = token.range.location
        let end = token.range.location + token.range.length
        guard start >= self.start, end <= self.end else { return self }
        let lo = visibleOffset(start)
        let hi = visibleOffset(end)
        guard hi > lo else { return self }
        var p = self
        let rs = Array(text.utf16)
        var newText = Array(rs[0..<lo])
        newText.append(LiveSyntax.objectRune)
        newText.append(contentsOf: rs[hi...])
        p.text = String(decoding: newText, as: UTF16.self)
        var newUnits = Array(units[0..<lo])
        newUnits.append(LiveProjectedRune(start: start, end: end))
        newUnits.append(contentsOf: units[hi...])
        p.units = newUnits
        p.objects = [lo]
        func mapOffset(_ at: Int) -> Int {
            if at <= lo { return at }
            if at < hi { return lo + 1 }
            return at - (hi - lo) + 1
        }
        for k in p.styles.indices {
            p.styles[k].start = mapOffset(p.styles[k].start)
            p.styles[k].end = mapOffset(p.styles[k].end)
        }
        return p
    }

    // 缓冲装载用：把可见文字中的时间占位按其源码区间展开为令牌原文
    //（U+FFFC 不外流；展开串经 EditorDocument 重扫会重新收簇）。
    func expandedBufferText() -> String {
        guard !objects.isEmpty else { return text }
        let textUnits = Array(text.utf16)
        let rawUnits = Array(raw.utf16)
        var out: [unichar] = []
        var cursor = 0
        for at in objects {
            guard units.indices.contains(at) else { continue }
            out.append(contentsOf: textUnits[cursor..<at])
            let unit = units[at]
            guard unit.start >= 0, unit.end <= rawUnits.count, unit.start <= unit.end else { continue }
            out.append(contentsOf: rawUnits[unit.start..<unit.end])
            cursor = at + 1
        }
        if cursor < textUnits.count {
            out.append(contentsOf: textUnits[cursor...])
        }
        return String(decoding: out, as: UTF16.self)
    }

    // 块 → 投影（对齐桌面 projectBlock：剥离块级标记得到可编辑区）。
    static func project(_ block: LiveBlock) -> LiveProjection {
        let rs = Array(block.raw.utf16)
        var lo = 0
        var hi = rs.count
        switch block.kind {
        case .heading, .quote, .list, .task:
            while lo < hi, rs[lo] == 0x20 || rs[lo] == 0x09 { lo += 1 }
            let fromIndent = String(decoding: rs[lo...], as: UTF16.self)
            switch block.kind {
            case .heading:
                while lo < hi, rs[lo] == 0x23 { lo += 1 }
            case .quote:
                if lo < hi { lo += 1 } // 桌面只剥一个 '>'
            case .list:
                lo += LiveSyntax.listMarkerLen(fromIndent)
            case .task:
                lo += LiveSyntax.taskPrefixLen(fromIndent)
            default:
                break
            }
            while lo < hi, rs[lo] == 0x20 || rs[lo] == 0x09 { lo += 1 }
        case .code:
            let ns = block.raw as NSString
            let firstNL = ns.range(of: "\n")
            if firstNL.location != NSNotFound {
                lo = firstNL.location + 1
                let openingLine = ns.substring(to: firstNL.location)
                let lastNL = ns.range(of: "\n", options: .backwards)
                if lastNL.location != NSNotFound,
                   let opening = MarkdownProtection.parseFenceOpeningLine(openingLine),
                   MarkdownProtection.isFenceClosingLine(ns.substring(from: lastNL.location + 1), opening: opening) {
                    hi = lastNL.location
                    if hi < lo { hi = lo }
                }
            } else {
                // 退化：整块只有一行围栏（刚键入的 ```）→ 可编辑区为零宽。
                lo = hi
            }
        case .rule:
            lo = hi
        case .para, .blank:
            break
        }
        lo = min(max(lo, 0), rs.count)
        hi = min(max(hi, lo), rs.count)
        var p = LiveProjection.region(block.raw, lo: lo, hi: hi, inline: block.kind != .code)
        if block.timeTokenRaw != nil {
            p = p.withTimeObject()
        }
        return p
    }

    // region：把 raw 的 [lo, hi) 投影为可见文字并建立映射（对齐桌面 projectRegion）。
    // 表格单元格转义（cell 分支）不移植：移动端无表格块。
    static func region(_ raw: String, lo: Int, hi: Int, inline: Bool) -> LiveProjection {
        let rs = Array(raw.utf16)
        let lo = min(max(lo, 0), rs.count)
        let hi = min(max(hi, lo), rs.count)
        var p = LiveProjection(raw: raw, text: "", start: lo, end: hi, units: [], styles: [], objects: [], isInline: inline)
        var spans: [LiveSpan] = []
        if inline {
            let segment = String(decoding: rs[lo..<hi], as: UTF16.self)
            spans = LiveInline.spans(in: segment)
        }
        var visible: [unichar] = []
        func appendRun(_ from: Int, _ to: Int) {
            var i = from
            while i < to {
                visible.append(rs[i])
                p.units.append(LiveProjectedRune(start: i, end: i + 1))
                i += 1
            }
        }
        var prev = lo
        for sp in spans {
            let a = lo + sp.start
            let z = lo + sp.end
            appendRun(prev, a)
            var inner = a + 1
            var close = z - 1
            if sp.kind == .bold {
                inner = a + 2
                close = z - 2
            }
            if sp.kind == .link {
                var k = a + 1
                var found = -1
                while k < z {
                    if rs[k] == 0x5D { found = k; break }
                    k += 1
                }
                if found >= 0 { close = found }
            }
            let visibleStart = visible.count
            appendRun(inner, close)
            p.styles.append(LiveStyleRange(start: visibleStart, end: visible.count, open: a, content: inner, close: close, tail: z, kind: sp.kind))
            prev = z
        }
        appendRun(prev, hi)
        p.text = String(decoding: visible, as: UTF16.self)
        return p
    }

    // UTF-16 序列相等（对齐工具）。
    static func matches(_ a: [unichar], _ aStart: Int, _ b: [unichar], _ bStart: Int, _ count: Int) -> Bool {
        guard count >= 0, aStart >= 0, bStart >= 0,
              aStart + count <= a.count, bStart + count <= b.count else { return false }
        for i in 0..<count where a[aStart + i] != b[bStart + i] { return false }
        return true
    }

    // prefix 不得落在代理对中间：公共前缀切进代理对时外扩。
    static func alignPrefix(_ old: [unichar], _ prefix: Int) -> Int {
        var p = prefix
        while p > 0, p < old.count,
              old[p - 1] >= 0xD800, old[p - 1] <= 0xDBFF,
              old[p] >= 0xDC00, old[p] <= 0xDFFF {
            p += 1
        }
        return p
    }

    // suffix 不得落在代理对中间：公共后缀切进代理对时外扩。
    static func alignSuffix(_ old: [unichar], _ suffix: Int, limit: Int) -> Int {
        var s = suffix
        while s < limit, old.count - s - 2 >= 0 {
            let hiIdx = old.count - s - 2
            let loIdx = old.count - s - 1
            if old[hiIdx] >= 0xD800, old[hiIdx] <= 0xDBFF,
               old[loIdx] >= 0xDC00, old[loIdx] <= 0xDFFF {
                s += 1
                continue
            }
            break
        }
        return s
    }
}

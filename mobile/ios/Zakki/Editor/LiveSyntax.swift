import Foundation

// 实时预览模型·行级语法原语（LiveSyntax）：空白/标题/列表/围栏判别、trim、
// 时间令牌拆分。全文件偏移为 UTF-16 code unit，坐标约定见 LiveBlockModel.swift。

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

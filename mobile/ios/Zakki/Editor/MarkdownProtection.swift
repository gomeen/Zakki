import Foundation

// Markdown 保护区分类（纯客户端职责，规格 §3.2）：围栏状态机 + 行内代码/链接 span。
// 语义对齐 internal/markdown（parseFenceOpeningLine / isFenceClosingLine / inlineSpans），
// 全部坐标为 UTF-16 code unit 半开区间，与 NSString/NSRange 及 FindTimeToken 的返回对齐。
// 本文件不做任何时间令牌识别——识别只经 core 绑定。
enum MarkdownProtection {
    // MARK: - 代码围栏

    // 开栏标记：围栏字符（` 或 ~）与最少长度。
    struct FenceMarker: Equatable {
        let character: unichar
        let size: Int
    }

    private static let backtick: unichar = 0x60 // `
    private static let tilde: unichar = 0x7E // ~
    private static let space: unichar = 0x20
    private static let tab: unichar = 0x09
    private static let openBracket: unichar = 0x5B // [
    private static let closeBracket: unichar = 0x5D // ]
    private static let openParen: unichar = 0x28 // (
    private static let closeParen: unichar = 0x29 // )
    static let newline: unichar = 0x0A

    // 去掉行尾 \r 与 ≤3 个前导空格；缩进超限返回 nil（对齐 fenceLineContent）。
    private static func fenceLineContent(_ line: String) -> String? {
        var content = line
        if content.hasSuffix("\r") { content.removeLast() }
        let ns = content as NSString
        var indent = 0
        while indent < ns.length && ns.character(at: indent) == space {
            indent += 1
            if indent > 3 { return nil }
        }
        return indent == 0 ? content : ns.substring(from: indent)
    }

    // 解析开栏行：≥3 个同字符；反引号开栏的 info 串不得再含反引号。
    static func parseFenceOpeningLine(_ line: String) -> FenceMarker? {
        guard let content = fenceLineContent(line) else { return nil }
        let ns = content as NSString
        let n = ns.length
        guard n >= 3 else { return nil }
        let first = ns.character(at: 0)
        guard first == backtick || first == tilde else { return nil }
        var size = 1
        while size < n && ns.character(at: size) == first { size += 1 }
        guard size >= 3 else { return nil }
        if first == backtick {
            var i = size
            while i < n {
                if ns.character(at: i) == backtick { return nil }
                i += 1
            }
        }
        return FenceMarker(character: first, size: size)
    }

    // 闭栏：同字符 run ≥ 开栏长度，其后仅允许空白。
    static func isFenceClosingLine(_ line: String, opening: FenceMarker) -> Bool {
        guard opening.size >= 3, let content = fenceLineContent(line) else { return false }
        let ns = content as NSString
        let n = ns.length
        guard n >= opening.size, ns.character(at: 0) == opening.character else { return false }
        var size = 1
        while size < n && ns.character(at: size) == opening.character { size += 1 }
        guard size >= opening.size else { return false }
        var tail = size
        while tail < n && (ns.character(at: tail) == space || ns.character(at: tail) == tab) { tail += 1 }
        return tail == n
    }

    // 逐行推进围栏状态；返回每行是否整行处于围栏内（开栏行与闭栏行自身也计入围栏，
    // 对齐桌面 view_highlight 的分类：这些行不做令牌扫描）。
    static func fencedLines(_ lines: [String]) -> [Bool] {
        var inFence = false
        var opening: FenceMarker?
        return lines.map { line in
            if inFence {
                if let open = opening, isFenceClosingLine(line, opening: open) {
                    inFence = false
                    opening = nil
                }
                return true
            }
            if let marker = parseFenceOpeningLine(line) {
                inFence = true
                opening = marker
                return true
            }
            return false
        }
    }

    // MARK: - 行内保护区

    // 行内代码（反引号对）与链接 [text](url) 的区间，含标记符；粗体/斜体不保护。
    // 移植 inlineSpans 的扫描语义，仅保留 code/link 两类（UTF-16 坐标与 rune 坐标
    // 对 ASCII 标记字符等价，代理对不会与之混淆）。
    static func inlineProtectedRanges(in line: String) -> [NSRange] {
        let ns = line as NSString
        let n = ns.length
        var out: [NSRange] = []
        var i = 0
        func nextOccurrence(_ ch: unichar, from start: Int) -> Int? {
            guard start < n else { return nil }
            var j = start
            while j < n {
                if ns.character(at: j) == ch { return j }
                j += 1
            }
            return nil
        }
        while i < n {
            let c = ns.character(at: i)
            switch c {
            case backtick:
                if let j = nextOccurrence(backtick, from: i + 1) {
                    out.append(NSRange(location: i, length: j + 1 - i))
                    i = j + 1
                    continue
                }
            case openBracket:
                if let cb = nextOccurrence(closeBracket, from: i + 1),
                   cb + 1 < n, ns.character(at: cb + 1) == openParen,
                   let pe = nextOccurrence(closeParen, from: cb + 2) {
                    out.append(NSRange(location: i, length: pe + 1 - i))
                    i = pe + 1
                    continue
                }
            default:
                break
            }
            i += 1
        }
        return out
    }

    // 生成 FindTimeToken 的 protectedJSON（[[start,end],…]）；无区间返回空串（绑定视作空区间）。
    static func protectedJSON(ranges: [NSRange]) -> String {
        guard !ranges.isEmpty else { return "" }
        let items = ranges.map { "[\($0.location),\($0.location + $0.length)]" }
        return "[" + items.joined(separator: ",") + "]"
    }
}

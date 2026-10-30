import Foundation

// 实时预览模型·跨块结构编辑（LiveDocEditing）：回车、块首退格与越界跨块的
// 纯函数语义（移植桌面 live_input.go），产出新文档 raw 与全文光标。

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

import Foundation

// 实时预览模型·可见文字投影：行内标记扫描（LiveInline）与块投影
//（LiveProjection），提供可见↔raw 双向偏移映射与编辑写回。

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

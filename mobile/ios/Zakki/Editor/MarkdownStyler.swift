import UIKit

// MARK: - 行级分类

// 列表 flavor：无序记符号、有序归并（1. 与 2. 仍属同一列表，不加段距）。
enum MarkdownListFlavor: Equatable {
    case unordered(unichar)
    case ordered
}

// 围栏行角色：开栏行/闭栏行只调色，内容行加 mono 与灰底。
enum MarkdownFenceRole: Equatable {
    case open, inside, close
}

// 单行块级分类。坐标一律为全文 UTF-16；分类只读，不改任何字符。
enum MarkdownLineKind: Equatable {
    case blank
    case heading(level: Int, markerRange: NSRange)
    case quote(level: Int, prefixRange: NSRange)
    case list(flavor: MarkdownListFlavor, markerRange: NSRange, contentColumn: Int)
    case task(flavor: MarkdownListFlavor, markerRange: NSRange, boxRange: NSRange, contentColumn: Int)
    case fence
    case paragraph

    var isHeading: Bool {
        if case .heading = self { return true }
        return false
    }

    // 段距签名：相邻行签名相同即同块延续（不给段距）；标题行走硬规则永远自成一块。
    // 桌面语义是块间 12pt；同一段落被硬换行拆开的行（签名同为 p）不吃满段距，
    // 这是编辑器逐行模型的近似——与桌面 goldmark 的懒延续行判定存在边缘差异，
    // 验收按「大致一致」口径（PR 说明）。
    var blockSignature: String? {
        switch self {
        case .blank, .heading:
            return nil
        case .quote(let level, _):
            return "q\(level)"
        case .list(let flavor, _, _), .task(let flavor, _, _, _):
            if case .unordered(let c) = flavor { return "lu\(c)" }
            return "lol"
        case .fence:
            return "f"
        case .paragraph:
            return "p"
        }
    }
}

// 分行结果：text 不含换行（行尾 \r 已剥离，分类与扫描都用它）。
struct MarkdownLine {
    let text: String
    let contentRange: NSRange // 不含行尾换行
    let fullRange: NSRange // 含行尾换行（末行无换行则等于 contentRange）
    let kind: MarkdownLineKind
}

// MARK: - 实时样式器（M2-5）

// 实时模式 Markdown 样式：只设置样式属性，绝不改动任何字符（缓冲与磁盘逐字一致）。
// 所有写入口都由 EditorTextView 在 isReconciling 包裹内调用，样式编辑经
// edited(.editedAttributes) 失效版面：不进撤销栈、不触发写回、观察者不重入。
final class MarkdownStyler {
    // 上次全量/增量后的逐行围栏状态（inFence 与 fencedLines 同口径）。
    private var lastFenceStates: [Bool] = []

    // 段落样式缓存：同参数复用同一 NSParagraphStyle，避免每行新建。
    private struct PSKey: Hashable {
        var spacing: CGFloat
        var firstLine: CGFloat
        var head: CGFloat
        var lineSpacing: CGFloat
    }
    private var psCache: [PSKey: NSParagraphStyle] = [:]

    // 悬挂缩进列宽缓存（按前缀串 + 字体）。
    private var prefixWidthCache: [String: CGFloat] = [:]

    // 测试探针：最近一次重上覆盖的行数与重上次数。
    private(set) var lastStyledLineCount = 0
    private(set) var restylePassCount = 0

    // 引用竖线 fragment 的行查询：每次重上后发布（全文行起点 + 分类）。
    private(set) var lineStarts: [Int] = []
    private(set) var lineKinds: [MarkdownLineKind] = []

    // MARK: 公开入口

    // 整篇重上：打开文件、live↔source 切换（走 init）、明暗切换、围栏状态变化。
    func restyleDocument(storage: NSTextStorage) {
        let (lines, roles) = prepare(storage: storage)
        lastFenceStates = roles.map { $0 != nil }
        guard !lines.isEmpty else {
            lastStyledLineCount = 0
            restylePassCount += 1
            return
        }
        applyStyles(storage: storage, lines: lines, roles: roles, lineIndexes: 0...(lines.count - 1))
        lastStyledLineCount = lines.count
        restylePassCount += 1
    }

    // 增量重上：只覆盖 dirtyRange 展开到整行的窗口（前后各多带一行，
    // 覆盖「上一行段距取决于下一行类型」与行拼接）；围栏状态相对上次变化则整篇重上。
    func restyleEditedLines(storage: NSTextStorage, dirty: NSRange) {
        let (lines, roles) = prepare(storage: storage)
        let fences = roles.map { $0 != nil }
        let firstLine = lineIndex(containing: dirty.location, count: lines.count)
        let lastLine = lineIndex(containing: dirty.location + max(dirty.length - 1, 0), count: lines.count)
        // 前缀比对只看 dirty 之前：围栏开闭只在 dirty 行及其上游产生分歧，
        // 前缀一致的下游行状态必然相同（插行/删行的整体平移不影响逐行真值）。
        let compareEnd = min(lastLine, min(fences.count, lastFenceStates.count) - 1)
        var fenceStable = true
        if compareEnd >= 0 {
            for i in 0...compareEnd where fences[i] != lastFenceStates[i] {
                fenceStable = false
                break
            }
        }
        guard fenceStable else {
            restyleDocument(storage: storage)
            return
        }
        lastFenceStates = fences
        let from = max(0, firstLine - 1)
        let to = min(lines.count - 1, lastLine + 1)
        guard from <= to else {
            lastStyledLineCount = 0
            restylePassCount += 1
            return
        }
        applyStyles(storage: storage, lines: lines, roles: roles, lineIndexes: from...to)
        lastStyledLineCount = to - from + 1
        restylePassCount += 1
    }

    // 引用竖线 fragment 查询：给定全文 UTF-16 偏移，返回所在行的引用层级（非引用行为 0）。
    func quoteLevel(atUTF16Offset offset: Int) -> Int {
        guard !lineStarts.isEmpty else { return 0 }
        var lo = 0, hi = lineStarts.count - 1
        var idx = 0
        while lo <= hi {
            let mid = (lo + hi) / 2
            if lineStarts[mid] <= offset {
                idx = mid
                lo = mid + 1
            } else {
                hi = mid - 1
            }
        }
        if case .quote(let level, _) = lineKinds[idx] { return level }
        return 0
    }

    // MARK: 准备（分行 + 分类 + 围栏角色）

    private func prepare(storage: NSTextStorage) -> (lines: [MarkdownLine], roles: [MarkdownFenceRole?]) {
        let raw = storage.string
        let total = (raw as NSString).length
        var lines: [MarkdownLine] = []
        lineStarts = []
        lineKinds = []
        guard total > 0 else {
            lastFenceStates = []
            return ([], [])
        }
        // 单趟 UTF-16 扫描分行；\r 剥离只影响分类文本，不影响区间。
        let units = Array(raw.utf16)
        var lineStart = 0
        var texts: [(text: String, start: Int, endWithNL: Int)] = []
        func emit(_ start: Int, newlineAt: Int?) {
            let contentEnd = newlineAt ?? total
            var text = String(decoding: units[start..<contentEnd], as: UTF16.self)
            if text.hasSuffix("\r") { text.removeLast() }
            texts.append((text, start, newlineAt.map { $0 + 1 } ?? total))
        }
        for i in 0..<total where units[i] == 0x0A {
            emit(lineStart, newlineAt: i)
            lineStart = i + 1
        }
        if lineStart < total {
            emit(lineStart, newlineAt: nil)
        }
        let textsOnly = texts.map(\.text)
        let roles = Self.fenceRowRoles(textsOnly)
        for (idx, item) in texts.enumerated() {
            let contentLength = (item.text as NSString).length
            let contentRange = NSRange(location: item.start, length: contentLength)
            let fullLength = item.endWithNL - item.start
            // 围栏角色折进分类：开/内/闭行统一走 .fence 样式分支（角色再分样式）。
            var kind = Self.classify(item.text)
            if roles[idx] != nil { kind = .fence }
            lines.append(MarkdownLine(text: item.text, contentRange: contentRange, fullRange: NSRange(location: item.start, length: fullLength), kind: kind))
            lineStarts.append(item.start)
            lineKinds.append(kind)
        }
        return (lines, roles)
    }

    // MARK: 块级样式

    private func applyStyles(storage: NSTextStorage, lines: [MarkdownLine], roles: [MarkdownFenceRole?], lineIndexes: ClosedRange<Int>) {
        guard !lines.isEmpty else { return }
        storage.beginEditing()
        defer { storage.endEditing() }
        var wrote = false
        for i in lineIndexes {
            let line = lines[i]
            let role = roles[i]
            let next = i + 1 < lines.count ? lines[i + 1].kind : nil
            wrote = applyLineBase(storage: storage, line: line, role: role, next: next) || wrote
        }
        guard wrote else { return }
        // 一次属性失效驱动版面重排（组字/对账路径由外层 isReconciling 吞掉通知）。
        // 幂等跳过路径无写入时不失效，整篇重上不惊动 TextKit。
        storage.edited(.editedAttributes, range: NSRange(location: 0, length: storage.length), changeInLength: 0)
    }

    // 最终属性 run：range 为全文坐标，attrs 即该区间应有的完整属性（setAttributes 语义）。
    private struct StyleRun {
        var range: NSRange
        var attrs: [NSAttributedString.Key: Any]
    }

    // 把键值叠加到 runs：相交 run 拆分后并入键值，保留此前叠加的结果（后叠加者胜）。
    private static func overlay(_ keyValues: [NSAttributedString.Key: Any], on range: NSRange, into runs: inout [StyleRun]) {
        guard range.length > 0 else { return }
        var out: [StyleRun] = []
        out.reserveCapacity(runs.count + 2)
        for run in runs {
            let intersect = NSIntersectionRange(run.range, range)
            if intersect.length <= 0 {
                out.append(run)
                continue
            }
            let head = NSRange(location: run.range.location, length: intersect.location - run.range.location)
            let tailLocation = intersect.location + intersect.length
            let tail = NSRange(location: tailLocation, length: run.range.location + run.range.length - tailLocation)
            if head.length > 0 { out.append(StyleRun(range: head, attrs: run.attrs)) }
            var merged = run.attrs
            for (key, value) in keyValues { merged[key] = value }
            out.append(StyleRun(range: intersect, attrs: merged))
            if tail.length > 0 { out.append(StyleRun(range: tail, attrs: run.attrs)) }
        }
        runs = out
    }

    // 本样式器托管的属性键；其余键（如组件字符的 .attachment）不属于样式器，
    // 写入时必须原样保留，否则收簇后附件会丢失。
    private static let managedKeys: Set<NSAttributedString.Key> = [
        .font, .foregroundColor, .backgroundColor, .paragraphStyle,
    ]

    // 幂等写入：区间当前属性与目标一致（字典相等且有效范围覆盖全区间）时跳过。
    // 围栏开关等触发的整篇重上大多行无变化，跳过可把这类重上降为纯计算；
    // 覆盖检查保证起点一致但中部残留旧样式时仍会重写。
    private func writeRun(_ attrs: [NSAttributedString.Key: Any], range: NSRange, on storage: NSTextStorage) -> Bool {
        guard range.length > 0, range.location >= 0, range.location + range.length <= storage.length else { return false }
        var effective = NSRange()
        guard let current = storage.attributes(at: range.location, effectiveRange: &effective) as? [NSAttributedString.Key: Any] else {
            storage.setAttributes(attrs, range: range)
            return true
        }
        // 目标字典 = 托管键取目标值 + 非托管键保留当前值（与旧 remove+add 语义一致）。
        var desired = attrs
        for (key, value) in current where !Self.managedKeys.contains(key) {
            desired[key] = value
        }
        if (current as NSDictionary) == (desired as NSDictionary),
           effective.location <= range.location,
           effective.location + effective.length >= range.location + range.length {
            return false
        }
        storage.setAttributes(desired, range: range)
        return true
    }

    private func applyLineBase(storage: NSTextStorage, line: MarkdownLine, role: MarkdownFenceRole?, next: MarkdownLineKind?) -> Bool {
        let gap = Self.blockGap(prev: line.kind, next: next)
        var attrs: [NSAttributedString.Key: Any] = [
            .font: Theme.Typography.bodyFont,
            .foregroundColor: Theme.Semantic.text,
            .paragraphStyle: paragraphStyle(
                spacing: gap, firstLine: 0, head: 0,
                lineSpacing: Theme.Typography.bodyParagraph.lineSpacing
            ),
        ]
        var muteRanges: [NSRange] = [] // 行内相对坐标，末尾统一换算全文坐标

        switch line.kind {
        case .blank, .paragraph:
            break
        case .heading(_, let markerRange):
            if case .heading(let level, _) = line.kind {
                attrs[.font] = Theme.Typography.headingFont(level: level)
                attrs[.paragraphStyle] = paragraphStyle(
                    spacing: gap, firstLine: 0, head: 0,
                    lineSpacing: Self.lineSpacing(font: Theme.Typography.headingFont(level: level), multiplier: Theme.Typography.lineHeight)
                )
            }
            muteRanges.append(markerRange)
        case .quote(let level, let prefixRange):
            attrs[.foregroundColor] = Theme.Semantic.textMuted
            let first = CGFloat(level) * Theme.Typography.quoteIndent
            // 软换行续行对齐到 `>` 串之后的内容列（首行从缩进位起步，标记本身占位）。
            let quotePrefix = (line.text as NSString).substring(to: prefixRange.location + prefixRange.length)
            attrs[.paragraphStyle] = paragraphStyle(
                spacing: gap, firstLine: first,
                head: first + prefixWidth(quotePrefix, font: Theme.Typography.bodyFont),
                lineSpacing: Theme.Typography.bodyParagraph.lineSpacing
            )
            muteRanges.append(prefixRange)
        case .list(_, let markerRange, let contentColumn), .task(_, let markerRange, _, let contentColumn):
            let prefix = (line.text as NSString).substring(to: contentColumn)
            attrs[.paragraphStyle] = paragraphStyle(
                spacing: gap, firstLine: 0,
                head: prefixWidth(prefix, font: Theme.Typography.bodyFont),
                lineSpacing: Theme.Typography.bodyParagraph.lineSpacing
            )
            muteRanges.append(markerRange)
            if case .task(_, _, let boxRange, _) = line.kind {
                muteRanges.append(boxRange)
            }
        case .fence:
            if role == .inside {
                attrs[.font] = Theme.Typography.codeFont
                attrs[.backgroundColor] = Theme.Semantic.codeBackground
                attrs[.paragraphStyle] = paragraphStyle(
                    spacing: 0, firstLine: 0, head: 0,
                    lineSpacing: Self.lineSpacing(font: Theme.Typography.codeFont, multiplier: Theme.Typography.codeLineHeight)
                )
            } else {
                // 开栏/闭栏行只调色，保持正文字体与段落。
                muteRanges.append(NSRange(location: 0, length: (line.text as NSString).length))
            }
        }

        // runs 从整行基底开始，mutes 与行内样式逐层叠加拆分，得到行内最终属性布局；
        // 稳态下全部命中幂等跳过。classify 返回的区间是行内相对坐标，统一换算全文坐标。
        var runs = [StyleRun(range: line.fullRange, attrs: attrs)]
        for range in muteRanges {
            let global = NSRange(location: line.contentRange.location + range.location, length: range.length)
            Self.overlay([.foregroundColor: Theme.Semantic.textMuted], on: global, into: &runs)
        }
        if line.kind != .fence, line.kind != .blank {
            appendInlineRuns(line: line, into: &runs)
        }
        // 组件字符携带附件等非托管属性，必须单独成 run 才能按字符精确保留。
        if line.text.contains("\u{FFFC}") {
            let u = Array(line.text.utf16)
            for (i, unit) in u.enumerated() where unit == 0xFFFC {
                Self.overlay([:], on: NSRange(location: line.contentRange.location + i, length: 1), into: &runs)
            }
        }
        var wrote = false
        for run in runs {
            wrote = writeRun(run.attrs, range: run.range, on: storage) || wrote
        }
        return wrote
    }

    // 块间段距（桌面语义 blockGap）：下一行是空行→段距记在本行；本行是空行→不加；
    // 标题行永远自成一块；其余按签名判同块延续。空行自身吃满一个行高，不再叠加段距。
    static func blockGap(prev: MarkdownLineKind, next: MarkdownLineKind?) -> CGFloat {
        guard let next else { return 0 }
        if prev == .blank { return 0 }
        if next == .blank { return Theme.Typography.blockGap }
        if prev.isHeading { return Theme.Typography.blockGap }
        if prev.blockSignature != next.blockSignature { return Theme.Typography.blockGap }
        return 0
    }

    // MARK: 行内样式

    // 保护区（行内代码/链接整体，含标记）单独着色；非保护区做粗斜体扫描。
    // 保护区内的 **、*、_ 一律不当作标记（沿用 MarkdownProtection 扫描口径）。
    private func appendInlineRuns(line: MarkdownLine, into runs: inout [StyleRun]) {
        let u = Array(line.text.utf16)
        let protected = MarkdownProtection.inlineProtectedRanges(in: line.text)
        var cursor = 0
        for pr in protected {
            let head = NSRange(location: cursor, length: pr.location - cursor)
            scanEmphasis(u: u, line: line, segment: head, into: &runs)
            appendProtectedRuns(u: u, line: line, range: pr, into: &runs)
            cursor = pr.location + pr.length
        }
        let tail = NSRange(location: cursor, length: u.count - cursor)
        scanEmphasis(u: u, line: line, segment: tail, into: &runs)
    }

    private func appendProtectedRuns(u: [unichar], line: MarkdownLine, range: NSRange, into runs: inout [StyleRun]) {
        guard u.count > 0, range.location + range.length <= u.count else { return }
        let global = NSRange(location: line.contentRange.location + range.location, length: range.length)
        let first = u[range.location]
        if first == 0x60 { // `
            // 行内代码：整段 mono + 灰底；内容保持正文色，两个反引号调灰。
            Self.overlay([
                .font: Theme.Typography.codeFont,
                .backgroundColor: Theme.Semantic.codeBackground,
            ], on: global, into: &runs)
            if range.length > 2 {
                Self.overlay(
                    [.foregroundColor: Theme.Semantic.text],
                    on: NSRange(location: global.location + 1, length: range.length - 2),
                    into: &runs
                )
            }
            Self.overlay([.foregroundColor: Theme.Semantic.textMuted], on: NSRange(location: global.location, length: 1), into: &runs)
            Self.overlay([.foregroundColor: Theme.Semantic.textMuted], on: NSRange(location: global.location + range.length - 1, length: 1), into: &runs)
        } else if first == 0x5B { // [
            // 链接：显示文字 accent 色，`[` 与 `](url)` 调灰。
            var close = range.location + 1
            while close < range.location + range.length, u[close] != 0x5D {
                close += 1
            }
            if close > range.location + 1 {
                Self.overlay(
                    [.foregroundColor: Theme.Semantic.accent],
                    on: NSRange(location: global.location + 1, length: close - range.location - 1),
                    into: &runs
                )
            }
            Self.overlay([.foregroundColor: Theme.Semantic.textMuted], on: NSRange(location: global.location, length: 1), into: &runs)
            let tailStart = global.location + (close - range.location)
            let tailRange = NSRange(location: tailStart, length: max(0, global.location + range.length - tailStart))
            if tailRange.length > 0 {
                Self.overlay([.foregroundColor: Theme.Semantic.textMuted], on: tailRange, into: &runs)
            }
        }
    }

    // 粗斜体扫描：只处理未保护区段；匹配失败的标记保持原样（不调灰），
    // 避免把字面 `**` 误示为标记。`**`/`__` 优先于单字符；`_` 系要求词边界
    //（对齐 CommonMark 的下划线词中不生效，snake_case 不变异）。
    // 内容字体从基底字体合成：整行基底在同一 run 模型内取值，与存储当前态无关。
    private func scanEmphasis(u: [unichar], line: MarkdownLine, segment: NSRange, into runs: inout [StyleRun]) {
        var i = segment.location
        let end = segment.location + segment.length
        let globalBase = line.contentRange.location
        while i < end {
            let c = u[i]
            if c == 0x2A || c == 0x5F {
                let doubleMarker = i + 1 < end && u[i + 1] == c
                let markerLength = doubleMarker ? 2 : 1
                // `*` 允许词中（CommonMark）；`_` 需要开闭两侧都不贴词字符。
                let boundaryOK = c == 0x2A || openFlankOK(u, at: i, segmentStart: segment.location)
                if boundaryOK, let close = findClosing(u, markerChar: c, markerLength: markerLength, from: i + markerLength, limit: end),
                   close + markerLength <= end {
                    if !doubleMarker, c == 0x5F {
                        // 闭侧词边界只约束下划线系；星号按 CommonMark 允许词中。
                        let afterClose = close + markerLength
                        let closeFlankOK = afterClose >= u.count || !Self.isWordChar(u[afterClose])
                        guard closeFlankOK else {
                            i += 1
                            continue
                        }
                    }
                    let contentRange = NSRange(location: globalBase + i + markerLength, length: close - i - markerLength)
                    if contentRange.length > 0 {
                        let traits: UIFontDescriptor.SymbolicTraits = doubleMarker ? [.traitBold] : [.traitItalic]
                        let baseFont = baseFont(forRuns: runs, at: contentRange.location)
                        if let composed = Self.font(baseFont, addingTraits: traits) {
                            Self.overlay([.font: composed], on: contentRange, into: &runs)
                        }
                    }
                    Self.overlay([.foregroundColor: Theme.Semantic.textMuted], on: NSRange(location: globalBase + i, length: markerLength), into: &runs)
                    Self.overlay([.foregroundColor: Theme.Semantic.textMuted], on: NSRange(location: globalBase + close, length: markerLength), into: &runs)
                    i = close + markerLength
                    continue
                }
            }
            i += 1
        }
    }

    // 区间所在 run 的字体（run 模型即行内最终布局，粗体套粗体等嵌套由此叠加）。
    private func baseFont(forRuns runs: [StyleRun], at location: Int) -> UIFont {
        for run in runs where NSLocationInRange(location, run.range) {
            return run.attrs[.font] as? UIFont ?? Theme.Typography.bodyFont
        }
        return Theme.Typography.bodyFont
    }

    // 在 [from, limit) 内找与 markerChar^markerLength 相同的闭合标记。
    private func findClosing(_ u: [unichar], markerChar: unichar, markerLength: Int, from: Int, limit: Int) -> Int? {
        var j = from
        while j + markerLength <= limit {
            if u[j] == markerChar,
               markerLength == 1 || u[j + 1] == markerChar {
                return j
            }
            j += 1
        }
        return nil
    }

    // `_` 开侧词边界：段首或前一字符非字母数字。
    private func openFlankOK(_ u: [unichar], at: Int, segmentStart: Int) -> Bool {
        guard at > segmentStart else { return true }
        return !Self.isWordChar(u[at - 1])
    }

    private static func isWordChar(_ c: unichar) -> Bool {
        // ASCII 词字符 + 非 ASCII（CJK 等按词字符处理，对齐 CommonMark flanking）。
        if c >= 0x80 { return true }
        return (c >= 0x30 && c <= 0x39) || (c >= 0x41 && c <= 0x5A) || (c >= 0x61 && c <= 0x7A) || c == 0x5F
    }

    static func font(_ base: UIFont, addingTraits traits: UIFontDescriptor.SymbolicTraits) -> UIFont? {
        guard let descriptor = base.fontDescriptor.withSymbolicTraits(base.fontDescriptor.symbolicTraits.union(traits)) else {
            return nil
        }
        return UIFont(descriptor: descriptor, size: base.pointSize)
    }

    // MARK: 分类与围栏

    // 围栏行角色（与 fencedLines 同一状态机，额外区分开/内/闭）。
    static func fenceRowRoles(_ lines: [String]) -> [MarkdownFenceRole?] {
        var inFence = false
        var opening: MarkdownProtection.FenceMarker?
        return lines.map { line in
            if inFence {
                if let open = opening, MarkdownProtection.isFenceClosingLine(line, opening: open) {
                    inFence = false
                    opening = nil
                    return .close
                }
                return .inside
            }
            if let marker = MarkdownProtection.parseFenceOpeningLine(line) {
                inFence = true
                opening = marker
                return .open
            }
            return nil
        }
    }

    private static func classify(_ line: String) -> MarkdownLineKind {
        let ns = line as NSString
        let n = ns.length
        var indent = 0
        while indent < min(3, n), ns.character(at: indent) == 0x20 {
            indent += 1
        }
        // 纯空白行视作空行（不逐字符分配子串）。
        var allBlank = true
        for i in indent..<n {
            let c = ns.character(at: i)
            if c != 0x20 && c != 0x09 {
                allBlank = false
                break
            }
        }
        if allBlank {
            return .blank
        }
        let first = ns.character(at: indent)

        // 标题：#{1,6} + 空格或行尾。
        if first == 0x23 {
            var run = 1
            while indent + run < n, ns.character(at: indent + run) == 0x23, run < 6 {
                run += 1
            }
            if run <= 6 {
                let after = indent + run
                if after >= n || ns.character(at: after) == 0x20 {
                    return .heading(level: run, markerRange: NSRange(location: indent, length: run))
                }
            }
        }

        // 引用：连续 `>`（层间可有一个空格）。
        if first == 0x3E {
            var i = indent
            var level = 0
            while i < n, ns.character(at: i) == 0x3E {
                level += 1
                i += 1
                if i < n, ns.character(at: i) == 0x20 { i += 1 }
            }
            return .quote(level: level, prefixRange: NSRange(location: indent, length: i - indent))
        }

        // 列表与任务。
        if first == 0x2D || first == 0x2A || first == 0x2B {
            if n == indent + 1 || ns.character(at: indent + 1) == 0x20 {
                let afterMarker = indent + 1 + (n > indent + 1 ? 1 : 0)
                if let task = taskBox(ns, from: afterMarker, lineEnd: n) {
                    return .task(flavor: .unordered(first), markerRange: NSRange(location: indent, length: 1), boxRange: task.box, contentColumn: task.contentColumn)
                }
                return .list(flavor: .unordered(first), markerRange: NSRange(location: indent, length: 1), contentColumn: afterMarker)
            }
        }
        if first >= 0x30, first <= 0x39 {
            var digits = 1
            while indent + digits < n, ns.character(at: indent + digits) >= 0x30, ns.character(at: indent + digits) <= 0x39, digits <= 9 {
                digits += 1
            }
            let dot = indent + digits
            if digits <= 9, dot < n {
                let punct = ns.character(at: dot)
                if punct == 0x2E || punct == 0x29 {
                    let afterMarker = dot + 1
                    if afterMarker >= n || ns.character(at: afterMarker) == 0x20 {
                        let contentColumn = afterMarker + (afterMarker < n ? 1 : 0)
                        if let task = taskBox(ns, from: contentColumn, lineEnd: n) {
                            return .task(flavor: .ordered, markerRange: NSRange(location: indent, length: dot - indent + 1), boxRange: task.box, contentColumn: task.contentColumn)
                        }
                        return .list(flavor: .ordered, markerRange: NSRange(location: indent, length: dot - indent + 1), contentColumn: contentColumn)
                    }
                }
            }
        }

        return .paragraph
    }

    // 任务框：from 处 [ ] / [x] / [X] 且后随空格或行尾；返回框区间与内容列。
    private static func taskBox(_ ns: NSString, from: Int, lineEnd: Int) -> (box: NSRange, contentColumn: Int)? {
        guard from + 3 <= lineEnd,
              ns.character(at: from) == 0x5B,
              ns.character(at: from + 1) == 0x20 || ns.character(at: from + 1) == 0x78 || ns.character(at: from + 1) == 0x58,
              ns.character(at: from + 2) == 0x5D else { return nil }
        let after = from + 3
        guard after >= lineEnd || ns.character(at: after) == 0x20 else { return nil }
        return (NSRange(location: from, length: 3), after + (after < lineEnd ? 1 : 0))
    }

    // MARK: 工具

    private func lineIndex(containing offset: Int, count: Int) -> Int {
        guard count > 0 else { return 0 }
        var idx = 0
        // lineStarts 已在 prepare 中同步填好。
        var lo = 0, hi = count - 1
        while lo <= hi {
            let mid = (lo + hi) / 2
            if mid < lineStarts.count, lineStarts[mid] <= offset {
                idx = mid
                lo = mid + 1
            } else {
                hi = mid - 1
            }
        }
        return idx
    }

    private func paragraphStyle(spacing: CGFloat, firstLine: CGFloat, head: CGFloat, lineSpacing: CGFloat) -> NSParagraphStyle {
        let key = PSKey(spacing: spacing, firstLine: firstLine, head: head, lineSpacing: lineSpacing)
        if let cached = psCache[key] { return cached }
        let mutable = NSMutableParagraphStyle()
        mutable.lineSpacing = lineSpacing
        mutable.paragraphSpacing = spacing
        mutable.firstLineHeadIndent = firstLine
        mutable.headIndent = head
        let style = mutable as NSParagraphStyle
        psCache[key] = style
        return style
    }

    // CSS line-height 语义：字号 × 倍数 与字体行盒的差额。
    private static func lineSpacing(font: UIFont, multiplier: CGFloat) -> CGFloat {
        max(0, font.pointSize * multiplier - font.lineHeight)
    }

    private func prefixWidth(_ prefix: String, font: UIFont) -> CGFloat {
        if let cached = prefixWidthCache[prefix] { return cached }
        let width = (prefix as NSString).size(withAttributes: [.font: font]).width
        prefixWidthCache[prefix] = width
        return width
    }
}

// MARK: - 引用竖线（TextKit 2 自定义 layout fragment）

// 引用行的 fragment：在正文绘制之外，于行块左缘画 quoteBorder 色竖线（宽 quoteBar）。
// 一个 NSTextLayoutFragment 对应一个文本元素（段落），软换行的多行共享同一 fragment，
// 竖线自然连成整块，与桌面引用条观感一致。
final class QuoteBarLayoutFragment: NSTextLayoutFragment {
    private let barColor: UIColor

    init(textElement: NSTextElement, range: NSTextRange, barColor: UIColor) {
        self.barColor = barColor
        super.init(textElement: textElement, range: range)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("fragment 不走归档")
    }

    override func draw(at renderingOrigin: CGPoint, in ctx: CGContext) {
        super.draw(at: renderingOrigin, in: ctx)
        let frame = layoutFragmentFrame
        let inset: CGFloat = 1
        let bar = CGRect(
            x: renderingOrigin.x,
            y: renderingOrigin.y + inset,
            width: Theme.Typography.quoteBar,
            height: max(0, frame.height - inset * 2)
        )
        ctx.saveGState()
        // 绘制发生在视图 draw 周期内，动态色按当前 traits 解析。
        barColor.setFill()
        ctx.fill(bar)
        ctx.restoreGState()
    }
}

// fragment 提供者：引用行返回 QuoteBarLayoutFragment，其余走默认 fragment。
// UITextView 自身是 textLayoutManager 的 delegate，这里用 forwarding 把未实现的
// 可选代理方法原样转回原 delegate，保留 UITextView 的既有行为；每次重上前
// EditorTextView 会核查 delegate 是否仍指向本对象（系统可能在内部重设）。
final class MarkdownFragmentProvider: NSObject, NSTextLayoutManagerDelegate {
    weak var owner: MarkdownStyler?
    weak var originalDelegate: NSObjectProtocol?

    override func responds(to aSelector: Selector!) -> Bool {
        super.responds(to: aSelector) || (originalDelegate?.responds(to: aSelector) ?? false)
    }

    override func forwardingTarget(for aSelector: Selector!) -> Any? {
        if super.responds(to: aSelector) { return nil }
        return originalDelegate
    }

    func textLayoutManager(_ textLayoutManager: NSTextLayoutManager, textLayoutFragmentFor location: any NSTextLocation, in textElement: NSTextElement) -> NSTextLayoutFragment {
        let range = textElement.elementRange
        guard let range,
              let owner,
              let contentStorage = textLayoutManager.textContentManager as? NSTextContentStorage else {
            return NSTextLayoutFragment(textElement: textElement, range: range)
        }
        let offset = contentStorage.offset(from: contentStorage.documentRange.location, to: range.location)
        guard owner.quoteLevel(atUTF16Offset: offset) > 0 else {
            return NSTextLayoutFragment(textElement: textElement, range: range)
        }
        return QuoteBarLayoutFragment(
            textElement: textElement, range: range,
            barColor: Theme.Semantic.quoteBorder
        )
    }
}

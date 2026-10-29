import Foundation
import Zakkicore

// MARK: - core 绑定封装（编辑器栈唯一的绑定 I/O 边界）

// FindTimeToken JSON 结果中的 suffix 字段（字段全出，见 bind.go suffixJSON）。
struct TimeSuffixInfo: Equatable, Decodable {
    var due: String
    var hasAlert: Bool
    var alertMs: Int64?
    var recur: String?
}

// 一次令牌识别结果：range 为行内 UTF-16 半开区间。
struct TimeTokenHit: Equatable {
    var range: NSRange
    var raw: String
    var suffix: TimeSuffixInfo
}

// MARK: - 文档片段

// 缓冲中的一个连续片段：普通文本，或已收簇的时间组件（缓冲中占一个 U+FFFC）。
enum EditorPiece: Equatable {
    case text(String)
    // 组件的原始 Markdown，如 "@2026-01-01 10:00 ~10m ^1d"。
    case token(String)

    var bufferText: String {
        if case .text(let s) = self { return s }
        return "\u{FFFC}"
    }

    var rawText: String {
        switch self {
        case .text(let s): return s
        case .token(let raw): return raw
        }
    }

    var bufferLength: Int {
        if case .text(let s) = self { return (s as NSString).length }
        return 1
    }

    var rawLength: Int { (rawText as NSString).length }
}

// MARK: - 文档内核

// 纯 Foundation 的编辑器文档：磁盘 raw ↔ 缓冲（U+FFFC 组件）双向投影与增量编辑。
// 时间语义全部经 core 绑定判定（识别走 FindTimeToken），本类型不做任何令牌正则；
// 保护区（围栏/行内代码/链接）由 MarkdownProtection 提供。
//
// 增量策略（D5）：apply 时按公共前后缀定位改动，只对受影响行做绑定重扫与片段重建，
// 禁止全量重排。重扫行为最终一致：改动只影响受影响行，未触行的簇在下次触及时收簇。
final class EditorDocument {
    // 一次缓冲编辑：replacedRange 为旧缓冲坐标，replacement 为归一化后的片段。
    struct BufferEdit {
        var replacedRange: NSRange
        var replacement: [EditorPiece]
    }

    // 需要落到宿主视图的缓冲替换（视图当前文本坐标）；piece 为替换后的片段。
    struct BufferReplacement: Equatable {
        var range: NSRange
        var piece: EditorPiece
    }

    struct EditOutcome {
        var replacements: [BufferReplacement] = []
    }

    private struct Line {
        var text: String // 不含换行符
        var rawStart: Int
        var rawLength: Int // 含行尾换行符
    }

    private(set) var pieces: [EditorPiece] = []
    // 投影缓存，随 pieces 重建。
    private(set) var bufferText = ""
    private(set) var rawText = ""
    private(set) var bufferLength = 0
    private(set) var rawLength = 0
    private var pieceBufferStarts: [Int] = []
    private var pieceRawStarts: [Int] = []
    private var lines: [Line] = []

    init(raw: String) {
        pieces = [.text(raw)]
        rebuildDerived()
        // 全量首扫：逐行经绑定识别收簇。
        if !lines.isEmpty {
            rescanLines(0...(lines.count - 1))
        }
    }

    // MARK: 查询

    // 缓冲区间的 raw 投影（剪贴板与写回共用，D2）：区间内组件展开为原始 Markdown。
    func raw(in bufferRange: NSRange) -> String {
        let range = clampBuffer(bufferRange)
        guard range.length > 0 else { return "" }
        var out = ""
        var cursor = 0
        for piece in pieces {
            let pieceRange = NSRange(location: cursor, length: piece.bufferLength)
            let intersect = NSIntersectionRange(pieceRange, range)
            if intersect.length > 0 {
                switch piece {
                case .text(let s):
                    out.append((s as NSString).substring(with: NSRange(location: intersect.location - pieceRange.location, length: intersect.length)))
                case .token(let raw):
                    // 组件在缓冲中占 1 个字符，任何非空交集都是整簇。
                    out.append(raw)
                }
            }
            cursor = pieceRange.location + pieceRange.length
        }
        return out
    }

    // 粘贴净化（D7）：外来缓冲没有组件归属，无主 U+FFFC 一律过滤。
    static func sanitizingPasted(_ text: String) -> String {
        guard text.contains("\u{FFFC}") else { return text }
        return text.replacingOccurrences(of: "\u{FFFC}", with: "")
    }

    // 缓冲区间覆盖的片段（宿主按文档投影渲染兜底修正时用）。
    func pieces(in bufferRange: NSRange) -> [EditorPiece] {
        let range = clampBuffer(bufferRange)
        guard range.length > 0 else { return [] }
        var out: [EditorPiece] = []
        var cursor = 0
        for piece in pieces {
            let pieceRange = NSRange(location: cursor, length: piece.bufferLength)
            let intersect = NSIntersectionRange(pieceRange, range)
            if intersect.length > 0 {
                switch piece {
                case .text(let s):
                    out.append(.text((s as NSString).substring(with: NSRange(location: intersect.location - pieceRange.location, length: intersect.length))))
                case .token:
                    out.append(piece)
                }
            }
            cursor = pieceRange.location + pieceRange.length
        }
        return out
    }

    // MARK: 编辑

    // 应用一次缓冲编辑并按需重扫受影响行。detectTokens=false 时跳过收簇识别
    //（撤销/重做回放与 IME 组字期间，D6）；返回需要宿主视图执行的替换（收组件）。
    @discardableResult
    func apply(_ edit: BufferEdit, detectTokens: Bool) -> EditOutcome {
        // 旧几何（变异前抓取，收簇 op 映射要用）。
        let oldBufferStarts = pieceBufferStarts
        let oldRawStarts = pieceRawStarts
        let oldPieces = pieces
        let oldRawLength = rawLength

        let range = clampBuffer(edit.replacedRange)
        let oldRawStart = rawOffset(ofBufferOffset: range.location)
        let oldRawEnd = rawOffset(ofBufferOffset: range.location + range.length)

        let (before, after) = splitPieces(atBufferRange: range)
        let replacement = Self.sanitized(edit.replacement)
        pieces = before + replacement + after
        rebuildDerived()

        guard detectTokens else { return EditOutcome() }

        // 受影响行：覆盖替换区域的新 raw 行（簇只会完整落在单行内，取整行重扫即可
        // 覆盖“键入使簇在光标前补完”的跨区段情形）。
        let regionRawLen = replacement.reduce(0) { $0 + $1.rawLength }
        let regionEnd = oldRawStart + regionRawLen
        guard !lines.isEmpty else { return EditOutcome() }
        let firstLine = lineIndex(atRawOffset: min(oldRawStart, rawLength))
        let lastLine = lineIndex(atRawOffset: max(oldRawStart, min(regionEnd - 1, rawLength - 1)))
        // 变异后、重扫前的几何快照：重扫会把本次命中的簇以 token 片元写回 pieces，
        // 命中映射若用重扫后的几何会落在命中自身的组件字符上，被“缓冲长度 1”
        // 的过滤条件误跳过。
        let mutatedPieces = pieces
        let mutatedBufferStarts = pieceBufferStarts
        let mutatedRawStarts = pieceRawStarts
        let hits = rescanLines(firstLine...lastLine)

        // 把命中区间映射回视图当前缓冲坐标（= 新缓冲：前缀与替换区与旧缓冲共几何）。
        // 替换区在识别路径上必为纯文本（token 片段只随撤销/重做路径进来，且不识别），
        // 故区内 raw 与缓冲偏移 1:1；替换区之前用旧几何、之后用变异后几何映射，
        // 才能正确跨越行内既有组件（其缓冲长度 1 ≠ raw 长度）。已是组件的命中
        //（缓冲长度 1）不产生替换。
        func mappedBufferOffset(_ newRaw: Int) -> Int {
            if newRaw <= oldRawStart {
                return bufferOffset(ofRawOffset: newRaw, pieces: oldPieces, bufferStarts: oldBufferStarts, rawStarts: oldRawStarts)
            }
            if newRaw < regionEnd {
                return range.location + (newRaw - oldRawStart)
            }
            return bufferOffset(ofRawOffset: newRaw, pieces: mutatedPieces, bufferStarts: mutatedBufferStarts, rawStarts: mutatedRawStarts)
        }

        var outcome = EditOutcome()
        for hit in hits {
            let start = mappedBufferOffset(hit.range.location)
            let end = mappedBufferOffset(hit.range.location + hit.range.length)
            guard end - start > 1 else { continue }
            outcome.replacements.append(BufferReplacement(range: NSRange(location: start, length: end - start), piece: .token(hit.raw)))
        }
        return outcome
    }

    // MARK: 识别（只走绑定）

    // 单行扫描一个令牌；protectedJSON 为该行 UTF-16 保护区。
    static func findTimeToken(in line: String, protectedJSON: String) -> TimeTokenHit? {
        var err: NSError?
        let out = ZakkicoreFindTimeToken(line, protectedJSON, &err)
        if let err {
            // 偏移由本端生成，正常不会到这；防御性视作无令牌，保留断言便于开发期暴露。
            assertionFailure("FindTimeToken 失败: \(err)")
            return nil
        }
        guard let data = out.data(using: .utf8),
              let result = try? JSONDecoder().decode(FindResult.self, from: data),
              result.found, let suffix = result.suffix else { return nil }
        let ns = line as NSString
        guard result.start >= 0, result.end <= ns.length, result.end > result.start else { return nil }
        return TimeTokenHit(range: NSRange(location: result.start, length: result.end - result.start), raw: ns.substring(with: NSRange(location: result.start, length: result.end - result.start)), suffix: suffix)
    }

    private struct FindResult: Decodable {
        var found: Bool
        var start: Int
        var end: Int
        var suffix: TimeSuffixInfo?
    }

    // MARK: 内部：派生缓存

    private func rebuildDerived() {
        // 清理组装过程中可能产生的空文本片段（空文档收敛为 pieces = []）。
        if pieces.contains(where: { if case .text(let s) = $0 { return s.isEmpty }; return false }) {
            pieces = pieces.filter { if case .text(let s) = $0 { return !s.isEmpty }; return true }
        }
        bufferText = pieces.map(\.bufferText).joined()
        rawText = pieces.map(\.rawText).joined()
        bufferLength = (bufferText as NSString).length
        rawLength = (rawText as NSString).length
        pieceBufferStarts = []
        pieceRawStarts = []
        var b = 0
        var r = 0
        for piece in pieces {
            pieceBufferStarts.append(b)
            pieceRawStarts.append(r)
            b += piece.bufferLength
            r += piece.rawLength
        }
        rebuildLines()
    }

    private func rebuildLines() {
        lines = []
        let ns = rawText as NSString
        var start = 0
        for i in 0...ns.length {
            if i == ns.length {
                lines.append(Line(text: ns.substring(with: NSRange(location: start, length: ns.length - start)), rawStart: start, rawLength: ns.length - start))
                break
            }
            if ns.character(at: i) == MarkdownProtection.newline {
                lines.append(Line(text: ns.substring(with: NSRange(location: start, length: i - start)), rawStart: start, rawLength: i - start + 1))
                start = i + 1
            }
        }
    }

    private func lineIndex(atRawOffset offset: Int) -> Int {
        guard !lines.isEmpty else { return 0 }
        var idx = 0
        for (i, line) in lines.enumerated() {
            if line.rawStart <= offset { idx = i } else { break }
        }
        return idx
    }

    // MARK: 内部：坐标映射

    private func clampBuffer(_ range: NSRange) -> NSRange {
        let location = min(max(range.location, 0), bufferLength)
        let end = min(max(range.location + range.length, location), bufferLength)
        return NSRange(location: location, length: end - location)
    }

    // 当前几何的坐标映射（模式切换的光标换算与测试共用）。
    // raw 偏移 → 缓冲偏移：簇内钳到组件首，簇尾及之后落到组件后一格。
    func bufferOffset(ofRawOffset offset: Int) -> Int {
        bufferOffset(ofRawOffset: offset, pieces: pieces, bufferStarts: pieceBufferStarts, rawStarts: pieceRawStarts)
    }

    // 缓冲偏移 → raw 偏移：组件首映射簇首，组件后映射簇尾。
    func rawOffset(ofBufferOffset offset: Int) -> Int {
        rawOffset(ofBufferOffset: offset, pieces: pieces, bufferStarts: pieceBufferStarts, rawStarts: pieceRawStarts)
    }

    // 通用映射：raw 偏移 → 缓冲偏移（用调用方抓取的前缀和，兼容变异前后两套几何）。
    // 组件片元占缓冲 1 个字符：簇尾及之后落到组件后一格，簇内钳到组件首
    //（组件不可内部定位，选区恢复时取边界即可）。
    private func bufferOffset(ofRawOffset offset: Int, pieces: [EditorPiece], bufferStarts: [Int], rawStarts: [Int]) -> Int {
        guard !pieces.isEmpty, !rawStarts.isEmpty else { return 0 }
        var idx = 0
        for (i, start) in rawStarts.enumerated() {
            if start <= offset { idx = i } else { break }
        }
        let piece = pieces[idx]
        let delta = offset - rawStarts[idx]
        if case .text(let s) = piece {
            return bufferStarts[idx] + min(delta, (s as NSString).length)
        }
        return delta >= piece.rawLength ? bufferStarts[idx] + 1 : bufferStarts[idx]
    }

    // 通用映射：缓冲偏移 → raw 偏移（用调用方抓取的前缀和，兼容变异前后两套几何）。
    // 组件片元占缓冲 1 个字符：组件后（delta ≥ 1）映射簇尾，组件首映射簇首。
    private func rawOffset(ofBufferOffset offset: Int, pieces: [EditorPiece], bufferStarts: [Int], rawStarts: [Int]) -> Int {
        guard !pieces.isEmpty, !bufferStarts.isEmpty else { return 0 }
        var idx = 0
        for (i, start) in bufferStarts.enumerated() {
            if start <= offset { idx = i } else { break }
        }
        let piece = pieces[idx]
        let delta = offset - bufferStarts[idx]
        if case .text(let s) = piece {
            return rawStarts[idx] + min(delta, (s as NSString).length)
        }
        return delta >= 1 ? rawStarts[idx] + piece.rawLength : rawStarts[idx]
    }

    // 按缓冲区间切分 pieces：区间前与区间后的片段（跨界文本片段沿边界拆开，
    // 区间内片段丢弃）。空区间（纯插入）在片段内部时同样正确拆为两半。
    private func splitPieces(atBufferRange range: NSRange) -> (before: [EditorPiece], after: [EditorPiece]) {
        var before: [EditorPiece] = []
        var after: [EditorPiece] = []
        var cursor = 0
        for piece in pieces {
            let pieceEnd = cursor + piece.bufferLength
            if pieceEnd <= range.location {
                before.append(piece)
            } else if cursor >= range.location + range.length {
                after.append(piece)
            } else {
                let headLength = max(0, range.location - cursor)
                let tailLength = max(0, pieceEnd - (range.location + range.length))
                if let p = substring(of: piece, head: headLength, tail: tailLength, side: .head) { before.append(p) }
                if let p = substring(of: piece, head: headLength, tail: tailLength, side: .tail) { after.append(p) }
            }
            cursor = pieceEnd
        }
        return (before, after)
    }

    private func splitPieces(atRawRange range: NSRange) -> (before: [EditorPiece], after: [EditorPiece]) {
        var before: [EditorPiece] = []
        var after: [EditorPiece] = []
        var cursor = 0
        for piece in pieces {
            let pieceEnd = cursor + piece.rawLength
            if pieceEnd <= range.location {
                before.append(piece)
            } else if cursor >= range.location + range.length {
                after.append(piece)
            } else {
                let headLength = max(0, range.location - cursor)
                let tailLength = max(0, pieceEnd - (range.location + range.length))
                if let p = rawSubstring(of: piece, head: headLength, tail: tailLength, side: .head) { before.append(p) }
                if let p = rawSubstring(of: piece, head: headLength, tail: tailLength, side: .tail) { after.append(p) }
            }
            cursor = pieceEnd
        }
        return (before, after)
    }

    private enum Side { case head, tail }

    // 取片段在切分边界一侧的部分；只在对应侧非空时返回（组件只能被完整保留或丢弃）。
    private func substring(of piece: EditorPiece, head: Int, tail: Int, side: Side) -> EditorPiece? {
        guard case .text(let s) = piece else { return nil }
        let length = (s as NSString).length
        switch side {
        case .head:
            guard head > 0 else { return nil }
            return .text((s as NSString).substring(with: NSRange(location: 0, length: head)))
        case .tail:
            guard tail > 0 else { return nil }
            return .text((s as NSString).substring(with: NSRange(location: length - tail, length: tail)))
        }
    }

    // raw 侧切分只发生在行边界；组件不跨行，不会被切到，防御下按文本处理。
    private func rawSubstring(of piece: EditorPiece, head: Int, tail: Int, side: Side) -> EditorPiece? {
        let length = piece.rawLength
        switch side {
        case .head:
            guard head > 0 else { return nil }
            return .text((piece.rawText as NSString).substring(with: NSRange(location: 0, length: head)))
        case .tail:
            guard tail > 0 else { return nil }
            return .text((piece.rawText as NSString).substring(with: NSRange(location: head + (length - head - tail), length: tail)))
        }
    }

    // MARK: 内部：受影响行重扫

    // 重扫给定行区间：重建这些行的 pieces，返回命中的令牌（新 raw 绝对坐标，升序不重叠）。
    // 已是组件的命中原样保留（重建出的仍是 .token 片段），不产生视图替换。
    @discardableResult
    private func rescanLines(_ range: ClosedRange<Int>) -> [TimeTokenHit] {
        let fenced = MarkdownProtection.fencedLines(lines.map(\.text))
        var spanPieces: [EditorPiece] = []
        var hits: [TimeTokenHit] = []
        var pendingText = ""
        func flushText() {
            if !pendingText.isEmpty {
                spanPieces.append(.text(pendingText))
                pendingText = ""
            }
        }
        for li in range.lowerBound...range.upperBound {
            let line = lines[li]
            defer { if li < range.upperBound { pendingText += "\n" } }
            guard !fenced[li] else {
                pendingText += line.text
                continue
            }
            // 行内扫描：识别首个令牌后从其尾部继续，找同行后续令牌。
            var searchStart = 0
            let lineNSString = line.text as NSString
            while searchStart <= lineNSString.length {
                let tail = lineNSString.substring(from: searchStart)
                var ranges = MarkdownProtection.inlineProtectedRanges(in: tail)
                ranges = ranges.map { NSRange(location: $0.location, length: $0.length) }
                if let hit = Self.findTimeToken(in: tail, protectedJSON: MarkdownProtection.protectedJSON(ranges: ranges)) {
                    let hitInLine = NSRange(location: searchStart + hit.range.location, length: hit.range.length)
                    let head = lineNSString.substring(with: NSRange(location: searchStart, length: hit.range.location))
                    pendingText += head
                    flushText()
                    spanPieces.append(.token(hit.raw))
                    // 命中区间必须换算 raw 绝对坐标：apply 的映射按全文 raw 偏移解释，
                    // 行内相对坐标只与首行重合时巧合成立，多行文档会收错区间。
                    hits.append(TimeTokenHit(range: NSRange(location: line.rawStart + hitInLine.location, length: hitInLine.length), raw: hit.raw, suffix: hit.suffix))
                    searchStart = hitInLine.location + hitInLine.length
                } else {
                    pendingText += tail
                    break
                }
            }
        }
        flushText()

        guard range.upperBound >= range.lowerBound, range.lowerBound < lines.count else { return hits }
        let spanStart = lines[range.lowerBound].rawStart
        // 重建 span 只覆盖到末行行尾文本（不含行尾换行）：spanPieces 只携带行内文本与
        // 行间换行，末行行尾换行留在 after 里。否则单行重扫会吞掉行尾换行，
        // 多行文档每次键入经 enforceBufferInvariant 被写坏（blank 行丢失）。
        let upperLine = lines[range.upperBound]
        let spanEnd = upperLine.rawStart + (upperLine.text as NSString).length
        let (before, after) = splitPieces(atRawRange: NSRange(location: spanStart, length: spanEnd - spanStart))
        pieces = before + spanPieces + after
        rebuildDerived()
        return hits
    }

    // 防御净化：文本片段内不允许出现无主 U+FFFC（缓冲中的 U+FFFC 只能来自组件片段）。
    private static func sanitized(_ pieces: [EditorPiece]) -> [EditorPiece] {
        pieces.compactMap { piece in
            switch piece {
            case .text(let s):
                let clean = sanitizingPasted(s)
                return clean.isEmpty ? nil : .text(clean)
            case .token:
                return piece
            }
        }
    }
}

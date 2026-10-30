import UIKit

// MARK: - 块内可见文字样式器（M2-6）

// M2-6 实时预览下缓冲只含投影出的「可见文字」（标记已剥离），本类给焦点块的
// 可见缓冲上样式：块级基底（标题字号/引用次要色/代码 mono）、投影行内样式
//（粗体/斜体/行内代码底色/链接强调色）与段行高。只设置样式属性，绝不改字符；
// 写入口由容器在 BlockEditorView.performAttributeOnly 内调用（对账静默）。
// M2-5 的「标记调淡」形态随标记隐藏被本票取代（历史记录见验证报告）。
final class MarkdownStyler {
    // 段落样式缓存：同参数复用同一 NSParagraphStyle，避免每次新建。
    private struct PSKey: Hashable {
        var lineSpacing: CGFloat
    }
    private var psCache: [PSKey: NSParagraphStyle] = [:]

    // 粗斜体合成字体缓存（基底字体 + traits）。
    private var fontCache: [String: UIFont] = [:]

    // MARK: 块级基底

    // 块的基底属性（可见文字共用）：字体/颜色/行高按块类型取桌面排版令牌。
    func baseAttributes(for block: LiveBlock) -> [NSAttributedString.Key: Any] {
        [
            .font: Self.baseFont(for: block),
            .foregroundColor: Self.baseColor(for: block),
            .paragraphStyle: paragraphStyle(for: block),
        ]
    }

    static func baseFont(for block: LiveBlock) -> UIFont {
        switch block.kind {
        case .heading:
            return Theme.Typography.headingFont(level: block.level)
        case .code:
            return Theme.Typography.codeFont
        default:
            return Theme.Typography.bodyFont
        }
    }

    static func baseColor(for block: LiveBlock) -> UIColor {
        block.kind == .quote ? Theme.Semantic.textMuted : Theme.Semantic.text
    }

    // 段行高：标题 1.25（桌面 styleHeading），代码 codeLineHeight，其余正文行高。
    private func paragraphStyle(for block: LiveBlock) -> NSParagraphStyle {
        let font = Self.baseFont(for: block)
        let multiplier: CGFloat
        switch block.kind {
        case .heading:
            multiplier = 1.25
        case .code:
            multiplier = Theme.Typography.codeLineHeight
        default:
            multiplier = Theme.Typography.lineHeight
        }
        let spacing = max(0, font.pointSize * multiplier - font.lineHeight)
        let key = PSKey(lineSpacing: spacing)
        if let cached = psCache[key] { return cached }
        let mutable = NSMutableParagraphStyle()
        mutable.lineSpacing = spacing
        let style = mutable as NSParagraphStyle
        psCache[key] = style
        return style
    }

    // MARK: 投影样式上缓冲

    // 把投影行内样式落到可见缓冲：runs 从基底开始，样式逐层叠加拆分
    //（setAttributes 语义），幂等跳过无变化的写入。返回是否有写入。
    @discardableResult
    func apply(_ storage: NSMutableAttributedString, block: LiveBlock, projection: LiveProjection) -> Bool {
        guard storage.length > 0 else { return false }
        storage.beginEditing()
        defer { storage.endEditing() }
        var wrote = false
        let base = baseAttributes(for: block)
        var cursor = 0
        for style in projection.styles {
            let start = min(max(style.start, 0), storage.length)
            let end = min(max(style.end, start), storage.length)
            if start > cursor {
                wrote = Self.writeRun(base, range: NSRange(location: cursor, length: start - cursor), on: storage) || wrote
            }
            wrote = Self.writeRun(Self.overlayAttributes(for: style, base: base, block: block),
                                  range: NSRange(location: start, length: end - start), on: storage) || wrote
            cursor = end
        }
        if cursor < storage.length {
            wrote = Self.writeRun(base, range: NSRange(location: cursor, length: storage.length - cursor), on: storage) || wrote
        }
        return wrote
    }

    // 插入点属性（typingAttributes 用）：新键入字符延续所在样式的字体/颜色。
    func attributesAtCaret(_ caret: Int, block: LiveBlock, projection: LiveProjection) -> [NSAttributedString.Key: Any] {
        var attrs = baseAttributes(for: block)
        for style in projection.styles where caret >= style.start && caret <= style.end {
            for (key, value) in Self.overlayAttributes(for: style, base: baseAttributes(for: block), block: block) {
                attrs[key] = value
            }
        }
        return attrs
    }

    static func overlayAttributes(for style: LiveStyleRange, base: [NSAttributedString.Key: Any], block: LiveBlock) -> [NSAttributedString.Key: Any] {
        var attrs = base
        var cache: [String: UIFont] = [:]
        switch style.kind {
        case .bold:
            if let font = base[.font] as? UIFont, let composed = Self.font(baseFont: font, traits: .traitBold, cache: &cache) {
                attrs[.font] = composed
            }
        case .italic:
            if let font = base[.font] as? UIFont, let composed = Self.font(baseFont: font, traits: .traitItalic, cache: &cache) {
                attrs[.font] = composed
            }
        case .code:
            attrs[.font] = Theme.Typography.codeFont
            attrs[.backgroundColor] = Theme.Semantic.codeBackground
            attrs[.foregroundColor] = Theme.Semantic.text
        case .link:
            attrs[.foregroundColor] = Theme.Semantic.accent
        }
        return attrs
    }

    static func font(baseFont: UIFont, traits: UIFontDescriptor.SymbolicTraits, cache: inout [String: UIFont]) -> UIFont? {
        let key = "\(baseFont.fontDescriptor.postscriptName)|\(traits.rawValue)"
        if let cached = cache[key] { return cached }
        guard let descriptor = baseFont.fontDescriptor.withSymbolicTraits(baseFont.fontDescriptor.symbolicTraits.union(traits)) else {
            return nil
        }
        let font = UIFont(descriptor: descriptor, size: baseFont.pointSize)
        cache[key] = font
        return font
    }

    // 样式器托管的属性键；其余键（组件字符的 .attachment 等）不属于样式器，
    // 写入时必须原样保留，否则收簇后附件 tokenRaw 丢失。
    private static let managedKeys: Set<NSAttributedString.Key> = [
        .font, .foregroundColor, .backgroundColor, .paragraphStyle,
    ]

    // 幂等写入：区间当前属性与目标一致（托管键相等且覆盖全区间）时跳过。
    // 非托管键必须保留（M2-5 同一硬约束）。
    static func writeRun(_ attrs: [NSAttributedString.Key: Any], range: NSRange, on storage: NSMutableAttributedString) -> Bool {
        guard range.length > 0, range.location >= 0, range.location + range.length <= storage.length else { return false }
        var effective = NSRange()
        guard let current = storage.attributes(at: range.location, effectiveRange: &effective) as? [NSAttributedString.Key: Any] else {
            storage.setAttributes(attrs, range: range)
            return true
        }
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
}

// MARK: - 非焦点块渲染（M2-6）

// 用投影构建非焦点块的富文本：与焦点块同一套主题令牌与行内样式；
// 时间占位渲染为胶囊附件（不可编辑态，点按开弹层属 M3）。
enum LiveBlockRender {
    static let styler = MarkdownStyler()

    static func attributedString(for block: LiveBlock, projection: LiveProjection, traits: UITraitCollection) -> NSAttributedString {
        let out = NSMutableAttributedString(string: projection.text, attributes: styler.baseAttributes(for: block))
        // 行内样式逐段套用（与焦点块同一 overlay 逻辑）。
        for style in projection.styles {
            guard style.start < projection.text.utf16.count else { continue }
            let length = min(style.end, projection.text.utf16.count) - style.start
            guard length > 0 else { continue }
            let attrs = stylerAttributes(style: style, block: block)
            out.addAttributes(attrs, range: NSRange(location: style.start, length: length))
        }
        // 时间占位 → 胶囊附件（tokenRaw 从投影单元的源码区间取）。
        for at in projection.objects {
            guard projection.units.indices.contains(at) else { continue }
            let unit = projection.units[at]
            let rawUnits = Array(block.raw.utf16)
            guard unit.start >= 0, unit.end <= rawUnits.count else { continue }
            let tokenRaw = String(decoding: rawUnits[unit.start..<unit.end], as: UTF16.self)
            let attachment = TimeTokenAttachment(tokenRaw: tokenRaw)
            attachment.refreshAppearance(traits: traits)
            let u = Array(out.string.utf16)
            guard at < u.count, u[at] == LiveSyntax.objectRune else { continue }
            let imageAttachment = NSAttributedString(attachment: attachment)
            out.replaceCharacters(in: NSRange(location: at, length: 1), with: imageAttachment)
        }
        return out
    }

    static func stylerAttributes(style: LiveStyleRange, block: LiveBlock) -> [NSAttributedString.Key: Any] {
        var cache: [String: UIFont] = [:]
        let base = styler.baseAttributes(for: block)
        switch style.kind {
        case .bold:
            if let font = base[.font] as? UIFont, let composed = MarkdownStyler.font(baseFont: font, traits: .traitBold, cache: &cache) {
                return [.font: composed]
            }
            return [:]
        case .italic:
            if let font = base[.font] as? UIFont, let composed = MarkdownStyler.font(baseFont: font, traits: .traitItalic, cache: &cache) {
                return [.font: composed]
            }
            return [:]
        case .code:
            return [.font: Theme.Typography.codeFont, .backgroundColor: Theme.Semantic.codeBackground]
        case .link:
            return [.foregroundColor: Theme.Semantic.accent]
        }
    }
}

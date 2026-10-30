import XCTest
@testable import Zakki

// 块内可见文字样式测试（M2-6）：M2-5 的「标记调淡」形态已被标记隐藏取代，
// 对应断言迁移为「标记不在可见文字中」；标题字号/粗斜体/行内代码底色/链接
// 强调色/引用次要色等非标记断言在投影样式下等价保留。样式不改字符、不进
// 撤销栈、无写回的不变式随容器测试覆盖。
@MainActor
final class MarkdownStylerTests: XCTestCase {
    private let styler = MarkdownStyler()

    @MainActor
    private func styled(_ raw: String) -> (NSMutableAttributedString, LiveProjection, LiveBlock) {
        let list = LiveBlocks.split(raw)
        let block = list.blocks[0]
        let proj = LiveProjection.project(block)
        let out = NSMutableAttributedString(string: proj.text, attributes: styler.baseAttributes(for: block))
        styler.apply(out, block: block, projection: proj)
        return (out, proj, block)
    }

    private func font(at range: NSRange, in text: NSAttributedString) -> UIFont? {
        text.attribute(.font, at: range.location, effectiveRange: nil) as? UIFont
    }

    private func resolved(_ color: Any?, dark: Bool) -> UIColor? {
        let traits = UITraitCollection(userInterfaceStyle: dark ? .dark : .light)
        return (color as? UIColor)?.resolvedColor(with: traits)
    }

    private func fg(at range: NSRange, in text: NSAttributedString, dark: Bool = false) -> UIColor? {
        resolved(text.attribute(.foregroundColor, at: range.location, effectiveRange: nil), dark: dark)
    }

    private func bg(at range: NSRange, in text: NSAttributedString) -> UIColor? {
        resolved(text.attribute(.backgroundColor, at: range.location, effectiveRange: nil), dark: false)
    }

    // MARK: 标记不可见（M2-5 调淤断言的隐藏等价）

    func testMarkersAreAbsentFromVisibleText() {
        // 块级标记不出现在可见文字里（M2-5 调淤断言的隐藏等价）。
        let cases: [(raw: String, hiddenMarkers: [String], visible: String)] = [
            ("# 标题", ["#"], "标题"),
            ("## 二级", ["#"], "二级"),
            ("- 列表", ["- "], "列表"),
            ("1. 有序", [], "有序"),
            ("- [ ] 任务", ["- [ ]", "[ ]"], "任务"),
            ("> 引用", [">"], "引用"),
        ]
        for tc in cases {
            let (text, proj, _) = styled(tc.raw)
            XCTAssertEqual(text.string, proj.text, "raw \(tc.raw) 样式不改字符")
            XCTAssertEqual(proj.text, tc.visible, "raw \(tc.raw) 可见文字")
            for marker in tc.hiddenMarkers {
                XCTAssertFalse(text.string.contains(marker), "raw \(tc.raw) 不含标记 \(marker)")
            }
        }
    }

    func testIncompleteSyntaxStaysVisibleUntilComplete() {
        // 输入中（语法不完整）标记是字面文本；补全后重投影即隐藏（成型时机）。
        let before = styled("**abc")
        XCTAssertEqual(before.0.string, "**abc", "不完整标记保持字面可见")
        let after = styled("**abc**")
        XCTAssertEqual(after.0.string, "abc", "补全后标记消失")
        let font = font(at: NSRange(location: 0, length: 3), in: after.0)
        XCTAssertTrue(font?.fontDescriptor.symbolicTraits.contains(.traitBold) ?? false, "补全后内容加粗")
    }

    // MARK: 块级基底（标题/引用/代码）

    func testHeadingVisibleTextUsesHeadingFont() {
        let (text, proj, block) = styled("## 二级标题")
        XCTAssertEqual(proj.text, "二级标题")
        let font = font(at: NSRange(location: 0, length: 4), in: text)
        let expectedH2 = Theme.Typography.headingFont(level: 2)
        XCTAssertEqual(font?.pointSize, expectedH2.pointSize, "标题字号")
        XCTAssertTrue(font?.fontDescriptor.symbolicTraits.contains(.traitBold) ?? false, "标题字重（TextKit 1 对 CJK 做解析回退，按渲染等价断言）")
        XCTAssertEqual(fg(at: NSRange(location: 0, length: 4), in: text), color(Theme.classicNeutralLight.text))
    }

    func testQuoteVisibleTextIsMuted() {
        let (text, _, _) = styled("> 引用文字")
        XCTAssertEqual(text.string, "引用文字", "> 不可见")
        XCTAssertEqual(fg(at: NSRange(location: 0, length: 4), in: text), color(Theme.classicNeutralLight.textMuted), "引用次要色")
        XCTAssertEqual(fg(at: NSRange(location: 0, length: 4), in: text, dark: true), color(Theme.classicNeutralDark.textMuted), "深色取深色值")
    }

    func testCodeBlockContentIsMono() {
        let list = LiveBlocks.split("```swift\nlet x = 1\n```")
        let block = list.blocks[0]
        let proj = LiveProjection.project(block)
        XCTAssertEqual(proj.text, "let x = 1", "围栏行不可见")
        let text = NSMutableAttributedString(string: proj.text, attributes: styler.baseAttributes(for: block))
        styler.apply(text, block: block, projection: proj)
        XCTAssertEqual(font(at: NSRange(location: 0, length: 9), in: text), Theme.Typography.codeFont, "内容行 mono")
    }

    // MARK: 行内样式

    func testBoldAsteriskSyntax() {
        let (text, proj, _) = styled("**b1** 尾")
        XCTAssertTrue(proj.text.hasPrefix("b1"), "标记隐藏")
        let range = NSRange(location: 0, length: 2)
        let font = font(at: range, in: text)
        XCTAssertTrue(font?.fontDescriptor.symbolicTraits.contains(.traitBold) ?? false, "加粗")
        XCTAssertEqual(font?.pointSize, Theme.Typography.bodySize, "粗体不放大")
        XCTAssertEqual(fg(at: range, in: text), color(Theme.classicNeutralLight.text))
    }

    func testAdjacentUnderscoresFollowDesktopLiteralRule() {
        // 桌面 inlineSpans：邻接 __ 不构成粗体（第二个 _ 起斜体，首尾 _ 字面可见）。
        let (text, proj, _) = styled("__b2__ 尾")
        XCTAssertEqual(proj.text, "_b2_ 尾", "桌面同款可见文字")
        let italic = (proj.text as NSString).range(of: "b2")
        XCTAssertTrue(font(at: italic, in: text)?
            .fontDescriptor.symbolicTraits.contains(.traitItalic) ?? false, "内部斜体")
    }

    func testItalicBothSyntaxes() {
        for raw in ["*i1* x", "_i2_ x"] {
            let (text, proj, _) = styled(raw)
            XCTAssertEqual(proj.text.hasPrefix("i"), true, "\(raw) 标记隐藏")
            let font = font(at: NSRange(location: 0, length: 2), in: text)
            XCTAssertTrue(font?.fontDescriptor.symbolicTraits.contains(.traitItalic) ?? false, "\(raw) 斜体")
        }
    }

    func testUnmatchedMarkersStayLiteralNotStyled() {
        let (text, proj, _) = styled("2 * 3 = 6")
        XCTAssertEqual(proj.text, "2 * 3 = 6", "孤星字面保留")
        let font = font(at: NSRange(location: 2, length: 1), in: text)
        XCTAssertEqual(font, Theme.Typography.bodyFont, "未匹配标记无样式")
    }

    func testIntrawordMarkersFollowDesktopInlineSpans() {
        // 桌面 live 预览（inlineSpans）无词边界规则：词中 _ 与 * 均成斜体，
        // 标记隐藏（M2-5 的 CommonMark 词边界口径只属源码高亮，本票按桌面口径）。
        let snake = styled("snake_case_name")
        XCTAssertEqual(snake.0.string, "snakecasename", "桌面同款可见文字")
        let italic = (snake.0.string as NSString).range(of: "case")
        XCTAssertTrue(font(at: italic, in: snake.0)?
            .fontDescriptor.symbolicTraits.contains(.traitItalic) ?? false, "词中下划线斜体")
        let star = styled("a*b*c")
        XCTAssertEqual(star.0.string, "abc")
        XCTAssertTrue(font(at: NSRange(location: 1, length: 1), in: star.0)?
            .fontDescriptor.symbolicTraits.contains(.traitItalic) ?? false, "词中星号斜体")
    }

    func testInlineCodeStylingWithoutBackticks() {
        let (text, proj, _) = styled("x `code` y")
        XCTAssertEqual(proj.text, "x code y", "反引号不可见")
        let codeRange = NSRange(location: 2, length: 4)
        XCTAssertEqual(font(at: codeRange, in: text), Theme.Typography.codeFont, "行内代码 mono")
        XCTAssertEqual(bg(at: codeRange, in: text), color(Theme.classicNeutralLight.codeBackground), "行内代码灰底")
        XCTAssertEqual(fg(at: codeRange, in: text), color(Theme.classicNeutralLight.text), "内容保持正文色")
    }

    func testLinkDisplayTextAccentWithoutMarkers() {
        let (text, proj, _) = styled("see [docs](http://e.com) now")
        XCTAssertEqual(proj.text, "see docs now", "[]() 不可见")
        let display = (proj.text as NSString).range(of: "docs")
        XCTAssertEqual(fg(at: display, in: text), color(Theme.classicNeutralLight.accent), "链接显示文字 accent")
        XCTAssertEqual(font(at: display, in: text), Theme.Typography.bodyFont)
    }

    // MARK: 保护区不解析（标记在代码/链接内是字面文本）

    func testBoldMarkersInsideInlineCodeStayLiteralAndVisible() {
        // 行内代码是字面文本域：内部的 ** 可见且不解析（桌面 spanCode 同款）。
        let (text, proj, _) = styled("a `b **c** d` e")
        XCTAssertTrue(proj.text.contains("**c**"), "代码内标记字面可见")
        let codeRange = (proj.text as NSString).range(of: "b **c** d")
        XCTAssertEqual(font(at: codeRange, in: text), Theme.Typography.codeFont, "整段 mono")
        XCTAssertFalse(font(at: codeRange, in: text)?
            .fontDescriptor.symbolicTraits.contains(.traitBold) ?? true, "代码内粗体不解析")
    }

    func testMarkersInsideLinkStayLiteral() {
        // 桌面 inlineSpans：链接 span 吞掉整体，内部标记字面可见、只按链接样式。
        let (text, proj, _) = styled("[**b**](http://e.com)")
        XCTAssertEqual(proj.text, "**b**", "桌面同款可见文字")
        XCTAssertEqual(fg(at: NSRange(location: 0, length: 5), in: text), color(Theme.classicNeutralLight.accent), "链接样式")
        XCTAssertFalse(font(at: NSRange(location: 0, length: 5), in: text)?
            .fontDescriptor.symbolicTraits.contains(.traitBold) ?? true, "链接内粗体不解析")
    }

    func testNoInlineParsingInsideFence() {
        let list = LiveBlocks.split("```\n**x**\n```")
        let block = list.blocks[0]
        let proj = LiveProjection.project(block)
        XCTAssertEqual(proj.text, "**x**", "围栏内容字面可见")
        XCTAssertTrue(proj.styles.isEmpty, "围栏内容不做行内解析")
    }

    // MARK: 单元格渲染（非焦点块）

    func testRenderedCellCarriesSameTokens() {
        let raw = "标题任务 @2026-01-01 10:00 尾"
        let list = LiveBlocks.split(raw)
        let block = list.blocks[0]
        let proj = LiveProjection.project(block)
        let attr = LiveBlockRender.attributedString(for: block, projection: proj, traits: UITraitCollection.current)
        XCTAssertEqual(attr.string, "标题任务 \u{FFFC} 尾", "非焦点块令牌渲染为胶囊占位")
        var hasAttachment = false
        attr.enumerateAttribute(.attachment, in: NSRange(location: 0, length: attr.length)) { value, _, _ in
            if value is TimeTokenAttachment { hasAttachment = true }
        }
        XCTAssertTrue(hasAttachment, "胶囊附件存在")
    }

    func testCellSpacingFollowsDesktopGapRules() {
        let list = LiveBlocks.split("甲\n\n\n乙\n- 项\n- 项二\n```go\ncode\n```\n尾")
        let blocks = list.blocks
        // 甲 → 分隔空行：两倍块距（分隔行 0 高）。
        XCTAssertEqual(LiveCellSpacing.gap(after: 0, blocks: blocks), Theme.Typography.blockGap * 2, accuracy: 0.5)
        // 真空段（非分隔）与下一段之间完整块距；分隔行自身 0 尾距由
        // LiveBlankCell.configure 承担（isSeparator → tailGap 0）。
        XCTAssertEqual(LiveCellSpacing.gap(after: 2, blocks: blocks), Theme.Typography.blockGap, accuracy: 0.5)
        // 同一列表相邻项收紧到四分之一。
        let listIdx = blocks.firstIndex { $0.kind == .list }!
        XCTAssertEqual(LiveCellSpacing.gap(after: listIdx, blocks: blocks), Theme.Typography.blockGap * 0.25, accuracy: 0.5)
        // 末行 0。
        XCTAssertEqual(LiveCellSpacing.gap(after: blocks.count - 1, blocks: blocks), 0)
    }

    // MARK: 粗斜体合成缓存

    func testFontCompositionCacheStable() {
        var cache: [String: UIFont] = [:]
        let base = Theme.Typography.bodyFont
        let first = MarkdownStyler.font(baseFont: base, traits: .traitBold, cache: &cache)
        let second = MarkdownStyler.font(baseFont: base, traits: .traitBold, cache: &cache)
        XCTAssertEqual(first, second)
        XCTAssertTrue(first?.fontDescriptor.symbolicTraits.contains(.traitBold) ?? false)
        XCTAssertEqual(first?.pointSize, base.pointSize)
    }

    private func color(_ c: ThemeRGB) -> UIColor { c.uiColor }
}

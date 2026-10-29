import XCTest
@testable import Zakki

// M2-5 实时样式测试：属性正确性（块级/行内/保护区/标记调灰）、关键不变式
//（字符逐字不变、撤销栈不变、无写回）、门控与增量路径、明暗重上、性能。
final class MarkdownStylerTests: XCTestCase {
    // MARK: 取属性助手

    @MainActor
    private func makeEditor(raw: String, writable: Bool = true) -> EditorTextView {
        EditorTextView(raw: raw, writable: writable)
    }

    private func font(at range: NSRange, in editor: EditorTextView) -> UIFont? {
        editor.textStorage.attribute(.font, at: range.location, effectiveRange: nil) as? UIFont
    }

    private func resolved(_ color: Any?, traits: UITraitCollection) -> UIColor? {
        (color as? UIColor)?.resolvedColor(with: traits)
    }

    private func fg(at range: NSRange, in editor: EditorTextView) -> UIColor? {
        resolved(editor.textStorage.attribute(.foregroundColor, at: range.location, effectiveRange: nil), traits: editor.traitCollection)
    }

    private func bg(at range: NSRange, in editor: EditorTextView) -> UIColor? {
        resolved(editor.textStorage.attribute(.backgroundColor, at: range.location, effectiveRange: nil), traits: editor.traitCollection)
    }

    private func paragraph(at range: NSRange, in editor: EditorTextView) -> NSParagraphStyle? {
        editor.textStorage.attribute(.paragraphStyle, at: range.location, effectiveRange: nil) as? NSParagraphStyle
    }

    private func range(_ text: String, of needle: String) -> NSRange {
        (text as NSString).range(of: needle)
    }

    private func color(_ c: ThemeRGB) -> UIColor { c.uiColor }

    // MARK: 块级：标题

    @MainActor
    func testHeadingFontsAndMutedMarkers() {
        let lines = (1...6).map { String(repeating: "#", count: $0) + " H\($0)" }
        let raw = lines.joined(separator: "\n")
        let editor = makeEditor(raw: raw)
        var offset = 0
        for (idx, line) in lines.enumerated() {
            let level = idx + 1
            let markerLen = level
            XCTAssertEqual(
                font(at: NSRange(location: offset + markerLen + 1, length: 2), in: editor),
                Theme.Typography.headingFont(level: level),
                "H\(level) 内容字体"
            )
            XCTAssertEqual(fg(at: NSRange(location: offset, length: markerLen), in: editor), color(Theme.classicNeutralLight.textMuted), "H\(level) 标记调灰")
            offset += (line as NSString).length + 1
        }
    }

    @MainActor
    func testNotHeadingWhenHashWithoutSpace() {
        let raw = "#tag"
        let editor = makeEditor(raw: raw)
        XCTAssertEqual(font(at: NSRange(location: 1, length: 3), in: editor), Theme.Typography.bodyFont)
        XCTAssertNotEqual(fg(at: NSRange(location: 0, length: 1), in: editor), color(Theme.classicNeutralLight.textMuted))
    }

    // MARK: 块级：列表与任务（悬挂缩进）

    @MainActor
    func testUnorderedListHangingIndentAndMutedMarker() {
        let raw = "- item one"
        let editor = makeEditor(raw: raw)
        let ps = paragraph(at: NSRange(location: 2, length: 4), in: editor)
        XCTAssertEqual(ps?.firstLineHeadIndent, 0, "首行顶格")
        // 续行缩进到内容列（"- " 的渲染宽度）。
        let contentX = ("- " as NSString).size(withAttributes: [.font: Theme.Typography.bodyFont]).width
        XCTAssertEqual(ps?.headIndent ?? -1, contentX, accuracy: 0.5)
        XCTAssertEqual(fg(at: NSRange(location: 0, length: 1), in: editor), color(Theme.classicNeutralLight.textMuted))
        XCTAssertEqual(fg(at: NSRange(location: 2, length: 4), in: editor), color(Theme.classicNeutralLight.text))
    }

    @MainActor
    func testOrderedListHangingIndent() {
        let raw = "10. item"
        let editor = makeEditor(raw: raw)
        let ps = paragraph(at: NSRange(location: 4, length: 4), in: editor)
        XCTAssertEqual(ps?.firstLineHeadIndent, 0)
        let contentX = ("10. " as NSString).size(withAttributes: [.font: Theme.Typography.bodyFont]).width
        XCTAssertEqual(ps?.headIndent ?? -1, contentX, accuracy: 0.5)
        XCTAssertEqual(fg(at: NSRange(location: 0, length: 3), in: editor), color(Theme.classicNeutralLight.textMuted))
    }

    @MainActor
    func testTaskRowBoxMutedAndHangingIndent() {
        let raw = "- [ ] todo"
        let editor = makeEditor(raw: raw)
        XCTAssertEqual(fg(at: NSRange(location: 0, length: 1), in: editor), color(Theme.classicNeutralLight.textMuted), "任务标记 -")
        XCTAssertEqual(fg(at: NSRange(location: 2, length: 3), in: editor), color(Theme.classicNeutralLight.textMuted), "任务框 [ ]")
        XCTAssertEqual(fg(at: NSRange(location: 6, length: 4), in: editor), color(Theme.classicNeutralLight.text), "任务文字保持正文色")
        let ps = paragraph(at: NSRange(location: 6, length: 4), in: editor)
        let contentX = ("- [ ] " as NSString).size(withAttributes: [.font: Theme.Typography.bodyFont]).width
        XCTAssertEqual(ps?.headIndent ?? -1, contentX, accuracy: 0.5)
        // 勾选态同样式。
        let editor2 = makeEditor(raw: "- [x] done")
        XCTAssertEqual(fg(at: NSRange(location: 2, length: 3), in: editor2), color(Theme.classicNeutralLight.textMuted))
    }

    // MARK: 块级：引用

    @MainActor
    func testQuoteIndentMuted() {
        let raw = "> quoted text"
        let editor = makeEditor(raw: raw)
        let ps = paragraph(at: NSRange(location: 2, length: 6), in: editor)
        XCTAssertEqual(ps?.firstLineHeadIndent ?? -1, Theme.Typography.quoteIndent, accuracy: 0.5)
        XCTAssertEqual(fg(at: NSRange(location: 2, length: 6), in: editor), color(Theme.classicNeutralLight.textMuted), "引用正文次要色")
        XCTAssertEqual(fg(at: NSRange(location: 0, length: 1), in: editor), color(Theme.classicNeutralLight.textMuted))
    }

    // MARK: 块级：围栏代码块

    @MainActor
    func testFenceContentRowsMonoWithBackgroundAndMarkerRowsMuted() {
        let raw = "```swift\nlet x = 1\n```"
        let editor = makeEditor(raw: raw)
        let open = NSRange(location: 0, length: 8)
        let content = NSRange(location: 9, length: 9)
        let close = NSRange(location: 19, length: 3)
        XCTAssertEqual(fg(at: open, in: editor), color(Theme.classicNeutralLight.textMuted), "开栏行只调色")
        XCTAssertEqual(font(at: open, in: editor), Theme.Typography.bodyFont)
        XCTAssertEqual(font(at: content, in: editor), Theme.Typography.codeFont, "内容行 mono")
        XCTAssertEqual(bg(at: content, in: editor), color(Theme.classicNeutralLight.codeBackground), "内容行灰底")
        XCTAssertEqual(fg(at: content, in: editor), color(Theme.classicNeutralLight.text))
        XCTAssertEqual(fg(at: close, in: editor), color(Theme.classicNeutralLight.textMuted), "闭栏行只调色")
        XCTAssertNil(editor.textStorage.attribute(.backgroundColor, at: close.location, effectiveRange: nil), "标记行无灰底")
    }

    @MainActor
    func testTildeFenceAlsoApplies() {
        let raw = "~~~\ncode\n~~~"
        let editor = makeEditor(raw: raw)
        XCTAssertEqual(font(at: NSRange(location: 4, length: 4), in: editor), Theme.Typography.codeFont)
        XCTAssertEqual(bg(at: NSRange(location: 4, length: 4), in: editor), color(Theme.classicNeutralLight.codeBackground))
    }

    // MARK: 块级：段距

    @MainActor
    func testBlockGapBetweenBlocksOnly() {
        let raw = "para one\n\npara two\n- list"
        let editor = makeEditor(raw: raw)
        // para one → 空行：段距记在 para one。
        let ps1 = paragraph(at: NSRange(location: 0, length: 4), in: editor)
        XCTAssertEqual(ps1?.paragraphSpacing ?? -1, Theme.Typography.blockGap, accuracy: 0.5, "块间 12pt")
        // 空行自身不吃段距。
        let psBlank = paragraph(at: NSRange(location: 9, length: 1), in: editor)
        XCTAssertEqual(psBlank?.paragraphSpacing ?? -1, 0, accuracy: 0.5)
        // para two → 列表：异类型块间。
        let ps2 = paragraph(at: NSRange(location: 10, length: 4), in: editor)
        XCTAssertEqual(ps2?.paragraphSpacing ?? -1, Theme.Typography.blockGap, accuracy: 0.5)
        // 段内硬换行（同为段落）不吃段距：a\nb。
        let editor2 = makeEditor(raw: "a\nb")
        let psA = paragraph(at: NSRange(location: 0, length: 1), in: editor2)
        XCTAssertEqual(psA?.paragraphSpacing ?? -1, 0, accuracy: 0.5, "同段落延续行无段距")
    }

    @MainActor
    func testHeadingAlwaysEndsABlock() {
        let raw = "# t1\n# t2"
        let editor = makeEditor(raw: raw)
        let ps = paragraph(at: NSRange(location: 0, length: 3), in: editor)
        XCTAssertEqual(ps?.paragraphSpacing ?? -1, Theme.Typography.blockGap, accuracy: 0.5, "相邻同级标题仍是两块")
    }

    // MARK: 行内

    @MainActor
    func testBoldBothSyntaxes() {
        let raw = "**b1** x __b2__"
        let editor = makeEditor(raw: raw)
        for (needle, base) in [("b1", 0), ("b2", 9)] {
            _ = base
            let r = range(raw, of: needle)
            let f = font(at: r, in: editor)
            XCTAssertTrue(f?.fontDescriptor.symbolicTraits.contains(.traitBold) ?? false, "\(needle) 加粗")
            XCTAssertEqual(f?.pointSize, Theme.Typography.bodySize)
            XCTAssertEqual(fg(at: r, in: editor), color(Theme.classicNeutralLight.text))
            XCTAssertEqual(fg(at: NSRange(location: r.location - 2, length: 2), in: editor), color(Theme.classicNeutralLight.textMuted), "\(needle) 开标记调灰")
            XCTAssertEqual(fg(at: NSRange(location: r.location + r.length, length: 2), in: editor), color(Theme.classicNeutralLight.textMuted), "\(needle) 闭标记调灰")
        }
    }

    @MainActor
    func testItalicBothSyntaxes() {
        let raw = "*i1* x _i2_"
        let editor = makeEditor(raw: raw)
        for needle in ["i1", "i2"] {
            let r = range(raw, of: needle)
            let f = font(at: r, in: editor)
            XCTAssertTrue(f?.fontDescriptor.symbolicTraits.contains(.traitItalic) ?? false, "\(needle) 斜体")
            XCTAssertEqual(fg(at: NSRange(location: r.location - 1, length: 1), in: editor), color(Theme.classicNeutralLight.textMuted))
            XCTAssertEqual(fg(at: NSRange(location: r.location + r.length, length: 1), in: editor), color(Theme.classicNeutralLight.textMuted))
        }
    }

    @MainActor
    func testIntrawordUnderscoreStaysLiteral() {
        let editor = makeEditor(raw: "snake_case_name")
        let r = range("snake_case_name", of: "case")
        XCTAssertFalse(font(at: r, in: editor)?.fontDescriptor.symbolicTraits.contains(.traitItalic) ?? true, "词中下划线不斜体")
        XCTAssertEqual(fg(at: NSRange(location: 5, length: 1), in: editor), color(Theme.classicNeutralLight.text), "未匹配标记不调灰")
    }

    @MainActor
    func testIntrawordAsteriskEmphasizes() {
        let editor = makeEditor(raw: "a*b*c")
        let r = range("a*b*c", of: "b")
        XCTAssertTrue(font(at: r, in: editor)?.fontDescriptor.symbolicTraits.contains(.traitItalic) ?? false, "词中星号按 CommonMark 生效")
    }

    @MainActor
    func testUnmatchedMarkersStayLiteral() {
        let editor = makeEditor(raw: "2 * 3 = 6")
        XCTAssertEqual(fg(at: NSRange(location: 2, length: 1), in: editor), color(Theme.classicNeutralLight.text), "孤星不调灰")
        XCTAssertEqual(font(at: NSRange(location: 2, length: 1), in: editor), Theme.Typography.bodyFont)
    }

    @MainActor
    func testInlineCodeStyling() {
        let raw = "x `code` y"
        let editor = makeEditor(raw: raw)
        let codeContent = NSRange(location: 3, length: 4)
        XCTAssertEqual(font(at: codeContent, in: editor), Theme.Typography.codeFont)
        XCTAssertEqual(bg(at: codeContent, in: editor), color(Theme.classicNeutralLight.codeBackground))
        XCTAssertEqual(fg(at: codeContent, in: editor), color(Theme.classicNeutralLight.text), "行内代码内容保持正文色")
        XCTAssertEqual(fg(at: NSRange(location: 2, length: 1), in: editor), color(Theme.classicNeutralLight.textMuted), "反引号调灰")
        XCTAssertEqual(fg(at: NSRange(location: 7, length: 1), in: editor), color(Theme.classicNeutralLight.textMuted))
    }

    @MainActor
    func testLinkDisplayTextAccent() {
        let raw = "see [docs](http://e.com) now"
        let editor = makeEditor(raw: raw)
        let display = range(raw, of: "docs")
        XCTAssertEqual(fg(at: display, in: editor), color(Theme.classicNeutralLight.accent), "链接显示文字 accent")
        XCTAssertEqual(font(at: display, in: editor), Theme.Typography.bodyFont)
        XCTAssertEqual(fg(at: NSRange(location: 4, length: 1), in: editor), color(Theme.classicNeutralLight.textMuted), "[ 调灰")
        XCTAssertEqual(fg(at: NSRange(location: 10, length: 1), in: editor), color(Theme.classicNeutralLight.textMuted), "]( 调灰")
        XCTAssertEqual(fg(at: NSRange(location: 11, length: 13), in: editor), color(Theme.classicNeutralLight.textMuted), "url 调灰")
    }

    // MARK: 保护区不解析

    @MainActor
    func testBoldMarkersInsideInlineCodeNotParsed() {
        let raw = "a `b **c** d` e"
        let editor = makeEditor(raw: raw)
        let inner = range(raw, of: "**c**")
        XCTAssertEqual(font(at: inner, in: editor), Theme.Typography.codeFont, "行内代码内保持 mono")
        XCTAssertFalse(font(at: inner, in: editor)?.fontDescriptor.symbolicTraits.contains(.traitBold) ?? true, "代码内粗体标记不解析")
        XCTAssertEqual(fg(at: inner, in: editor), color(Theme.classicNeutralLight.text), "代码内不调灰")
    }

    @MainActor
    func testMarkersInsideLinkNotParsed() {
        let raw = "[**b**](http://e.com)"
        let editor = makeEditor(raw: raw)
        let inner = range(raw, of: "**b**")
        XCTAssertFalse(font(at: inner, in: editor)?.fontDescriptor.symbolicTraits.contains(.traitBold) ?? true, "链接文字内粗体标记不解析")
    }

    @MainActor
    func testBoldInsideFenceNotParsed() {
        let raw = "```\n**x**\n```"
        let editor = makeEditor(raw: raw)
        let r = range(raw, of: "**x**")
        XCTAssertEqual(font(at: r, in: editor), Theme.Typography.codeFont)
        XCTAssertFalse(font(at: r, in: editor)?.fontDescriptor.symbolicTraits.contains(.traitBold) ?? true, "围栏内容不做行内解析")
    }

    // MARK: 关键不变式：字符不变、撤销栈不变、无写回

    @MainActor
    func testStylingPreservesStringUndoStackAndNoWriteback() {
        let raw = "# 标题\n\n- 列表 **加粗**\n> 引用 `code`\n```\nfmt.Println()\n```\n末段"
        let editor = makeEditor(raw: raw)
        var wroteBack = false
        editor.onContentChanged = { _ in wroteBack = true }
        let undoBefore = editor.undoUnitCount
        let redoBefore = editor.redoUnitCount
        let stringBefore = editor.textStorage.string
        editor.applyMarkdownThemeRefresh()
        XCTAssertEqual(editor.textStorage.string, stringBefore, "上样式前后字符逐字一致")
        XCTAssertEqual(editor.undoUnitCount, undoBefore, "样式不进撤销栈")
        XCTAssertEqual(editor.redoUnitCount, redoBefore)
        XCTAssertFalse(wroteBack, "样式不触发写回")
        XCTAssertEqual(editor.document.rawText, raw, "raw 投影与磁盘一致")
    }

    @MainActor
    func testTypingAddsExactlyOneUndoUnitDespiteRestyle() {
        let editor = makeEditor(raw: "# head\nbody")
        editor.selectedRange = NSRange(location: 11, length: 0)
        editor.insertText("!")
        XCTAssertEqual(editor.document.rawText, "# head\nbody!")
        XCTAssertEqual(editor.undoUnitCount, 1, "一次键入一个撤销单元")
        editor.undoManager?.undo()
        XCTAssertEqual(editor.document.rawText, "# head\nbody")
    }

    // MARK: 组字门控

    @MainActor
    func testMarkedTextNotStyledUntilUnmark() {
        let editor = makeEditor(raw: "")
        editor.setMarkedText("**组字**", selectedRange: NSRange(location: 6, length: 0))
        XCTAssertNotNil(editor.markedTextRange)
        let r = NSRange(location: 0, length: 6)
        XCTAssertEqual(font(at: r, in: editor), Theme.Typography.bodyFont, "组字期间不上样式")
        XCTAssertEqual(fg(at: r, in: editor), color(Theme.classicNeutralLight.text))
        editor.unmarkText()
        let content = NSRange(location: 2, length: 2)
        XCTAssertTrue(font(at: content, in: editor)?.fontDescriptor.symbolicTraits.contains(.traitBold) ?? false, "上屏后补样式")
    }

    // MARK: 增量路径

    @MainActor
    func testIncrementalRestyleTouchesOnlyEditedLineWindow() {
        let lines = (0..<30).map { "line \($0)" }
        let raw = lines.joined(separator: "\n")
        let editor = makeEditor(raw: raw)
        XCTAssertEqual(editor.markdownStyler.restylePassCount, 1, "init 全量一次")
        // 在第 15 行中部键入一个字符。
        var offset = 0
        for i in 0..<15 { offset += (lines[i] as NSString).length + 1 }
        editor.selectedRange = NSRange(location: offset + 3, length: 0)
        editor.insertText("X")
        XCTAssertEqual(editor.markdownStyler.restylePassCount, 2)
        XCTAssertLessThanOrEqual(editor.markdownStyler.lastStyledLineCount, 4, "增量只覆盖编辑行邻域而非全篇")
        XCTAssertEqual(editor.document.rawText, editor.textStorage.string)
    }

    @MainActor
    func testFenceStateChangeTriggersFullRestyle() {
        let lines = (0..<10).map { "line \($0)" }
        let raw = lines.joined(separator: "\n")
        let editor = makeEditor(raw: raw)
        // 在第 5 行行首开围栏：围栏状态向下游传播 → 整篇重上。
        var offset = 0
        for i in 0..<5 { offset += (lines[i] as NSString).length + 1 }
        editor.selectedRange = NSRange(location: offset, length: 0)
        editor.insertText("```\n")
        XCTAssertEqual(editor.markdownStyler.lastStyledLineCount, 11, "整篇重上（10 行 + 拆行）")
        let contentStart = offset + 4
        XCTAssertEqual(font(at: NSRange(location: contentStart + 2, length: 4), in: editor), Theme.Typography.codeFont, "下游行进入围栏样式")
        // 闭栏后恢复增量。
        let styledBefore = editor.markdownStyler.lastStyledLineCount
        _ = styledBefore
        editor.selectedRange = NSRange(location: contentStart, length: 0)
        editor.insertText("x")
        XCTAssertLessThanOrEqual(editor.markdownStyler.lastStyledLineCount, 4, "围栏内键入仍走增量")
    }

    // MARK: 撤销/重做回放后的样式

    @MainActor
    func testUndoRedoReplayRestoresCorrectStyles() {
        let raw = "**bold** tail"
        let editor = makeEditor(raw: raw)
        editor.selectedRange = NSRange(location: 13, length: 0)
        editor.insertText(" x")
        XCTAssertEqual(editor.document.rawText, "**bold** tail x")
        editor.undoManager?.undo()
        XCTAssertEqual(editor.document.rawText, raw)
        let bold = range(editor.textStorage.string, of: "bold")
        XCTAssertTrue(font(at: bold, in: editor)?.fontDescriptor.symbolicTraits.contains(.traitBold) ?? false, "撤销回放后样式正确")
        editor.undoManager?.redo()
        let bold2 = range(editor.textStorage.string, of: "bold")
        XCTAssertTrue(font(at: bold2, in: editor)?.fontDescriptor.symbolicTraits.contains(.traitBold) ?? false, "重做回放后样式正确")
    }

    // MARK: 明暗切换重上

    @MainActor
    func testDarkModeSwitchRestylesDocument() {
        let raw = "> quote\nplain"
        let editor = makeEditor(raw: raw)
        let window = UIWindow(frame: CGRect(x: 0, y: 0, width: 320, height: 240))
        editor.frame = window.bounds.insetBy(dx: 0, dy: 40)
        window.addSubview(editor)
        window.makeKeyAndVisible()
        window.layoutIfNeeded()
        let passesBefore = editor.markdownStyler.restylePassCount
        editor.overrideUserInterfaceStyle = .dark
        window.layoutIfNeeded()
        XCTAssertGreaterThan(editor.markdownStyler.restylePassCount, passesBefore, "明暗切换触发整篇重上")
        let quote = NSRange(location: 2, length: 5)
        XCTAssertEqual(fg(at: quote, in: editor), color(Theme.classicNeutralDark.textMuted), "深色下引用次要色取深色值")
        window.removeFromSuperview()
    }

    // MARK: 引用竖线 fragment

    @MainActor
    func testQuoteLinesUseBarLayoutFragment() {
        let editor = makeEditor(raw: "> quoted\nplain")
        let window = UIWindow(frame: CGRect(x: 0, y: 0, width: 320, height: 240))
        editor.frame = window.bounds.insetBy(dx: 0, dy: 40)
        window.addSubview(editor)
        window.makeKeyAndVisible()
        window.layoutIfNeeded()
        var sawQuoteBar = false
        var sawPlain = false
        editor.textLayoutManager?.enumerateTextLayoutFragments(
            from: nil, options: [.ensuresLayout]
        ) { fragment in
            if fragment is QuoteBarLayoutFragment { sawQuoteBar = true }
            if type(of: fragment) == NSTextLayoutFragment.self { sawPlain = true }
            return true
        }
        XCTAssertTrue(sawQuoteBar, "引用行走自定义竖线 fragment")
        XCTAssertTrue(sawPlain, "普通行走默认 fragment")
        window.removeFromSuperview()
    }

    // MARK: 组件字符与样式共存

    // 整篇重上不得抹掉组件字符的附件属性（tokenRaw 随附件走，丢了会退化成无主
    // fffc 被对账清除）。setAttributes 语义下这是硬约束。
    @MainActor
    func testThemeRefreshPreservesTokenAttachment() {
        let raw = "keep @2026-01-01 10:00 tail"
        let editor = makeEditor(raw: raw)
        editor.applyMarkdownThemeRefresh()
        let fffc = "\u{FFFC}"
        XCTAssertEqual(editor.textStorage.string, "keep \(fffc) tail")
        let attachment = editor.textStorage.attribute(.attachment, at: 5, effectiveRange: nil) as? TimeTokenAttachment
        XCTAssertEqual(attachment?.tokenRaw, "@2026-01-01 10:00")
        XCTAssertEqual(editor.document.rawText, raw)
        XCTAssertEqual(editor.undoUnitCount, 0, "样式不进撤销栈")
    }

    // MARK: 性能

    @MainActor
    func testFullRestyle2000LinesWithinBudget() {
        // 2000 行混合 Markdown：每 10 行一组（标题/段落/粗斜体/列表/任务/引用/代码块）。
        var lines: [String] = []
        lines.reserveCapacity(2000)
        for i in 0..<200 {
            lines.append("## 第 \(i) 节 **标题**")
            lines.append("正文段落，含 *斜体* 与 `行内代码` 以及 [链接](http://e.com/\(i))。")
            lines.append("- 列表项 \(i)")
            lines.append("- [ ] 任务 \(i)")
            lines.append("> 引用 \(i)")
            lines.append("```swift")
            lines.append("let v\(i) = \(i)")
            lines.append("```")
            lines.append("")
            lines.append("尾段 \(i)")
        }
        let raw = lines.joined(separator: "\n")
        let editor = makeEditor(raw: raw)
        XCTAssertEqual((editor.textStorage.string as NSString).length, (raw as NSString).length)
        // 预热一次后取三次整篇重上的最大值。走生产入口（isReconciling 包裹，
        // 失效通知不重入）：直调 restyleDocument 会在通知里再跑一遍，口径失真。
        // 幂等写入使稳态重上零写入；Debug 模拟器实测约 0.19s（见 PR 实测值），
        // 预算取 2× 余量，用于兜住意外复杂度回退（如 O(n²)）。
        editor.applyMarkdownThemeRefresh()
        var worst: Double = 0
        for _ in 0..<3 {
            let t0 = CFAbsoluteTimeGetCurrent()
            editor.applyMarkdownThemeRefresh()
            let elapsed = CFAbsoluteTimeGetCurrent() - t0
            worst = max(worst, elapsed)
        }
        XCTAssertLessThanOrEqual(worst, 0.4, "2000 行整篇重上应 ≤400ms（Debug 模拟器，实测见 PR）")
        XCTAssertEqual(editor.textStorage.string, raw, "重上前后字符逐字一致")
        XCTAssertEqual(editor.document.rawText, raw)
    }
}

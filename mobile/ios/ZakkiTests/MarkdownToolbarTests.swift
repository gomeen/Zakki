import XCTest
@testable import Zakki

// M2-2 Markdown 快捷栏测试：动作经 MarkdownSnippet 作用于 EditorTextView（走
// insertText 原生记账路径，收组件/撤销语义复用）与纯文本 UITextView（源码模式
// 共用语义）；时间入口只生成展示文本并按既有规则收组件，全程无弹层。
final class MarkdownToolbarTests: XCTestCase {
    private let cluster = "@2026-01-01 10:00"
    private let fffc = "\u{FFFC}"

    @MainActor
    private func makeEditor(raw: String) -> EditorTextView {
        EditorTextView(raw: raw, writable: true)
    }

    // MARK: 前缀类

    // 光标在行中间：前缀插入所在行行首，光标平移到原位。
    @MainActor
    func testHeadingPrefixInsertsAtLineStart() {
        let editor = makeEditor(raw: "first\nsecond")
        editor.selectedRange = NSRange(location: 9, length: 0)
        MarkdownSnippet.apply(.heading1, to: editor)
        XCTAssertEqual(editor.text, "first\n# second")
        XCTAssertEqual(editor.selectedRange.location, 11, "光标平移到插入后的原位")
    }

    // 单行文档：前缀落在文档首。
    @MainActor
    func testTaskPrefixAtDocumentStart() {
        let editor = makeEditor(raw: "todo")
        editor.selectedRange = NSRange(location: 2, length: 0)
        MarkdownSnippet.apply(.task, to: editor)
        XCTAssertEqual(editor.text, "- [ ] todo")
        XCTAssertEqual(editor.selectedRange.location, 8)
    }

    // 前缀作用于含组件的行：既有组件不重复收簇，行内容不丢（坐标映射回归）。
    @MainActor
    func testQuotePrefixOnLineWithTokenKeepsTokenAndTail() {
        let editor = makeEditor(raw: "hi \(cluster) tail")
        editor.selectedRange = NSRange(location: 2, length: 0)
        MarkdownSnippet.apply(.quote, to: editor)
        XCTAssertEqual(editor.document.rawText, "> hi \(cluster) tail")
        XCTAssertEqual(editor.text, "> hi \(fffc) tail")
        XCTAssertTrue(editor.textStorage.attribute(.attachment, at: 5, effectiveRange: nil) is TimeTokenAttachment)
    }

    // MARK: 包裹类

    // 有选区：包裹并重新选中内容。
    @MainActor
    func testBoldWrapsSelection() {
        let editor = makeEditor(raw: "hello world")
        editor.selectedRange = NSRange(location: 6, length: 5)
        MarkdownSnippet.apply(.bold, to: editor)
        XCTAssertEqual(editor.text, "hello **world**")
        XCTAssertEqual(editor.selectedRange, NSRange(location: 8, length: 5), "包裹后重新选中内容")
    }

    // 无选区：插占位对、光标落在对中间。
    @MainActor
    func testCodeWithoutSelectionInsertsPlaceholderPair() {
        let editor = makeEditor(raw: "a")
        editor.selectedRange = NSRange(location: 1, length: 0)
        MarkdownSnippet.apply(.code, to: editor)
        XCTAssertEqual(editor.text, "a``")
        XCTAssertEqual(editor.selectedRange.location, 2, "光标落在占位对中间")
    }

    // 包裹选区含组件字符：组件不动，标记包在投影两侧，raw 往返一致。
    @MainActor
    func testBoldWrapKeepsTokenInsideSelection() {
        let editor = makeEditor(raw: "x \(cluster) y")
        editor.selectedRange = NSRange(location: 1, length: 3) // " " + 组件 + " "
        MarkdownSnippet.apply(.bold, to: editor)
        XCTAssertEqual(editor.text, "x** \(fffc) **y")
        XCTAssertEqual(editor.document.rawText, "x** \(cluster) **y")
        XCTAssertTrue(editor.textStorage.attribute(.attachment, at: 4, effectiveRange: nil) is TimeTokenAttachment)
    }

    // MARK: 插入时间

    // 插入时间：产生合法令牌文本并按既有规则自动收为组件（无弹层——动作只落文本）。
    @MainActor
    func testInsertTimeProducesTokenTextAndCollapses() {
        let editor = makeEditor(raw: "")
        MarkdownSnippet.apply(.insertTime, to: editor)
        let raw = editor.document.rawText
        // 显示层格式：@yyyy-MM-dd HH:mm 加尾随空格（生成待插入文本，不是令牌解析）。
        XCTAssertNotNil(raw.range(of: "^@\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2} $", options: .regularExpression), "raw=\(raw)")
        XCTAssertEqual(editor.text, fffc + " ", "合法令牌自动收为组件，尾随空格留在组件后")
        XCTAssertNotNil(editor.textStorage.attribute(.attachment, at: 0, effectiveRange: nil) as? TimeTokenAttachment)
    }

    // 时间文本确定性：固定时刻与日历下输出次日的墙上时刻。
    func testTimeTokenTextUsesTomorrow() {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "Asia/Shanghai")!
        let now = Date(timeIntervalSince1970: 1_767_225_600) // 2026-01-01 08:00 +08
        XCTAssertEqual(MarkdownSnippet.timeTokenText(now: now, calendar: calendar), "@2026-01-02 08:00 ")
    }

    // 源码模式插时间：只落纯文本，不组件化。
    @MainActor
    func testInsertTimeInSourceModeStaysPlainText() {
        let source = UITextView()
        MarkdownSnippet.apply(.insertTime, to: source)
        XCTAssertEqual(source.text, MarkdownSnippet.timeTokenText())
        XCTAssertFalse(source.text?.contains(fffc) ?? true)
    }

    // MARK: 工具栏接线

    // 按钮点击经 onAction 送达对应动作。
    @MainActor
    func testToolbarButtonDeliversAction() {
        let toolbar = MarkdownToolbar()
        var actions: [MarkdownSnippetAction] = []
        toolbar.onAction = { actions.append($0) }
        XCTAssertEqual(toolbar.buttons.count, 8, "快捷栏共 8 个入口")
        toolbar.buttons[0].sendActions(for: .touchUpInside)
        toolbar.buttons[7].sendActions(for: .touchUpInside)
        XCTAssertEqual(actions.count, 2)
        XCTAssertEqual(actions[0], .heading1)
        XCTAssertEqual(actions[1], .insertTime)
    }

    // 编辑器装配后两种模式的文本视图都挂上快捷栏。
    @MainActor
    func testToolbarAttachedInBothModes() {
        let vc = EditorViewController(raw: "abc", writable: true, reason: "")
        _ = vc.view
        XCTAssertNotNil(vc.editorView?.inputAccessoryView)
        vc.toggleMode()
        XCTAssertNotNil(vc.sourceView?.inputAccessoryView)
    }
}

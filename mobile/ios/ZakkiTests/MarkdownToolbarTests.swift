import XCTest
@testable import Zakki

// M2-6 Markdown 快捷栏测试：源码模式动作经 MarkdownSnippet 作用于纯文本
// UITextView（M2-2 语义原样）；实时模式动作经容器落源码级语义（见
// LiveEditorContainerTests）；时间入口只生成展示文本，全程无弹层。
final class MarkdownToolbarTests: XCTestCase {
    private let cluster = "@2026-01-01 10:00"
    private let fffc = "\u{FFFC}"

    // MARK: 源码模式（纯文本 UITextView，M2-2 语义不变）

    // 光标在行中间：前缀插入所在行行首，光标平移到原位。
    @MainActor
    func testHeadingPrefixInsertsAtLineStart() {
        let source = UITextView()
        source.text = "first\nsecond"
        source.selectedRange = NSRange(location: 9, length: 0)
        MarkdownSnippet.apply(.heading1, to: source)
        XCTAssertEqual(source.text, "first\n# second")
        XCTAssertEqual(source.selectedRange.location, 11, "光标平移到插入后的原位")
    }

    // 单行文档：前缀落在文档首。
    @MainActor
    func testTaskPrefixAtDocumentStart() {
        let source = UITextView()
        source.text = "todo"
        source.selectedRange = NSRange(location: 2, length: 0)
        MarkdownSnippet.apply(.task, to: source)
        XCTAssertEqual(source.text, "- [ ] todo")
        XCTAssertEqual(source.selectedRange.location, 8)
    }

    // MARK: 插入时间

    // 插入时间：产生合法令牌文本；经实时缓冲管线自动收为组件（无弹层）。
    @MainActor
    func testInsertTimeProducesTokenTextAndCollapses() {
        let view = BlockEditorView(writable: true)
        let list = LiveBlocks.split("")
        view.load(projection: LiveProjection.project(list.blocks[0]), block: list.blocks[0], selection: nil)
        MarkdownSnippet.apply(.insertTime, to: view)
        let raw = view.document.rawText
        // 显示层格式：@yyyy-MM-dd HH:mm 加尾随空格（生成待插入文本，不是令牌解析）。
        XCTAssertNotNil(raw.range(of: "^@\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2} $", options: .regularExpression), "raw=\(raw)")
        XCTAssertEqual(view.text, fffc + " ", "合法令牌自动收为组件，尾随空格留在组件后")
        XCTAssertNotNil(view.textStorage.attribute(.attachment, at: 0, effectiveRange: nil) as? TimeTokenAttachment)
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

    // 编辑器装配后两种模式的文本视图都挂上快捷栏（实时为焦点块编辑器）。
    @MainActor
    func testToolbarAttachedInBothModes() {
        let vc = EditorViewController(raw: "abc", writable: true, reason: "")
        _ = vc.view
        vc.liveView?.frame = CGRect(x: 0, y: 0, width: 390, height: 800)
        vc.liveView?.layoutIfNeeded()
        vc.liveView?.rawSelection = NSRange(location: 0, length: 0)
        XCTAssertNotNil(vc.liveView?.activeEditor?.inputAccessoryView, "实时焦点块编辑器挂快捷栏")
        vc.toggleMode()
        XCTAssertNotNil(vc.sourceView?.inputAccessoryView, "源码视图挂快捷栏")
    }
}

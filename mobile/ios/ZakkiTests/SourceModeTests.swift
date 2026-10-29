import XCTest
@testable import Zakki

// M2-2 双模式切换测试：直接驱动 EditorViewController（访问 view 触发装配），
// 验证实时↔源码往返文本无损、光标/选区保留、撤销栈清空与源码模式纯文本语义。
// 横屏不做 UI 自动化（布局断言意义有限），人工验收项见 PR。
final class SourceModeTests: XCTestCase {
    private let cluster = "@2026-01-01 10:00"
    private let fffc = "\u{FFFC}"

    @MainActor
    private func makeViewController(raw: String) -> EditorViewController {
        let vc = EditorViewController(raw: raw, writable: true, reason: "")
        _ = vc.view // 触发 viewDidLoad 装配
        return vc
    }

    @MainActor
    private func type(_ text: String, into editor: EditorTextView) {
        for ch in text {
            editor.insertText(String(ch))
        }
    }

    // MARK: 文本往返

    // 实时→源码→实时：组件、普通文本与未知簇均无损。
    @MainActor
    func testRoundTripPreservesTokensTextAndUnknownCluster() {
        let raw = "before \(cluster) after\n\(cluster) ^abc 尾\n中文🎉"
        let vc = makeViewController(raw: raw)
        vc.toggleMode()
        XCTAssertEqual(vc.mode, .source)
        XCTAssertEqual(vc.sourceView?.text, raw, "源码视图展示原始 Markdown（组件位置为令牌文本）")
        vc.toggleMode()
        XCTAssertEqual(vc.mode, .live)
        XCTAssertEqual(vc.editorView?.document.rawText, raw)
        XCTAssertTrue(vc.editorView?.text?.contains(fffc) ?? false, "实时模式组件照常渲染")
        XCTAssertTrue(vc.editorView?.text?.contains("^abc") ?? false, "未知簇保持文本")
    }

    // 源码模式编辑（含增删令牌文本）→ 切回实时重新收簇。
    @MainActor
    func testSourceEditRebuildsDocumentAndRecollapsesOnReturn() {
        let vc = makeViewController(raw: "hello")
        vc.toggleMode()
        guard let source = vc.sourceView else {
            return XCTFail("缺少源码视图")
        }
        source.selectedRange = NSRange(location: 5, length: 0)
        source.insertText(" \(cluster)")
        XCTAssertEqual(source.text, "hello \(cluster)")
        // 源码中删掉部分令牌文本（尾部 " 10:00" 共 6 字符），切回后不成簇。
        source.selectedRange = NSRange(location: 23, length: 0)
        for _ in 0..<6 {
            source.deleteBackward()
        }
        XCTAssertEqual(source.text, "hello @2026-01-01")
        vc.toggleMode()
        XCTAssertEqual(vc.editorView?.document.rawText, "hello @2026-01-01")
        XCTAssertFalse(vc.editorView?.text?.contains(fffc) ?? true, "不完整簇不组件化")

        // 补完令牌再切回：重新收簇。
        vc.toggleMode()
        guard let source2 = vc.sourceView else {
            return XCTFail("缺少源码视图")
        }
        source2.selectedRange = NSRange(location: (source2.text as NSString?)?.length ?? 0, length: 0)
        source2.insertText(" 10:00")
        vc.toggleMode()
        XCTAssertEqual(vc.editorView?.text, "hello \(fffc)", "完整簇在切回实时时重新收组件")
        XCTAssertEqual(vc.editorView?.document.rawText, "hello \(cluster)")
    }

    // 源码模式纯文本语义：键入令牌字面量不组件化、不触发实时缓冲对账。
    @MainActor
    func testSourceModeKeepsTokenAsPlainText() {
        let vc = makeViewController(raw: "")
        vc.toggleMode()
        guard let source = vc.sourceView else {
            return XCTFail("缺少源码视图")
        }
        source.insertText(cluster)
        XCTAssertEqual(source.text, cluster)
        XCTAssertFalse(source.text?.contains(fffc) ?? true, "源码模式不做令牌收簇")
        XCTAssertNil(vc.editorView, "源码模式不存在实时文档")
    }

    // MARK: 写回上报

    // 源码模式编辑实时上报 raw：保存回调拿 textView.text，而非 document.rawText。
    @MainActor
    func testSourceEditsReportRawViaWriteback() {
        let vc = makeViewController(raw: "abc")
        var reported: [String] = []
        vc.onTextChange = { reported.append($0) }
        vc.toggleMode()
        guard let source = vc.sourceView else {
            return XCTFail("缺少源码视图")
        }
        source.insertText("!")
        XCTAssertEqual(reported.last, "abc!")
    }

    // MARK: 光标/选区

    // 光标在组件后：切源码映射到 raw 簇尾，切回实时恢复到组件后。
    @MainActor
    func testCaretAfterTokenSurvivesRoundTrip() {
        let raw = "a \(cluster) b"
        let vc = makeViewController(raw: raw)
        guard let editor = vc.editorView else {
            return XCTFail("缺少实时视图")
        }
        editor.selectedRange = NSRange(location: 3, length: 0)
        vc.toggleMode()
        let clusterLen = (cluster as NSString).length
        XCTAssertEqual(vc.sourceView?.selectedRange.location, 2 + clusterLen, "组件后的光标映射到 raw 簇尾")
        vc.toggleMode()
        XCTAssertEqual(vc.editorView?.selectedRange.location, 3, "切回实时光标恢复到组件后")
    }

    // 选中组件：往返后仍选中该组件（缓冲 1 字符 ↔ raw 整簇）。
    @MainActor
    func testTokenSelectionSurvivesRoundTrip() {
        let raw = "a \(cluster) b"
        let vc = makeViewController(raw: raw)
        guard let editor = vc.editorView else {
            return XCTFail("缺少实时视图")
        }
        editor.selectedRange = NSRange(location: 2, length: 1)
        vc.toggleMode()
        let clusterLen = (cluster as NSString).length
        XCTAssertEqual(vc.sourceView?.selectedRange, NSRange(location: 2, length: clusterLen), "源码中选中整簇文本")
        vc.toggleMode()
        XCTAssertEqual(vc.editorView?.selectedRange, NSRange(location: 2, length: 1), "切回实时恢复选中组件")
    }

    // raw 簇内选区切回实时钳到组件首（组件不可内部定位）。
    @MainActor
    func testSelectionInsideTokenClampsToTokenStart() {
        let raw = "a \(cluster) b"
        let vc = makeViewController(raw: raw)
        vc.toggleMode()
        guard let source = vc.sourceView else {
            return XCTFail("缺少源码视图")
        }
        source.selectedRange = NSRange(location: 5, length: 2)
        vc.toggleMode()
        XCTAssertEqual(vc.editorView?.selectedRange, NSRange(location: 2, length: 0))
    }

    // MARK: 撤销栈

    // 撤销栈随模式切换清空（视图整体重建的既定语义）。
    @MainActor
    func testUndoStackClearsOnModeSwitch() {
        let vc = makeViewController(raw: "")
        guard let editor = vc.editorView else {
            return XCTFail("缺少实时视图")
        }
        type("hello", into: editor)
        XCTAssertEqual(editor.undoManager?.canUndo, true)
        vc.toggleMode()
        vc.toggleMode()
        XCTAssertEqual(vc.editorView?.undoManager?.canUndo, false, "切回实时后撤销栈为空")
        XCTAssertEqual(vc.editorView?.document.rawText, "hello", "编辑内容不因切换丢失")
    }

    // MARK: 只读

    // 只读文件不提供模式切换与实时文档编辑（保持 M2-1 现状）。
    @MainActor
    func testReadOnlyHasNoToggleEntries() {
        let vc = EditorViewController(raw: "locked", writable: false, reason: "")
        _ = vc.view
        XCTAssertFalse(vc.editorView?.isEditable ?? true)
        vc.toggleMode()
        XCTAssertEqual(vc.mode, .live, "只读不进入源码模式")
        XCTAssertNil(vc.sourceView)
    }
}

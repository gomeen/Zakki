import XCTest
@testable import Zakki

// UITextView 适配层集成测试：在 test host 里直接调输入 API（不依赖成为 key window
// first responder），验证键入收簇、撤销/重做往返、剪贴板 raw 投影与组字门控。
final class EditorTextViewIntegrationTests: XCTestCase {
    private let cluster = "@2026-01-01 10:00"
    private let fffc = "\u{FFFC}"

    @MainActor
    private func makeEditor(raw: String, writable: Bool = true) -> EditorTextView {
        EditorTextView(raw: raw, writable: writable)
    }

    @MainActor
    private func type(_ text: String, into editor: EditorTextView) {
        for ch in text {
            editor.insertText(String(ch))
        }
    }

    // MARK: 键入收簇与撤销往返

    @MainActor
    func testTypingCollapsesIntoAttachmentWithRaw() {
        let editor = makeEditor(raw: "")
        type(cluster, into: editor)
        // 缓冲收成单个 U+FFFC，附件携带原始 Markdown（D1）。
        XCTAssertEqual(editor.text, fffc)
        let attachment = editor.textStorage.attribute(.attachment, at: 0, effectiveRange: nil) as? TimeTokenAttachment
        XCTAssertEqual(attachment?.tokenRaw, cluster)
        XCTAssertEqual(editor.document.rawText, cluster)
    }

    @MainActor
    func testDeleteBackwardRemovesWholeCluster() {
        let editor = makeEditor(raw: "")
        type(cluster, into: editor)
        editor.selectedRange = NSRange(location: 1, length: 0)
        editor.deleteBackward()
        XCTAssertEqual(editor.text, "")
        XCTAssertEqual(editor.document.rawText, "")
        XCTAssertFalse(editor.text?.contains(fffc) ?? true)
    }

    @MainActor
    func testUndoRedoRoundTripRestoresRawAndAttachment() {
        let editor = makeEditor(raw: "")
        type(cluster, into: editor)
        XCTAssertEqual(editor.text, fffc)
        guard let undoManager = editor.undoManager else {
            return XCTFail("编辑器撤销栈不可用")
        }
        XCTAssertTrue(undoManager.canUndo)
        // 第一次撤销：收组件逆操作，缓冲回到原文。
        undoManager.undo()
        XCTAssertEqual(editor.text, cluster)
        XCTAssertEqual(editor.document.rawText, cluster)
        // 重做：附件恢复，tokenRaw 随属性回来。
        XCTAssertTrue(undoManager.canRedo)
        undoManager.redo()
        XCTAssertEqual(editor.text, fffc)
        let attachment = editor.textStorage.attribute(.attachment, at: 0, effectiveRange: nil) as? TimeTokenAttachment
        XCTAssertEqual(attachment?.tokenRaw, cluster)
        XCTAssertEqual(editor.document.rawText, cluster)
        // 再撤销：回到原文（重新键入收簇的语义由文档层测试覆盖）。
        undoManager.undo()
        XCTAssertEqual(editor.text, cluster)
        XCTAssertEqual(editor.document.rawText, cluster)
    }

    @MainActor
    func testUndoOfTypingKeepsDocumentInSync() {
        let editor = makeEditor(raw: "hello ")
        type("abc", into: editor)
        XCTAssertEqual(editor.document.rawText, "hello abc")
        editor.undoManager?.undo()
        XCTAssertEqual(editor.text, "hello ")
        XCTAssertEqual(editor.document.rawText, "hello ")
    }

    // MARK: 剪贴板

    @MainActor
    func testCopyWritesRawProjection() {
        let editor = makeEditor(raw: "a \(cluster) b")
        // 选区覆盖组件与前后各一个普通字符（缓冲坐标）。
        editor.selectedRange = NSRange(location: 1, length: 3)
        editor.copy(nil)
        XCTAssertEqual(UIPasteboard.general.string, " \(cluster) ")
    }

    @MainActor
    func testCutWritesRawProjectionAndDeletes() {
        let editor = makeEditor(raw: "keep \(cluster) tail")
        editor.selectedRange = NSRange(location: 5, length: 1) // 组件字符
        editor.cut(nil)
        XCTAssertEqual(UIPasteboard.general.string, cluster)
        XCTAssertEqual(editor.text, "keep  tail")
        XCTAssertEqual(editor.document.rawText, "keep  tail")
        // 撤销剪切恢复组件。
        editor.undoManager?.undo()
        XCTAssertEqual(editor.text, "keep \(fffc) tail")
        XCTAssertEqual(editor.document.rawText, "keep \(cluster) tail")
    }

    @MainActor
    func testPasteCollapsesScannedClustersAndFiltersForeignObjects() {
        let editor = makeEditor(raw: "")
        UIPasteboard.general.string = "hi \(fffc)\(cluster)\(fffc)!"
        editor.paste(nil)
        XCTAssertEqual(editor.text, "hi \(fffc)!")
        XCTAssertEqual(editor.document.rawText, "hi \(cluster)!")
        // 粘贴是一个撤销单元。
        editor.undoManager?.undo()
        XCTAssertEqual(editor.text, "")
    }

    // MARK: 组字（marked text）

    @MainActor
    func testMarkedTextSuppressesCollapseUntilUnmark() {
        let editor = makeEditor(raw: "")
        // 模拟拼音组字：整簇先以 marked text 进入缓冲。
        editor.setMarkedText(cluster, selectedRange: NSRange(location: (cluster as NSString).length, length: 0))
        XCTAssertNotNil(editor.markedTextRange)
        XCTAssertEqual(editor.text, cluster)
        // 组字期间不收组件。
        XCTAssertFalse(editor.text?.contains(fffc) ?? true)
        XCTAssertEqual(editor.document.rawText, cluster)
        // 组字结束（上屏）：恢复识别并收组件。
        editor.unmarkText()
        XCTAssertEqual(editor.text, fffc)
        let attachment = editor.textStorage.attribute(.attachment, at: 0, effectiveRange: nil) as? TimeTokenAttachment
        XCTAssertEqual(attachment?.tokenRaw, cluster)
        XCTAssertEqual(editor.document.rawText, cluster)
    }

    @MainActor
    func testMarkedTextInsideClusterSuffixDoesNotRecollapse() {
        let editor = makeEditor(raw: "")
        type(cluster, into: editor)
        XCTAssertEqual(editor.text, fffc)
        // 组件后继续组字（如继续输入拼音）：不触发重扫重排。
        editor.setMarkedText("ceshi", selectedRange: NSRange(location: 5, length: 0))
        XCTAssertEqual(editor.text, fffc + "ceshi")
        XCTAssertEqual(editor.document.rawText, cluster + "ceshi")
        editor.unmarkText()
        XCTAssertEqual(editor.text, fffc + "ceshi")
        XCTAssertEqual(editor.document.rawText, cluster + "ceshi")
    }

    // MARK: 只读

    @MainActor
    func testReadOnlyEditorIsNotEditableAndCutDoesNothing() {
        UIPasteboard.general.string = ""
        let editor = makeEditor(raw: "locked", writable: false)
        // 只读门面是 isEditable=false（键盘输入被系统拒绝）；剪切被本类早退，不写剪贴板不落存储。
        XCTAssertFalse(editor.isEditable)
        editor.selectedRange = NSRange(location: 0, length: 3)
        editor.cut(nil)
        XCTAssertEqual(editor.text, "locked")
        XCTAssertEqual(editor.document.rawText, "locked")
        XCTAssertEqual(UIPasteboard.general.string, "")
    }
}

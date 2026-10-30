import XCTest
@testable import Zakki

// 块编辑器集成测试（M2-6）：在 test host 里直接调输入 API（不依赖成为 key window
// first responder），验证可见缓冲下的键入收簇、剪贴板 raw 投影与组字门控。
// 撤销/重做是容器级文档事务，见 LiveEditorContainerTests。
final class BlockEditorViewTests: XCTestCase {
    private let cluster = "@2026-01-01 10:00"
    private let fffc = "\u{FFFC}"

    @MainActor
    private func makeEditor(_ raw: String, writable: Bool = true) -> BlockEditorView {
        let view = BlockEditorView(writable: writable)
        let list = LiveBlocks.split(raw)
        let block = list.blocks[0]
        view.load(projection: LiveProjection.project(block), block: block, selection: nil)
        return view
    }

    @MainActor
    private func type(_ text: String, into editor: BlockEditorView) {
        for ch in text {
            editor.insertText(String(ch))
        }
    }

    // MARK: 键入收簇

    @MainActor
    func testTypingCollapsesIntoAttachmentWithRaw() {
        let editor = makeEditor("")
        type(cluster, into: editor)
        // 缓冲收成单个 U+FFFC，附件携带原始 Markdown（D1）。
        XCTAssertEqual(editor.text, fffc)
        let attachment = editor.textStorage.attribute(.attachment, at: 0, effectiveRange: nil) as? TimeTokenAttachment
        XCTAssertEqual(attachment?.tokenRaw, cluster)
        XCTAssertEqual(editor.document.rawText, cluster)
    }

    // 空缓冲整段 insertText（快捷栏插时间的程序化路径）照样收簇（iOS 26 空文档
    // 路径走直接存储写规避，语义不变）。
    @MainActor
    func testWholeTokenInsertOnEmptyBufferCollapses() {
        let editor = makeEditor("")
        editor.insertText(cluster)
        XCTAssertEqual(editor.text, fffc)
        XCTAssertEqual(editor.document.rawText, cluster)
    }

    @MainActor
    func testDeleteBackwardRemovesWholeCluster() {
        let editor = makeEditor("")
        type(cluster, into: editor)
        editor.selectedRange = NSRange(location: 1, length: 0)
        editor.deleteBackward()
        XCTAssertEqual(editor.text, "")
        XCTAssertEqual(editor.document.rawText, "")
        XCTAssertFalse(editor.text?.contains(fffc) ?? true)
    }

    // MARK: 剪贴板（raw 投影 = 可见文字 + 完整令牌）

    @MainActor
    func testCopyWritesRawProjection() {
        let editor = makeEditor("a \(cluster) b")
        // 可见缓冲为 "a \(fffc) b"，选区覆盖组件与前后各一个普通字符。
        editor.selectedRange = NSRange(location: 1, length: 3)
        editor.copy(nil)
        XCTAssertEqual(UIPasteboard.general.string, " \(cluster) ")
    }

    @MainActor
    func testCutWritesRawProjectionAndReportsChange() {
        let editor = makeEditor("keep \(cluster) tail")
        editor.selectedRange = NSRange(location: 5, length: 1) // 组件字符
        var reported: [String] = []
        editor.onBufferChange = { raw, _, _, _ in reported.append(raw) }
        editor.cut(nil)
        XCTAssertEqual(UIPasteboard.general.string, cluster)
        XCTAssertEqual(editor.text, "keep  tail")
        XCTAssertEqual(editor.document.rawText, "keep  tail")
        XCTAssertEqual(reported.last, "keep  tail", "剪切经对账上报缓冲变化（容器记账撤销）")
    }

    @MainActor
    func testPasteCollapsesScannedClustersAndFiltersForeignObjects() {
        let editor = makeEditor("")
        UIPasteboard.general.string = "hi \(fffc)\(cluster)\(fffc)!"
        editor.paste(nil)
        XCTAssertEqual(editor.text, "hi \(fffc)!")
        XCTAssertEqual(editor.document.rawText, "hi \(cluster)!")
    }

    // MARK: 组字（marked text）

    @MainActor
    func testMarkedTextSuppressesCollapseUntilUnmark() {
        let editor = makeEditor("")
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
        let editor = makeEditor("")
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

    // MARK: 边界钩子（容器接管跨块语义）

    @MainActor
    func testEnterAtBlockEndAsksContainerFirst() {
        let editor = makeEditor("甲乙")
        var enterAsked = false
        editor.shouldDelegateEnter = {
            enterAsked = true
            return true // 容器已处理（拆块）
        }
        editor.selectedRange = NSRange(location: 2, length: 0)
        editor.insertText("\n")
        XCTAssertTrue(enterAsked, "回车先询问容器（拆块/续行语义）")
        XCTAssertEqual(editor.text, "甲乙", "容器消费后回车不落缓冲")
    }

    @MainActor
    func testEnterFallsThroughToNativeWhenContainerDeclines() {
        let editor = makeEditor("code")
        editor.shouldDelegateEnter = { false } // 容器拒绝（如代码块内原生换行）
        editor.selectedRange = NSRange(location: 1, length: 0)
        editor.insertText("\n")
        XCTAssertEqual(editor.text, "c\node", "容器拒绝后回车走原生路径")
    }

    @MainActor
    func testBackspaceAtStartAsksContainer() {
        let editor = makeEditor("内容")
        var asked = false
        editor.shouldDelegateBackspaceAtStart = {
            asked = true
            return true
        }
        editor.selectedRange = NSRange(location: 0, length: 0)
        editor.deleteBackward()
        XCTAssertTrue(asked, "块首退格先询问容器（并块语义）")
        XCTAssertEqual(editor.text, "内容", "容器消费后退格不落缓冲")
    }

    @MainActor
    func testBackspaceInsideVisibleTextDeletesPairwise() {
        let editor = makeEditor("中文🎉尾")
        // 光标在 🎉 之后（可见 4）：按代理对成对删除。
        editor.selectedRange = NSRange(location: 4, length: 0)
        editor.deleteBackward()
        XCTAssertEqual(editor.text, "中文尾")
    }

    // MARK: 只读

    @MainActor
    func testReadOnlyEditorIsNotEditableAndCutDoesNothing() {
        UIPasteboard.general.string = ""
        let editor = makeEditor("locked", writable: false)
        XCTAssertFalse(editor.isEditable)
        editor.selectedRange = NSRange(location: 0, length: 3)
        editor.cut(nil)
        XCTAssertEqual(editor.text, "locked")
        XCTAssertEqual(editor.document.rawText, "locked")
        XCTAssertEqual(UIPasteboard.general.string, "")
    }

    // MARK: 装载（投影 → 可见缓冲）

    @MainActor
    func testLoadStripsMarkersAndCollapsesTokens() {
        // 块源码带块级/行内标记与令牌：可见缓冲只有可见文字 + U+FFFC。
        let view = BlockEditorView(writable: true)
        let list = LiveBlocks.split("- [ ] 任务 @2026-01-01 10:00 **尾**")
        let block = list.blocks[0]
        view.load(projection: LiveProjection.project(block), block: block, selection: nil)
        XCTAssertEqual(view.text, "任务 \u{FFFC} 尾", "标记不可见，令牌是单组件字符")
        XCTAssertEqual(view.document.rawText, "任务 @2026-01-01 10:00 尾", "文档 raw 展开令牌、不含块级/行内标记")
        XCTAssertNotNil(view.textStorage.attribute(.attachment, at: 3, effectiveRange: nil) as? TimeTokenAttachment)
    }
}

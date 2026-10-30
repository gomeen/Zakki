import XCTest
@testable import Zakki

// M2-6 双模式切换测试：直接驱动 EditorViewController（访问 view 触发装配），
// 验证实时↔源码往返文本无损、光标/选区经 raw 偏移映射保留、撤销栈清空与
// 源码模式纯文本语义。横屏不做 UI 自动化（布局断言意义有限），人工验收项见 PR。
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
    private func type(_ text: String, into editor: BlockEditorView) {
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
        XCTAssertEqual(vc.liveView?.raw, raw, "实时容器 raw 逐字一致")
        // 组件照常识别、未知簇保持文本（块级断言；渲染在激活后可见）。
        let blocks = vc.liveView?.blockList.blocks ?? []
        XCTAssertTrue(blocks.contains { $0.timeTokenRaw == cluster }, "令牌被识别为时间块")
        XCTAssertTrue(blocks.contains { $0.raw.contains("^abc") }, "未知簇保持文本")
        // 激活首块验证渲染投影。
        vc.liveView?.rawSelection = NSRange(location: 0, length: 0)
        XCTAssertTrue(vc.liveView?.activeEditor?.text.contains(fffc) ?? false, "实时模式组件照常渲染")
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
        XCTAssertEqual(vc.liveView?.raw, "hello @2026-01-01")
        vc.liveView?.rawSelection = NSRange(location: ((vc.liveView?.raw ?? "") as NSString).length, length: 0)
        XCTAssertFalse(vc.liveView?.activeEditor?.text.contains(fffc) ?? true, "不完整簇不组件化")

        // 补完令牌再切回：重新收簇。
        vc.toggleMode()
        guard let source2 = vc.sourceView else {
            return XCTFail("缺少源码视图")
        }
        source2.selectedRange = NSRange(location: (source2.text as NSString?)?.length ?? 0, length: 0)
        source2.insertText(" 10:00")
        vc.toggleMode()
        vc.liveView?.rawSelection = NSRange(location: ((vc.liveView?.raw ?? "") as NSString).length, length: 0)
        XCTAssertEqual(vc.liveView?.activeEditor?.text, "hello \(fffc)", "完整簇在切回实时时重新收组件")
        XCTAssertEqual(vc.liveView?.raw, "hello \(cluster)")
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
        XCTAssertNil(vc.liveView, "源码模式不存在实时容器")
    }

    // MARK: 写回上报

    // 源码模式编辑实时上报 raw：保存回调拿 textView.text；实时模式容器上报 raw。
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

    @MainActor
    func testLiveEditsReportRawViaWriteback() throws {
        let vc = makeViewController(raw: "abc")
        var reported: [String] = []
        vc.onTextChange = { reported.append($0) }
        vc.liveView?.rawSelection = NSRange(location: 3, length: 0)
        type("!", into: try XCTUnwrap(vc.liveView?.activeEditor))
        XCTAssertEqual(reported.last, "abc!", "实时键入经容器上报 raw")
    }

    // MARK: 光标/选区

    // 光标在组件后：切源码映射到 raw 簇尾，切回实时恢复到组件后。
    // 簇内 raw 偏移钳到组件后（桌面 visibleOffset 口径；M2-5 曾钳组件首，
    // 本票以桌面为准）。
    @MainActor
    func testCaretAfterTokenSurvivesRoundTrip() {
        let raw = "a \(cluster) b"
        let clusterLen = (cluster as NSString).length
        let vc = makeViewController(raw: raw)
        vc.liveView?.rawSelection = NSRange(location: 2 + clusterLen, length: 0)
        XCTAssertEqual(vc.liveView?.activeEditor?.selectedRange.location, 3, "raw 簇尾映射到组件后")
        vc.toggleMode()
        XCTAssertEqual(vc.sourceView?.selectedRange.location, 2 + clusterLen, "组件后的光标映射到 raw 簇尾")
        vc.toggleMode()
        XCTAssertEqual(vc.liveView?.activeEditor?.selectedRange.location, 3, "切回实时光标恢复到组件后")
        // 簇内偏移单向钳到组件后。
        vc.liveView?.rawSelection = NSRange(location: 5, length: 0)
        XCTAssertEqual(vc.liveView?.activeEditor?.selectedRange.location, 3)
        XCTAssertEqual(vc.liveView?.rawSelection.location, 2 + clusterLen)
    }

    // 选中组件：往返后仍选中该组件（可见 1 字符 ↔ raw 整簇）。
    @MainActor
    func testTokenSelectionSurvivesRoundTrip() {
        let raw = "a \(cluster) b"
        let clusterLen = (cluster as NSString).length
        let vc = makeViewController(raw: raw)
        vc.liveView?.rawSelection = NSRange(location: 2, length: clusterLen)
        XCTAssertEqual(vc.liveView?.activeEditor?.selectedRange, NSRange(location: 2, length: 1), "源码选整簇 → 选中组件")
        vc.toggleMode()
        XCTAssertEqual(vc.sourceView?.selectedRange, NSRange(location: 2, length: clusterLen), "源码中选中整簇文本")
        vc.toggleMode()
        XCTAssertEqual(vc.liveView?.activeEditor?.selectedRange, NSRange(location: 2, length: 1), "切回实时恢复选中组件")
    }

    // raw 簇内选区切回实时扩到整组件（桌面 visibleSelection 同款）。
    @MainActor
    func testSelectionInsideTokenExpandsToWholeComponent() {
        let raw = "a \(cluster) b"
        let vc = makeViewController(raw: raw)
        vc.toggleMode()
        guard let source = vc.sourceView else {
            return XCTFail("缺少源码视图")
        }
        source.selectedRange = NSRange(location: 5, length: 2)
        vc.toggleMode()
        XCTAssertEqual(vc.liveView?.activeEditor?.selectedRange, NSRange(location: 2, length: 1), "簇内选区钳为选中整组件")
    }

    // MARK: 撤销栈

    // 撤销栈随模式切换清空（容器整体重建的既定语义）。
    @MainActor
    func testUndoStackClearsOnModeSwitch() throws {
        let vc = makeViewController(raw: "")
        vc.liveView?.rawSelection = NSRange(location: 0, length: 0)
        type("hello", into: try XCTUnwrap(vc.liveView?.activeEditor))
        XCTAssertEqual(vc.liveView?.canUndo, true)
        vc.toggleMode()
        vc.toggleMode()
        XCTAssertEqual(vc.liveView?.canUndo, false, "切回实时后撤销栈为空")
        XCTAssertEqual(vc.liveView?.raw, "hello", "编辑内容不因切换丢失")
    }

    // MARK: 只读

    // 只读文件不提供模式切换与实时编辑（保持 M2-1 现状）。
    @MainActor
    func testReadOnlyHasNoToggleEntries() {
        let vc = EditorViewController(raw: "locked", writable: false, reason: "")
        _ = vc.view
        vc.liveView?.liveBlockCellTapped(0, visibleCaret: 0)
        XCTAssertNil(vc.liveView?.activeEditor, "只读不进入编辑")
        vc.toggleMode()
        XCTAssertEqual(vc.mode, .live, "只读不进入源码模式")
        XCTAssertNil(vc.sourceView)
    }
}

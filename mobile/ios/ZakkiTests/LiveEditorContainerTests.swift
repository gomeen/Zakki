import XCTest
@testable import Zakki

// 实时预览块容器测试（M2-6）：焦点迁移、键入写回 raw 逐字保真、回车拆块/退格
// 并块（对齐桌面边界用例）、容器级撤销/重做（含焦点移动）、raw 选区映射
//（模式切换）、快捷栏源码级动作、围栏草稿态、渲染标记不可见与性能预算。
@MainActor
final class LiveEditorContainerTests: XCTestCase {
    private let cluster = "@2026-01-01 10:00"
    private let fffc = "\u{FFFC}"

    private func makeContainer(_ raw: String, writable: Bool = true) -> LiveEditorContainer {
        let container = LiveEditorContainer(raw: raw, writable: writable)
        container.frame = CGRect(x: 0, y: 0, width: 390, height: 800)
        container.layoutIfNeeded()
        return container
    }

    // 激活包含 doc 偏移的块并把光标放到偏移处（等价桌面 focusBlock）。
    private func activateAt(_ container: LiveEditorContainer, rawOffset: Int) {
        container.rawSelection = NSRange(location: rawOffset, length: 0)
    }

    private func type(_ text: String, into container: LiveEditorContainer) {
        guard let editor = container.activeEditor else {
            return XCTFail("缺少焦点块编辑器")
        }
        for ch in text {
            editor.insertText(String(ch))
        }
    }

    // MARK: 激活与标记不可见

    func testActivateShowsVisibleTextOnly() {
        let container = makeContainer("# 标题\n\n- [ ] 任务 **加粗**\n> 引用 `码`\n```\ncode\n```\n尾段")
        XCTAssertNil(container.activeEditor, "初始纯预览无焦点块")
        activateAt(container, rawOffset: 2)
        guard let editor = container.activeEditor else { return XCTFail("激活失败") }
        XCTAssertEqual(editor.text, "标题", "焦点块只见可见文字（标记全程隐藏）")
        XCTAssertFalse(editor.text?.contains("#") ?? true)
        XCTAssertEqual(container.raw, "# 标题\n\n- [ ] 任务 **加粗**\n> 引用 `码`\n```\ncode\n```\n尾段", "激活不改 raw")
    }

    func testRenderedCellsHideMarkers() {
        let container = makeContainer("# 标题\n\n- [ ] 任务\n> 引用\n```swift\nlet x = 1\n```\n正 **粗** 尾")
        for (index, block) in container.blockList.blocks.enumerated() where index != container.activeIndex {
            let proj = LiveProjection.project(block)
            let attr = LiveBlockRender.attributedString(for: block, projection: proj, traits: UITraitCollection.current)
            let text = attr.string
            switch block.kind {
            case .heading:
                XCTAssertFalse(text.contains("#"), "标题标记不可见")
            case .task:
                XCTAssertFalse(text.contains("- [ ]"), "任务标记不可见")
            case .quote:
                XCTAssertFalse(text.contains(">"), "引用标记不可见")
            case .para:
                XCTAssertFalse(text.contains("**"), "行内粗体标记不可见")
            case .code:
                XCTAssertFalse(text.contains("```"), "围栏行不可见")
                XCTAssertTrue(text.contains("let x = 1"), "代码内容可见")
            default:
                break
            }
        }
    }

    // MARK: 键入写回

    func testTypingWritesBackExactRawAndKeepsProjection() {
        let container = makeContainer("甲乙")
        activateAt(container, rawOffset: 2)
        type("丙", into: container)
        XCTAssertEqual(container.raw, "甲乙丙", "raw 逐字保真")
        XCTAssertEqual(container.activeEditor?.text, "甲乙丙", "缓冲与投影一致")
        XCTAssertFalse(container.activeEditor?.text.contains("\u{FFFC}") ?? true)
    }

    func testTypingIntoBoldKeepsSourceWrappers() {
        let container = makeContainer("**粗体**")
        activateAt(container, rawOffset: 4) // 可见末尾（raw "粗体**" 后）
        type("文", into: container)
        XCTAssertEqual(container.raw, "**粗体文**", "包裹保留")
        XCTAssertEqual(container.activeEditor?.text, "粗体文")
    }

    func testTypingAfterTokenCollapsesNewToken() {
        let container = makeContainer("正文 \(cluster)")
        activateAt(container, rawOffset: (container.raw as NSString).length)
        type(" \(cluster)", into: container)
        XCTAssertEqual(container.activeEditor?.text, "正文 \(fffc) \(fffc)", "新令牌照常收簇")
        XCTAssertEqual(container.raw, "正文 \(cluster) \(cluster)", "raw 展开两个令牌")
    }

    // MARK: 回车拆块 / 退格并块（容器管线，对齐桌面边界用例）

    func testEnterSplitsAndBackspaceRestoresWithCaret() {
        let container = makeContainer("甲乙")
        activateAt(container, rawOffset: 1)
        container.activeEditor?.insertText("\n")
        XCTAssertEqual(container.raw, "甲\n\n乙")
        guard let editor = container.activeEditor else { return XCTFail() }
        XCTAssertEqual(editor.text, "乙", "焦点移到新块")
        XCTAssertEqual(editor.selectedRange.location, 0, "光标在新块开头")
        // 一次退格还原（桌面 Enter/Backspace round trip）。
        editor.deleteBackward()
        XCTAssertEqual(container.raw, "甲乙")
        XCTAssertEqual(container.activeEditor?.text, "甲乙")
        XCTAssertEqual(container.activeEditor?.selectedRange.location, 1, "光标回到拆分点")
    }

    func testEnterOnListContinuesPrefix() {
        let container = makeContainer("- 甲")
        activateAt(container, rawOffset: 3)
        container.activeEditor?.insertText("\n")
        XCTAssertEqual(container.raw, "- 甲\n- ")
        XCTAssertEqual(container.activeEditor?.text, "", "焦点在新列表项（可见文字为空）")
        type("乙", into: container)
        XCTAssertEqual(container.raw, "- 甲\n- 乙")
    }

    func testBackspaceAtBlockStartMergesParagraphs() {
        let container = makeContainer("甲\n\n乙\n\n尾")
        activateAt(container, rawOffset: 3) // 乙 块首
        container.activeEditor?.deleteBackward()
        XCTAssertEqual(container.raw, "甲乙\n\n尾", "桌面边界用例：段间并块")
        XCTAssertEqual(container.activeEditor?.selectedRange.location, 1, "光标落在上一块末尾")
    }

    func testBackspaceOnBareListMarkerExitsList() {
        let container = makeContainer("甲\n\n- \n\n尾")
        activateAt(container, rawOffset: 3) // "- " 块首
        container.activeEditor?.deleteBackward()
        XCTAssertEqual(container.raw, "甲\n\n\n\n尾")
        XCTAssertEqual(container.activeEditor?.text, "", "标记剥掉后是空段")
    }

    func testCodeBlockBoundaryDoesNotMerge() {
        let container = makeContainer("```\nx\n```\n\n尾")
        activateAt(container, rawOffset: 11) // 尾 块首
        container.activeEditor?.deleteBackward()
        XCTAssertEqual(container.raw, "```\nx\n```\n\n尾", "代码块边界不并块")
        XCTAssertEqual(container.activeEditor?.text, "尾")
    }

    // MARK: 围栏草稿态

    func testFenceDraftStaysLiteralThenAutoClosesOnEnter() {
        let container = makeContainer("")
        activateAt(container, rawOffset: 0)
        type("```", into: container)
        XCTAssertTrue(container.isFenceDraftActive, "键入 ``` 进入草稿态")
        XCTAssertEqual(container.activeEditor?.text, "```", "草稿态字面可见（语法不完整是字面文本）")
        container.activeEditor?.insertText("\n")
        XCTAssertFalse(container.isFenceDraftActive)
        XCTAssertEqual(container.raw, "```\n\n```", "回车自动补闭栏")
        XCTAssertEqual(container.activeEditor?.text, "", "焦点进入代码内容区")
        XCTAssertEqual(container.activeEditor?.selectedRange.location, 0)
    }

    func testCompletedFenceRendersAsCodeBlock() {
        let container = makeContainer("```\nlet x = 1\n```")
        activateAt(container, rawOffset: 13) // 内容行末尾
        guard container.activeEditor != nil else { return XCTFail() }
        XCTAssertEqual(container.activeEditor?.text, "let x = 1", "代码块可编辑区只有内容行（围栏行隐藏）")
        type("2", into: container)
        XCTAssertEqual(container.raw, "```\nlet x = 12\n```")
    }

    // MARK: 容器级撤销/重做

    func testTypingUndoRedoRoundTripMergesCollapse() {
        let container = makeContainer("")
        activateAt(container, rawOffset: 0)
        type(cluster, into: container)
        XCTAssertEqual(container.activeEditor?.text, fffc)
        XCTAssertEqual(container.raw, cluster)
        XCTAssertTrue(container.canUndo)
        container.undo()
        XCTAssertEqual(container.raw, "", "键入+收簇合并为一个事务，一步撤销")
        XCTAssertEqual(container.activeEditor?.text, "")
        container.redo()
        XCTAssertEqual(container.raw, cluster)
        XCTAssertEqual(container.activeEditor?.text, fffc)
        let attachment = container.activeEditor?.textStorage.attribute(.attachment, at: 0, effectiveRange: nil) as? TimeTokenAttachment
        XCTAssertEqual(attachment?.tokenRaw, cluster, "重做恢复附件")
    }

    func testUndoAcrossBlocksRestoresFocusAndCaret() {
        // 桌面 live_history：连续键入与紧随的回车拆块同为 editInput 且光标连续
        // → 合并为一个事务，一次撤销整体回退。
        let container = makeContainer("甲乙")
        activateAt(container, rawOffset: 1)
        type("X", into: container)
        XCTAssertEqual(container.raw, "甲X乙")
        container.activeEditor?.insertText("\n") // 拆块
        XCTAssertEqual(container.raw, "甲X\n\n乙")
        XCTAssertEqual(container.undoStackDepthForTesting, 1, "键入+拆块合并为一个事务")
        container.undo()
        XCTAssertEqual(container.raw, "甲乙", "一次撤销回退键入+拆块")
        XCTAssertEqual(container.activeEditor?.selectedRange.location, 1, "光标回键入点")
        XCTAssertFalse(container.canUndo, "没有更早的事务")
        container.redo()
        XCTAssertEqual(container.raw, "甲X\n\n乙")
        XCTAssertEqual(container.activeEditor?.text, "乙", "重做后焦点在新块")
    }

    func testPasteIsSeparateUndoUnit() {
        let container = makeContainer("hello")
        activateAt(container, rawOffset: 5)
        UIPasteboard.general.string = " 世界 \(cluster)"
        container.activeEditor?.paste(nil)
        XCTAssertEqual(container.activeEditor?.text, "hello 世界 \(fffc)")
        XCTAssertEqual(container.raw, "hello 世界 \(cluster)")
        container.undo()
        XCTAssertEqual(container.raw, "hello", "粘贴（含其触发的收簇）一次整退")
    }

    // MARK: raw 选区映射（模式切换契约）

    func testRawSelectionRoundTripAcrossToken() {
        let container = makeContainer("a \(cluster) b")
        let clusterLen = (cluster as NSString).length
        activateAt(container, rawOffset: 2 + clusterLen) // 组件后（raw 簇尾）
        XCTAssertEqual(container.activeEditor?.selectedRange.location, 3, "raw 簇尾映射到组件后")
        let mapped = container.rawSelection
        XCTAssertEqual(mapped.location, 2 + clusterLen, "可见组件后换算回 raw 簇尾")
        // raw 簇内选区钳到组件整体（桌面 visibleSelection 扩到整组件）。
        container.rawSelection = NSRange(location: 5, length: 2)
        XCTAssertEqual(container.activeEditor?.selectedRange, NSRange(location: 2, length: 1), "簇内选区映射为选中整组件")
        XCTAssertEqual(container.rawSelection, NSRange(location: 2, length: (cluster as NSString).length), "选区换算回 raw 为整簇")
    }

    func testRawSelectionWithoutActiveMapsToDocumentEnd() {
        let container = makeContainer("abc")
        XCTAssertEqual(container.rawSelection.location, 3, "无焦点块映射文末")
    }

    // MARK: 快捷栏（容器级源码语义）

    func testToolbarPrefixPrependsToActiveBlock() {
        let container = makeContainer("正文")
        activateAt(container, rawOffset: 2)
        container.applyToolbarAction(.heading1)
        XCTAssertEqual(container.raw, "# 正文", "前缀作用于块行首")
        XCTAssertEqual(container.activeEditor?.text, "正文", "标记随即隐藏")
        XCTAssertEqual(container.activeEditor?.selectedRange.location, 2, "光标平移不回退")
        // 撤销：结构化命令独立成事务。
        container.undo()
        XCTAssertEqual(container.raw, "正文")
    }

    func testToolbarWrapKeepsVisibleSelection() {
        let container = makeContainer("你好世界")
        activateAt(container, rawOffset: 0)
        container.activeEditor?.selectedRange = NSRange(location: 0, length: 2)
        container.applyToolbarAction(.bold)
        XCTAssertEqual(container.raw, "**你好**世界")
        XCTAssertEqual(container.activeEditor?.text, "你好世界", "标记不可见")
        XCTAssertEqual(container.activeEditor?.selectedRange, NSRange(location: 0, length: 2), "可见选区原位保留")
    }

    func testToolbarInsertTimeCollapsesViaBufferPipeline() {
        let container = makeContainer("")
        activateAt(container, rawOffset: 0)
        container.applyToolbarAction(.insertTime)
        let raw = container.raw
        XCTAssertNotNil(raw.range(of: "^@\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2} $", options: .regularExpression), "raw=\(raw)")
        XCTAssertEqual(container.activeEditor?.text, fffc + " ", "合法令牌自动收为组件")
    }

    func testToolbarWithoutActiveFocusesEndFirst() {
        let container = makeContainer("尾段")
        XCTAssertNil(container.activeEditor)
        container.applyToolbarAction(.bullet)
        XCTAssertEqual(container.raw, "- 尾段")
        XCTAssertEqual(container.activeEditor?.text, "尾段", "无焦点时先激活末块再落动作")
    }

    // MARK: 点击迁移焦点

    func testTapDelegationMovesFocus() {
        let container = makeContainer("甲\n\n乙")
        activateAt(container, rawOffset: 0) // 激活甲
        XCTAssertEqual(container.activeEditor?.text, "甲")
        // 点击乙块（桌面 live_click）：光标按点击位置（这里模拟可见 1）。
        container.liveBlockCellTapped(2, visibleCaret: 1)
        XCTAssertEqual(container.activeEditor?.text, "乙")
        XCTAssertEqual(container.activeEditor?.selectedRange.location, 1)
        XCTAssertEqual(container.raw, "甲\n\n乙", "点击不改 raw")
    }

    func testTapOnTimeBlockPutsCaretAtEnd() {
        let container = makeContainer("甲\n\n任务 \(cluster)")
        container.liveBlockCellTapped(2, visibleCaret: 0)
        XCTAssertEqual(container.activeEditor?.selectedRange.location,
                       container.activeEditor?.text.utf16.count, "时间行首次点击光标落块末（桌面同款）")
    }

    // MARK: 只读

    func testReadOnlyContainerNeverActivates() {
        let container = makeContainer("locked", writable: false)
        container.liveBlockCellTapped(0, visibleCaret: 0)
        XCTAssertNil(container.activeEditor, "只读不进入编辑")
        XCTAssertNil(container.activeProjection)
    }

    // MARK: 跨块方向键

    func testArrowCrossSkipsSeparator() {
        let container = makeContainer("甲\n\n乙")
        activateAt(container, rawOffset: 0)
        container.handleArrowCrossForTesting(1)
        XCTAssertEqual(container.activeEditor?.text, "乙", "块尾向右跨块（跳过分隔空行）")
        XCTAssertEqual(container.activeEditor?.selectedRange.location, 0)
        container.handleArrowCrossForTesting(-1)
        XCTAssertEqual(container.activeEditor?.text, "甲", "块首向左跨块")
        XCTAssertEqual(container.activeEditor?.selectedRange.location, 1)
    }

    // MARK: 样式与写回不变式

    func testStylePassesDoNotTouchRawOrUndoStack() {
        let container = makeContainer("# 标题\n\n- [ ] 任务 **加粗** @2026-01-01 10:00\n尾")
        activateAt(container, rawOffset: 2)
        guard let editor = container.activeEditor else { return XCTFail() }
        let rawBefore = container.raw
        var wroteBack = false
        container.onRawChange = { _ in wroteBack = true }
        container.applyMarkdownThemeRefreshForTesting()
        XCTAssertEqual(container.raw, rawBefore, "样式不改 raw")
        XCTAssertFalse(wroteBack, "样式不触发写回")
        XCTAssertEqual(container.activeEditor?.text, editor.text, "样式不改缓冲文字")
        // 标题块可见文字带标题字体（块级基底样式仍生效）。
        let font = editor.textStorage.attribute(.font, at: 0, effectiveRange: nil) as? UIFont
        XCTAssertEqual(font, Theme.Typography.headingFont(level: 1))
    }

    // MARK: 性能预算

    func testSplitAndPopulation2000LinesWithinBudget() {
        // 2000 行混合 Markdown（与 M2-5 性能测试同一构造）。
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
        // 分块成本（每键全量重切的热路径）。
        let t0 = CFAbsoluteTimeGetCurrent()
        let list = LiveBlocks.split(raw)
        for block in list.blocks where block.startLine < 100 {
            _ = LiveProjection.project(block)
        }
        let splitCost = CFAbsoluteTimeGetCurrent() - t0
        print("PERF split+project2000lines: \(Int(splitCost * 1000))ms")
        XCTAssertLessThanOrEqual(splitCost, 0.25, "2000 行全量分块应 ≤250ms（Debug 模拟器，实测见 PR），实测 \(splitCost * 1000)ms")
        XCTAssertEqual(list.count, 1600, "块数符合预期（每组 8 块）")
        // 容器装配 + 激活末块 + 键入结算（端到端可交互成本）。
        let t1 = CFAbsoluteTimeGetCurrent()
        let container = makeContainer(raw)
        container.layoutIfNeeded()
        activateAt(container, rawOffset: (raw as NSString).length)
        type("X", into: container)
        let interactiveCost = CFAbsoluteTimeGetCurrent() - t1
        print("PERF assemble+activate+type2000lines: \(Int(interactiveCost * 1000))ms")
        XCTAssertLessThanOrEqual(interactiveCost, 2.5, "2000 行装配+激活+键入应 ≤2.5s（Debug 模拟器，实测见 PR），实测 \(interactiveCost * 1000)ms")
        // 稳态键入结算（末段中部的单键写回+重切）。
        var settle: Double = 0
        for _ in 0..<5 {
            let t2 = CFAbsoluteTimeGetCurrent()
            type("Y", into: container)
            settle = max(settle, CFAbsoluteTimeGetCurrent() - t2)
        }
        print("PERF keystroke-settle2000lines(worst of 5): \(Int(settle * 1000))ms")
        XCTAssertEqual(container.raw, raw + "X" + "YYYYY", "长文键入 raw 逐字保真")
    }

    // MARK: 渲染块命中回归

    // 点击坐标必须命中渲染 cell（回归：命中落空时点击无法进入编辑，
    // 用户表现为「预览不能编辑和选中」）。容器挂进真实窗口后对每个
    // 块中心做 hitTest，断言命中视图位于某个 LiveBaseCell 内。
    @MainActor
    func testHitTestFallsIntoRenderedCell() throws {
        let raw = "# 标题一\n\n段落文字，若干内容用来占据实际块高。\n\n> 引用行\n\n- 列表项\n\n```swift\nprint(1)\n```\n\n尾段文字。"
        let container = makeContainer(raw)
        let window = UIWindow(frame: CGRect(x: 0, y: 0, width: 354, height: 800))
        window.addSubview(container)
        window.makeKeyAndVisible()
        container.frame = window.bounds
        container.setNeedsLayout()
        container.layoutIfNeeded()

        let frames = container.blockLayoutFramesForTesting
        XCTAssertGreaterThan(frames.count, 4, "演示文档应有多个块")
        func inCell(_ v: UIView?) -> Bool {
            var cur: UIView? = v
            while let c = cur {
                if c is LiveBaseCell { return true }
                cur = c.superview
            }
            return false
        }
        var checked = 0
        let types = container.mountedCellTypesForTesting
        for (index, frame) in frames.enumerated() where frame.height > 8 {
            // 空白分隔 cell（块间距）设计上不可点（桌面分隔行同语义），跳过。
            guard types.indices.contains(index), types[index] != "LiveBlankCell" else { continue }
            let point = CGPoint(x: Theme.Typography.padX + frame.midX,
                                y: Theme.Typography.padY + frame.minY + min(8, frame.height / 2))
            let hit = container.hitTest(point, with: nil)
            if !inCell(hit) {
                let hitKind = String(describing: Swift.type(of: hit))
                let subs = container.contentView.subviews.map { "\(Swift.type(of: $0)) \($0.frame)" }
                XCTFail("块 \(index) 中心 hitTest 落空：hit=\(hitKind) point=\(point) mounted=\(container.mountedCellTypesForTesting) subs=\(subs)")
                return
            }
            checked += 1
        }
        XCTAssertGreaterThan(checked, 4)
    }
}

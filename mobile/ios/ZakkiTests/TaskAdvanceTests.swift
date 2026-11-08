import XCTest
@testable import Zakki

// 完成推进与弹层写回的容器级测试（M3-1，D17）：
//   - 带重复任务勾选即 Advance（改写锚点保持未完成），ended 才标记完成；
//   - 非重复任务翻转勾选；未知规则整簇任务按普通任务翻转；
//   - 渲染 cell 的勾选不依赖焦点块；
//   - 弹层写回走 ReplaceTimeToken + 既有管线（可撤销、清除、软换行段行级替换）。
@MainActor
final class TaskAdvanceTests: XCTestCase {
    private let fffc = "\u{FFFC}"

    override func setUp() {
        super.setUp()
        NotesCore.setLocation(TimeZone.current.identifier)
    }

    private func makeContainer(_ raw: String, writable: Bool = true) -> LiveEditorContainer {
        let container = LiveEditorContainer(raw: raw, writable: writable)
        container.frame = CGRect(x: 0, y: 0, width: 390, height: 800)
        container.layoutIfNeeded()
        return container
    }

    // MARK: 重复任务推进

    func testCheckingRecurringTaskAdvancesAnchor() {
        // 锚点远在过去（每天重复），勾选应改写锚点到下一实例且保持未完成。
        let container = makeContainer("- [ ] 晨会 @2026-01-01 09:00 ~5m ^1d")
        container.toggleTask(at: 0)
        let raw = container.raw
        XCTAssertFalse(raw.contains("[x]"), "推进后保持未完成")
        XCTAssertTrue(raw.hasPrefix("- [ ] 晨会 @"))
        // 新锚点为未来实例：解析 raw 的 dueMs 验证推进（core 校验合法性）。
        guard let hit = EditorDocument.findTimeToken(in: raw, protectedJSON: "") else {
            return XCTFail("令牌丢失")
        }
        XCTAssertEqual(hit.suffix.recur, "^1d", "重复段原文保留")
        XCTAssertEqual(hit.suffix.hasAlert, true, "提醒段保留")
        XCTAssertEqual(hit.suffix.alertMs, 300_000)
        let nowMs = Int64(Date().timeIntervalSince1970 * 1000)
        XCTAssertGreaterThan(hit.suffix.dueMs ?? 0, nowMs - 24 * 3600 * 1000, "锚点推进到当前实例之后")
        XCTAssertEqual(container.undoStack.count, 1, "推进进撤销栈")
    }

    func testUndoRestoresAdvancedAnchor() {
        let container = makeContainer("- [ ] 晨会 @2026-01-01 09:00 ~5m ^1d")
        let before = container.raw
        container.toggleTask(at: 0)
        container.undo()
        XCTAssertEqual(container.raw, before, "撤销恢复原锚点")
    }

    func testEndedRecurringTaskCompletesOnCheck() {
        // 结束日期已过：勾选标记完成（Advance ended）。
        let container = makeContainer("- [ ] 已完结 @2026-01-01 09:00 ~5m ^1d<2026-06-30")
        container.toggleTask(at: 0)
        XCTAssertTrue(container.raw.contains("- [x]"), "序列已结束标记完成")
        XCTAssertFalse(container.raw.contains("- [ ] "), "不再保持未完成")
    }

    func testPlainTaskStillFlips() {
        let container = makeContainer("- [ ] 普通 @2027-01-20 10:00 ~10m")
        container.toggleTask(at: 0)
        XCTAssertTrue(container.raw.contains("- [x] 普通"), "非重复任务勾选照常翻转")
        container.toggleTask(at: 0)
        XCTAssertTrue(container.raw.contains("- [ ] 普通"), "再点反勾")
    }

    func testUnknownRecurTaskFlipsLikePlain() {
        let container = makeContainer("- [ ] 未知规则 @2027-01-20 10:00 ~10m ^hello")
        container.toggleTask(at: 0)
        XCTAssertTrue(container.raw.contains("- [x] 未知规则"), "未知规则整簇任务按普通任务翻转")
        XCTAssertTrue(container.raw.contains("^hello"), "未知规则原文保留")
    }

    func testToggleOnUnfocusedBlockWorks() {
        // 渲染 cell 的勾选（无焦点块）：M3 起生效（M2 只支持焦点块路径）。
        let container = makeContainer("第一段\n\n- [ ] 待办 @2027-01-20 10:00 ~10m")
        XCTAssertNil(container.activeEditor, "无焦点块")
        container.toggleTask(at: 2)
        XCTAssertTrue(container.raw.contains("- [x] 待办"), "无焦点块也能勾选")
    }

    // MARK: 弹层写回（桌面 applyTimePopover 同构）

    func testPopoverApplyRewritesDueKeepsRecurRaw() {
        let container = makeContainer("交付 @2027-01-20 10:00 ~10m ^1w@1,3")
        let block = container.blockList.blocks[0]
        let (lineRaw, lineIndex) = LiveEditorContainer.tokenLine(in: block, tokenRaw: block.timeTokenRaw ?? "")!
        guard let hit = EditorDocument.findTimeToken(in: lineRaw, protectedJSON: "") else { return XCTFail("识别失败") }
        var draft = TimeTokenDraft(hit: hit)
        draft.due = TimeTokenDraft.parseDue("2027-02-03 08:30")!
        container.applyTimePopover(blockIndex: 0, lineIndex: lineIndex, lineRaw: lineRaw,
                                   draft: draft, recurDraft: draft.recur)
        XCTAssertTrue(container.raw.contains("@2027-02-03 08:30"), "时刻改写")
        XCTAssertTrue(container.raw.contains("~10m"), "提醒段保留")
        XCTAssertTrue(container.raw.contains("^1w@1,3"), "未改保留 ^ 原文")
        XCTAssertEqual(container.undoStack.count, 1, "写回进撤销栈")
    }

    func testPopoverClearRemovesToken() {
        let container = makeContainer("交付 @2027-01-20 10:00 ~10m ^1d")
        let block = container.blockList.blocks[0]
        let (lineRaw, lineIndex) = LiveEditorContainer.tokenLine(in: block, tokenRaw: block.timeTokenRaw ?? "")!
        guard let hit = EditorDocument.findTimeToken(in: lineRaw, protectedJSON: "") else { return XCTFail("识别失败") }
        let draft = TimeTokenDraft(hit: hit)
        container.applyTimePopover(blockIndex: 0, lineIndex: lineIndex, lineRaw: lineRaw,
                                   draft: nil, recurDraft: draft.recur)
        XCTAssertFalse(container.raw.contains("@"), "清除时间移除令牌")
        XCTAssertEqual(container.raw, "交付")
    }

    func testPopoverApplyOnSoftWrappedParagraphTargetsTokenLine() {
        // 软换行段：只改令牌所在源码行，其余行原样保留（桌面 replaceBlockTimeToken）。
        let container = makeContainer("第一行内容\n第二行 @2027-01-20 10:00 ~10m\n第三行内容")
        let block = container.blockList.blocks[0]
        let (lineRaw, lineIndex) = LiveEditorContainer.tokenLine(in: block, tokenRaw: block.timeTokenRaw ?? "")!
        XCTAssertEqual(lineIndex, 1, "定位到含令牌的源码行")
        guard let hit = EditorDocument.findTimeToken(in: lineRaw, protectedJSON: "") else { return XCTFail("识别失败") }
        var draft = TimeTokenDraft(hit: hit)
        draft.due = TimeTokenDraft.parseDue("2027-03-01 09:15")!
        container.applyTimePopover(blockIndex: 0, lineIndex: lineIndex, lineRaw: lineRaw,
                                   draft: draft, recurDraft: draft.recur)
        XCTAssertEqual(container.raw, "第一行内容\n第二行 @2027-03-01 09:15 ~10m\n第三行内容", "其余行逐字保留")
    }

    func testPopoverApplyInsideCodeFenceIsRejected() {
        // 行内代码内的令牌：保护区让 core 判定无令牌 → 无弹层入口、写回无操作。
        let container = makeContainer("示例 `@2027-01-20 10:00 ~10m` 尾")
        let block = container.blockList.blocks[0]
        guard let (lineRaw, lineIndex) = LiveEditorContainer.tokenLine(in: block, tokenRaw: "@2027-01-20 10:00 ~10m") else {
            return XCTFail("原文应包含令牌文本")
        }
        let protected = MarkdownProtection.protectedJSON(ranges: MarkdownProtection.inlineProtectedRanges(in: lineRaw))
        XCTAssertNil(EditorDocument.findTimeToken(in: lineRaw, protectedJSON: protected),
                     "保护区命中后无令牌（弹层不可打开）")
        XCTAssertNil(LiveEditorContainer.replaceTokenLine(lineRaw, suffixJSON: nil),
                     "写回无操作（清除也被保护区拒绝）")
        XCTAssertEqual(container.raw, "示例 `@2027-01-20 10:00 ~10m` 尾", "原文不动")
        XCTAssertEqual(lineIndex, 0)
    }

    func testFocusedBlockApplySyncsBuffer() {
        // 焦点块写回后缓冲同步（组件仍为单 U+FFFC 片元）。
        let container = makeContainer("交付 @2027-01-20 10:00 ~10m ^1d")
        container.rawSelection = NSRange(location: 2, length: 0)
        guard container.activeEditor != nil else { return XCTFail("激活失败") }
        let block = container.blockList.blocks[container.activeIndex!]
        let (lineRaw, lineIndex) = LiveEditorContainer.tokenLine(in: block, tokenRaw: block.timeTokenRaw ?? "")!
        guard let hit = EditorDocument.findTimeToken(in: lineRaw, protectedJSON: "") else { return XCTFail("识别失败") }
        var draft = TimeTokenDraft(hit: hit)
        draft.due = TimeTokenDraft.parseDue("2027-02-03 08:30")!
        container.applyTimePopover(blockIndex: 0, lineIndex: lineIndex, lineRaw: lineRaw,
                                   draft: draft, recurDraft: draft.recur)
        XCTAssertEqual(container.activeEditor?.document.bufferText.contains(fffc), true, "改写后仍收为组件")
        XCTAssertTrue(container.activeEditor?.document.bufferText.contains("交付") ?? false, "正文可见")
    }

    // MARK: tokenLine 定位

    func testTokenLineFindsCorrectLine() {
        let block = LiveBlocks.split("a\nb @2027-01-20 10:00\nc").blocks[0]
        let found = LiveEditorContainer.tokenLine(in: block, tokenRaw: "@2027-01-20 10:00")
        XCTAssertEqual(found?.line, "b @2027-01-20 10:00")
        XCTAssertEqual(found?.index, 1)
        XCTAssertNil(LiveEditorContainer.tokenLine(in: block, tokenRaw: "@2030-01-01 00:00"))
    }
}

import XCTest
@testable import Zakki

// M2-6 阶段一：块模型与投影测试。分块规则、标记剥离、不完整语法字面保留、
// 成型时机、映射往返、拆并块、时间令牌不受影响。期望值全部取自桌面源码
//（internal/app/live_blocks.go / live_projection.go / live_input.go 及其测试）。
final class LiveBlockModelTests: XCTestCase {
    // MARK: 分块规则

    func testSplitBasicBlockKinds() {
        let list = LiveBlocks.split("# 标题\n\n正文一行\n- 列表\n- [ ] 任务\n> 引用\n---\n\n```\ncode\n```\n尾段")
        let kinds = list.blocks.map(\.kind)
        XCTAssertEqual(kinds, [.heading, .blank, .para, .list, .task, .quote, .rule, .blank, .code, .para])
        XCTAssertEqual(list.blocks[0].level, 1)
        XCTAssertEqual(list.blocks[3].number, 0, "无序列表序号为 0")
        XCTAssertFalse(list.blocks[4].checked)
        XCTAssertEqual(list.blocks[8].codeLines, ["code"])
    }

    func testParagraphAbsorbsConsecutivePlainLines() {
        let list = LiveBlocks.split("甲\n乙\n丙")
        XCTAssertEqual(list.blocks.count, 1)
        XCTAssertEqual(list.blocks[0].kind, .para)
        XCTAssertEqual(list.blocks[0].startLine, 0)
        XCTAssertEqual(list.blocks[0].endLine, 2)
        XCTAssertEqual(list.blocks[0].raw, "甲\n乙\n丙")
    }

    func testUnclosedFenceRunsToDocumentEnd() {
        let list = LiveBlocks.split("```go\nx := 1\ny := 2")
        XCTAssertEqual(list.blocks.count, 1)
        XCTAssertEqual(list.blocks[0].kind, .code)
        XCTAssertEqual(list.blocks[0].fenceInfo, "go")
        XCTAssertEqual(list.blocks[0].codeLines, ["x := 1", "y := 2"])
        XCTAssertEqual(list.blocks[0].endLine, 2)
    }

    func testHeadingLevelClampToFour() {
        // 桌面 parseLines：level > 4 钳到 4，正文只剥 4 个 #。
        let list = LiveBlocks.split("##### 五级")
        XCTAssertEqual(list.blocks[0].kind, .heading)
        XCTAssertEqual(list.blocks[0].level, 4)
        let p = LiveProjection.project(list.blocks[0])
        XCTAssertEqual(p.text, "五级", "投影剥掉全部 #（桌面 projectBlock 同款；parseLines 只把 level 钳到 4）")
    }

    func testEmptyDocumentSplitsToOneBlankBlock() {
        let list = LiveBlocks.split("")
        XCTAssertEqual(list.blocks.count, 1)
        XCTAssertEqual(list.blocks[0].kind, .blank)
        XCTAssertEqual(list.blocks[0].raw, "")
    }

    // MARK: 分隔空行与真空段落

    func testLiveSeparatorSemantics() {
        let list = LiveBlocks.split("甲\n\n\n乙")
        // blocks: para, blank(分隔), blank(真空段), para
        XCTAssertEqual(list.blocks.map(\.kind), [.para, .blank, .blank, .para])
        XCTAssertFalse(LiveBlocks.isLiveSeparator(list.blocks, 0))
        XCTAssertTrue(LiveBlocks.isLiveSeparator(list.blocks, 1))
        XCTAssertFalse(LiveBlocks.isLiveSeparator(list.blocks, 2))
    }

    func testLeadingDoubleBlankSeparator() {
        // 桌面注释：至少两个前导空行时，最后一个分隔后面的正文。
        let list = LiveBlocks.split("\n\n甲")
        XCTAssertTrue(LiveBlocks.isLiveSeparator(list.blocks, 1))
        XCTAssertFalse(LiveBlocks.isLiveSeparator(list.blocks, 0))
    }

    // MARK: 投影（可见文字）

    func testProjectionStripsBlockMarkers() {
        for (raw, visible) in [
            ("# 标题", "标题"),
            ("## 二级", "二级"),
            ("- 列表", "列表"),
            ("1. 有序", "有序"),
            ("10. 十号", "十号"),
            ("- [ ] 待办", "待办"),
            ("- [x] 完成", "完成"),
            ("> 引用", "引用"),
            ("正文", "正文"),
        ] {
            let list = LiveBlocks.split(raw)
            let p = LiveProjection.project(list.blocks[0])
            XCTAssertEqual(p.text, visible, "raw \(raw) 的可见文字")
        }
    }

    func testFenceProjectionExposesOnlyContentLines() {
        let list = LiveBlocks.split("```swift\nlet x = 1\nlet y = 2\n```")
        let p = LiveProjection.project(list.blocks[0])
        XCTAssertEqual(p.text, "let x = 1\nlet y = 2", "围栏行不可见不可编辑")
        XCTAssertEqual(p.start, 9, "可编辑区从开栏行之后开始")
        XCTAssertEqual(p.end, 28, "可编辑区在闭栏行之前结束")
        // 围栏行在区外：sourceOffset 钳到两端。
        XCTAssertEqual(p.sourceOffset(0), 9, "区外源偏移钳到可编辑区起点")
        XCTAssertEqual(p.visibleOffset(0), 0, "围栏前缀之前的源偏移映到可见 0")
    }

    func testRuleBlockHasEmptyEditableRegion() {
        let list = LiveBlocks.split("---")
        let p = LiveProjection.project(list.blocks[0])
        XCTAssertTrue(p.text.isEmpty)
        XCTAssertEqual(p.start, 3)
        XCTAssertEqual(p.end, 3)
    }

    // MARK: 行内标记与「输入中即时成型」

    func testInlineProjectionStripsMatchedMarkers() {
        let list = LiveBlocks.split("**粗体** 尾")
        let p = LiveProjection.project(list.blocks[0])
        XCTAssertEqual(p.text, "粗体 尾")
        XCTAssertEqual(p.styles.count, 1)
        XCTAssertEqual(p.styles[0].kind, LiveSpanKind.bold)
        let visible = Array(p.text.utf16)
        XCTAssertEqual(String(decoding: visible[p.styles[0].start..<p.styles[0].end], as: UTF16.self), "粗体")
    }

    func testIncompleteSyntaxStaysLiteralThenFormsOnCompletion() {
        // 输入 **abc：无闭合 → 标记是字面文本。
        let incomplete = LiveBlocks.split("**abc")
        var p = LiveProjection.project(incomplete.blocks[0])
        XCTAssertEqual(p.text, "**abc", "不完整语法保持字面")
        XCTAssertTrue(p.styles.isEmpty)
        // 补全闭合 → 重新投影即渲染态（成型时机 = 重新分块解析）。
        let complete = LiveBlocks.split("**abc**")
        p = LiveProjection.project(complete.blocks[0])
        XCTAssertEqual(p.text, "abc")
        XCTAssertEqual(p.styles.first?.kind, .bold)
    }

    func testUnmatchedSingleAsteriskStaysLiteral() {
        let list = LiveBlocks.split("2 * 3 = 6")
        let p = LiveProjection.project(list.blocks[0])
        XCTAssertEqual(p.text, "2 * 3 = 6")
        XCTAssertTrue(p.styles.isEmpty, "孤星不当作标记")
    }

    func testInlineCodeAndLinkKinds() {
        let list = LiveBlocks.split("x `code` y [链](http://e.com)")
        let p = LiveProjection.project(list.blocks[0])
        XCTAssertEqual(p.text, "x code y 链")
        XCTAssertEqual(p.styles.map(\.kind), [.code, .link])
    }

    // MARK: 偏移映射往返

    func testOffsetMappingRoundTrip() {
        let raws = [
            "# 中文标题",
            "- [ ] 任务 **加粗** 尾",
            ("> 引用 `码`"),
            "**a**中*英*文",
        ]
        for raw in raws {
            let list = LiveBlocks.split(raw)
            let p = LiveProjection.project(list.blocks[0])
            for visible in 0...p.units.count {
                let src = p.sourceOffset(visible)
                let back = p.visibleOffset(src)
                // 区内往返一致；端点允许钳制。
                if src >= p.start, src <= p.end {
                    XCTAssertEqual(back, min(visible, p.units.count), "raw \(raw) visible=\(visible)")
                }
            }
        }
    }

    func testVisibleSelectionExpandsAcrossObject() {
        let list = LiveBlocks.split("- [ ] 任务 @2026-01-01 10:00 尾")
        let p = LiveProjection.project(list.blocks[0])
        XCTAssertEqual(p.objects.count, 1)
        let objectAt = p.objects[0]
        // 源选区覆盖令牌 → 可见选区扩到整组件。
        let tokenUnit = p.units[objectAt]
        let (s, e) = p.visibleSelection(tokenUnit.start, tokenUnit.end)
        XCTAssertEqual(s, objectAt)
        XCTAssertEqual(e, objectAt + 1)
    }

    // MARK: 编辑写回（replacing）

    func testReplaceTypingInsideBoldKeepsWrappers() {
        let list = LiveBlocks.split("**粗体**")
        let p = LiveProjection.project(list.blocks[0])
        // 光标在可见末尾输入「文」。
        let (newRaw, _) = p.replacing("粗体文", caret: 3)
        XCTAssertEqual(newRaw, "**粗体文**", "包裹保留")
    }

    func testReplaceDeletingWholeStyleRemovesWrappers() {
        let list = LiveBlocks.split("前 **粗体** 尾")
        let p = LiveProjection.project(list.blocks[0])
        // 可见 2..<4 = 「粗体」，整体删除。
        var next = Array(p.text.utf16)
        next.removeSubrange(2..<4)
        let nextText = String(decoding: next, as: UTF16.self)
        let (newRaw, _) = p.replacing(nextText, caret: 2)
        XCTAssertEqual(newRaw, "前  尾", "整段样式删除后包裹一并移除")
    }

    func testReplaceSelectionKeepsWrappers() {
        let list = LiveBlocks.split("**中文** 尾")
        let p = LiveProjection.project(list.blocks[0])
        // 选中可见 0..<2（中文）替换为「替换」（桌面 TestVisibleSelectionPreservesWrappers）。
        let (newRaw, _) = p.replacing("替换 尾", caret: 2, selectedStart: 0, selectedEnd: 2)
        XCTAssertEqual(newRaw, "**替换** 尾")
    }

    func testFragmentClosesAndReopensStyleAcrossSplit() {
        let list = LiveBlocks.split("**粗体正文**")
        let p = LiveProjection.project(list.blocks[0])
        // 在可见 2 处拆分：左片段闭样式、右片段重开样式。
        XCTAssertEqual(p.fragment(0, 2), "**粗体**")
        XCTAssertEqual(p.fragment(2, 4), "**正文**")
    }

    // MARK: 时间令牌

    func testTimeTokenCollapsesToSingleObjectRune() {
        let list = LiveBlocks.split("- [ ] 任务 @2026-01-01 10:00 ~10m 尾")
        let p = LiveProjection.project(list.blocks[0])
        XCTAssertEqual(p.objects.count, 1)
        XCTAssertEqual(p.text, "任务 \u{FFFC} 尾", "令牌在可见文字中是单 U+FFFC")
        let unit = p.units[p.objects[0]]
        // 单个占位映射整簇源码。
        XCTAssertEqual(unit.end - unit.start, (("@2026-01-01 10:00 ~10m") as NSString).length)
    }

    func testReplaceAfterObjectAutoInsertsSpace() {
        let list = LiveBlocks.split("正文 @2026-01-01 10:00")
        let p = LiveProjection.project(list.blocks[0])
        let objectAt = p.objects[0]
        // 组件后紧贴输入（无空白）：自动补一个空格保持令牌合法。
        var next = Array(p.text.utf16)
        next.insert(0x41, at: objectAt + 1)
        let nextText = String(decoding: next, as: UTF16.self)
        let (newRaw, _) = p.replacing(nextText, caret: objectAt + 2)
        XCTAssertEqual(newRaw, "正文 @2026-01-01 10:00 A")
    }

    func testDeletingObjectRemovesWholeToken() {
        let list = LiveBlocks.split("正文 @2026-01-01 10:00 尾")
        let p = LiveProjection.project(list.blocks[0])
        let objectAt = p.objects[0]
        var next = Array(p.text.utf16)
        next.remove(at: objectAt)
        let nextText = String(decoding: next, as: UTF16.self)
        let (newRaw, _) = p.replacing(nextText, caret: objectAt)
        XCTAssertEqual(newRaw, "正文  尾", "删除占位符删除整个令牌源码")
    }

    func testTokenDetectionGoesThroughCoreBindingOnly() {
        // 未知簇（不合语法）不收组件、不产生占位。
        let list = LiveBlocks.split("- [ ] ^abc @not-a-time 尾")
        let p = LiveProjection.project(list.blocks[0])
        XCTAssertTrue(p.objects.isEmpty, "未知簇保持普通文本")
        XCTAssertFalse(p.text.contains("\u{FFFC}"))
    }

    // MARK: 代理对（emoji）边界

    func testReplaceWithEmojiDoesNotSplitSurrogatePair() {
        let list = LiveBlocks.split("中文🎉尾")
        let p = LiveProjection.project(list.blocks[0])
        // 在 🎉 前插入 X：diff 边界不得切进代理对。
        let (newRaw, caret) = p.replacing("中文X🎉尾", caret: 3)
        XCTAssertEqual(newRaw, "中文X🎉尾")
        XCTAssertEqual(caret, 3, "光标落在 X 之后（raw 坐标）")
    }

    // MARK: 回车拆块（对齐桌面 applyProjectedHint / afterEnter 期望）

    private func enterResult(_ doc: String, block: Int, visibleCaret: Int) -> LiveDocEditing.Result {
        let list = LiveBlocks.split(doc)
        let proj = LiveProjection.project(list.blocks[block])
        return LiveDocEditing.enter(doc, list: list, index: block, visibleCaret: visibleCaret, projection: proj)
    }

    func testEnterSplitsParagraphIntoTwoBlocks() {
        let doc = "甲乙"
        guard case .edited(let edit) = enterResult(doc, block: 0, visibleCaret: 1) else {
            return XCTFail("回车应产生结构编辑")
        }
        XCTAssertEqual(edit.doc, "甲\n\n乙")
        // 桌面 round-trip：新块开头，一次退格可还原。
        let list2 = LiveBlocks.split(edit.doc)
        // 光标落在新块（乙）开头；块首退格并回上一块，一次退格还原。
        XCTAssertEqual(LiveDocEditing.backspaceAtStart(edit.doc, list: list2, index: 2)?.doc, doc)
    }

    func testEnterOnListContinuesWithPrefixAndIncrementedNumber() {
        guard case .edited(let edit) = enterResult("1. 甲", block: 0, visibleCaret: 3) else {
            return XCTFail()
        }
        XCTAssertEqual(edit.doc, "1. 甲\n2. ")
        guard case .edited(let bullet) = enterResult("- 甲", block: 0, visibleCaret: 3) else {
            return XCTFail()
        }
        XCTAssertEqual(bullet.doc, "- 甲\n- ")
        guard case .edited(let task) = enterResult("- [x] 甲", block: 0, visibleCaret: 1) else {
            return XCTFail()
        }
        XCTAssertEqual(task.doc, "- [x] 甲\n- [ ] ", "任务续行 [x] 归一为 [ ]")
    }

    func testEnterInsideBoldSplitsWithFragments() {
        // 粗体内回车：左侧闭样式、右侧重开样式。
        guard case .edited(let edit) = enterResult("**甲乙**", block: 0, visibleCaret: 1) else {
            return XCTFail()
        }
        XCTAssertEqual(edit.doc, "**甲**\n\n**乙**")
    }

    func testEnterAtVisibleStartPrependsEmptyParagraph() {
        guard case .edited(let edit) = enterResult("甲", block: 0, visibleCaret: 0) else {
            return XCTFail()
        }
        XCTAssertEqual(edit.doc, "\n\n甲")
        XCTAssertEqual(edit.caret, 2)
    }

    func testEnterInBlankParagraphAppendsAnotherBlank() {
        // 桌面 TestLiveRepeatedEnterBackspace：两次回车 a → a\n\n\n。
        var doc = "a"
        var caretOffset = LiveBlocks.split(doc).startOffset(of: 0) + 1
        for _ in 0..<2 {
            let list = LiveBlocks.split(doc)
            let (index, blockStart) = list.blockOf(caretOffset)
            guard case .edited(let edit) = LiveDocEditing.enter(
                doc, list: list, index: index,
                visibleCaret: caretOffset - blockStart,
                projection: LiveProjection.project(list.blocks[index]))
            else {
                return XCTFail()
            }
            doc = edit.doc
            caretOffset = edit.caret
        }
        XCTAssertEqual(doc, "a\n\n\n")
        // 第一次退格只去掉多出来的空段。
        let list = LiveBlocks.split(doc)
        let (index, blockStart) = list.blockOf(caretOffset)
        let edit = LiveDocEditing.backspaceAtStart(doc, list: list, index: index)
        XCTAssertEqual(edit?.doc, "a\n\n", "文末连续空段一次只删一行")
        XCTAssertEqual(edit?.caret ?? -1, LiveSyntax.length("a\n\n"), "光标落在新文末")
        // 第二次退格回到 a。
        let list2 = LiveBlocks.split(edit!.doc)
        let (index2, _) = list2.blockOf(edit!.caret)
        let edit2 = LiveDocEditing.backspaceAtStart(edit!.doc, list: list2, index: index2)
        XCTAssertEqual(edit2?.doc, "a")
        XCTAssertEqual(edit2?.caret, 1, "光标回到 a 之后")
    }

    func testEnterOnBareListMarkerExitsList() {
        // 桌面 TestEmptyListExitAfterMultilineBlockKeepsCaret：空列表项回车退出，
        // 光标落在原行首（剥掉标记后的空行）。
        let doc = "甲\n乙\n\n- \n\n尾"
        let list = LiveBlocks.split(doc)
        XCTAssertEqual(list.blocks[2].kind, .list)
        guard case .edited(let edit) = LiveDocEditing.enter(
            doc, list: list, index: 2, visibleCaret: 0,
            projection: LiveProjection.project(list.blocks[2]))
        else {
            return XCTFail()
        }
        XCTAssertEqual(edit.doc, "甲\n乙\n\n\n\n尾")
        XCTAssertEqual(edit.caret, list.startOffset(of: 2), "光标留在原行首")
    }

    func testEnterOnCodeBlockIsNative() {
        let doc = "```\nx\n```"
        let list = LiveBlocks.split(doc)
        guard case .native = LiveDocEditing.enter(doc, list: list, index: 0, visibleCaret: 1,
                                                  projection: LiveProjection.project(list.blocks[0])) else {
            return XCTFail("代码块回车应走原生路径")
        }
    }

    func testEnterOnLoneFenceAutoCloses() {
        // 桌面 completeFenceDraft：键入 ``` 后回车自动补闭栏，光标进内容行。
        let doc = "```"
        let list = LiveBlocks.split(doc)
        guard case .edited(let edit) = LiveDocEditing.enter(doc, list: list, index: 0, visibleCaret: 0,
                                                            projection: LiveProjection.project(list.blocks[0])) else {
            return XCTFail()
        }
        XCTAssertEqual(edit.doc, "```\n\n```")
        XCTAssertEqual(edit.caret, 4, "光标落在内容行行首")
    }

    // MARK: 块首退格（对齐桌面 TestLiveBackspaceBoundary 全部用例）

    private func backspaceResult(_ doc: String, block: Int) -> LiveDocEdit? {
        let list = LiveBlocks.split(doc)
        return LiveDocEditing.backspaceAtStart(doc, list: list, index: block)
    }

    func testBackspaceBoundaryDesktopCases() {
        // 用例表 = 桌面 live_boundary_test.go TestLiveBackspaceBoundary。
        let cases: [(doc: String, block: Int, want: String, caretText: String)] = [
            ("甲\n\n乙\n\n尾", 2, "甲乙\n\n尾", "甲"),
            ("甲\n乙\n\n丙", 2, "甲\n乙丙", "甲\n乙"),
            ("甲\n\n乙\n尾", 2, "甲乙\n尾", "甲"),
            ("甲\n\n\n乙", 3, "甲\n\n乙", "甲\n\n"),
            ("\n\n甲", 2, "甲", ""),
            ("- 甲\n- 乙\n尾", 1, "- 甲\n乙\n尾", "- 甲\n"),
            ("```\nx\n```\n\n尾", 2, "```\nx\n```\n\n尾", ""),
        ]
        for tc in cases {
            let edit = backspaceResult(tc.doc, block: tc.block)
            if tc.want == tc.doc {
                XCTAssertNil(edit, "doc \(tc.doc)：代码块边界不并块")
                continue
            }
            XCTAssertEqual(edit?.doc, tc.want, "doc \(tc.doc) 并块结果")
            if !tc.caretText.isEmpty {
                let caret = edit?.caret ?? -1
                let head = LiveSyntax.length(tc.caretText)
                XCTAssertEqual(caret, head, "doc \(tc.doc) 光标落在上一块末尾")
            }
        }
    }

    func testBackspaceOnBareMarkerRemovesMarkerOnly() {
        let doc = "甲\n乙\n\n- \n\n尾"
        let list = LiveBlocks.split(doc)
        let edit = LiveDocEditing.backspaceAtStart(doc, list: list, index: 2)
        XCTAssertEqual(edit?.doc, "甲\n乙\n\n\n\n尾")
        XCTAssertEqual(edit?.caret, list.startOffset(of: 2))
    }

    // MARK: 跨块方向键

    func testCrossBlockSkipsSeparator() {
        let list = LiveBlocks.split("甲\n\n乙")
        // 乙块首向左 → 甲块末尾。
        let backward = LiveDocEditing.crossBlock(list.blocks, from: 2, direction: -1)
        XCTAssertEqual(backward?.index, 0)
        XCTAssertEqual(backward?.atEnd, true)
        // 甲块尾向右 → 乙块开头。
        let forward = LiveDocEditing.crossBlock(list.blocks, from: 0, direction: 1)
        XCTAssertEqual(forward?.index, 2)
        XCTAssertEqual(forward?.atEnd, false)
    }

    // MARK: 任务勾选与列表序号

    func testQuoteWithTaskPrefixDetectsChecked() {
        let list = LiveBlocks.split("> - [x] 完成")
        XCTAssertEqual(list.blocks[0].kind, .quote)
        XCTAssertTrue(list.blocks[0].checked, "桌面口径：引用内任务前缀含 [x] 即勾选")
    }

    // MARK: 围栏草稿态识别

    func testIsFenceDraftLine() {
        XCTAssertTrue(LiveSyntax.isFenceDraftLine("```"))
        XCTAssertTrue(LiveSyntax.isFenceDraftLine("```swift"))
        XCTAssertTrue(LiveSyntax.isFenceDraftLine("~~~"))
        XCTAssertTrue(LiveSyntax.isFenceDraftLine("``"))
        XCTAssertFalse(LiveSyntax.isFenceDraftLine("```\ncode"))
        XCTAssertFalse(LiveSyntax.isFenceDraftLine("`` `"))
        XCTAssertFalse(LiveSyntax.isFenceDraftLine("    ```"))
    }
}

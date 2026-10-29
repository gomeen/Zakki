import XCTest
@testable import Zakki

// 文档内核测试：纯 Foundation 驱动 EditorDocument，令牌识别走真实绑定
// （ZakkicoreFindTimeToken），与磁盘 Markdown 逐字节对账。
final class EditorDocumentTests: XCTestCase {
    private let cluster = "@2026-01-01 10:00"
    private let clusterWithSuffix = "@2026-01-01 10:00 ~10m ^1d"
    private let fffc = "\u{FFFC}"

    // MARK: 投影

    // 初始 raw→pieces：完整合法簇收组件，磁盘原文与 raw 投影一致。
    func testInitialRawToPiecesProjection() {
        let raw = "before \(cluster) after"
        let doc = EditorDocument(raw: raw)
        XCTAssertEqual(doc.pieces, [.text("before "), .token(cluster), .text(" after")])
        XCTAssertEqual(doc.rawText, raw)
        XCTAssertEqual(doc.bufferText, "before \(fffc) after")
        XCTAssertEqual(doc.bufferLength, 14)
    }

    // raw→buffer→raw 幂等：已收组件的文档再投影不漂移。
    func testRawProjectionIdempotent() {
        let raw = "中文🎉 \(cluster) ~10m 尾行\n```go\n\(cluster)\n```"
        let doc = EditorDocument(raw: raw)
        XCTAssertEqual(doc.rawText, raw)
        let again = EditorDocument(raw: doc.rawText)
        XCTAssertEqual(again.pieces, doc.pieces)
        XCTAssertEqual(again.bufferText, doc.bufferText)
    }

    // 多令牌同行：扫描继续到行尾，逐个收簇。
    func testMultipleTokensOnOneLine() {
        let raw = "\(cluster) 与 \(cluster) ~5m"
        let doc = EditorDocument(raw: raw)
        XCTAssertEqual(doc.pieces, [.token(cluster), .text(" 与 "), .token("\(cluster) ~5m")])
        XCTAssertEqual(doc.bufferText, "\(fffc) 与 \(fffc)")
    }

    // MARK: 键入收簇

    // 逐字符键入完整合法簇：最后一击触发 collapse，raw 投影与磁盘原文一致。
    func testTypingCompleteClusterCollapses() {
        let doc = EditorDocument(raw: "")
        var ops: [EditorDocument.BufferReplacement] = []
        for ch in cluster {
            let edit = EditorDocument.BufferEdit(
                replacedRange: NSRange(location: doc.bufferLength, length: 0),
                replacement: [.text(String(ch))]
            )
            ops += doc.apply(edit, detectTokens: true).replacements
        }
        // 倒数第二击还不成簇；最后一击收组件，替换区间覆盖整个簇。
        XCTAssertEqual(ops.count, 1)
        XCTAssertEqual(ops[0].range, NSRange(location: 0, length: (cluster as NSString).length))
        XCTAssertEqual(ops[0].piece, .token(cluster))
        XCTAssertEqual(doc.pieces, [.token(cluster)])
        XCTAssertEqual(doc.rawText, cluster)
        XCTAssertEqual(doc.bufferText, fffc)
    }

    // 键入带提醒与重复的完整簇：整簇（含后缀）收进组件。
    func testTypingClusterWithSuffixCollapsesWholeCluster() {
        let doc = EditorDocument(raw: "待办 ")
        for ch in clusterWithSuffix {
            let edit = EditorDocument.BufferEdit(
                replacedRange: NSRange(location: doc.bufferLength, length: 0),
                replacement: [.text(String(ch))]
            )
            _ = doc.apply(edit, detectTokens: true)
        }
        XCTAssertEqual(doc.pieces, [.text("待办 "), .token(clusterWithSuffix)])
        XCTAssertEqual(doc.rawText, "待办 \(clusterWithSuffix)")
        // 后缀投影来自 core：due 为分钟精度墙上时刻、有提醒、带重复规则。
        let hit = EditorDocument.findTimeToken(in: clusterWithSuffix, protectedJSON: "")
        XCTAssertEqual(hit?.suffix.due, "2026-01-01 10:00")
        XCTAssertEqual(hit?.suffix.hasAlert, true)
        XCTAssertEqual(hit?.suffix.recur, "^1d")
    }

    // 单次整段插入（粘贴路径）：未知 ^ 簇按普通文本不收。
    func testUnknownRecurClusterStaysText() {
        let doc = EditorDocument(raw: "")
        let text = "\(cluster) ^abc"
        let outcome = doc.apply(
            .init(replacedRange: NSRange(location: 0, length: 0), replacement: [.text(text)]),
            detectTokens: true
        )
        XCTAssertTrue(outcome.replacements.isEmpty)
        XCTAssertEqual(doc.pieces, [.text(text)])
        XCTAssertEqual(doc.rawText, text)
        XCTAssertFalse(doc.bufferText.contains(fffc))
    }

    // 粘贴净化：外来无主 U+FFFC 过滤，其余原文进入重扫并正常收簇。
    func testPastedTextFiltersForeignObjectCharsAndCollapses() {
        let doc = EditorDocument(raw: "")
        let pasted = "hi \(fffc)\(cluster)\(fffc)!"
        _ = doc.apply(
            .init(replacedRange: NSRange(location: 0, length: 0), replacement: [.text(EditorDocument.sanitizingPasted(pasted))]),
            detectTokens: true
        )
        XCTAssertEqual(doc.pieces, [.text("hi "), .token(cluster), .text("!")])
        XCTAssertEqual(doc.rawText, "hi \(cluster)!")
    }

    // MARK: 保护区

    // 围栏内令牌不收组件（初始化路径）；围栏外照常收。
    func testFencedTokenNotCollapsed() {
        let raw = "\(cluster)\n```\n\(cluster)\n```\n\(cluster) ~1h"
        let doc = EditorDocument(raw: raw)
        XCTAssertEqual(doc.pieces, [
            .token(cluster),
            .text("\n```\n\(cluster)\n```\n"),
            .token("\(cluster) ~1h"),
        ])
    }

    // 行内代码与链接内令牌不收组件；粗体斜体不是保护区，具中令牌照常收。
    func testInlineCodeAndLinkTokensNotCollapsed() {
        let raw = "`\(cluster)` 与 [文档](\(cluster)) 与 **\(cluster)**"
        let doc = EditorDocument(raw: raw)
        XCTAssertEqual(doc.pieces, [
            .text("`\(cluster)` 与 [文档](\(cluster)) 与 **"),
            .token(cluster),
            .text("**"),
        ])
        XCTAssertEqual(doc.rawText, raw)
    }

    // 波浪号围栏同样保护；缩进 ≤3 空格才算开栏。
    func testTildeFenceAndIndentRules() {
        let raw = "~~~\n\(cluster)\n~~~\n    ```\n\(cluster)\n    ```"
        let doc = EditorDocument(raw: raw)
        // 4 空格缩进的 ``` 不是围栏，其后令牌正常收簇。
        XCTAssertEqual(doc.pieces, [.text("~~~\n\(cluster)\n~~~\n    ```\n"), .token(cluster), .text("\n    ```")])
    }

    // MARK: 删除与撤销

    // 退格删组件：缓冲少一个 U+FFFC，raw 整簇消失。
    func testBackspaceRemovesWholeCluster() {
        let doc = EditorDocument(raw: "前 \(cluster) 后")
        XCTAssertEqual(doc.bufferText, "前 \(fffc) 后")
        // 删除缓冲中偏移 2 处的组件字符（"前 " 之后）。
        let outcome = doc.apply(
            .init(replacedRange: NSRange(location: 2, length: 1), replacement: []),
            detectTokens: true
        )
        XCTAssertTrue(outcome.replacements.isEmpty)
        XCTAssertEqual(doc.pieces, [.text("前  后")])
        XCTAssertEqual(doc.rawText, "前  后")
        XCTAssertFalse(doc.bufferText.contains(fffc))
    }

    // 文档级 undo/redo 互逆：collapse 撤销回到原文，重做恢复组件，raw 往返一致。
    func testUndoRedoRoundTrip() {
        let raw = "a \(cluster) b"
        let doc = EditorDocument(raw: raw)
        XCTAssertEqual(doc.pieces[1], .token(cluster))

        // undo：组件字符被替换回原文（属性不含组件），跳过收簇识别。
        let undoEdit = EditorDocument.BufferEdit(
            replacedRange: NSRange(location: 2, length: 1),
            replacement: [.text(cluster)]
        )
        let undoOutcome = doc.apply(undoEdit, detectTokens: false)
        XCTAssertTrue(undoOutcome.replacements.isEmpty)
        XCTAssertEqual(doc.pieces, [.text("a "), .text(cluster), .text(" b")])
        XCTAssertEqual(doc.rawText, raw)

        // redo：原文区间被替换回组件（含附件属性），同样跳过识别防重复收簇。
        let clusterLen = (cluster as NSString).length
        let redoEdit = EditorDocument.BufferEdit(
            replacedRange: NSRange(location: 2, length: clusterLen),
            replacement: [.token(cluster)]
        )
        let redoOutcome = doc.apply(redoEdit, detectTokens: false)
        XCTAssertTrue(redoOutcome.replacements.isEmpty)
        XCTAssertEqual(doc.pieces, [.text("a "), .token(cluster), .text(" b")])
        XCTAssertEqual(doc.rawText, raw)
        XCTAssertEqual(doc.bufferText, "a \(fffc) b")
    }

    // 撤销后再次编辑：识别不残留抑制，清空后重新键入仍能正常收簇。
    func testEditAfterUndoStillCollapses() {
        let doc = EditorDocument(raw: "\(cluster) ok")
        XCTAssertEqual(doc.pieces[0], .token(cluster))
        let clusterLen = (cluster as NSString).length
        // undo：组件字符被替换回原文（detectTokens=false 不做识别）。
        _ = doc.apply(.init(replacedRange: NSRange(location: 0, length: 1), replacement: [.text(cluster)]), detectTokens: false)
        XCTAssertEqual(doc.rawText, "\(cluster) ok")
        // 清空（同样不识别），再整段键入新簇：识别已恢复，正常收簇。
        _ = doc.apply(.init(replacedRange: NSRange(location: 0, length: doc.bufferLength), replacement: []), detectTokens: true)
        XCTAssertTrue(doc.bufferText.isEmpty)
        let outcome = doc.apply(.init(replacedRange: NSRange(location: 0, length: 0), replacement: [.text(cluster)]), detectTokens: true)
        XCTAssertEqual(outcome.replacements.count, 1)
        XCTAssertEqual(doc.pieces, [.token(cluster)])
    }

    // MARK: UTF-16 偏移

    // CJK + emoji 夹具：命中与替换区间按 UTF-16 code unit 对齐（emoji 占 2 个单元）。
    func testCJKAndEmojiUTF16Offsets() {
        let doc = EditorDocument(raw: "")
        let prefix = "中文🎉 "
        for ch in prefix {
            _ = doc.apply(.init(replacedRange: NSRange(location: doc.bufferLength, length: 0), replacement: [.text(String(ch))]), detectTokens: true)
        }
        var typed = ""
        for ch in cluster {
            typed += String(ch)
            let outcome = doc.apply(.init(replacedRange: NSRange(location: doc.bufferLength, length: 0), replacement: [.text(String(ch))]), detectTokens: true)
            if typed == cluster {
                guard let op = outcome.replacements.first else {
                    XCTFail("最后一击应收组件")
                    return
                }
                let expected = (prefix as NSString).length
                XCTAssertEqual(op.range, NSRange(location: expected, length: (cluster as NSString).length))
            }
        }
        XCTAssertEqual(doc.rawText, prefix + cluster)
        XCTAssertEqual(doc.bufferText, prefix + fffc)
        XCTAssertEqual(doc.bufferLength, (prefix as NSString).length + 1)
    }

    // MARK: raw 区间投影

    // 剪贴板投影：选区跨组件时展开为原始 Markdown（D2/D7）。
    func testRawInBufferRangeExpandsTokens() {
        let doc = EditorDocument(raw: "a \(cluster) b")
        // 选区覆盖组件与前后的部分文本。
        let range = NSRange(location: 1, length: 3) // " " + FFFC + " "
        XCTAssertEqual(doc.raw(in: range), " \(cluster) ")
        XCTAssertEqual(doc.raw(in: NSRange(location: 0, length: doc.bufferLength)), "a \(cluster) b")
        XCTAssertEqual(doc.raw(in: NSRange(location: 2, length: 1)), cluster)
    }

    // 删除跨组件选区：整簇消失且不产生额外替换。
    func testDeleteAcrossTokenRemovesWholeCluster() {
        let doc = EditorDocument(raw: "keep \(cluster) drop")
        let bufferLen = doc.bufferLength
        _ = doc.apply(.init(replacedRange: NSRange(location: 0, length: bufferLen - 4), replacement: []), detectTokens: true)
        XCTAssertEqual(doc.rawText, "drop")
        XCTAssertFalse(doc.bufferText.contains(fffc))
    }

    // MARK: 坐标映射（缓冲 ↔ raw，模式切换与命中替换共用）

    // 组件片元端点的坐标映射：组件前/后必须映射到 raw 的簇首/簇尾，簇内钳到簇首。
    func testBufferRawOffsetMappingAtTokenEndpoints() {
        let doc = EditorDocument(raw: "a \(cluster) b")
        let clusterLen = (cluster as NSString).length
        // 缓冲: a(0) ' '(1) FFFC(2) ' '(3) b(4)；raw: a(0) ' '(1) 簇(2..<2+len) ' ' b。
        XCTAssertEqual(doc.rawOffset(ofBufferOffset: 0), 0)
        XCTAssertEqual(doc.rawOffset(ofBufferOffset: 2), 2, "组件首 → 簇首")
        XCTAssertEqual(doc.rawOffset(ofBufferOffset: 3), 2 + clusterLen, "组件后 → 簇尾")
        XCTAssertEqual(doc.rawOffset(ofBufferOffset: 5), doc.rawLength, "文档尾")
        XCTAssertEqual(doc.bufferOffset(ofRawOffset: 2), 2, "簇首 → 组件首")
        XCTAssertEqual(doc.bufferOffset(ofRawOffset: 2 + clusterLen), 3, "簇尾 → 组件后")
        XCTAssertEqual(doc.bufferOffset(ofRawOffset: doc.rawLength), doc.bufferLength, "raw 尾 → 缓冲尾")
        XCTAssertEqual(doc.bufferOffset(ofRawOffset: 5), 2, "簇内钳到组件首（组件不可内部定位）")
    }

    // 组件后键入普通文本：重扫会再次命中既有组件，但其缓冲区间长度为 1，不得产生替换。
    func testTypingRightAfterTokenDoesNotReproduceReplacement() {
        let doc = EditorDocument(raw: "")
        for ch in cluster {
            _ = doc.apply(.init(replacedRange: NSRange(location: doc.bufferLength, length: 0), replacement: [.text(String(ch))]), detectTokens: true)
        }
        XCTAssertEqual(doc.bufferText, fffc)
        let outcome = doc.apply(
            .init(replacedRange: NSRange(location: 1, length: 0), replacement: [.text(" x")]),
            detectTokens: true
        )
        XCTAssertTrue(outcome.replacements.isEmpty, "既有组件的重扫命中不得产生视图替换")
        XCTAssertEqual(doc.bufferText, "\(fffc) x")
        XCTAssertEqual(doc.rawText, "\(cluster) x")
    }

    // 组件前键入普通文本：命中既有组件时区间必须收敛为组件本身，不得吞掉行尾文本。
    func testTypingBeforeTokenKeepsTailText() {
        let raw = "hi \(cluster) tail"
        let doc = EditorDocument(raw: raw)
        let outcome = doc.apply(
            .init(replacedRange: NSRange(location: 0, length: 0), replacement: [.text("> ")]),
            detectTokens: true
        )
        XCTAssertTrue(outcome.replacements.isEmpty, "既有组件的重扫命中不得产生视图替换")
        XCTAssertEqual(doc.rawText, "> \(raw)")
        XCTAssertEqual(doc.bufferText, "> hi \(fffc) tail")
    }

    // 围栏内键入令牌：不收组件（增量路径与初始化路径一致）。
    func testTypingInsideFenceDoesNotCollapse() {
        let doc = EditorDocument(raw: "```\n")
        for ch in cluster {
            _ = doc.apply(.init(replacedRange: NSRange(location: doc.bufferLength, length: 0), replacement: [.text(String(ch))]), detectTokens: true)
        }
        XCTAssertEqual(doc.pieces, [.text("```\n"), .text(cluster)])
        XCTAssertFalse(doc.bufferText.contains(fffc))
    }
}

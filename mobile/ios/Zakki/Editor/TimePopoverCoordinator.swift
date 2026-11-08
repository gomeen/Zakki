import SwiftUI
import UIKit
import Zakkicore

// MARK: - 时间弹层协调（容器 → 弹层 → 写回）

// 打开入口（D17）：焦点块的胶囊点击（BlockEditorView）与渲染块胶囊点击
// （LiveTextCell）都汇到 openTimePopover；数据经 §3.2 绑定函数，写回走
// 既有编辑管线并进撤销（桌面 applyTimePopover 同构）。
extension LiveEditorContainer {
    // 从命中的令牌原文打开弹层。tokenRaw 来自点击的 TimeTokenAttachment。
    func openTimePopover(tokenRaw: String, blockIndex: Int) {
        guard writable, blockList.blocks.indices.contains(blockIndex) else { return }
        let block = blockList.blocks[blockIndex]
        // 定位令牌所在源码行（软换行段可能多行；桌面 replaceBlockTimeToken 同口径）。
        guard let (lineRaw, lineIndex) = Self.tokenLine(in: block, tokenRaw: tokenRaw) else { return }
        let protected = MarkdownProtection.protectedJSON(ranges: MarkdownProtection.inlineProtectedRanges(in: lineRaw))
        guard let hit = EditorDocument.findTimeToken(in: lineRaw, protectedJSON: protected) else { return }
        let box = TimeTokenDraft(hit: hit).box()
        let apply: (TimeTokenDraft?, RecurDraft) -> Void = { [weak self] draft, recurDraft in
            self?.applyTimePopover(blockIndex: blockIndex, lineIndex: lineIndex,
                                   lineRaw: lineRaw, draft: draft, recurDraft: recurDraft)
        }
        TimePopoverPresenter.present(from: self, box: box,
                                     onDone: { apply(box.draft(), box.recur) },
                                     onClear: { apply(nil, box.recur) },
                                     onCancel: {})
    }

    // 块内首个包含该令牌的源码行与行号。
    static func tokenLine(in block: LiveBlock, tokenRaw: String) -> (line: String, index: Int)? {
        for (i, line) in block.raw.components(separatedBy: "\n").enumerated() where line.contains(tokenRaw) {
            return (line, i)
        }
        return nil
    }

    // 弹层写回：draft=nil 表示清除时间；其余按桌面 applyTimePopover 语义
    //（preset≠none 但草稿组装不出合法规则时保留原 recur 段，未改保留 `^` 原文）。
    func applyTimePopover(blockIndex: Int, lineIndex: Int, lineRaw: String,
                          draft: TimeTokenDraft?, recurDraft: RecurDraft) {
        guard writable, blockList.blocks.indices.contains(blockIndex) else { return }
        let block = blockList.blocks[blockIndex]
        let suffixJSON: String?
        if let draft {
            suffixJSON = Self.suffixJSON(of: draft, recurDraft: recurDraft, block: block)
        } else {
            suffixJSON = nil
        }
        guard var lines = blockLines(of: block, lineIndex: lineIndex) else { return }
        guard let newLineRaw = Self.replaceTokenLine(lineRaw, suffixJSON: suffixJSON) else { return }
        lines[lineIndex] = newLineRaw
        let newBlockRaw = lines.joined(separator: "\n")
        applyLineEdit(blockIndex: blockIndex, newBlockRaw: newBlockRaw)
    }

    // 块内行数组（校验行号有效）。
    private func blockLines(of block: LiveBlock, lineIndex: Int) -> [String]? {
        let lines = block.raw.components(separatedBy: "\n")
        guard lines.indices.contains(lineIndex) else { return nil }
        return lines
    }

    // suffix JSON 组装；dueMs 仅输出、忽略输入（规格 §3.2），写回由 core 按
    // due + 时区重算。recur 段按草稿 token 组装（含「未改保留原文」）。
    static func suffixJSON(of draft: TimeTokenDraft, recurDraft: RecurDraft, block: LiveBlock) -> String {
        var payload: [String: Any] = [
            "due": TimeTokenDraft.formatDue(draft.due),
            "dueMs": 0,
            "alertMs": draft.hasAlert ? draft.alertMs : 0,
            "hasAlert": draft.hasAlert,
            "recur": recurDraft.token(),
        ]
        // 桌面 parity：preset≠none 但草稿组装不出合法规则时保留原 recur 段。
        if let recurText = payload["recur"] as? String, recurText.isEmpty,
           recurDraft.preset != "none",
           let originalRecur = block.timeTokenRaw
               .flatMap({ EditorDocument.findTimeToken(in: $0, protectedJSON: "")?.suffix.recur })
               .flatMap({ $0.isEmpty ? nil : $0 }) {
            payload["recur"] = originalRecur
        }
        guard let data = try? JSONSerialization.data(withJSONObject: payload),
              let json = String(data: data, encoding: .utf8) else { return "" }
        return json
    }

    // 行级令牌替换（绑定 ReplaceTimeToken；清除传 null）。失败返回 nil 不动原文。
    static func replaceTokenLine(_ lineRaw: String, suffixJSON: String?) -> String? {
        let protected = MarkdownProtection.protectedJSON(ranges: MarkdownProtection.inlineProtectedRanges(in: lineRaw))
        var err: NSError?
        let out = ZakkicoreReplaceTimeToken(lineRaw, suffixJSON, protected, &err)
        if let err {
            // 调用方传入的 suffix 由本端构造，正常不会失败；防御性返回原文。
            assertionFailure("ReplaceTimeToken 失败: \(err)")
            return nil
        }
        return out == lineRaw ? nil : out
    }

    // 行级写回的公共入口：块行替换 + 记账（可撤销）+ 重挂；焦点块同步缓冲，
    // 其余块不动键盘焦点。
    func applyLineEdit(blockIndex: Int, newBlockRaw: String) {
        guard writable, blockList.blocks.indices.contains(blockIndex) else { return }
        let block = blockList.blocks[blockIndex]
        guard newBlockRaw != block.raw else { return }
        let beforeDoc = raw
        let newDoc = LiveDocEditing.spliceLines(beforeDoc, block.startLine, block.endLine, newBlockRaw).joined(separator: "\n")
        let caret = min(trackedDocCaret, (newDoc as NSString).length)
        if blockIndex == activeIndex, activeEditor != nil {
            applyStructural(LiveDocEdit(doc: newDoc, caret: caret), kind: .command, fencePair: false)
        } else {
            applyOutOfBand(newDoc: newDoc)
        }
    }

    // 无焦点块路径（渲染 cell 打开的弹层/勾选推进）：记账 + 重切 + 重挂，不动焦点。
    func applyOutOfBand(newDoc: String) {
        let beforeDoc = raw
        guard newDoc != beforeDoc else { return }
        raw = newDoc
        let oldBlocks = blockList.blocks
        blockList = LiveBlocks.split(newDoc)
        recordOp(before: beforeDoc, after: newDoc, beforeCaret: trackedDocCaret,
                 afterCaret: trackedDocCaret, kind: .command, fencePair: false)
        remountChangedBlocks(oldBlocks, blockList.blocks)
        onRawChange?(newDoc)
    }
}

// MARK: - 弹层呈现（UIKit sheet 承载 SwiftUI 内容）

enum TimePopoverPresenter {
    // 从容器宿主视图控制器呈现（medium detent sheet）。
    static func present(from container: LiveEditorContainer, box: TimeTokenDraftBox,
                        onDone: @escaping () -> Void,
                        onClear: @escaping () -> Void,
                        onCancel: @escaping () -> Void) {
        let view = TimeTokenPopoverView(draft: box, onDone: onDone, onClear: onClear, onCancel: onCancel)
        let controller = UIHostingController(rootView: view)
        if let sheet = controller.sheetPresentationController {
            sheet.detents = [.medium(), .large()]
            sheet.prefersGrabberVisible = true
        }
        container.nearestViewController?.present(controller, animated: true)
    }
}

extension UIView {
    // 沿响应链找最近的视图控制器（容器无直接回指宿主的通道）。
    var nearestViewController: UIViewController? {
        var responder: UIResponder? = next
        while let r = responder {
            if let vc = r as? UIViewController { return vc }
            responder = r.next
        }
        return nil
    }
}

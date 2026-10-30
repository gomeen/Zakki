import Foundation

// 实时预览容器的文档变更与撤销事务（LiveEditorContainer 扩展）：键入写回、
// 跨块结构编辑、文档级撤销/重做记账。容器状态（DocOp、撤销双栈、光标跟踪）
// 与装配/焦点管理在 LiveEditorContainer.swift；被本文件访问的容器成员因拆分
// 开放为模块内可见。

extension LiveEditorContainer {
    // MARK: 键入写回（可见文字 → 块源码 → 文档 raw）

    func blockEditorDidChange(bufferRaw: String, kind: LiveEditKind, caretEstimate: Int) {
        guard let view = activeEditor, let idx = activeIndex, blockList.blocks.indices.contains(idx),
              let proj = activeProjection else { return }
        let block = blockList.blocks[idx]
        let bufferCaret = max(caretEstimate, 0)
        let hint = selectionHint
        selectionHint = nil
        let (newBlockRaw, sourceCaret) = proj.replacing(
            bufferRaw, caret: bufferCaret,
            selectedStart: hint?.location ?? -1,
            selectedEnd: hint.map { $0.location + $0.length } ?? -1
        )
        let beforeDoc = raw
        guard newBlockRaw != block.raw else {
            // 源码未变（收簇前后等价等）：缓冲与投影对齐即可。
            activeProjection = LiveProjection.project(block)
            applyStyles(to: view, block: block, projection: activeProjection)
            updateTypingAttributes()
            return
        }
        let blockStart = blockList.startOffset(of: idx)
        let beforeCaret = trackedDocCaret
        let newDoc = LiveDocEditing.spliceLines(beforeDoc, block.startLine, block.endLine, newBlockRaw).joined(separator: "\n")
        // 结算光标：直接存储写路径的选区同步发生在存储通知之后，回调期读到的
        // selectedRange 可能滞后；输入类事务按「前光标 + 全文增量」托底。
        let growth = (newDoc as NSString).length - (beforeDoc as NSString).length
        let settledCaret = kind == .command || kind == .ime
            ? blockStart + sourceCaret
            : max(blockStart + sourceCaret, beforeCaret + growth)
        let docCaret = min(max(settledCaret, 0), (newDoc as NSString).length)
        raw = newDoc
        let oldBlocks = blockList.blocks
        blockList = LiveBlocks.split(newDoc)
        recordOp(before: beforeDoc, after: newDoc, beforeCaret: beforeCaret, afterCaret: docCaret,
                 kind: kind, fencePair: false)
        trackedDocCaret = docCaret
        let (newIdx, newBlockStart) = blockList.blockOf(docCaret)
        let localCaret = max(0, docCaret - newBlockStart)
        if newIdx == idx {
            pendingSourceCaret = localCaret
            remountChangedBlocks(oldBlocks, blockList.blocks)
            syncActiveBuffer(deferred: true)
        } else {
            remountChangedBlocks(oldBlocks, blockList.blocks)
            activate(newIdx, sourceCaret: localCaret)
        }
        scrollCaretIntoView()
        onRawChange?(newDoc)
    }

    // MARK: 跨块结构编辑

    func handleEnter() -> Bool {
        guard let view = activeEditor, let idx = activeIndex, blockList.blocks.indices.contains(idx),
              let proj = activeProjection else { return false }
        let caret = view.selectedRange.location
        switch LiveDocEditing.enter(raw, list: blockList, index: idx, visibleCaret: caret, projection: proj) {
        case .edited(let edit):
            applyStructural(edit, kind: .command, fencePair: true)
            return true
        case .native, .unchanged:
            return false
        }
    }

    func handleBackspaceAtStart() -> Bool {
        guard let idx = activeIndex, blockList.blocks.indices.contains(idx) else { return false }
        guard let edit = LiveDocEditing.backspaceAtStart(raw, list: blockList, index: idx) else {
            return false
        }
        applyStructural(edit, kind: .command, fencePair: false)
        return true
    }

    func handleArrowCross(_ direction: Int) {
        guard let idx = activeIndex else { return }
        guard let target = LiveDocEditing.crossBlock(blockList.blocks, from: idx, direction: direction) else { return }
        let block = blockList.blocks[target.index]
        let end = LiveSyntax.length(block.raw)
        let caret = target.atEnd ? end : 0
        activate(target.index, sourceCaret: caret)
    }

    // 结构编辑统一入口：记账 + 重切 + 重定位焦点。
    func applyStructural(_ edit: LiveDocEdit, kind: LiveEditKind, fencePair: Bool) {
        let beforeDoc = raw
        let beforeCaret = trackedDocCaret
        guard edit.doc != beforeDoc else { return }
        raw = edit.doc
        let oldBlocks = blockList.blocks
        blockList = LiveBlocks.split(edit.doc)
        recordOp(before: beforeDoc, after: edit.doc, beforeCaret: beforeCaret, afterCaret: edit.caret,
                 kind: kind, fencePair: fencePair)
        trackedDocCaret = edit.caret
        let (idx, blockStart) = blockList.blockOf(min(max(edit.caret, 0), (edit.doc as NSString).length))
        let localCaret = max(0, edit.caret - blockStart)
        remountChangedBlocks(oldBlocks, blockList.blocks)
        if idx == activeIndex {
            pendingSourceCaret = localCaret
            syncActiveBuffer()
        } else {
            activate(idx, sourceCaret: localCaret)
        }
        scrollCaretIntoView()
        onRawChange?(edit.doc)
    }

    // 当前文档级光标（无焦点块的兜底）。
    var currentDocCaretFallback: Int { (raw as NSString).length }

    // MARK: 撤销/重做（文档级事务，块视图只路由）

    func undo() {
        guard let op = undoStack.popLast() else { return }
        redoStack.append(op)
        applyDocOp(op, reverse: true)
    }

    func redo() {
        guard let op = redoStack.popLast() else { return }
        undoStack.append(op)
        applyDocOp(op, reverse: false)
    }

    func applyDocOp(_ op: DocOp, reverse: Bool) {
        let fragment = reverse ? op.oldFragment : op.newFragment
        let caret = reverse ? op.beforeCaret : op.afterCaret
        // range 记在 before 文档：撤销在 after 文档里移除 newFragment，
        // 重做在 before 文档里移除 oldFragment（纯插入/删除同式成立）。
        let removedLength = reverse ? LiveSyntax.length(op.newFragment) : LiveSyntax.length(op.oldFragment)
        var units = Array(raw.utf16)
        let location = min(max(op.range.location, 0), units.count)
        let end = min(max(op.range.location + removedLength, location), units.count)
        units.replaceSubrange(location..<end, with: Array(fragment.utf16))
        let doc = String(decoding: units, as: UTF16.self)
        raw = doc
        let oldBlocks = blockList.blocks
        blockList = LiveBlocks.split(doc)
        trackedDocCaret = caret
        remountChangedBlocks(oldBlocks, blockList.blocks)
        let (idx, blockStart) = blockList.blockOf(min(max(caret, 0), units.count))
        let localCaret = max(0, caret - blockStart)
        if idx == activeIndex {
            pendingSourceCaret = localCaret
            syncActiveBuffer()
        } else {
            activate(idx, sourceCaret: localCaret)
        }
        scrollCaretIntoView()
        onRawChange?(doc)
    }

    // 记账：区间片段 diff + 连续键入合并（对齐桌面 rememberLiveEdit）。
    func recordOp(before: String, after: String, beforeCaret: Int, afterCaret: Int,
                          kind: LiveEditKind, fencePair: Bool) {
        guard before != after else { return }
        let uBefore = Array(before.utf16)
        let uAfter = Array(after.utf16)
        var prefix = 0
        while prefix < uBefore.count, prefix < uAfter.count, uBefore[prefix] == uAfter[prefix] {
            prefix += 1
        }
        var suffix = 0
        while suffix < uBefore.count - prefix, suffix < uAfter.count - prefix,
              uBefore[uBefore.count - 1 - suffix] == uAfter[uAfter.count - 1 - suffix] {
            suffix += 1
        }
        // 纯插入锚定光标而非 diff 端点：重复字符在末尾有多个等价 diff，
        // 连续键入合并依赖「op 位置 == 上一事务结束光标」这一连续性判据。
        var location = prefix
        if prefix >= uBefore.count - suffix {
            location = min(prefix, max(0, beforeCaret))
        }
        let range = NSRange(location: location, length: uBefore.count - suffix - prefix)
        let oldFragment = String(decoding: uBefore[prefix..<(uBefore.count - suffix)], as: UTF16.self)
        let newFragment = String(decoding: uAfter[prefix..<(uAfter.count - suffix)], as: UTF16.self)

        // 连续键入合并：上一事务也是键入、光标连续且本次为纯插入（不合并删除）。
        if let last = undoStack.last, last.kind == .typing, kind == .typing,
           oldFragment.isEmpty,
           last.afterCaret == range.location,
           last.afterCaret >= last.range.location,
           last.afterCaret <= last.range.location + LiveSyntax.length(last.newFragment) {
            let delta = range.location - last.range.location
            let lastUnits = Array(last.newFragment.utf16)
            let ins = Array(newFragment.utf16)
            let mergedNew = String(decoding: lastUnits[0..<delta] + ins + lastUnits[delta...], as: UTF16.self)
            undoStack[undoStack.count - 1] = DocOp(
                range: last.range, oldFragment: last.oldFragment, newFragment: mergedNew,
                beforeCaret: last.beforeCaret, afterCaret: afterCaret, kind: .typing
            )
            redoStack.removeAll()
            return
        }
        // 围栏逐键输入与回车补闭栏合并成一个事务（桌面 editFencePair）。
        // 合并语义 = 展开上一事务的 after 到本次 after（before 不变）：新片段在
        // delta 处插入；旧片段代表第一次转换的删除量，本次是纯追加时原样保留
        //（不得按 delta 切片——纯插入的 oldFragment 为空，delta 只对新片段合法）。
        if fencePair, let last = undoStack.last, last.kind == .typing,
           last.afterCaret == range.location,
           last.afterCaret >= last.range.location,
           last.afterCaret <= last.range.location + LiveSyntax.length(last.newFragment) {
            let delta = range.location - last.range.location
            let lastUnits = Array(last.newFragment.utf16)
            let insNew = Array(newFragment.utf16)
            let mergedNew = String(decoding: lastUnits[0..<delta] + insNew + lastUnits[delta...], as: UTF16.self)
            undoStack[undoStack.count - 1] = DocOp(
                range: last.range, oldFragment: last.oldFragment, newFragment: mergedNew,
                beforeCaret: last.beforeCaret, afterCaret: afterCaret, kind: .command
            )
            redoStack.removeAll()
            return
        }
        undoStack.append(DocOp(range: range, oldFragment: oldFragment, newFragment: newFragment,
                               beforeCaret: beforeCaret, afterCaret: afterCaret, kind: kind))
        if undoStack.count > undoLimit {
            undoStack.removeFirst()
        }
        redoStack.removeAll()
    }
}

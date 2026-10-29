import UIKit

// 撤销路由：NSUndoManager 的分组时机受 UITextView 内部选择更新干扰，
// 编辑器改用显式双栈；UITextView 经 responder chain 找到的 undoManager 把
// undo/redo 路由到该栈，canUndo/canRedo 同步供菜单态使用。
final class EditorUndoManager: UndoManager {
    var canUndoRoute: (() -> Bool)?
    var canRedoRoute: (() -> Bool)?
    var undoRoute: (() -> Void)?
    var redoRoute: (() -> Void)?

    override var canUndo: Bool { canUndoRoute?() ?? super.canUndo }
    override var canRedo: Bool { canRedoRoute?() ?? super.canRedo }
    override func undo() { undoRoute?() }
    override func redo() { redoRoute?() }
}

// UITextView 适配层（D4）：放行 UITextView 的原生输入实现（TextKit2 的
// insertText:/deleteBackward: 内部有自身簿记，绕开会错乱），随后以前后差分记录
// (undo, redo) 原子操作到显式双栈。缓冲一致性由存储通知统一对账（键入、撤销/重做
// 回放、程序化替换只有这一条入口）；收组件注册撤销步（D6）；copy/cut/paste 覆写为
// raw 投影往返（D7）。
final class EditorTextView: UITextView, UITextViewDelegate {
    private(set) var document: EditorDocument

    // 内容实际变化后回调（含收组件），宿主接防抖保存；参数是当前 raw。
    var onContentChanged: ((String) -> Void)?

    private var isReconciling = false
    private var isApplyingEdit = false
    // 粘贴是一个独立撤销单元：其触发的收组件并入同单元，一次撤销回退整次粘贴。
    private var isPasteUnit = false
    // 输入事务期间（recordNativeEdit 的 edit() 内）触发的收组件先挂起执行，待输入
    // 操作记账完成后再落到存储并入栈：既保证观察差分只含原生输入，又保证撤销序
    // 为「先撤销收组件、再撤销输入」（undo 栈 LIFO）。
    private struct DeferredCollapse {
        var range: NSRange
        var piece: EditorPiece
    }
    private var deferredCollapses: [DeferredCollapse] = []

    // MARK: 撤销双栈

    private struct UndoOp {
        var undo: () -> Void
        var redo: () -> Void
    }

    // isTyping 单元吸收连续键入；点选位移/结构性操作闭合它（身份转非键入）。
    private struct UndoUnit {
        var isTyping: Bool
        var ops: [UndoOp] = []
    }

    private var undoStack: [UndoUnit] = []
    private var redoStack: [UndoUnit] = []
    private var isReplaying = false

    private let editorUndoManager = EditorUndoManager()
    private var storageObserver: NSObjectProtocol?
    private var pillTap: UITapGestureRecognizer?

    // M2-5 实时样式：只加属性不改字符，全部写入口都在 isReconciling 包裹内
    //（不进撤销栈、不触发 onContentChanged、观察者不重入）。
    let markdownStyler = MarkdownStyler()
    private let fragmentProvider = MarkdownFragmentProvider()

    // 测试探针：显式撤销/重做单元数（样式通路不得改变这两个数）。
    var undoUnitCount: Int { undoStack.count }
    var redoUnitCount: Int { redoStack.count }

    init(raw: String, writable: Bool) {
        document = EditorDocument(raw: raw)
        super.init(frame: .zero, textContainer: nil)
        backgroundColor = Theme.Semantic.background
        // iOS 无公开的选区叠色 API：选区与光标由 accent 驱动（主题选区叠色
        // 令牌保留在 Theme.Semantic 供后续接入）。
        tintColor = Theme.Semantic.accent
        font = Theme.Typography.bodyFont
        textContainerInset = UIEdgeInsets(
            top: Theme.Typography.padY,
            left: Theme.Typography.padX,
            bottom: Theme.Typography.padY,
            right: Theme.Typography.padX
        )
        isEditable = writable
        isSelectable = true
        delegate = self
        keyboardDismissMode = .interactive
        typingAttributes = EditorRender.baseAttributes
        // 挂引用竖线 fragment 提供者早于首渲染，首次排版即走自定义 fragment。
        ensureFragmentProviderInstalled()
        // 初始渲染先于挂观察者，避免自身装配触发对账。
        attributedText = EditorRender.pieces(document.pieces)
        applyInitialMarkdownStyles()
        // 用通知而非 storage.delegate：UITextView 可能已占用该代理位，通知支持多观察者。
        storageObserver = NotificationCenter.default.addObserver(
            forName: NSTextStorage.didProcessEditingNotification,
            object: textStorage,
            queue: .main
        ) { [weak self] note in
            guard let self, !self.isReconciling, let storage = note.object as? NSTextStorage else { return }
            self.reconcile(storage: storage, editedRange: storage.editedRange)
        }
        editorUndoManager.canUndoRoute = { [weak self] in !(self?.undoStack.isEmpty ?? true) }
        editorUndoManager.canRedoRoute = { [weak self] in !(self?.redoStack.isEmpty ?? true) }
        editorUndoManager.undoRoute = { [weak self] in self?.performUndo() }
        editorUndoManager.redoRoute = { [weak self] in self?.performRedo() }
        // 点组件只吞 tap：命中组件字符的点按不开菜单、不产生编辑行为（时间弹层属 M3，此处仅为钩子）。
        let pillTap = UITapGestureRecognizer(target: self, action: #selector(pillTapped(_:)))
        addGestureRecognizer(pillTap)
        self.pillTap = pillTap
        // 装配期 UITraitCollection.current 未必等于视图最终外观，按视图 traits
        // 重画一次胶囊位图；入层次后如遇明暗切换再由 traitCollectionDidChange 接手。
        refreshPillAppearances()
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("编辑器视图不走 IB")
    }

    deinit {
        if let storageObserver {
            NotificationCenter.default.removeObserver(storageObserver)
        }
    }

    override var undoManager: UndoManager? { editorUndoManager }

    var isUndoingOrRedoing: Bool { isReplaying }

    // 收组件只在无组字缓冲且非撤销/重做回放时进行（D6）。
    private var canCollapse: Bool {
        markedTextRange == nil && !isReplaying
    }

    // MARK: 输入路径（原生编辑 + 事后记账）

    // 键入保持原生实现（TextKit2 的 insertText: 内部簿记与组字兼容，不可绕开），
    // 仅用前后差分补记撤销原子操作；连续键入并入同一撤销单元。
    // 例外：空缓冲时 UIKit 的 insertText: 空文档内部路径会在随后的收组件直接
    // 存储写之后用陈旧区间查询存储（iOS 26 实测 attributedSubstring 越界；
    // 非空缓冲与 paste: 路径无此问题），改走直接存储写，记账管线不变。
    override func insertText(_ text: String) {
        guard isEditable, !isReplaying, !isApplyingEdit else {
            super.insertText(text)
            return
        }
        if textStorage.length == 0 {
            insertByDirectWrite(text)
            return
        }
        recordNativeEdit(UndoUnit(isTyping: true)) { super.insertText(text) }
    }

    // 空缓冲插入：直接写存储（与剪切/退格同一机制）并同步插入点，
    // 撤销/收组件仍经 recordNativeEdit 同一管线。
    private func insertByDirectWrite(_ text: String) {
        let attr = NSAttributedString(string: text, attributes: typingAttributes)
        recordNativeEdit(UndoUnit(isTyping: true)) { [weak self] in
            guard let self else { return }
            self.textStorage.replaceCharacters(in: NSRange(location: 0, length: 0), with: attr)
            self.syncCaretAfterDirectWrite(at: (text as NSString).length)
        }
    }

    override func deleteBackward() {
        // 组字期间的退格由 UIKit 的组字逻辑处理（收缩 marked text），不落这里。
        guard markedTextRange == nil else {
            super.deleteBackward()
            return
        }
        guard isEditable, !isReplaying, !isApplyingEdit else {
            super.deleteBackward()
            return
        }
        // 不走原生 deleteBackward：直接存储写之后 UIKit 的内部插入点会滞后，
        // 原生删除按旧位置计算会越界。此处按选区/前一字符（代理对成对）直接删。
        guard let range = deletionRange() else {
            super.deleteBackward()
            return
        }
        replaceRecordingUndo(range: range, with: NSAttributedString(), unit: UndoUnit(isTyping: !isPasteUnit))
        syncCaretAfterDirectWrite(at: range.location)
    }

    // 退格目标区间：有选区删选区；否则删光标前一字符（代理对成对删除；
    // 多标量 emoji 组 UIKit 按整字素删，此处按单个代理对删，M2-1 行为差异可接受）。
    private func deletionRange() -> NSRange? {
        let selection = selectedRange
        if selection.length > 0 { return selection }
        let ns = textStorage.string as NSString
        let caret = selection.location
        guard caret > 0 else { return nil }
        var start = caret - 1
        if start > 0 {
            let high = ns.character(at: start - 1)
            let low = ns.character(at: start)
            if high >= 0xD800, high <= 0xDBFF, low >= 0xDC00, low <= 0xDFFF {
                start -= 1
            }
        }
        return NSRange(location: start, length: caret - start)
    }

    // 直接存储写并记撤销原子操作（收组件/剪切等离散动作共用）。
    private func replaceRecordingUndo(range: NSRange, with replacement: NSAttributedString, unit: UndoUnit) {
        let clamped = clamp(range, to: textStorage.length)
        let old = textStorage.attributedSubstring(from: clamped)
        let op = UndoOp(
            undo: { [weak self] in self?.applyOp(NSRange(location: clamped.location, length: (replacement.string as NSString).length), old) },
            redo: { [weak self] in self?.applyOp(clamped, replacement) }
        )
        textStorage.replaceCharacters(in: clamped, with: replacement)
        record(unit: unit, op: op)
    }

    // 执行一段原生编辑并记录撤销原子操作；期间收组件操作延后入栈，保证
    // 撤销序为「先撤销收组件、再撤销本次输入」（undo 栈 LIFO）。
    private func recordNativeEdit(_ unit: UndoUnit, _ edit: () -> Void) {
        isApplyingEdit = true
        defer { isApplyingEdit = false }
        let beforeLength = textStorage.length
        let before = textStorage.attributedSubstring(from: NSRange(location: 0, length: beforeLength))
        edit()
        let afterLength = textStorage.length
        let after = textStorage.attributedSubstring(from: NSRange(location: 0, length: afterLength))
        if beforeLength == afterLength, before.string == after.string { return }
        let prefix = Self.commonPrefixLength(before.string, after.string)
        let suffix = Self.commonSuffixLength(before.string, after.string, limit: max(0, min(beforeLength, afterLength) - prefix))
        let removed = before.attributedSubstring(from: NSRange(location: prefix, length: beforeLength - suffix - prefix))
        let inserted = after.attributedSubstring(from: NSRange(location: prefix, length: afterLength - suffix - prefix))
        let op = UndoOp(
            undo: { [weak self] in self?.applyOp(NSRange(location: prefix, length: inserted.length), removed) },
            redo: { [weak self] in self?.applyOp(NSRange(location: prefix, length: removed.length), inserted) }
        )
        record(unit: unit, op: op)
        flushDeferredCollapses(mergeIntoPaste: isPasteUnit)
    }

    override func cut(_ sender: Any?) {
        let range = selectedRange
        guard range.length > 0, isEditable, !isReplaying else { return }
        // 剪贴板写 raw 投影（外部拿到可读 Markdown），缓冲删除自成撤销单元。
        UIPasteboard.general.string = document.raw(in: range)
        replaceRecordingUndo(range: range, with: NSAttributedString(), unit: UndoUnit(isTyping: false))
        syncCaretAfterDirectWrite(at: range.location)
    }

    override func paste(_ sender: Any?) {
        guard isEditable, !isReplaying, let text = UIPasteboard.general.string else { return }
        // 粘贴是独立撤销单元，走同一插入管线：剪贴板 plain string 是 raw 投影（D7），
        // 进来再经扫描收组件；外来无主 U+FFFC 先行净化。
        isPasteUnit = true
        defer { isPasteUnit = false }
        recordNativeEdit(UndoUnit(isTyping: false)) {
            super.insertText(EditorDocument.sanitizingPasted(text))
        }
    }

    override func copy(_ sender: Any?) {
        let range = selectedRange
        guard range.length > 0 else {
            super.copy(sender)
            return
        }
        UIPasteboard.general.string = document.raw(in: range)
    }

    // 组字提交（上屏）后恢复收簇识别（D6）。
    override func unmarkText() {
        super.unmarkText()
        guard !isReconciling, !isReplaying else { return }
        reconcile(storage: textStorage, editedRange: selectedRange)
    }

    // 点选位移结束一段连续键入。
    func textViewDidChangeSelection(_ textView: UITextView) {
        guard !isApplyingEdit, !isReplaying else { return }
        closeTypingUnit()
    }

    // MARK: 撤销栈操作

    private func record(unit newUnit: UndoUnit, op: UndoOp) {
        // 键入并入栈顶键入单元；其余各自成单元。
        if newUnit.isTyping, let last = undoStack.last, last.isTyping {
            undoStack[undoStack.count - 1].ops.append(op)
            redoStack.removeAll()
            return
        }
        var unit = newUnit
        unit.ops.append(op)
        undoStack.append(unit)
        redoStack.removeAll()
    }

    // 键入单元的“闭合”即不再吸收后续键入。
    private func closeTypingUnit() {
        if let last = undoStack.last, last.isTyping {
            undoStack[undoStack.count - 1].isTyping = false
        }
    }

    // 撤销/重做与收组件的区间替换直接写存储：UIKit 输入管线会过滤 U+FFFC
    //（insertText/replace/text 赋值均丢弃对象字符），组件字符只能经存储写入；
    // 附件属性随字符一并恢复（D1）。真实键盘场景的会话衔接由「收组件后
    // attributedText 全量重渲染」的 spike 已验证模式兜底，人工验收项覆盖。
    private func applyOp(_ range: NSRange, _ attr: NSAttributedString) {
        let clamped = clamp(range, to: textStorage.length)
        textStorage.replaceCharacters(in: clamped, with: attr)
        syncCaretAfterDirectWrite(at: clamped.location + attr.length)
    }

    // 直接写存储后 UIKit 内部插入点不会自动跟随，双通道显式同步，
    // 避免后续原生输入按旧插入点计算（越界/错位）。
    private func syncCaretAfterDirectWrite(at offset: Int) {
        selectedRange = NSRange(location: offset, length: 0)
        if let start = position(from: beginningOfDocument, offset: offset),
           let end = position(from: start, offset: 0) {
            selectedTextRange = textRange(from: start, to: end)
        }
    }

    private func performUndo() {
        guard var unit = undoStack.popLast() else { return }
        // 键入单元内操作倒序回退；回放期间对账跳过收簇识别（D6）。
        isReplaying = true
        defer { isReplaying = false }
        for op in unit.ops.reversed() {
            op.undo()
        }
        redoStack.append(unit)
    }

    private func performRedo() {
        guard var unit = redoStack.popLast() else { return }
        isReplaying = true
        defer { isReplaying = false }
        for op in unit.ops {
            op.redo()
        }
        undoStack.append(unit)
    }

    // MARK: 存储对账

    // 存储内容变化的对账入口：diff 同步文档，再落实文档的收组件决策。
    private func reconcile(storage: NSTextStorage, editedRange: NSRange) {
        // 收簇挂起期间 document 有意领先于存储（M2-5 样式失效的再入通知也落在此窗口），
        // 此时 diff 会把文档重置回存储旧文本并重复挂起收簇；挂起 flush 落存储后的
        // 通知才是权威对账时机。
        guard deferredCollapses.isEmpty else { return }
        isReconciling = true
        defer { isReconciling = false }

        let newBuffer = storage.string
        let oldBuffer = document.bufferText
        var mutated = false

        if newBuffer != oldBuffer {
            // 公共前后缀定位改动；替换内容从存储的属性里读（撤销恢复的附件带 tokenRaw）。
            let prefix = Self.commonPrefixLength(oldBuffer, newBuffer)
            let oldLength = (oldBuffer as NSString).length
            let newLength = (newBuffer as NSString).length
            let suffixLimit = min(oldLength, newLength) - prefix
            let suffix = Self.commonSuffixLength(oldBuffer, newBuffer, limit: max(0, suffixLimit))
            let oldRange = NSRange(location: prefix, length: oldLength - suffix - prefix)
            let insertedRange = NSRange(location: prefix, length: newLength - suffix - prefix)
            let replacement = pieces(in: insertedRange, of: storage)
            let outcome = document.apply(
                .init(replacedRange: oldRange, replacement: replacement),
                detectTokens: canCollapse
            )
            mutated = true
            applyReplacements(outcome.replacements)
        } else if editedRange.location != NSNotFound, editedRange.location >= 0 {
            // 文本未变但存储有编辑动作（组字提交清属性等）：在改动行补一次收簇识别。
            let location = min(editedRange.location, document.bufferLength)
            let outcome = document.apply(
                .init(replacedRange: NSRange(location: location, length: 0), replacement: []),
                detectTokens: canCollapse
            )
            mutated = !outcome.replacements.isEmpty
            applyReplacements(outcome.replacements)
        }

        enforceBufferInvariant()
        applyMarkdownStyles(after: newBuffer, in: storage, editedRange: editedRange)
        if mutated {
            onContentChanged?(document.rawText)
        }
    }

    // MARK: 实时样式（M2-5）

    // init 全量渲染后的整篇上样式；观察者尚未挂上，包 isReconciling 仅保持口径统一。
    private func applyInitialMarkdownStyles() {
        isReconciling = true
        defer { isReconciling = false }
        markdownStyler.restyleDocument(storage: textStorage)
    }

    // 对账收尾后补齐样式：以对账入口的存储快照与收尾快照做公共前后缀定位脏区
    //（涵盖原生输入、收簇替换与不变式修正的全部落点，撤销/重做回放路径的
    // applyOp 直写存储也会经通知进这里，回放后的受影响行由此补上样式）。
    // 组字期间不上样式（canCollapse 同款门控时机），提交后的 reconcile 会补上。
    private func applyMarkdownStyles(after entryBuffer: String, in storage: NSTextStorage, editedRange: NSRange) {
        guard markedTextRange == nil else { return }
        ensureFragmentProviderInstalled()
        let finalBuffer = storage.string
        let dirty: NSRange?
        if finalBuffer != entryBuffer {
            let prefix = Self.commonPrefixLength(entryBuffer, finalBuffer)
            let oldLength = (entryBuffer as NSString).length
            let newLength = (finalBuffer as NSString).length
            let suffixLimit = min(oldLength, newLength) - prefix
            let suffix = Self.commonSuffixLength(entryBuffer, finalBuffer, limit: max(0, suffixLimit))
            dirty = NSRange(location: prefix, length: max(0, newLength - suffix - prefix))
        } else if editedRange.location != NSNotFound, editedRange.location >= 0 {
            dirty = editedRange
        } else {
            dirty = nil
        }
        guard let dirty else { return }
        markdownStyler.restyleEditedLines(storage: storage, dirty: dirty)
    }

    // 明暗切换整篇重上：动态色自动跟随，重上同时刷新段样式与引用竖线颜色。
    // internal：测试走同一路径验证不变式（样式不改字符/不进撤销栈/无写回）。
    func applyMarkdownThemeRefresh() {
        guard markedTextRange == nil else { return }
        isReconciling = true
        defer { isReconciling = false }
        ensureFragmentProviderInstalled()
        markdownStyler.restyleDocument(storage: textStorage)
    }

    // 引用竖线 fragment 提供者：UITextView 自身是 textLayoutManager 的 delegate，
    // 提供者用 forwarding 保留其原行为；系统可能在内部重设 delegate，每次重上前
    // 核查并重挂。
    private func ensureFragmentProviderInstalled() {
        guard let layoutManager = textLayoutManager else { return }
        if layoutManager.delegate !== fragmentProvider {
            fragmentProvider.owner = markdownStyler
            fragmentProvider.originalDelegate = layoutManager.delegate
            layoutManager.delegate = fragmentProvider
        }
    }

    private func applyReplacements(_ ops: [EditorDocument.BufferReplacement]) {
        for op in ops {
            let clamped = clamp(op.range, to: textStorage.length)
            // 输入事务内：挂起到事务结束再落存储（保证观察差分只含原生输入，
            // 且撤销序为「先撤销收组件、再撤销输入」）。
            if isApplyingEdit {
                deferredCollapses.append(DeferredCollapse(range: clamped, piece: op.piece))
                continue
            }
            let old = textStorage.attributedSubstring(from: clamped)
            let replacement = EditorRender.piece(op.piece)
            let collapse = collapseOp(range: clamped, old: old, replacement: replacement)
            textStorage.replaceCharacters(in: clamped, with: replacement)
            if isPasteUnit, !undoStack.isEmpty {
                undoStack[undoStack.count - 1].ops.append(collapse)
            } else {
                record(unit: UndoUnit(isTyping: false), op: collapse)
            }
        }
    }

    // 执行挂起的收组件：粘贴期间并入粘贴单元（一次撤销整回退），其余自成撤销步。
    private func flushDeferredCollapses(mergeIntoPaste: Bool) {
        let items = deferredCollapses
        deferredCollapses = []
        for item in items {
            let old = textStorage.attributedSubstring(from: item.range)
            let replacement = EditorRender.piece(item.piece)
            let collapse = collapseOp(range: item.range, old: old, replacement: replacement)
            textStorage.replaceCharacters(in: item.range, with: replacement)
            if mergeIntoPaste, !undoStack.isEmpty {
                undoStack[undoStack.count - 1].ops.append(collapse)
            } else {
                record(unit: UndoUnit(isTyping: false), op: collapse)
            }
            // 收组件后光标落在组件后（D6）。
            syncCaretAfterDirectWrite(at: item.range.location + 1)
        }
    }

    private func collapseOp(range: NSRange, old: NSAttributedString, replacement: NSAttributedString) -> UndoOp {
        let undoRange = NSRange(location: range.location, length: replacement.length)
        return UndoOp(
            undo: { [weak self] in self?.applyOp(undoRange, old) },
            redo: { [weak self] in self?.applyOp(range, replacement) }
        )
    }

    // 兜底不变式：存储内容必须等于文档缓冲投影（外来附件字符等异常路径的最终修正）。
    // 收组件挂起期间文档有意领先于存储，跳过。
    private func enforceBufferInvariant() {
        guard deferredCollapses.isEmpty else { return }
        let actual = textStorage.string
        let expected = document.bufferText
        guard actual != expected else { return }
        let prefix = Self.commonPrefixLength(actual, expected)
        let actualLength = (actual as NSString).length
        let expectedLength = (expected as NSString).length
        let suffix = Self.commonSuffixLength(actual, expected, limit: max(0, min(actualLength, expectedLength) - prefix))
        let range = NSRange(location: prefix, length: actualLength - suffix - prefix)
        let replacement = EditorRender.pieces(
            document.pieces(in: NSRange(location: prefix, length: expectedLength - suffix - prefix))
        )
        textStorage.replaceCharacters(in: range, with: replacement)
    }

    // MARK: 输入内容→文档片段

    // 从富文本提取替换片段：组件附件读回 tokenRaw（撤销/重做回放路径），
    // 无主附件字符与杂散 U+FFFC 过滤（D7）。
    private func pieces(in range: NSRange, of storage: NSTextStorage) -> [EditorPiece] {
        let clamped = clamp(range, to: storage.length)
        guard clamped.length > 0 else { return [] }
        var out: [EditorPiece] = []
        storage.enumerateAttributes(in: clamped) { attrs, subRange, _ in
            let text = storage.attributedSubstring(from: subRange).string
            if let attachment = attrs[.attachment] as? TimeTokenAttachment {
                out.append(.token(attachment.tokenRaw))
            } else {
                let cleaned = EditorDocument.sanitizingPasted(text)
                if !cleaned.isEmpty {
                    out.append(.text(cleaned))
                }
            }
        }
        return out
    }

    // MARK: 点组件只吞 tap（时间弹层属 M3）

    // 胶囊位图不带动态色：明暗外观切换后按新 traits 重画全部附件（原地换图，
    // 不改文本、不进撤销栈）；期间跳过对账，属性编辑的通知不进收簇识别。
    override func traitCollectionDidChange(_ previousTraitCollection: UITraitCollection?) {
        super.traitCollectionDidChange(previousTraitCollection)
        guard let previousTraitCollection,
              previousTraitCollection.hasDifferentColorAppearance(comparedTo: traitCollection) else { return }
        refreshPillAppearances()
        applyMarkdownThemeRefresh()
    }

    private func refreshPillAppearances() {
        let storage = textStorage
        var hasPill = false
        storage.enumerateAttribute(
            .attachment,
            in: NSRange(location: 0, length: storage.length)
        ) { value, _, _ in
            guard let attachment = value as? TimeTokenAttachment else { return }
            attachment.refreshAppearance(traits: self.traitCollection)
            hasPill = true
        }
        guard hasPill else { return }
        isReconciling = true
        defer { isReconciling = false }
        storage.edited(.editedAttributes, range: NSRange(location: 0, length: storage.length), changeInLength: 0)
    }

    @objc private func pillTapped(_ recognizer: UITapGestureRecognizer) {
        // M3 在此打开时间弹层；M2-1 刻意无动作。
    }

    // 仅自身组件点击手势按命中生效，不影响系统手势。
    override func gestureRecognizerShouldBegin(_ gestureRecognizer: UIGestureRecognizer) -> Bool {
        if gestureRecognizer === pillTap {
            return isPillHit(at: gestureRecognizer.location(in: self))
        }
        return super.gestureRecognizerShouldBegin(gestureRecognizer)
    }

    private func isPillHit(at point: CGPoint) -> Bool {
        guard let position = closestPosition(to: point) else { return false }
        let offset = offset(from: beginningOfDocument, to: position)
        let ns = textStorage.string as NSString
        var candidates = [offset]
        if offset > 0 { candidates.append(offset - 1) }
        for candidate in candidates {
            guard candidate < ns.length else { continue }
            if ns.substring(with: NSRange(location: candidate, length: 1)) == "\u{FFFC}" {
                return true
            }
        }
        return false
    }

    // MARK: 工具

    private func clamp(_ range: NSRange, to length: Int) -> NSRange {
        let location = min(max(range.location, 0), length)
        let end = min(max(range.location + range.length, location), length)
        return NSRange(location: location, length: end - location)
    }

    // MARK: UTF-16 公共前后缀

    static func commonPrefixLength(_ a: String, _ b: String) -> Int {
        let n = min(a.utf16.count, b.utf16.count)
        guard n > 0 else { return 0 }
        var i = 0
        var itA = a.utf16.makeIterator()
        var itB = b.utf16.makeIterator()
        while i < n, let x = itA.next(), let y = itB.next(), x == y {
            i += 1
        }
        return i
    }

    static func commonSuffixLength(_ a: String, _ b: String, limit: Int) -> Int {
        let au = Array(a.utf16)
        let bu = Array(b.utf16)
        var i = 0
        while i < limit, i < au.count, i < bu.count, au[au.count - 1 - i] == bu[bu.count - 1 - i] {
            i += 1
        }
        return i
    }
}

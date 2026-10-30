import UIKit

// 撤销路由：块编辑器自身不持撤销栈（容器持有文档级事务栈，同构桌面
// live_history）；UITextView 经 responder chain 找到的 undoManager 把
// undo/redo 路由到容器，canUndo/canRedo 同步供菜单态使用。
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

// 块编辑事务种类（容器记账用，对齐桌面 editKind）：连续键入合并为一个撤销
// 事务；粘贴、IME 一次确认、剪切与结构化命令各自独立成事务。
enum LiveEditKind: Equatable {
    case typing
    case paste
    case ime
    case command
}

// 块编辑器（M2-6）：实时模式下焦点块的编辑面。缓冲只承载该块的「可见文字」
//（标记已由 LiveProjection 剥离），时间令牌仍收成单个 U+FFFC + 附件（管线
// 与 M2-1 相同：core 绑定识别、保护区、粘贴净化、剪贴板 raw 投影）。
// 缓冲一致性由存储通知统一对账；撤销/重做路由到容器；回车、块首退格与
// 越界方向键先询问容器（跨块结构编辑），未消费时走原生路径。
final class BlockEditorView: UITextView, UITextViewDelegate {
    private(set) var document = EditorDocument(raw: "")

    // 缓冲实际变化后回调（含收簇）：document.rawText（令牌已展开，U+FFFC 不外流）、
    // 事务种类、编辑前可见选区（replace 保留包裹的消歧提示）与结算后光标估算
    //（直接存储写路径的选区同步发生在存储通知之后，回调期 selectedRange 滞后）。
    var onBufferChange: ((String, LiveEditKind, NSRange?, Int) -> Void)?
    // 选区变化（容器更新 rawSelection 与插入点属性）。
    var onSelectionChanged: (() -> Void)?
    // 回车询问容器：返回 true 表示容器已按块边界语义处理（拆块/补围栏等）。
    var shouldDelegateEnter: (() -> Bool)?
    // 块首退格询问容器：返回 true 表示容器已并块。
    var shouldDelegateBackspaceAtStart: (() -> Bool)?
    // 方向键越过块边界（-1 向上/向左，+1 向下/向右）。
    var onArrowCross: ((Int) -> Void)?

    // 撤销管理器由容器注入（文档级事务栈在容器，同构桌面 live_history）。
    var editorUndoManager = EditorUndoManager()

    private var isReconciling = false
    private var isApplyingEdit = false
    private var pendingKind: LiveEditKind?
    private var prevComposing = false
    private var isPasteUnit = false
    private var editSelectionHint: NSRange?

    // 收簇挂起窗口：document 有意领先于存储，期间对账跳过（M2-5 同不变式）。
    private struct DeferredCollapse {
        var range: NSRange
        var piece: EditorPiece
    }
    private var deferredCollapses: [DeferredCollapse] = []

    private var storageObserver: NSObjectProtocol?
    private var pillTap: UITapGestureRecognizer?

    // 最近一次同步的投影：容器据此判断投影是否变化（桌面 mdProjection 同款比较）。
    private(set) var lastSyncedProjection: LiveProjection?

    var isUndoingOrRedoing: Bool { false }

    init(writable: Bool) {
        super.init(frame: .zero, textContainer: nil)
        backgroundColor = .clear
        tintColor = Theme.Semantic.accent
        font = Theme.Typography.bodyFont
        textContainerInset = .zero
        isEditable = writable
        isSelectable = true
        delegate = self
        keyboardDismissMode = .interactive
        typingAttributes = EditorRender.baseAttributes
        // 初始空文档；真实缓冲由容器 load(projection:) 装载。
        textStorage.setAttributedString(EditorRender.pieces(document.pieces))
        storageObserver = NotificationCenter.default.addObserver(
            forName: NSTextStorage.didProcessEditingNotification,
            object: textStorage,
            queue: .main
        ) { [weak self] note in
            guard let self, !self.isReconciling, let storage = note.object as? NSTextStorage else { return }
            self.reconcile(storage: storage, editedRange: storage.editedRange)
        }
        // 点组件只吞 tap（时间弹层属 M3，此处仅为钩子）。
        let pillTap = UITapGestureRecognizer(target: self, action: #selector(pillTapped(_:)))
        addGestureRecognizer(pillTap)
        self.pillTap = pillTap
        refreshPillAppearances()
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("块编辑器不走 IB")
    }

    deinit {
        if let storageObserver {
            NotificationCenter.default.removeObserver(storageObserver)
        }
    }

    override var undoManager: UndoManager? { editorUndoManager }

    var canCollapse: Bool { markedTextRange == nil }

    // MARK: 缓冲装载（容器驱动）

    // 用投影装载可见缓冲：时间占位按其源码区间展开后经 EditorDocument 重新
    // 收簇（附件 tokenRaw 随之恢复）；期间对账静默，不产生 onBufferChange。
    func load(projection: LiveProjection, block: LiveBlock, selection: NSRange?) {
        let expanded = projection.expandedBufferText()
        document = EditorDocument(raw: expanded)
        lastSyncedProjection = projection
        isReconciling = true
        defer { isReconciling = false }
        textStorage.setAttributedString(EditorRender.pieces(document.pieces))
        if let selection {
            let length = (textStorage.string as NSString).length
            let location = min(max(selection.location, 0), length)
            let end = min(max(selection.location + selection.length, location), length)
            selectedRange = NSRange(location: location, length: end - location)
        }
        refreshPillAppearances()
    }

    // 属性-only 编辑入口（样式重上）：对账静默、通知不重入。
    func performAttributeOnly(_ work: () -> Void) {
        isReconciling = true
        defer { isReconciling = false }
        work()
    }

    // MARK: 输入路径

    override func insertText(_ text: String) {
        guard isEditable, !isApplyingEdit else {
            super.insertText(text)
            return
        }
        if text == "\n", markedTextRange == nil, let delegateEnter = shouldDelegateEnter, delegateEnter() {
            return
        }
        if textStorage.length == 0 {
            insertByDirectWrite(text)
            return
        }
        runNativeEdit(kind: .typing) { super.insertText(text) }
    }

    private func insertByDirectWrite(_ text: String) {
        let attr = NSAttributedString(string: text, attributes: typingAttributes)
        runNativeEdit(kind: .typing) { [weak self] in
            guard let self else { return }
            self.textStorage.replaceCharacters(in: NSRange(location: 0, length: 0), with: attr)
            self.syncCaretAfterDirectWrite(at: (text as NSString).length)
        }
    }

    override func deleteBackward() {
        guard markedTextRange == nil else {
            super.deleteBackward()
            return
        }
        guard isEditable, !isApplyingEdit else {
            super.deleteBackward()
            return
        }
        if selectedRange.location == 0, selectedRange.length == 0 {
            // 可见块首退格：容器决定并块；首块无动作。
            if let atStart = shouldDelegateBackspaceAtStart, atStart() { return }
            return
        }
        guard let range = deletionRange() else {
            super.deleteBackward()
            return
        }
        replaceCharactersDirect(range: range)
        syncCaretAfterDirectWrite(at: range.location)
    }

    // 退格目标区间：有选区删选区；否则删光标前一字符（代理对成对删除）。
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

    // 直接存储写（退格/剪切与撤销回放等离散动作共用）。
    private func replaceCharactersDirect(range: NSRange, kind: LiveEditKind = .typing) {
        let clamped = clamp(range, to: textStorage.length)
        runNativeEdit(kind: kind) { [weak self] in
            guard let self else { return }
            self.textStorage.replaceCharacters(in: clamped, with: NSAttributedString())
        }
    }

    // 执行一段原生编辑；期间触发的收簇先挂起，输入落定后统一 flush，
    // 保证容器观察到的缓冲差分只含本次输入。编辑前选区留作容器 replace 消歧提示。
    private func runNativeEdit(kind: LiveEditKind, _ edit: () -> Void) {
        isApplyingEdit = true
        pendingKind = kind
        if editSelectionHint == nil {
            editSelectionHint = selectedRange
        }
        defer { isApplyingEdit = false }
        edit()
        flushDeferredCollapses(mergeIntoPaste: kind == .paste)
    }

    // 组字开始：记下组字前的选区（提交时保留包裹的消歧提示，桌面 mdCaret 同源）。
    override func setMarkedText(_ markedText: String?, selectedRange: NSRange) {
        if markedTextRange == nil, editSelectionHint == nil {
            editSelectionHint = self.selectedRange
        }
        super.setMarkedText(markedText, selectedRange: selectedRange)
    }

    override func cut(_ sender: Any?) {
        let range = selectedRange
        guard range.length > 0, isEditable else { return }
        // 剪贴板写 raw 投影（外部拿到可读 Markdown + 完整令牌）。
        UIPasteboard.general.string = document.raw(in: range)
        editSelectionHint = selectedRange
        replaceCharactersDirect(range: range, kind: .command)
        syncCaretAfterDirectWrite(at: range.location)
    }

    override func paste(_ sender: Any?) {
        guard isEditable, let text = UIPasteboard.general.string else { return }
        // 粘贴是独立撤销单元；外来无主 U+FFFC 先行净化，合法簇经扫描收组件。
        editSelectionHint = selectedRange
        isPasteUnit = true
        defer { isPasteUnit = false }
        runNativeEdit(kind: .paste) {
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

    override func unmarkText() {
        super.unmarkText()
        guard !isReconciling else { return }
        reconcile(storage: textStorage, editedRange: selectedRange)
    }

    // MARK: 跨块方向键（桌面 crossBlock 语义）

    // 方向键仅在可见块边界处注入自定义 key command（硬件键盘）：光标在块首
    // 向左/上跨块、块尾向右/下跨块；其余情形保持系统原生移动。
    override var keyCommands: [UIKeyCommand]? {
        var commands = super.keyCommands ?? []
        guard markedTextRange == nil, selectedRange.length == 0 else { return commands }
        let location = selectedRange.location
        let length = textStorage.length
        func command(_ input: String, _ direction: Int) -> UIKeyCommand {
            UIKeyCommand(
                input: input, modifierFlags: [],
                action: direction < 0 ? #selector(crossBackwardCommand(_:)) : #selector(crossForwardCommand(_:))
            )
        }
        if location == 0 {
            commands.append(command(UIKeyCommand.inputLeftArrow, -1))
            commands.append(command(UIKeyCommand.inputUpArrow, -1))
        }
        if location >= length {
            commands.append(command(UIKeyCommand.inputRightArrow, 1))
            commands.append(command(UIKeyCommand.inputDownArrow, 1))
        }
        return commands
    }

    @objc private func crossBackwardCommand(_ command: UIKeyCommand) {
        onArrowCross?(-1)
    }

    @objc private func crossForwardCommand(_ command: UIKeyCommand) {
        onArrowCross?(1)
    }

    func textViewDidChangeSelection(_ textView: UITextView) {
        guard !isApplyingEdit else { return }
        onSelectionChanged?()
    }

    // MARK: 存储对账（M2-1/M2-5 同一管线：diff 同步文档 + 收组件决策）

    private func reconcile(storage: NSTextStorage, editedRange: NSRange) {
        guard deferredCollapses.isEmpty else { return }
        isReconciling = true
        defer { isReconciling = false }

        let newBuffer = storage.string
        let oldBuffer = document.bufferText
        var mutated = false

        var caretEstimate = selectedRange.location
        if newBuffer != oldBuffer {
            let prefix = Self.commonPrefixLength(oldBuffer, newBuffer)
            let oldLength = (oldBuffer as NSString).length
            let newLength = (newBuffer as NSString).length
            let suffixLimit = min(oldLength, newLength) - prefix
            let suffix = Self.commonSuffixLength(oldBuffer, newBuffer, limit: max(0, suffixLimit))
            let oldRange = NSRange(location: prefix, length: oldLength - suffix - prefix)
            let insertedRange = NSRange(location: prefix, length: newLength - suffix - prefix)
            // 结算光标 = max(当前选区, 插入区末端)：原生插入总把光标推进到新内容
            // 之后；直接存储写的选区同步稍后才发生（桌面 nativeCaretAfterEdit 同源）。
            caretEstimate = max(caretEstimate, insertedRange.location + insertedRange.length)
            let replacement = pieces(in: insertedRange, of: storage)
            let outcome = document.apply(
                .init(replacedRange: oldRange, replacement: replacement),
                detectTokens: canCollapse
            )
            mutated = true
            applyReplacements(outcome.replacements)
        } else if editedRange.location != NSNotFound, editedRange.location >= 0 {
            let location = min(editedRange.location, document.bufferLength)
            let outcome = document.apply(
                .init(replacedRange: NSRange(location: location, length: 0), replacement: []),
                detectTokens: canCollapse
            )
            mutated = !outcome.replacements.isEmpty
            applyReplacements(outcome.replacements)
        }

        enforceBufferInvariant()
        if mutated {
            let composing = markedTextRange != nil
            var kind: LiveEditKind
            if let pending = pendingKind {
                kind = pending
                pendingKind = nil
            } else {
                kind = prevComposing && !composing ? .ime : .typing
            }
            prevComposing = composing
            let hint = editSelectionHint
            editSelectionHint = nil
            onBufferChange?(document.rawText, kind, hint, caretEstimate)
        } else {
            prevComposing = markedTextRange != nil
            editSelectionHint = nil
        }
    }

    private func applyReplacements(_ ops: [EditorDocument.BufferReplacement]) {
        for op in ops {
            let clamped = clamp(op.range, to: textStorage.length)
            if isApplyingEdit {
                deferredCollapses.append(DeferredCollapse(range: clamped, piece: op.piece))
                continue
            }
            let replacement = EditorRender.piece(op.piece)
            textStorage.replaceCharacters(in: clamped, with: replacement)
            syncCaretAfterDirectWrite(at: clamped.location + 1)
        }
    }

    private func flushDeferredCollapses(mergeIntoPaste: Bool) {
        let items = deferredCollapses
        deferredCollapses = []
        for item in items {
            let replacement = EditorRender.piece(item.piece)
            textStorage.replaceCharacters(in: item.range, with: replacement)
            // 收组件后光标落在组件后（D6）。
            syncCaretAfterDirectWrite(at: item.range.location + 1)
        }
    }

    // 兜底不变式：存储内容必须等于文档缓冲投影。
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

    override func traitCollectionDidChange(_ previousTraitCollection: UITraitCollection?) {
        super.traitCollectionDidChange(previousTraitCollection)
        guard let previousTraitCollection,
              previousTraitCollection.hasDifferentColorAppearance(comparedTo: traitCollection) else { return }
        refreshPillAppearances()
    }

    func refreshPillAppearances() {
        let storage = textStorage
        var hasPill = false
        storage.enumerateAttribute(
            .attachment,
            in: NSRange(location: 0, length: storage.length)
        ) { value, _, _ in
            guard let attachment = value as? TimeTokenAttachment else { return }
            attachment.refreshAppearance(traits: traitCollection)
            hasPill = true
        }
        guard hasPill else { return }
        isReconciling = true
        defer { isReconciling = false }
        storage.edited(.editedAttributes, range: NSRange(location: 0, length: storage.length), changeInLength: 0)
    }

    @objc private func pillTapped(_ recognizer: UITapGestureRecognizer) {
        // M3 在此打开时间弹层；M2 刻意无动作。
    }

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

    // 光标矩形（容器滚动锚定用）；越界钳到文档长度。
    func caretRect(for offset: Int) -> CGRect {
        let length = textStorage.length
        let location = min(max(offset, 0), length)
        guard let start = position(from: beginningOfDocument, offset: location),
              let end = position(from: start, offset: 0),
              let range = textRange(from: start, to: end) else {
            return CGRect(x: 0, y: 0, width: 1, height: font?.lineHeight ?? 20)
        }
        let rect = firstRect(for: range)
        if rect.width == 0, rect.height == 0 {
            return CGRect(x: 0, y: 0, width: 1, height: font?.lineHeight ?? 20)
        }
        return rect
    }

    private func syncCaretAfterDirectWrite(at offset: Int) {
        selectedRange = NSRange(location: offset, length: 0)
        if let start = position(from: beginningOfDocument, offset: offset),
           let end = position(from: start, offset: 0) {
            selectedTextRange = textRange(from: start, to: end)
        }
    }

    private func clamp(_ range: NSRange, to length: Int) -> NSRange {
        let location = min(max(range.location, 0), length)
        let end = min(max(range.location + range.length, location), length)
        return NSRange(location: location, length: end - location)
    }

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

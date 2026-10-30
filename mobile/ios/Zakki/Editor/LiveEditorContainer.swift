import UIKit

// MARK: - 实时预览块容器（M2-6，同构桌面 livePane + live_edit）

// 文档按块虚拟化渲染（显式方案，同桌面 livePane：只布局视口附近块）：
// 块高表 = 离线排版测量（NSLayoutManager，按块身份+宽度缓存）+ 尾距（桌面
// listGapAfter）；非焦点块为不可编辑渲染视图，焦点块为 BlockEditorView（可见
// 文字，高度取编辑器实测并随输入更新）。容器持有文档 raw（磁盘逐字投影的
// 唯一事实源）、块列表与文档级撤销双栈（桌面 live_history 同构）。
// 文档变更与撤销事务（键入写回/结构编辑/撤销重做）拆至 LiveEditorTransactions.swift，
// 被其访问的成员因拆分开放为模块内可见。
final class LiveEditorContainer: UIView {
    // raw 实际变化后回调（宿主接防抖保存）。
    var onRawChange: ((String) -> Void)?

    var raw: String
    var blockList: LiveBlockList
    private(set) var activeIndex: Int?
    private let writable: Bool

    // 模块内可见（@testable 命中回归测试需要读 guide/约束状态）。
    let scrollView = UIScrollView()
    let contentView = UIView()

    // 块高表：y/height（height 含尾距）。
    private struct BlockLayout {
        var y: CGFloat
        var height: CGFloat
    }
    private var layouts: [BlockLayout] = []
    private var contentHeightConstraint: NSLayoutConstraint?

    // 已挂载的块视图（index → view + 块身份）；身份变化即重建。
    private struct MountedView {
        var view: LiveBaseCell
        var identity: String
    }
    private var mounted: [Int: MountedView] = [:]

    // 焦点块状态。
    private(set) var activeEditor: BlockEditorView?
    var activeProjection: LiveProjection?
    private var activeCell: LiveActiveCell?
    var pendingSourceCaret: Int?
    private var pendingVisibleSelection: NSRange?
    private var lastReportedSel = NSRange(location: 0, length: 0)
    var selectionHint: NSRange? // 编辑前可见选区（replace 消歧）
    private var suppressSelectionReports = false

    // 文档级撤销双栈（对齐桌面 liveUndo/liveRedo：区间片段 + 前后光标）。
    struct DocOp {
        var range: NSRange // 在 before 文档中的替换区间
        var oldFragment: String
        var newFragment: String
        var beforeCaret: Int
        var afterCaret: Int
        var kind: LiveEditKind
    }
    var undoStack: [DocOp] = []
    var redoStack: [DocOp] = []
    let undoLimit = 200

    let styler = MarkdownStyler()
    private let editorUndoManager = EditorUndoManager()
    private let metrics = LiveBlockMetrics()

    var canUndo: Bool { !undoStack.isEmpty }
    var canRedo: Bool { !redoStack.isEmpty }

    // 测试探针：焦点块是否处于围栏草稿态（键入 ``` 后的字面投影）。
    var isFenceDraftActive: Bool {
        guard let idx = activeIndex, blockList.blocks.indices.contains(idx) else { return false }
        let block = blockList.blocks[idx]
        return block.kind == .code && LiveSyntax.isFenceDraftLine(block.raw)
    }

    // 测试探针：容器级撤销事务数。
    var undoStackDepthForTesting: Int { undoStack.count }

    // 测试探针：块布局 frame（hitTest 回归测试用块坐标定位命中点）。
    var blockLayoutFramesForTesting: [CGRect] {
        layouts.map { CGRect(x: 0, y: $0.y, width: contentViewportWidth, height: $0.height) }
    }

    // 测试探针：当前挂载的块视图（hitTest 回归测试断言命中 cell 类型）。
    var mountedCellTypesForTesting: [String] {
        contentView.subviews.map { String(describing: type(of: $0)) }
    }


    init(raw: String, writable: Bool) {
        self.raw = raw
        self.blockList = LiveBlocks.split(raw)
        self.writable = writable
        super.init(frame: .zero)
        backgroundColor = Theme.Semantic.background
        setupScrollView()
        rebuildLayout()
        editorUndoManager.canUndoRoute = { [weak self] in self?.canUndo ?? false }
        editorUndoManager.canRedoRoute = { [weak self] in self?.canRedo ?? false }
        editorUndoManager.undoRoute = { [weak self] in self?.undo() }
        editorUndoManager.redoRoute = { [weak self] in self?.redo() }
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("容器不走 IB")
    }

    private func setupScrollView() {
        scrollView.translatesAutoresizingMaskIntoConstraints = false
        scrollView.backgroundColor = .clear
        scrollView.showsVerticalScrollIndicator = true
        scrollView.keyboardDismissMode = .interactive
        scrollView.contentInset = UIEdgeInsets(
            top: Theme.Typography.padY,
            left: Theme.Typography.padX,
            bottom: Theme.Typography.padY,
            right: Theme.Typography.padX
        )
        scrollView.delegate = self
        addSubview(scrollView)
        contentView.translatesAutoresizingMaskIntoConstraints = false
        scrollView.addSubview(contentView)
        contentHeightConstraint = contentView.heightAnchor.constraint(equalToConstant: 0)
        NSLayoutConstraint.activate([
            scrollView.topAnchor.constraint(equalTo: topAnchor),
            scrollView.leadingAnchor.constraint(equalTo: leadingAnchor),
            scrollView.trailingAnchor.constraint(equalTo: trailingAnchor),
            scrollView.bottomAnchor.constraint(equalTo: bottomAnchor),
            contentView.topAnchor.constraint(equalTo: scrollView.contentLayoutGuide.topAnchor),
            // 宽度必须显式钉到可见宽：块 cell 用 translates frame（无宽度约束贡献），
            // 四边都钉 contentLayoutGuide 时 guide 宽度会塌缩为 0，cell 溢出渲染
            // 看得见但 hitTest 全部落空（点击无法进入编辑）。
            contentView.leadingAnchor.constraint(equalTo: scrollView.contentLayoutGuide.leadingAnchor),
            contentView.widthAnchor.constraint(equalTo: scrollView.frameLayoutGuide.widthAnchor),
            contentView.bottomAnchor.constraint(equalTo: scrollView.contentLayoutGuide.bottomAnchor),
            contentHeightConstraint!,
        ])
    }

    // 键盘工具栏挂到焦点编辑器上（两种模式的文本视图共用同一实例）。
    var inputAccessoryViewForEditor: UIView?

    var contentViewportWidth: CGFloat {
        max(1, scrollView.contentInset.left + scrollView.contentInset.right == 0
            ? bounds.width
            : bounds.width - scrollView.contentInset.left - scrollView.contentInset.right)
    }

    // MARK: 块高表（离线排版测量 + 尾距）

    private func blockContentHeight(_ block: LiveBlock, width: CGFloat) -> CGFloat {
        if let idx = activeIndex, blockList.blocks.indices.contains(idx), blockList.blocks[idx].startLine == block.startLine {
            // 焦点块：编辑器实测（随输入换行更新）。
            if let editor = activeEditor {
                return editor.sizeThatFits(CGSize(width: width, height: .greatestFiniteMagnitude)).height
            }
        }
        let projection = LiveProjection.project(block)
        let attributed = LiveBlockRender.attributedString(for: block, projection: projection, traits: traitCollection)
        return metrics.height(for: block, attributed: attributed, width: width)
    }

    private func rebuildLayout() {
        let width = contentViewportWidth
        layouts.removeAll(keepingCapacity: true)
        var y: CGFloat = 0
        for (i, block) in blockList.blocks.enumerated() {
            let gap = LiveCellSpacing.gap(after: i, blocks: blockList.blocks)
            let content: CGFloat
            if block.kind == .blank {
                content = (block.raw.isEmpty && LiveBlocks.isLiveSeparator(blockList.blocks, i)) ? 0 : 20
            } else if block.kind == .rule {
                content = 1
            } else {
                content = blockContentHeight(block, width: width)
            }
            let height = content + gap
            layouts.append(BlockLayout(y: y, height: height))
            y += height
        }
        contentHeightConstraint?.constant = y
        invalidateIntrinsicContentSize()
    }

    // 从 index 起重算（前缀块的 y/高度不变，测量缓存命中便宜）。
    private func relayoutFrom(_ index: Int) {
        let width = contentViewportWidth
        var y = index == 0 ? 0 : layouts[index - 1].y + layouts[index - 1].height
        for i in index..<blockList.blocks.count {
            let block = blockList.blocks[i]
            let gap = LiveCellSpacing.gap(after: i, blocks: blockList.blocks)
            let content: CGFloat
            if block.kind == .blank {
                content = (block.raw.isEmpty && LiveBlocks.isLiveSeparator(blockList.blocks, i)) ? 0 : 20
            } else if block.kind == .rule {
                content = 1
            } else {
                content = blockContentHeight(block, width: width)
            }
            let height = content + gap
            if i < layouts.count {
                layouts[i] = BlockLayout(y: y, height: height)
            } else {
                layouts.append(BlockLayout(y: y, height: height))
            }
            y += height
        }
        contentHeightConstraint?.constant = y
        layoutVisibleBlocks()
    }

    // MARK: 视口挂载（attach/detach）

    private func layoutVisibleBlocks() {
        guard bounds.width > 0 else { return }
        let visible = CGRect(origin: scrollView.contentOffset, size: bounds.size)
            .insetBy(dx: 0, dy: -120) // 上下缓冲，滚动不闪
        var needed = Set<Int>()
        for (i, layout) in layouts.enumerated() {
            let frame = CGRect(x: 0, y: layout.y, width: contentViewportWidth, height: layout.height)
            if frame.intersects(visible) {
                needed.insert(i)
                if mounted[i] == nil {
                    mount(index: i)
                }
            }
        }
        for (i, entry) in mounted where !needed.contains(i) {
            if i == activeIndex { continue } // 焦点编辑器常驻（正在输入，可能短暂离屏）
            entry.view.removeFromSuperview()
            mounted[i] = nil
        }
    }

    private func mount(index: Int) {
        guard blockList.blocks.indices.contains(index) else { return }
        let block = blockList.blocks[index]
        let identity = block.visualIdentity
        let gap = LiveCellSpacing.gap(after: index, blocks: blockList.blocks)
        let view: LiveBaseCell
        if index == activeIndex, let cell = activeCell {
            view = cell
        } else if index == activeIndex {
            let cell = LiveActiveCell(frame: .zero)
            activeCell = cell
            let editor = activeEditor ?? makeActiveEditor(index: index)
            activeEditor = editor
            cell.embed(editor, block: block, traits: traitCollection, gap: gap)
            view = cell
        } else {
            view = makeContentCell(index: index, block: block, gap: gap)
        }
        view.blockIndex = index
        let y = layouts.indices.contains(index) ? layouts[index].y : 0
        let height = layouts.indices.contains(index) ? layouts[index].height : 0
        view.frame = CGRect(x: 0, y: y, width: contentViewportWidth, height: height)
        contentView.addSubview(view)
        mounted[index] = MountedView(view: view, identity: identity)
    }

    private func makeContentCell(index: Int, block: LiveBlock, gap: CGFloat) -> LiveBaseCell {
        switch block.kind {
        case .blank:
            let cell = LiveBlankCell(frame: .zero)
            cell.configure(index: index, isSeparator: LiveBlocks.isLiveSeparator(blockList.blocks, index), gap: gap)
            cell.onTap = { [weak self] i, caret in self?.liveBlockCellTapped(i, visibleCaret: caret) }
            return cell
        case .rule:
            let cell = LiveRuleCell(frame: .zero)
            cell.configure(index: index, gap: gap)
            cell.onTap = { [weak self] i, caret in self?.liveBlockCellTapped(i, visibleCaret: caret) }
            return cell
        case .code:
            let cell = LiveCodeCell(frame: .zero)
            let proj = LiveProjection.project(block)
            cell.configure(block: block, attributed: LiveBlockRender.attributedString(for: block, projection: proj, traits: traitCollection),
                           index: index, gap: gap, traits: traitCollection)
            cell.onTap = { [weak self] i, caret in self?.liveBlockCellTapped(i, visibleCaret: caret) }
            return cell
        default:
            let cell = LiveTextCell(frame: .zero)
            let proj = LiveProjection.project(block)
            cell.configure(block: block, attributed: LiveBlockRender.attributedString(for: block, projection: proj, traits: traitCollection),
                           index: index, gap: gap)
            cell.onTap = { [weak self] i, caret in self?.liveBlockCellTapped(i, visibleCaret: caret) }
            return cell
        }
    }

    // 内容变化的块重挂（身份变化 → 重建视图）；焦点块由 syncActiveBuffer 更新。
    func remountChangedBlocks(_ old: [LiveBlock], _ new: [LiveBlock]) {
        guard old.count == new.count else {
            remountAll()
            return
        }
        for (i, block) in new.enumerated() {
            guard i != activeIndex else { continue }
            if let entry = mounted[i], entry.identity != block.visualIdentity {
                entry.view.removeFromSuperview()
                mounted[i] = nil
                if layouts.indices.contains(i), mounted[i] == nil {
                    mount(index: i)
                }
            }
        }
    }

    private func remountAll() {
        for (i, entry) in mounted {
            if i != activeIndex { entry.view.removeFromSuperview() }
            if i != activeIndex { mounted[i] = nil }
        }
        layoutVisibleBlocks()
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        // 宽度变化（旋转/分屏）：块高表整体重算。
        if abs(lastLayoutWidth - contentViewportWidth) > 0.5 {
            lastLayoutWidth = contentViewportWidth
            metrics.removeAll()
            rebuildLayout()
            remountAll()
        } else {
            layoutVisibleBlocks()
        }
    }
    private var lastLayoutWidth: CGFloat = 0

    override func traitCollectionDidChange(_ previousTraitCollection: UITraitCollection?) {
        super.traitCollectionDidChange(previousTraitCollection)
        guard let previousTraitCollection,
              previousTraitCollection.hasDifferentColorAppearance(comparedTo: traitCollection) else { return }
        // 胶囊位图与卡片描边按新 traits 重画（不动文本、不进撤销栈）。
        metrics.removeAll()
        remountAll()
        if let view = activeEditor, let idx = activeIndex, blockList.blocks.indices.contains(idx) {
            view.refreshPillAppearances()
            applyStyles(to: view, block: blockList.blocks[idx], projection: activeProjection)
        }
    }

    // MARK: 焦点迁移与激活

    // 点击渲染块进入编辑（桌面 live_click）。时间行首次点击光标落块末。
    func activate(_ index: Int, sourceCaret: Int? = nil, visibleSelection: NSRange? = nil) {
        guard writable, blockList.blocks.indices.contains(index) else { return }
        let old = activeIndex
        activeIndex = index
        pendingSourceCaret = sourceCaret
        pendingVisibleSelection = visibleSelection
        let block = blockList.blocks[index]
        let mappedSource: Int
        if let sourceCaret {
            mappedSource = sourceCaret
        } else if let visible = visibleSelection {
            let proj = LiveProjection.project(block)
            mappedSource = proj.sourceOffset(min(max(visible.location, 0), proj.units.count))
        } else {
            mappedSource = 0
        }
        trackedDocCaret = blockList.startOffset(of: index)
            + min(max(mappedSource, 0), LiveSyntax.length(block.raw))
        if old != index {
            // 焦点迁移：旧/新 index 上的挂载全部失效（旧焦点块回渲染视图、
            // 新 index 上的内容视图让位给焦点编辑器），再急切装配编辑器
            //（目标块可能离屏，块高表需要它的实测高度）。
            activeEditor = nil
            activeCell = nil
            for stale in [old, index].compactMap({ $0 }) where mounted[stale] != nil {
                mounted[stale]?.view.removeFromSuperview()
                mounted[stale] = nil
            }
            activeEditor = makeActiveEditor(index: index)
            rebuildLayout()
            layoutVisibleBlocks()
            scrollToActive()
            syncActiveBuffer()
        } else {
            // 同块重激活（rawSelection/再次点击）：消费 pending 选区重同步。
            syncActiveBuffer()
        }
    }

    // 桌面 focusBlockEnd：最后一个非文末空块的末尾。
    func focusBlockEnd() {
        guard writable, !blockList.blocks.isEmpty else { return }
        var idx = blockList.blocks.count - 1
        while idx > 0, idx == blockList.blocks.count - 1,
              blockList.blocks[idx].kind == .blank, blockList.blocks[idx].raw == "" {
            idx -= 1
        }
        let end = LiveSyntax.length(blockList.blocks[idx].raw)
        activate(idx, sourceCaret: end)
    }

    private func scrollToActive() {
        guard let idx = activeIndex, layouts.indices.contains(idx) else { return }
        let frame = CGRect(x: 0, y: layouts[idx].y, width: contentViewportWidth, height: layouts[idx].height)
        let visible = CGRect(origin: scrollView.contentOffset, size: bounds.size)
        guard !visible.contains(frame) else { return }
        let target: CGRect
        if frame.height <= visible.height {
            target = frame
        } else {
            target = CGRect(x: 0, y: frame.minY, width: 1, height: min(frame.height, visible.height * 0.8))
        }
        scrollView.scrollRectToVisible(target.offsetBy(dx: 0, dy: scrollView.contentInset.top), animated: false)
    }

    // 光标滚入视口（键入换行/长块内移动）。
    func scrollCaretIntoView() {
        guard let view = activeEditor, let idx = activeIndex, layouts.indices.contains(idx) else { return }
        layoutIfNeeded()
        let caretRect = view.caretRect(for: view.selectedRange.location)
        let local = CGRect(x: caretRect.minX,
                           y: layouts[idx].y + min(caretRect.minY, max(0, layouts[idx].height - 2)),
                           width: max(caretRect.width, 1), height: max(caretRect.height, 1))
        let visible = CGRect(origin: scrollView.contentOffset, size: bounds.size)
        guard !visible.contains(local) else { return }
        scrollView.scrollRectToVisible(local.insetBy(dx: 0, dy: -24), animated: false)
    }

    // MARK: 焦点块编辑器装配

    private func projectionForActive(_ block: LiveBlock) -> LiveProjection {
        // 围栏草稿态：键入 ``` 后回车前按字面文本投影（桌面 fenceDraft）。
        if block.kind == .code, LiveSyntax.isFenceDraftLine(block.raw) {
            return LiveProjection.region(block.raw, lo: 0, hi: LiveSyntax.length(block.raw), inline: false)
        }
        return LiveProjection.project(block)
    }

    private func makeActiveEditor(index: Int) -> BlockEditorView {
        let block = blockList.blocks[index]
        let proj = projectionForActive(block)
        let view = BlockEditorView(writable: writable)
        view.inputAccessoryView = inputAccessoryViewForEditor
        view.onBufferChange = { [weak self] bufferRaw, kind, hint, caretEstimate in
            self?.selectionHint = hint
            self?.blockEditorDidChange(bufferRaw: bufferRaw, kind: kind, caretEstimate: caretEstimate)
        }
        view.onSelectionChanged = { [weak self] in
            self?.activeSelectionChanged()
        }
        view.shouldDelegateEnter = { [weak self] in
            self?.handleEnter() ?? false
        }
        view.shouldDelegateBackspaceAtStart = { [weak self] in
            self?.handleBackspaceAtStart() ?? false
        }
        view.onArrowCross = { [weak self] direction in
            self?.handleArrowCross(direction)
        }
        view.editorUndoManager = editorUndoManager
        let selection: NSRange?
        if let caret = pendingSourceCaret {
            let (s, e) = proj.visibleSelection(caret, caret)
            selection = NSRange(location: s, length: max(0, e - s))
        } else if let visible = pendingVisibleSelection {
            selection = visible
        } else {
            // 无显式落点（如明暗切换重装配）：钳回上次上报的选区。
            let length = (proj.text as NSString).length
            let location = min(max(lastReportedSel.location, 0), length)
            let end = min(max(lastReportedSel.location + lastReportedSel.length, location), length)
            selection = NSRange(location: location, length: end - location)
        }
        pendingSourceCaret = nil
        pendingVisibleSelection = nil
        view.load(projection: proj, block: block, selection: selection)
        activeProjection = proj
        lastReportedSel = view.selectedRange
        applyStyles(to: view, block: block, projection: proj)
        updateTypingAttributes()
        DispatchQueue.main.async { [weak view] in
            view?.becomeFirstResponder()
        }
        return view
    }

    func applyStyles(to view: BlockEditorView, block: LiveBlock, projection: LiveProjection?) {
        guard let projection else { return }
        view.performAttributeOnly { [styler] in
            styler.apply(view.textStorage, block: block, projection: projection)
        }
    }

    func updateTypingAttributes() {
        guard let view = activeEditor, let idx = activeIndex,
              blockList.blocks.indices.contains(idx), let proj = activeProjection else { return }
        view.typingAttributes = styler.attributesAtCaret(
            view.selectedRange.location, block: blockList.blocks[idx], projection: proj
        )
    }

    private func activeSelectionChanged() {
        guard let view = activeEditor else { return }
        guard !suppressSelectionReports else { return }
        lastReportedSel = view.selectedRange
        updateTypingAttributes()
        // 焦点块高度可能随换行变化：重排其后的块并保持光标可见。
        if let idx = activeIndex, layouts.indices.contains(idx) {
            let width = contentViewportWidth
            let height = view.sizeThatFits(CGSize(width: width, height: .greatestFiniteMagnitude)).height
                + LiveCellSpacing.gap(after: idx, blocks: blockList.blocks)
            if abs(layouts[idx].height - height) > 0.5 {
                relayoutFrom(idx)
            }
        }
        scrollCaretIntoView()
    }

    // 缓冲重同步。投影文字与缓冲一致时不动缓冲（保持原生光标），仅补样式；
    // 需要整体重载缓冲时（结构变化/显式选区）分两条路：
    //   - 容器驱动的同步（撤销/激活/快捷栏）：立即重载（无 UIKit 输入在途）；
    //   - 编辑器输入管线路径（键入/回车/退格回调）：推迟到下一 runloop 重载，
    //     输入管线结算期间重写缓冲会让 UIKit 内部插入点失步（键入错位）。
    private var pendingBufferLoad: (block: LiveBlock, projection: LiveProjection, selection: NSRange?)?
    private var bufferLoadScheduled = false
    // 容器自跟踪的文档级光标（op 记账的连续性判据）：输入期间用上一事务的
    // afterCaret，避免经陈旧投影换算（投影在输入结算时可能尚未跟上）。
    var trackedDocCaret = 0

    func syncActiveBuffer(deferred: Bool = false) {
        guard let view = activeEditor, let idx = activeIndex, blockList.blocks.indices.contains(idx) else { return }
        let block = blockList.blocks[idx]
        let proj = projectionForActive(block)
        let caret = pendingSourceCaret
        let visibleSel = pendingVisibleSelection
        pendingSourceCaret = nil
        pendingVisibleSelection = nil
        let bufferMatches = view.document.bufferText == proj.text
        // 模型侧投影与样式立即生效（下一次 replace 的基准；属性-only 在输入
        // 管线内安全，M2-5 同款）；只有缓冲整体重载才延迟。
        activeProjection = proj
        applyStyles(to: view, block: block, projection: proj)
        updateTypingAttributes()
        let needsLoad = caret != nil || visibleSel != nil || !bufferMatches
        guard needsLoad else { return }
        let selection: NSRange?
        if let caret {
            let (s, e) = proj.visibleSelection(caret, caret)
            selection = NSRange(location: s, length: max(0, e - s))
        } else {
            selection = visibleSel
        }
        if deferred {
            pendingBufferLoad = (block, proj, selection)
            scheduleBufferLoad()
        } else {
            performBufferLoad(block: block, projection: proj, selection: selection)
        }
    }

    private func scheduleBufferLoad() {
        guard !bufferLoadScheduled else { return }
        bufferLoadScheduled = true
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            bufferLoadScheduled = false
            guard let load = pendingBufferLoad else { return }
            pendingBufferLoad = nil
            // 推迟期间焦点可能已迁移：只对仍在位的编辑器重载。
            guard activeEditor != nil else { return }
            performBufferLoad(block: load.block, projection: load.projection, selection: load.selection)
        }
    }

    private func performBufferLoad(block: LiveBlock, projection: LiveProjection, selection: NSRange?) {
        guard let view = activeEditor else { return }
        suppressSelectionReports = true
        view.load(projection: projection, block: block, selection: selection)
        suppressSelectionReports = false
        lastReportedSel = view.selectedRange
        activeProjection = projection
        applyStyles(to: view, block: block, projection: projection)
        updateTypingAttributes()
        if let idx = activeIndex, layouts.indices.contains(idx) {
            relayoutFrom(idx)
        }
    }

    // MARK: raw 选区映射（模式切换）

    // live 的 raw 选区 = 焦点块投影换算；无焦点块时映射到文末。
    var rawSelection: NSRange {
        get {
            guard let view = activeEditor, let idx = activeIndex,
                  blockList.blocks.indices.contains(idx), let proj = activeProjection else {
                return NSRange(location: (raw as NSString).length, length: 0)
            }
            let blockStart = blockList.startOffset(of: idx)
            let sel = view.selectedRange
            let s = proj.sourceOffset(sel.location)
            let e = proj.sourceOffset(sel.location + sel.length)
            return NSRange(location: blockStart + min(s, e), length: abs(e - s))
        }
        set {
            guard writable else { return }
            let length = (raw as NSString).length
            let loc = min(max(newValue.location, 0), length)
            let end = min(max(newValue.location + newValue.length, 0), length)
            let (idx, blockStart) = blockList.blockOf(loc)
            let proj = LiveProjection.project(blockList.blocks[idx])
            let (vs, ve) = proj.visibleSelection(loc - blockStart, end - blockStart)
            activate(idx, visibleSelection: NSRange(location: min(vs, ve), length: abs(ve - vs)))
        }
    }

    // MARK: 快捷栏（live 模式经容器落源码级语义）

    func applyToolbarAction(_ action: MarkdownSnippetAction) {
        guard writable else { return }
        if activeIndex == nil {
            focusBlockEnd()
        }
        guard let idx = activeIndex, blockList.blocks.indices.contains(idx) else { return }
        if action == .insertTime {
            // 插入时间令牌文本 → 走缓冲既有扫描收簇。
            activeEditor?.insertText(MarkdownSnippet.timeTokenText())
            return
        }
        if let marker = MarkdownSnippet.wraps[action] {
            wrapSelection(marker, at: idx)
            return
        }
        if let prefix = MarkdownSnippet.prefixes[action] {
            prependPrefix(prefix, at: idx)
        }
    }

    // 包裹类：可见选区两端在源码里插入标记（标记不可见，可见选区原位保留）。
    private func wrapSelection(_ marker: String, at idx: Int) {
        guard let view = activeEditor, let proj = activeProjection,
              blockList.blocks.indices.contains(idx) else { return }
        let sel = view.selectedRange
        let block = blockList.blocks[idx]
        let s = proj.sourceOffset(sel.location)
        let e = proj.sourceOffset(sel.location + sel.length)
        let lo = min(s, e), hi = max(s, e)
        let rawUnits = Array(block.raw.utf16)
        let markerUnits = Array(marker.utf16)
        var out: [unichar] = []
        out.append(contentsOf: rawUnits[0..<lo])
        out.append(contentsOf: markerUnits)
        out.append(contentsOf: rawUnits[lo..<hi])
        out.append(contentsOf: markerUnits)
        out.append(contentsOf: rawUnits[hi...])
        let newRaw = String(decoding: out, as: UTF16.self)
        // 标记不可见：可见选区原位保留（包裹后仍选中同一段可见文字）。
        applyActiveBlockRaw(newRaw, at: idx, sourceCaret: hi + markerUnits.count * 2,
                            visibleSelection: sel)
    }

    // 前缀类：作用于当前块行首（与源码模式同一语义，块即行）；光标平移前缀长。
    private func prependPrefix(_ prefix: String, at idx: Int) {
        guard blockList.blocks.indices.contains(idx) else { return }
        let block = blockList.blocks[idx]
        let caret = max(0, trackedDocCaret - blockList.startOffset(of: idx))
        let newRaw = prefix + block.raw
        applyActiveBlockRaw(newRaw, at: idx,
                            sourceCaret: min(caret + LiveSyntax.length(prefix), LiveSyntax.length(newRaw)),
                            visibleSelection: nil)
    }

    // 焦点块源码直改（快捷栏等结构化命令）：记账 + 重切 + 重同步。
    private func applyActiveBlockRaw(_ newBlockRaw: String, at idx: Int, sourceCaret: Int, visibleSelection: NSRange?) {
        guard activeEditor != nil, blockList.blocks.indices.contains(idx) else { return }
        let block = blockList.blocks[idx]
        let beforeDoc = raw
        guard newBlockRaw != block.raw else { return }
        let blockStart = blockList.startOffset(of: idx)
        let newDoc = LiveDocEditing.spliceLines(beforeDoc, block.startLine, block.endLine, newBlockRaw).joined(separator: "\n")
        let docCaret = blockStart + min(sourceCaret, LiveSyntax.length(newBlockRaw))
        raw = newDoc
        let oldBlocks = blockList.blocks
        blockList = LiveBlocks.split(newDoc)
        recordOp(before: beforeDoc, after: newDoc, beforeCaret: trackedDocCaret, afterCaret: docCaret,
                 kind: .command, fencePair: false)
        trackedDocCaret = docCaret
        let (newIdx, newBlockStart) = blockList.blockOf(min(max(docCaret, 0), (newDoc as NSString).length))
        remountChangedBlocks(oldBlocks, blockList.blocks)
        let localCaret = max(0, docCaret - newBlockStart)
        if newIdx == idx {
            pendingSourceCaret = localCaret
            if let visibleSelection {
                pendingVisibleSelection = visibleSelection
                pendingSourceCaret = nil
            }
            syncActiveBuffer()
        } else {
            activate(newIdx, sourceCaret: localCaret)
        }
        onRawChange?(newDoc)
    }

    // MARK: 测试钩子（生产路径与测试同一套样式/结构入口）

    func handleArrowCrossForTesting(_ direction: Int) {
        handleArrowCross(direction)
    }

    // 明暗切换等场景的整块样式重上（属性-only，不改字符不进撤销栈）。
    func applyMarkdownThemeRefreshForTesting() {
        guard let view = activeEditor, let idx = activeIndex, blockList.blocks.indices.contains(idx),
              let proj = activeProjection else { return }
        view.performAttributeOnly { [styler] in
            styler.apply(view.textStorage, block: blockList.blocks[idx], projection: proj)
        }
        updateTypingAttributes()
    }
}

// MARK: - 滚动驱动挂载

extension LiveEditorContainer: UIScrollViewDelegate {
    func scrollViewDidScroll(_ scrollView: UIScrollView) {
        layoutVisibleBlocks()
    }
}

extension LiveEditorContainer: LiveBlockCellDelegate {
    func liveBlockCellTapped(_ index: Int, visibleCaret: Int) {
        guard writable, index != activeIndex, blockList.blocks.indices.contains(index) else { return }
        let block = blockList.blocks[index]
        // 桌面 focusPreviewBlock：时间行首次点击光标默认落块末。
        if block.timeTokenRaw != nil {
            activate(index, sourceCaret: LiveSyntax.length(block.raw))
            return
        }
        activate(index, visibleSelection: NSRange(location: visibleCaret, length: 0))
    }
}

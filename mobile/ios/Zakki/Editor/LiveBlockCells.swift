import UIKit

// MARK: - 非焦点块渲染视图（TextKit 1）

// 非焦点块用 TextKit 1 渲染：NSLayoutManager 提供点击→可见偏移的命中测试
//（桌面 visibleCaretAt 的等价物）。isScrollEnabled=false 自适应内容高度。
final class PlainRenderTextView: UITextView {
    private let renderLayoutManager = NSLayoutManager()
    private let renderTextContainer: NSTextContainer
    private let renderTextStorage = NSTextStorage()

    init() {
        // TextKit1 栈：经指定初始化传入自有 textContainer（UITextView 由此走
        // NSLayoutManager 路径并暴露同一栈），供点击→可见偏移命中测试使用。
        let container = NSTextContainer(size: CGSize(width: 0, height: CGFloat.greatestFiniteMagnitude))
        renderTextContainer = container
        renderTextStorage.addLayoutManager(renderLayoutManager)
        renderLayoutManager.addTextContainer(container)
        super.init(frame: .zero, textContainer: container)
        isEditable = false
        isSelectable = false
        isScrollEnabled = false
        backgroundColor = .clear
        textContainerInset = .zero
        textContainer.lineFragmentPadding = 0
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("渲染视图不走 IB")
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        renderTextContainer.size = CGSize(width: bounds.width, height: .greatestFiniteMagnitude)
    }

    // 点击点 → 可见 UTF-16 偏移；点在末行下方归文末，行尾右侧空白归该行行尾
    //（桌面：短行右侧空白光标停在该行末尾，末块下方纸面归文末）。
    func caretOffset(at point: CGPoint) -> Int {
        let length = renderTextStorage.length
        guard length > 0 else { return 0 }
        let used = renderLayoutManager.usedRect(for: renderTextContainer)
        if point.y > used.maxY {
            return length
        }
    let index = renderLayoutManager.characterIndex(
            for: point, in: renderTextContainer,
            fractionOfDistanceBetweenInsertionPoints: nil
        )
        guard index < length else { return length }
        let fragment = renderLayoutManager.lineFragmentUsedRect(forGlyphAt: index, effectiveRange: nil)
        if point.x > fragment.maxX {
            var lineRange = NSRange()
            _ = renderLayoutManager.lineFragmentRect(forGlyphAt: index, effectiveRange: &lineRange)
            return NSMaxRange(lineRange)
        }
        return index
    }

    // 已排版文字的底部（textView 坐标系）：文字之下到 view 底部是尾部间隙，
    // 间隙点击的块归属判定用。
    func textUsedBottom() -> CGFloat {
        renderLayoutManager.ensureLayout(for: renderTextContainer)
        return renderLayoutManager.usedRect(for: renderTextContainer).maxY
    }
}

// MARK: - 单元格协议与间距

protocol LiveBlockCellDelegate: AnyObject {
    func liveBlockCellTapped(_ index: Int, visibleCaret: Int)
}

// 单元格尾部间距（桌面 listGapAfter 画在块尾 padding 里）：下一块是分隔空行 →
// 两倍块距（分隔行自身 0 高）；同一列表相邻项收紧到四分之一；其余完整块距。
enum LiveCellSpacing {
    static func gap(after index: Int, blocks: [LiveBlock]) -> CGFloat {
        let gap = Theme.Typography.blockGap
        guard index >= 0, index < blocks.count - 1 else { return 0 }
        if LiveBlocks.isLiveSeparator(blocks, index + 1) { return gap * 2 }
        let cur = blocks[index]
        let next = blocks[index + 1]
        let sameList = (cur.kind == .list || cur.kind == .task) && (next.kind == .list || next.kind == .task)
        if sameList { return (gap * 0.25 * 2).rounded() / 2 }
        return gap
    }
}

// 块视图公共基类：容器手动布局（y/高度由 LiveEditorContainer 的块高表给出），
// 视图自身只负责内容与点击上报。
class LiveBaseCell: UIView {
    var blockIndex = 0
    // 点击回调（容器据此迁移焦点）；onTap(viewIndex, visibleCaret)。
    var onTap: ((Int, Int) -> Void)?
    private(set) var tailGap: CGFloat = 0

    // 块视图由容器手动布置 frame（y/高来自块高表），保持 translates = true；
    // 子视图仍用约束锚定到 self 边缘。false + 无约束会让 Auto Layout 按
    // intrinsic 尺寸重排，破坏虚拟化布局（叠印）。
    override init(frame: CGRect) {
        super.init(frame: frame)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("块视图不走 IB")
    }

    // 尾距由容器计算进块高，视图自身不再约束底部空隙。
    func setTailGap(_ gap: CGFloat) {
        tailGap = gap
    }

    func addTapRecognizer(to view: UIView, action: Selector) {
        view.addGestureRecognizer(UITapGestureRecognizer(target: self, action: action))
    }

    func reportTap(visibleCaret: Int) {
        onTap?(blockIndex, visibleCaret)
    }
}

// MARK: 空行单元格：非分隔空段高 20 可点（桌面 Height(20)）；分隔行 0 高不可点。

final class LiveBlankCell: LiveBaseCell {
    private(set) var isSeparator = true

    override init(frame: CGRect) {
        super.init(frame: frame)
        addTapRecognizer(to: self, action: #selector(tapped(_:)))
    }

    func configure(index: Int, isSeparator: Bool, gap: CGFloat) {
        blockIndex = index
        self.isSeparator = isSeparator
        isUserInteractionEnabled = !isSeparator
        setTailGap(isSeparator ? 0 : gap)
    }

    @objc private func tapped(_ recognizer: UITapGestureRecognizer) {
        reportTap(visibleCaret: 0)
    }
}

// MARK: 分隔线单元格（桌面 ruleLine：1pt rule 色）。

final class LiveRuleCell: LiveBaseCell {

    override init(frame: CGRect) {
        super.init(frame: frame)
        let rule = UIView()
        rule.translatesAutoresizingMaskIntoConstraints = false
        rule.backgroundColor = Theme.Semantic.rule
        rule.layer.cornerRadius = 0.5
        addSubview(rule)
        NSLayoutConstraint.activate([
            rule.topAnchor.constraint(equalTo: topAnchor),
            rule.leadingAnchor.constraint(equalTo: leadingAnchor),
            rule.trailingAnchor.constraint(equalTo: trailingAnchor),
            rule.heightAnchor.constraint(equalToConstant: 1),
            bottomAnchor.constraint(equalTo: rule.bottomAnchor),
        ])
        addTapRecognizer(to: self, action: #selector(tapped(_:)))
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("单元格不走 IB")
    }

    func configure(index: Int, gap: CGFloat) {
        blockIndex = index
        setTailGap(gap)
    }

    @objc private func tapped(_ recognizer: UITapGestureRecognizer) {
        reportTap(visibleCaret: 0)
    }
}

// MARK: 文本单元格（段落/标题/引用/列表/任务）：[饰件][渲染文本]。

final class LiveTextCell: LiveBaseCell {
    let textView = PlainRenderTextView()
    private let ornamentLabel = UILabel()
    private let quoteBar = UIView()
    private var ornamentTopConstraint: NSLayoutConstraint?
    private var textLeadingConstraint: NSLayoutConstraint?
    private var kind: LiveBlockKind = .para
    // 点 ☐/☑ 饰件翻转勾选（不落光标、不激活行——桌面：勾选框点击不冒泡）。
    var onToggleTask: (() -> Void)?
    // 点时间胶囊开弹层（D17）：交出命中的令牌原文。
    var onTokenTap: ((String) -> Void)?

    override init(frame: CGRect) {
        super.init(frame: frame)
        for view in [quoteBar, ornamentLabel, textView] {
            view.translatesAutoresizingMaskIntoConstraints = false
            addSubview(view)
        }
        quoteBar.backgroundColor = Theme.Semantic.quoteBorder
        quoteBar.layer.cornerRadius = Theme.Typography.quoteBar / 2
        quoteBar.isHidden = true
        ornamentLabel.isHidden = true
        ornamentLabel.isUserInteractionEnabled = false
        let top = ornamentLabel.topAnchor.constraint(equalTo: topAnchor)
        ornamentTopConstraint = top
        let leading = textView.leadingAnchor.constraint(equalTo: leadingAnchor)
        textLeadingConstraint = leading
        NSLayoutConstraint.activate([
            quoteBar.topAnchor.constraint(equalTo: topAnchor, constant: 2),
            quoteBar.leadingAnchor.constraint(equalTo: leadingAnchor),
            quoteBar.widthAnchor.constraint(equalToConstant: Theme.Typography.quoteBar),
            quoteBar.bottomAnchor.constraint(equalTo: bottomAnchor, constant: -2),
            textView.topAnchor.constraint(equalTo: topAnchor),
            leading,
            textView.trailingAnchor.constraint(equalTo: trailingAnchor),
            bottomAnchor.constraint(equalTo: textView.bottomAnchor),
            ornamentLabel.leadingAnchor.constraint(equalTo: leadingAnchor),
            top,
        ])
        addTapRecognizer(to: textView, action: #selector(textTapped(_:)))
        addTapRecognizer(to: self, action: #selector(chromeTapped(_:)))
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("单元格不走 IB")
    }

    // 配置：attributed 为投影渲染串；列表/任务/引用块带饰件并让出悬挂缩进。
    func configure(block: LiveBlock, attributed: NSAttributedString, index: Int, gap: CGFloat) {
        blockIndex = index
        kind = block.kind
        setTailGap(gap)
        textView.attributedText = attributed
        quoteBar.isHidden = block.kind != .quote
        if block.kind == .quote {
            ornamentLabel.isHidden = true
            textLeadingConstraint?.constant = Theme.Typography.quoteBar + 8
        } else if let marker = Self.marker(for: block) {
            ornamentLabel.text = marker.text
            // 无障碍：任务方框暴露为可点元素（VoiceOver/UI 测试点击入口）。
            ornamentLabel.isAccessibilityElement = block.kind == .task
            ornamentLabel.accessibilityLabel = block.kind == .task
                ? Strings.taskCheckbox(block.checked) : nil
            // 任务方框在手机端仍偏小（用户二次反馈）：用独立大字号 26pt 绘制
            // ☐/☑（字形约 18pt 见方）；缩进宽度用同一字体量，避免与正文重叠。
            let markerFont: UIFont = block.kind == .task
                ? .systemFont(ofSize: 26)
                : Theme.Typography.bodyFont
            ornamentLabel.font = markerFont
            ornamentLabel.textColor = marker.color
            ornamentLabel.isHidden = false
            let markerWidth = (marker.text as NSString).size(withAttributes: [.font: markerFont]).width
            textLeadingConstraint?.constant = ceil(markerWidth) + 8
            // 大字形以基线对齐正文字首行：顶部上提半个字号差，视觉与正文居中。
            ornamentTopConstraint?.constant = -ceil((markerFont.pointSize - Theme.Typography.bodySize) / 4)
        } else {
            ornamentLabel.isHidden = true
            textLeadingConstraint?.constant = 0
        }
    }

    struct Marker {
        var text: String
        var color: UIColor
    }

    // 列表符号（桌面 listMarker：无序 "•"、有序 "N."，accent 色）与任务框
    // ☐/☑（M3 才可点选，M2-6 仅展示）。
    static func marker(kind: LiveBlockKind, checked: Bool, number: Int) -> Marker? {
        switch kind {
        case .list:
            return number > 0
                ? Marker(text: "\(number).", color: Theme.Semantic.accent)
                : Marker(text: "•", color: Theme.Semantic.accent)
        case .task:
            return checked
                ? Marker(text: "☑", color: Theme.Semantic.accent)
                : Marker(text: "☐", color: Theme.Semantic.textMuted)
        default:
            return nil
        }
    }

    static func marker(for block: LiveBlock) -> Marker? {
        marker(kind: block.kind, checked: block.checked, number: block.number)
    }

    // 任务饰件（☐/☑）命中判定（textView 坐标系）：手指目标按 HIG 放到约 30pt，
    // 字形本身只有十来 pt，只按字形框判定会频繁落空（真机反馈勾不中）。
    private func taskOrnamentHit(_ pointInTextView: CGPoint) -> Bool {
        guard kind == .task, !ornamentLabel.isHidden else { return false }
        let frame = convert(ornamentLabel.bounds, from: ornamentLabel).insetBy(dx: -9, dy: -9)
        let p = convert(pointInTextView, from: textView)
        return frame.contains(p)
    }

    // 点击点处的令牌附件（候选含左邻字符；D17 任务行与非任务行同样开弹层）。
    private func tokenAttachment(at pointInTextView: CGPoint) -> TimeTokenAttachment? {
        let offset = textView.caretOffset(at: pointInTextView)
        guard let attributed = textView.attributedText else { return nil }
        let ns = attributed.string as NSString
        var candidates = [offset]
        if offset > 0 { candidates.append(offset - 1) }
        for candidate in candidates {
            guard candidate < ns.length else { continue }
            guard ns.substring(with: NSRange(location: candidate, length: 1)) == "\u{FFFC}" else { continue }
            if let attachment = attributed.attribute(.attachment, at: candidate, effectiveRange: nil)
                as? TimeTokenAttachment {
                return attachment
            }
        }
        return nil
    }

    @objc private func textTapped(_ recognizer: UITapGestureRecognizer) {
        let point = recognizer.location(in: textView)
        if taskOrnamentHit(point) {
            onToggleTask?()
            return
        }
        // 胶囊命中优先于进入编辑（D17：点组件开弹层，不落光标）。
        if let attachment = tokenAttachment(at: point) {
            onTokenTap?(attachment.tokenRaw)
            return
        }
        // 块尾间隙下半段归下一块行首（最近行吸附跨块）：textView 被约束拉伸
        // 覆盖到间隙区，手指点下一段首行上沿落在间隙里会被本块吞成块末、
        // 光标跳走（真机反馈：点首行光标不动/回原处）。
        if gapRoutesToNextBlock(at: convert(point, from: textView)) {
            onTap?(blockIndex + 1, 0)
            return
        }
        reportTap(visibleCaret: textView.caretOffset(at: point))
    }

    // 点击点是否落在尾间隙的下半段（归下一块）。textView 被拉伸覆盖间隙，
    // 故入参为 cell 坐标；测试可见以锁定归属语义。
    func gapRoutesToNextBlock(at pointInCell: CGPoint) -> Bool {
        // textView 被约束拉伸覆盖整个 cell（间隙在文字排版区之下），
        // 阈值用已排版文字底而非 frame 底。
        guard tailGap > 0, !textView.isHidden else { return false }
        let textBottom = textView.frame.minY + textView.textUsedBottom()
        return pointInCell.y > textBottom + tailGap / 2
    }

    @objc private func chromeTapped(_ recognizer: UITapGestureRecognizer) {
        let inCell = recognizer.location(in: self)
        if taskOrnamentHit(convert(inCell, to: textView)) {
            onToggleTask?()
            return
        }
        let inTextView = convert(inCell, to: textView)
        // 胶囊命中优先于进入编辑（D17：点组件开弹层，不落光标）。
        if let attachment = tokenAttachment(at: inTextView) {
            onTokenTap?(attachment.tokenRaw)
            return
        }
        // 块尾间隙下半段归下一块行首（最近行吸附跨块）：段落间分隔空行处的
        // 间隙带高达两倍块距，手指点下一段首行时上沿落在间隙里会被上一块
        // 吞掉、光标跳走（真机反馈：点首行光标不动）。
        if gapRoutesToNextBlock(at: inCell) {
            onTap?(blockIndex + 1, 0)
            return
        }
        // 点在饰件/行内留白：换算到最近行的光标（桌面：点行内留白停在最近文字）。
        // 原实现一律落块末，真机反馈点字符间空隙会跳到行尾。
        let clamped = CGPoint(x: max(0, min(inTextView.x, textView.bounds.width - 1)),
                              y: max(0, min(inTextView.y, textView.bounds.height - 1)))
        reportTap(visibleCaret: textView.caretOffset(at: clamped))
    }
}

// MARK: 代码单元格（桌面 codeCard：surface 底 + rule 描边 + 圆角 + 内边距）。

final class LiveCodeCell: LiveBaseCell {
    private let card = UIView()
    let textView = PlainRenderTextView()

    override init(frame: CGRect) {
        super.init(frame: frame)
        card.translatesAutoresizingMaskIntoConstraints = false
        addSubview(card)
        textView.translatesAutoresizingMaskIntoConstraints = false
        card.addSubview(textView)
        NSLayoutConstraint.activate([
            card.topAnchor.constraint(equalTo: topAnchor),
            card.leadingAnchor.constraint(equalTo: leadingAnchor),
            card.trailingAnchor.constraint(equalTo: trailingAnchor),
            bottomAnchor.constraint(equalTo: card.bottomAnchor),
            textView.topAnchor.constraint(equalTo: card.topAnchor, constant: Theme.Typography.codePadY),
            textView.leadingAnchor.constraint(equalTo: card.leadingAnchor, constant: Theme.Typography.codePadX),
            textView.trailingAnchor.constraint(equalTo: card.trailingAnchor, constant: -Theme.Typography.codePadX),
            card.bottomAnchor.constraint(equalTo: textView.bottomAnchor, constant: Theme.Typography.codePadY),
        ])
        addTapRecognizer(to: card, action: #selector(tapped(_:)))
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("单元格不走 IB")
    }

    func configure(block: LiveBlock, attributed: NSAttributedString, index: Int, gap: CGFloat, traits: UITraitCollection) {
        blockIndex = index
        setTailGap(gap)
        textView.attributedText = attributed
        applyChrome(traits: traits)
    }

    func applyChrome(traits: UITraitCollection) {
        let resolved = UITraitCollection(userInterfaceStyle: traits.userInterfaceStyle)
        card.backgroundColor = Theme.Semantic.surface.resolvedColor(with: resolved)
        card.layer.cornerRadius = Theme.Typography.codeRadius
        card.layer.borderWidth = 1
        card.layer.borderColor = Theme.Semantic.rule.resolvedColor(with: resolved).cgColor
        card.clipsToBounds = true
    }

    @objc private func tapped(_ recognizer: UITapGestureRecognizer) {
        let point = recognizer.location(in: textView)
        reportTap(visibleCaret: textView.caretOffset(at: point))
    }
}

// MARK: 焦点块单元格：饰件 + BlockEditorView（代码块包卡片）。
// 编辑器视图由容器创建并持有，单元格只负责摆放；重用时先摘下旧编辑器。

final class LiveActiveCell: LiveBaseCell {
    private let quoteBar = UIView()
    private let ornamentLabel = UILabel()
    private let card = UIView()
    private var ornamentTopConstraint: NSLayoutConstraint?
    private var editorConstraints: [NSLayoutConstraint] = []
    private(set) var editor: BlockEditorView?
    private(set) var isCodeLayout = false

    override init(frame: CGRect) {
        super.init(frame: frame)
        for view in [quoteBar, ornamentLabel, card] {
            view.translatesAutoresizingMaskIntoConstraints = false
            addSubview(view)
        }
        quoteBar.backgroundColor = Theme.Semantic.quoteBorder
        quoteBar.layer.cornerRadius = Theme.Typography.quoteBar / 2
        card.layer.cornerRadius = Theme.Typography.codeRadius
        card.layer.borderWidth = 1
        card.clipsToBounds = true
        quoteBar.isHidden = true
        ornamentLabel.isHidden = true
        card.isHidden = true
        let top = ornamentLabel.topAnchor.constraint(equalTo: topAnchor)
        ornamentTopConstraint = top
        NSLayoutConstraint.activate([
            quoteBar.topAnchor.constraint(equalTo: topAnchor, constant: 2),
            quoteBar.leadingAnchor.constraint(equalTo: leadingAnchor),
            quoteBar.widthAnchor.constraint(equalToConstant: Theme.Typography.quoteBar),
            quoteBar.bottomAnchor.constraint(equalTo: bottomAnchor, constant: -2),
            ornamentLabel.leadingAnchor.constraint(equalTo: leadingAnchor),
            top,
            card.leadingAnchor.constraint(equalTo: leadingAnchor),
            card.trailingAnchor.constraint(equalTo: trailingAnchor),
        ])
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("单元格不走 IB")
    }

    func detachEditor() {
        embed(nil, block: LiveBlock(kind: .para, startLine: 0, endLine: 0, raw: ""), traits: UITraitCollection.current, gap: 0)
    }

    func embed(_ editor: BlockEditorView?, block: LiveBlock, traits: UITraitCollection, gap: CGFloat) {
        NSLayoutConstraint.deactivate(editorConstraints)
        editorConstraints.removeAll()
        self.editor?.removeFromSuperview()
        self.editor = editor
        let kind = block.kind
        quoteBar.isHidden = kind != .quote
        card.isHidden = kind != .code
        isCodeLayout = kind == .code
        applyChrome(traits: traits)

        var markerWidth: CGFloat = 0
        if let marker = LiveTextCell.marker(for: block) {
            ornamentLabel.text = marker.text
            ornamentLabel.font = Theme.Typography.bodyFont
            ornamentLabel.textColor = marker.color
            ornamentLabel.isHidden = false
            markerWidth = ceil((marker.text as NSString).size(withAttributes: [.font: Theme.Typography.bodyFont]).width) + 8
            ornamentTopConstraint?.constant = 0
        } else {
            ornamentLabel.isHidden = true
        }
        if kind == .quote {
            markerWidth = Theme.Typography.quoteBar + 8
        }

        guard let editor else { return }
        editor.translatesAutoresizingMaskIntoConstraints = false
        if isCodeLayout {
            // 代码块：编辑器放进卡片（卡片负责 surface 底与描边）。
            card.addSubview(editor)
            editorConstraints.append(contentsOf: [
                topAnchor.constraint(equalTo: card.topAnchor),
                editor.topAnchor.constraint(equalTo: card.topAnchor, constant: Theme.Typography.codePadY),
                editor.leadingAnchor.constraint(equalTo: card.leadingAnchor, constant: Theme.Typography.codePadX),
                editor.trailingAnchor.constraint(equalTo: card.trailingAnchor, constant: -Theme.Typography.codePadX),
                bottomAnchor.constraint(equalTo: card.bottomAnchor, constant: -gap),
            ])
        } else {
            addSubview(editor)
            editorConstraints.append(contentsOf: [
                editor.topAnchor.constraint(equalTo: topAnchor),
                editor.leadingAnchor.constraint(equalTo: leadingAnchor, constant: markerWidth),
                editor.trailingAnchor.constraint(equalTo: trailingAnchor),
                bottomAnchor.constraint(equalTo: editor.bottomAnchor, constant: -gap),
            ])
        }
        NSLayoutConstraint.activate(editorConstraints)
    }

    func applyChrome(traits: UITraitCollection) {
        guard isCodeLayout else { return }
        let resolved = UITraitCollection(userInterfaceStyle: traits.userInterfaceStyle)
        card.backgroundColor = Theme.Semantic.surface.resolvedColor(with: resolved)
        card.layer.borderColor = Theme.Semantic.rule.resolvedColor(with: resolved).cgColor
    }
}

// MARK: - 块高测量（离线排版）

// NSLayoutManager 离线排版测高：与 PlainRenderTextView 同一 TextKit 1 管线，
// 结果与实际渲染一致；按块身份 + 宽度缓存（虚拟化下滚动只测一次）。
final class LiveBlockMetrics {
    private struct Entry {
        var height: CGFloat
    }
    private var cache: [String: Entry] = [:]
    private let storage = NSTextStorage()
    private let layoutManager = NSLayoutManager()
    private let container = NSTextContainer(size: CGSize(width: 0, height: CGFloat.greatestFiniteMagnitude))

    init() {
        storage.addLayoutManager(layoutManager)
        layoutManager.addTextContainer(container)
        container.lineFragmentPadding = 0
    }

    func removeAll() {
        cache.removeAll()
    }

    func height(for block: LiveBlock, attributed: NSAttributedString, width: CGFloat) -> CGFloat {
        let key = "\(block.visualIdentity)|\(Int(width))|\(block.kind == .code ? 1 : 0)"
        if let cached = cache[key] { return cached.height }
        container.size = CGSize(width: width, height: CGFloat.greatestFiniteMagnitude)
        storage.setAttributedString(attributed)
        layoutManager.ensureLayout(for: container)
        var height = layoutManager.usedRect(for: container).height
        if height <= 0 {
            // 空内容块以一个行高兜底（对齐桌面空段落行高）。
            height = ceil(Theme.Typography.bodyFont.lineHeight * Theme.Typography.lineHeight)
        }
        cache[key] = Entry(height: height)
        return height
    }
}

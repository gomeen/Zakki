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

// 单元格公共基类：尾距约束（content 底到单元格底）+ 点击上报。
class LiveBaseCell: UICollectionViewCell {
    var blockIndex = 0
    weak var delegate: LiveBlockCellDelegate?
    // gapConstraint：contentView.bottom == 内容.bottom - tailGap（子类建线后赋值）。
    var gapConstraint: NSLayoutConstraint? {
        didSet { gapConstraint?.constant = -tailGap }
    }
    var tailGap: CGFloat = 0 {
        didSet { gapConstraint?.constant = -tailGap }
    }

    override init(frame: CGRect) {
        super.init(frame: frame)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("单元格不走 IB")
    }

    func addTapRecognizer(to view: UIView, action: Selector) {
        view.addGestureRecognizer(UITapGestureRecognizer(target: self, action: action))
    }

    func reportTap(visibleCaret: Int) {
        delegate?.liveBlockCellTapped(blockIndex, visibleCaret: visibleCaret)
    }
}

// MARK: 空行单元格：非分隔空段高 20 可点（桌面 Height(20)）；分隔行 0 高不可点。

final class LiveBlankCell: LiveBaseCell {
    static let reuseIdentifier = "live-blank"
    private var heightConstraint: NSLayoutConstraint?

    override init(frame: CGRect) {
        super.init(frame: frame)
        let constraint = contentView.heightAnchor.constraint(equalToConstant: 0)
        heightConstraint = constraint
        NSLayoutConstraint.activate([constraint])
        addTapRecognizer(to: contentView, action: #selector(tapped(_:)))
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("单元格不走 IB")
    }

    func configure(index: Int, isSeparator: Bool, gap: CGFloat) {
        blockIndex = index
        isUserInteractionEnabled = !isSeparator
        heightConstraint?.constant = isSeparator ? 0 : 20
        tailGap = isSeparator ? 0 : gap
    }

    @objc private func tapped(_ recognizer: UITapGestureRecognizer) {
        reportTap(visibleCaret: 0)
    }
}

// MARK: 分隔线单元格（桌面 ruleLine：1pt rule 色）。

final class LiveRuleCell: LiveBaseCell {
    static let reuseIdentifier = "live-rule"

    override init(frame: CGRect) {
        super.init(frame: frame)
        let rule = UIView()
        rule.translatesAutoresizingMaskIntoConstraints = false
        rule.backgroundColor = Theme.Semantic.rule
        rule.layer.cornerRadius = 0.5
        contentView.addSubview(rule)
        NSLayoutConstraint.activate([
            rule.topAnchor.constraint(equalTo: contentView.topAnchor),
            rule.leadingAnchor.constraint(equalTo: contentView.leadingAnchor),
            rule.trailingAnchor.constraint(equalTo: contentView.trailingAnchor),
            rule.heightAnchor.constraint(equalToConstant: 1),
        ])
        gapConstraint = contentView.bottomAnchor.constraint(equalTo: rule.bottomAnchor)
        gapConstraint?.isActive = true
        addTapRecognizer(to: contentView, action: #selector(tapped(_:)))
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("单元格不走 IB")
    }

    func configure(index: Int, gap: CGFloat) {
        blockIndex = index
        tailGap = gap
    }

    @objc private func tapped(_ recognizer: UITapGestureRecognizer) {
        reportTap(visibleCaret: 0)
    }
}

// MARK: 文本单元格（段落/标题/引用/列表/任务）：[饰件][渲染文本]。

final class LiveTextCell: LiveBaseCell {
    static let reuseIdentifier = "live-text"
    let textView = PlainRenderTextView()
    private let ornamentLabel = UILabel()
    private let quoteBar = UIView()
    private var ornamentTopConstraint: NSLayoutConstraint?
    private var textLeadingConstraint: NSLayoutConstraint?

    override init(frame: CGRect) {
        super.init(frame: frame)
        for view in [quoteBar, ornamentLabel, textView] {
            view.translatesAutoresizingMaskIntoConstraints = false
            contentView.addSubview(view)
        }
        quoteBar.backgroundColor = Theme.Semantic.quoteBorder
        quoteBar.layer.cornerRadius = Theme.Typography.quoteBar / 2
        quoteBar.isHidden = true
        ornamentLabel.isHidden = true
        let top = ornamentLabel.topAnchor.constraint(equalTo: contentView.topAnchor)
        ornamentTopConstraint = top
        let leading = textView.leadingAnchor.constraint(equalTo: contentView.leadingAnchor)
        textLeadingConstraint = leading
        let gap = contentView.bottomAnchor.constraint(equalTo: textView.bottomAnchor)
        gapConstraint = gap
        NSLayoutConstraint.activate([
            quoteBar.topAnchor.constraint(equalTo: contentView.topAnchor, constant: 2),
            quoteBar.leadingAnchor.constraint(equalTo: contentView.leadingAnchor),
            quoteBar.widthAnchor.constraint(equalToConstant: Theme.Typography.quoteBar),
            quoteBar.bottomAnchor.constraint(equalTo: contentView.bottomAnchor, constant: -2),
            textView.topAnchor.constraint(equalTo: contentView.topAnchor),
            leading,
            textView.trailingAnchor.constraint(equalTo: contentView.trailingAnchor),
            gap,
            ornamentLabel.leadingAnchor.constraint(equalTo: contentView.leadingAnchor),
            top,
        ])
        addTapRecognizer(to: textView, action: #selector(textTapped(_:)))
        addTapRecognizer(to: contentView, action: #selector(chromeTapped(_:)))
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("单元格不走 IB")
    }

    // 配置：attributed 为投影渲染串；列表/任务/引用块带饰件并让出悬挂缩进。
    func configure(block: LiveBlock, attributed: NSAttributedString, index: Int, gap: CGFloat) {
        blockIndex = index
        tailGap = gap
        textView.attributedText = attributed
        quoteBar.isHidden = block.kind != .quote
        if block.kind == .quote {
            ornamentLabel.isHidden = true
            textLeadingConstraint?.constant = Theme.Typography.quoteBar + 8
        } else if let marker = Self.marker(for: block) {
            ornamentLabel.text = marker.text
            ornamentLabel.font = Theme.Typography.bodyFont
            ornamentLabel.textColor = marker.color
            ornamentLabel.isHidden = false
            let markerWidth = (marker.text as NSString).size(withAttributes: [.font: Theme.Typography.bodyFont]).width
            textLeadingConstraint?.constant = ceil(markerWidth) + 8
            ornamentTopConstraint?.constant = 0
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

    @objc private func textTapped(_ recognizer: UITapGestureRecognizer) {
        let point = recognizer.location(in: textView)
        reportTap(visibleCaret: textView.caretOffset(at: point))
    }

    @objc private func chromeTapped(_ recognizer: UITapGestureRecognizer) {
        // 点在饰件/留白：光标落块末（桌面点行内空白光标停在最近文字）。
        reportTap(visibleCaret: (textView.text as NSString?)?.length ?? 0)
    }
}

// MARK: 代码单元格（桌面 codeCard：surface 底 + rule 描边 + 圆角 + 内边距）。

final class LiveCodeCell: LiveBaseCell {
    static let reuseIdentifier = "live-code"
    private let card = UIView()
    let textView = PlainRenderTextView()

    override init(frame: CGRect) {
        super.init(frame: frame)
        card.translatesAutoresizingMaskIntoConstraints = false
        contentView.addSubview(card)
        textView.translatesAutoresizingMaskIntoConstraints = false
        card.addSubview(textView)
        NSLayoutConstraint.activate([
            card.topAnchor.constraint(equalTo: contentView.topAnchor),
            card.leadingAnchor.constraint(equalTo: contentView.leadingAnchor),
            card.trailingAnchor.constraint(equalTo: contentView.trailingAnchor),
            textView.topAnchor.constraint(equalTo: card.topAnchor, constant: Theme.Typography.codePadY),
            textView.leadingAnchor.constraint(equalTo: card.leadingAnchor, constant: Theme.Typography.codePadX),
            textView.trailingAnchor.constraint(equalTo: card.trailingAnchor, constant: -Theme.Typography.codePadX),
            card.bottomAnchor.constraint(equalTo: textView.bottomAnchor, constant: Theme.Typography.codePadY),
        ])
        gapConstraint = contentView.bottomAnchor.constraint(equalTo: card.bottomAnchor)
        gapConstraint?.isActive = true
        addTapRecognizer(to: card, action: #selector(tapped(_:)))
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("单元格不走 IB")
    }

    func configure(block: LiveBlock, attributed: NSAttributedString, index: Int, gap: CGFloat, traits: UITraitCollection) {
        blockIndex = index
        tailGap = gap
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
    static let reuseIdentifier = "live-active"
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
            contentView.addSubview(view)
        }
        quoteBar.backgroundColor = Theme.Semantic.quoteBorder
        quoteBar.layer.cornerRadius = Theme.Typography.quoteBar / 2
        card.layer.cornerRadius = Theme.Typography.codeRadius
        card.layer.borderWidth = 1
        card.clipsToBounds = true
        quoteBar.isHidden = true
        ornamentLabel.isHidden = true
        card.isHidden = true
        let top = ornamentLabel.topAnchor.constraint(equalTo: contentView.topAnchor)
        ornamentTopConstraint = top
        NSLayoutConstraint.activate([
            quoteBar.topAnchor.constraint(equalTo: contentView.topAnchor, constant: 2),
            quoteBar.leadingAnchor.constraint(equalTo: contentView.leadingAnchor),
            quoteBar.widthAnchor.constraint(equalToConstant: Theme.Typography.quoteBar),
            quoteBar.bottomAnchor.constraint(equalTo: contentView.bottomAnchor, constant: -2),
            ornamentLabel.leadingAnchor.constraint(equalTo: contentView.leadingAnchor),
            top,
            card.leadingAnchor.constraint(equalTo: contentView.leadingAnchor),
            card.trailingAnchor.constraint(equalTo: contentView.trailingAnchor),
        ])
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("单元格不走 IB")
    }

    override func prepareForReuse() {
        super.prepareForReuse()
        embed(nil, block: LiveBlock(kind: .para, startLine: 0, endLine: 0, raw: ""), traits: traitCollection, gap: 0)
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
                contentView.topAnchor.constraint(equalTo: card.topAnchor),
                editor.topAnchor.constraint(equalTo: card.topAnchor, constant: Theme.Typography.codePadY),
                editor.leadingAnchor.constraint(equalTo: card.leadingAnchor, constant: Theme.Typography.codePadX),
                editor.trailingAnchor.constraint(equalTo: card.trailingAnchor, constant: -Theme.Typography.codePadX),
                card.bottomAnchor.constraint(equalTo: editor.bottomAnchor, constant: Theme.Typography.codePadY),
                contentView.bottomAnchor.constraint(equalTo: card.bottomAnchor, constant: -gap),
            ])
        } else {
            contentView.addSubview(editor)
            editorConstraints.append(contentsOf: [
                editor.topAnchor.constraint(equalTo: contentView.topAnchor),
                editor.leadingAnchor.constraint(equalTo: contentView.leadingAnchor, constant: markerWidth),
                editor.trailingAnchor.constraint(equalTo: contentView.trailingAnchor),
                contentView.bottomAnchor.constraint(equalTo: editor.bottomAnchor, constant: -gap),
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

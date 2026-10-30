import UIKit

// 编辑器视图控制器：承载实时/源码双模式编辑、右上模式切换浮动钮与底部
// Markdown 快捷栏，桥接 SwiftUI 宿主回调。时间弹层属 M3；点组件只吞 tap
// 的行为在 EditorTextView 内实现。
final class EditorViewController: UIViewController {
    var onTextChange: ((String) -> Void)?
    var onEditingSessionEnd: (() -> Void)?

    // 编辑模式：live 为组件渲染的编辑缓冲；source 为原始 Markdown 纯文本直编
    //（不扫描不收簇，组件位置显示原始令牌文本）。
    enum Mode { case live, source }
    private(set) var mode: Mode = .live

    // live 视图：切回实时时用当前源码文本整体重建（init 全量首扫），撤销双栈
    // 随实例丢弃——模式切换清空撤销栈是既定语义（视图各自重建）。
    private(set) var editorView: EditorTextView?
    // source 视图：进入源码模式时创建，离开时丢弃（撤销栈同样清空）。
    private(set) var sourceView: UITextView?
    private let writable: Bool

    // 右上浮动切换钮（毛玻璃）：仅可写文件提供；只读文件保持 M2-1 现状。
    private var modeFab: UIVisualEffectView?
    private var modeIcon: UIImageView?

    // 底部 Markdown 快捷栏（inputAccessoryView）：两种模式的文本视图共用同一实例。
    private let toolbar = MarkdownToolbar()

    init(raw: String, writable: Bool, reason: String) {
        self.writable = writable
        editorView = EditorTextView(raw: raw, writable: writable)
        super.init(nibName: nil, bundle: nil)
        title = nil // 由 SwiftUI 导航栏提供
        if !writable {
            editorView?.isEditable = false
            configureReadOnlyBanner(reason: reason)
        }
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("编辑器不走 IB")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = Theme.Semantic.background
        if let editorView {
            install(editorView)
            editorView.onContentChanged = { [weak self] raw in
                self?.onTextChange?(raw)
            }
        }
        if writable {
            toolbar.onAction = { [weak self] action in
                guard let self, let active = self.activeTextView() else { return }
                MarkdownSnippet.apply(action, to: active)
            }
            setupModeFab()
        }
    }

    // 文本视图装配：顶到 view 顶部、底部随键盘（keyboardLayoutGuide，横竖屏通用）。
    private func install(_ textView: UITextView) {
        textView.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(textView)
        if writable {
            textView.inputAccessoryView = toolbar
        }
        NSLayoutConstraint.activate([
            textView.topAnchor.constraint(equalTo: view.topAnchor),
            textView.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            textView.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            textView.bottomAnchor.constraint(equalTo: view.keyboardLayoutGuide.topAnchor),
        ])
        // 模式切换重新装配文本视图（addSubview 追加到最顶层）会把模式浮动钮压在
        // 下面：首次切换后钮即消失且点按落在新视图上，必须随装配重新置顶。
        if let modeFab {
            view.bringSubviewToFront(modeFab)
        }
    }

    private func activeTextView() -> UITextView? {
        switch mode {
        case .live: return editorView
        case .source: return sourceView
        }
    }

    // MARK: 模式切换

    // 切换语义（PR 已注明）：
    // - live→source：展示 document 的 raw 投影（组件位置即原始令牌文本）；
    // - source→live：用当前源码文本重建 EditorDocument（init 全量首扫重新收簇），
    //   外来无主 U+FFFC 先行净化；
    // - 撤销栈随视图重建清空（可接受，两种模式各自的栈不复用）；
    // - 源码模式编辑经 delegate 以 textView.text 为 raw 上报写回（此时没有文档，
    //   不走 document.rawText）；
    // - 光标/选区经缓冲↔raw 坐标映射保留；映射点落在组件内部时钳到组件边界，
    //   越界钳到文档尾。
    func toggleMode() {
        guard writable else { return }
        switch mode {
        case .live: enterSourceMode()
        case .source: enterLiveMode()
        }
    }

    private func enterSourceMode() {
        guard let editor = editorView else { return }
        let doc = editor.document
        let selection = editor.selectedRange
        let rawStart = doc.rawOffset(ofBufferOffset: selection.location)
        let rawEnd = doc.rawOffset(ofBufferOffset: selection.location + selection.length)
        let raw = doc.rawText
        let wasFirstResponder = editor.isFirstResponder
        editor.removeFromSuperview()
        editorView = nil

        let source = UITextView()
        source.backgroundColor = Theme.Semantic.background
        source.font = .monospacedSystemFont(ofSize: 14, weight: .regular)
        source.textColor = Theme.Semantic.text
        source.textContainerInset = UIEdgeInsets(top: 12, left: 12, bottom: 16, right: 12)
        source.isEditable = writable
        source.keyboardDismissMode = .interactive
        source.delegate = self
        source.text = raw
        source.typingAttributes = [
            .font: UIFont.monospacedSystemFont(ofSize: 14, weight: .regular),
            .foregroundColor: UIColor.label,
        ]
        install(source)
        clampSelection(into: source, start: rawStart, end: rawEnd)
        sourceView = source
        mode = .source
        updateModeFab()
        if wasFirstResponder {
            source.becomeFirstResponder()
        }
    }

    private func enterLiveMode() {
        guard let source = sourceView else { return }
        let selection = source.selectedRange
        let raw = source.text ?? ""
        let wasFirstResponder = source.isFirstResponder
        source.removeFromSuperview()
        sourceView = nil

        // 用当前源码文本整体重建：init 全量首扫收簇；外来无主 U+FFFC 净化。
        let editor = EditorTextView(raw: EditorDocument.sanitizingPasted(raw), writable: writable)
        install(editor)
        editor.onContentChanged = { [weak self] raw in
            self?.onTextChange?(raw)
        }
        let doc = editor.document
        let bufferStart = doc.bufferOffset(ofRawOffset: selection.location)
        let bufferEnd = doc.bufferOffset(ofRawOffset: selection.location + selection.length)
        editor.selectedRange = NSRange(location: min(bufferStart, bufferEnd), length: abs(bufferEnd - bufferStart))
        editorView = editor
        mode = .live
        updateModeFab()
        if wasFirstResponder {
            editor.becomeFirstResponder()
        }
    }

    // 选区落位：位置钳到文档长度内，start/end 归序。
    private func clampSelection(into textView: UITextView, start: Int, end: Int) {
        let length = (textView.text as NSString?)?.length ?? 0
        let location = min(max(min(start, end), 0), length)
        let selEnd = min(max(max(start, end), location), length)
        textView.selectedRange = NSRange(location: location, length: selEnd - location)
    }

    // MARK: 浮动切换钮

    private func setupModeFab() {
        let fab = UIVisualEffectView(effect: UIBlurEffect(style: .systemThinMaterial))
        fab.clipsToBounds = true
        fab.layer.cornerRadius = 21
        fab.translatesAutoresizingMaskIntoConstraints = false
        fab.isAccessibilityElement = true
        let icon = UIImageView()
        icon.preferredSymbolConfiguration = UIImage.SymbolConfiguration(pointSize: 13, weight: .semibold)
        icon.tintColor = .label
        icon.translatesAutoresizingMaskIntoConstraints = false
        fab.contentView.addSubview(icon)
        // 先入层级再激活约束：约束跨视图时要求共同祖先。
        view.addSubview(fab)
        NSLayoutConstraint.activate([
            fab.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 10),
            fab.trailingAnchor.constraint(equalTo: view.trailingAnchor, constant: -14),
            fab.widthAnchor.constraint(equalToConstant: 42),
            fab.heightAnchor.constraint(equalToConstant: 42),
            icon.centerXAnchor.constraint(equalTo: fab.contentView.centerXAnchor),
            icon.centerYAnchor.constraint(equalTo: fab.contentView.centerYAnchor),
        ])
        let tap = UITapGestureRecognizer(target: self, action: #selector(toggleModeTapped))
        fab.addGestureRecognizer(tap)
        view.bringSubviewToFront(fab)
        modeFab = fab
        modeIcon = icon
        updateModeFab()
    }

    // 图标指向要切去的模式：实时显源码符号，源码显渲染文档符号。
    private func updateModeFab() {
        guard let icon = modeIcon else { return }
        let toSource = mode == .live
        icon.image = UIImage(systemName: toSource ? "chevron.left.forwardslash.chevron.right" : "doc.richtext.fill")
        let label = toSource ? Strings.modeToggleToSource : Strings.modeToggleToLive
        icon.accessibilityLabel = label
        modeFab?.accessibilityLabel = label
    }

    @objc private func toggleModeTapped() {
        toggleMode()
    }

    // MARK: 只读横幅

    // 只读文件在编辑器内保留写保护横幅（与列表页一致）。
    private func configureReadOnlyBanner(reason: String) {
        let banner = ReadOnlyBanner(message: Strings.bannerMessage(for: reason))
        banner.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(banner)
        NSLayoutConstraint.activate([
            banner.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor, constant: 0),
            banner.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            banner.trailingAnchor.constraint(equalTo: view.trailingAnchor),
        ])
        // 横幅盖在编辑器上方：编辑器顶到 view 顶部，横幅悬浮（简单起见不用额外布局）。
        view.bringSubviewToFront(banner)
    }

    override func viewWillDisappear(_ animated: Bool) {
        super.viewWillDisappear(animated)
        // 离开编辑器立即落盘待写改动（幂等）。
        onEditingSessionEnd?()
        onEditingSessionEnd = nil
    }
}

// 源码模式的文本变化上报：textView.text 即磁盘 raw（源码模式没有文档实例，
// 不经 document.rawText），写回防抖管道与实时模式共用。
extension EditorViewController: UITextViewDelegate {
    func textViewDidChange(_ textView: UITextView) {
        guard mode == .source, textView === sourceView else { return }
        onTextChange?(textView.text ?? "")
    }
}

// 只读横幅（M2-1 临时样式，M2-3 按主题重做）。
final class ReadOnlyBanner: UIView {
    init(message: String) {
        super.init(frame: .zero)
        let stack = UIStackView(arrangedSubviews: [
            {
                let icon = UIImageView(image: UIImage(systemName: "lock.fill"))
                icon.setContentHuggingPriority(.required, for: NSLayoutConstraint.Axis.horizontal)
                return icon
            }(),
            {
                let label = UILabel()
                label.text = message
                label.font = UIFont.preferredFont(forTextStyle: .footnote)
                label.numberOfLines = 0
                return label
            }(),
        ])
        stack.axis = .horizontal
        stack.spacing = 8
        stack.alignment = UIStackView.Alignment.top
        stack.translatesAutoresizingMaskIntoConstraints = false
        addSubview(stack)
        backgroundColor = .systemOrange
        NSLayoutConstraint.activate([
            stack.topAnchor.constraint(equalTo: layoutMarginsGuide.topAnchor, constant: 8),
            stack.bottomAnchor.constraint(equalTo: layoutMarginsGuide.bottomAnchor, constant: -8),
            stack.leadingAnchor.constraint(equalTo: layoutMarginsGuide.leadingAnchor, constant: 12),
            stack.trailingAnchor.constraint(equalTo: layoutMarginsGuide.trailingAnchor, constant: -12),
        ])
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("横幅不走 IB")
    }
}

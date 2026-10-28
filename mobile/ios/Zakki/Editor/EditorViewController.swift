import UIKit

// 编辑器视图控制器：承载 EditorTextView 与只读横幅，桥接 SwiftUI 宿主回调。
// 时间弹层属 M3；点组件只吞 tap 的行为在 EditorTextView 内实现。
final class EditorViewController: UIViewController {
    var onTextChange: ((String) -> Void)?
    var onEditingSessionEnd: (() -> Void)?

    private let editorView: EditorTextView
    private let writable: Bool

    init(raw: String, writable: Bool, reason: String) {
        self.writable = writable
        editorView = EditorTextView(raw: raw, writable: writable)
        super.init(nibName: nil, bundle: nil)
        title = nil // 由 SwiftUI 导航栏提供
        if !writable {
            editorView.isEditable = false
            configureReadOnlyBanner(reason: reason)
        }
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("编辑器不走 IB")
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .systemBackground
        editorView.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(editorView)
        editorView.onContentChanged = { [weak self] raw in
            self?.onTextChange?(raw)
        }
        NSLayoutConstraint.activate([
            editorView.topAnchor.constraint(equalTo: view.topAnchor),
            editorView.leadingAnchor.constraint(equalTo: view.leadingAnchor),
            editorView.trailingAnchor.constraint(equalTo: view.trailingAnchor),
            editorView.bottomAnchor.constraint(equalTo: view.keyboardLayoutGuide.topAnchor),
        ])
    }

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

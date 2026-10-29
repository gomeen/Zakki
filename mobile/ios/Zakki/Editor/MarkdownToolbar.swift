import UIKit

// MARK: - 快捷栏动作

// 底部 Markdown 快捷栏的动作集：前缀类插入到光标所在行行首；包裹类作用于选区
//（无选区插占位对）；插入时间只生成“当前时刻 + 1 天”的令牌文本，随后由实时
// 模式既有扫描管线收为组件（时间弹层属 M3，本票全程无弹层）。
enum MarkdownSnippetAction {
    case heading1
    case heading2
    case bullet
    case task
    case quote
    case bold
    case code
    case insertTime
}

// MARK: - 底部快捷栏（inputAccessoryView）

// 两种模式（实时/源码）的文本视图共用同一工具栏实例；按钮动作统一交
// MarkdownSnippet 落到当前激活的文本视图。按钮字形是 Markdown 记号符号
//（非文案，不本地化），语义经无障碍标签（Strings）表达。
final class MarkdownToolbar: UIView {
    var onAction: ((MarkdownSnippetAction) -> Void)?

    private static let items: [(glyph: String, action: MarkdownSnippetAction, accessibility: String)] = [
        ("H1", .heading1, Strings.toolbarHeading1),
        ("H2", .heading2, Strings.toolbarHeading2),
        ("•", .bullet, Strings.toolbarBulletList),
        ("☑", .task, Strings.toolbarTaskList),
        ("❝", .quote, Strings.toolbarQuote),
        ("B", .bold, Strings.toolbarBold),
        ("`", .code, Strings.toolbarCode),
        ("@", .insertTime, Strings.toolbarInsertTime),
    ]

    // 测试取按钮触发 onAction 接线。
    private(set) var buttons: [UIButton] = []

    override init(frame: CGRect) {
        super.init(frame: frame)
        autoresizingMask = [.flexibleWidth]
        backgroundColor = .secondarySystemBackground
        let stack = UIStackView()
        stack.axis = .horizontal
        stack.distribution = .fillEqually
        stack.spacing = 2
        stack.translatesAutoresizingMaskIntoConstraints = false
        addSubview(stack)
        for item in Self.items {
            let button = UIButton(type: .system)
            button.setTitle(item.glyph, for: .normal)
            button.titleLabel?.font = .monospacedSystemFont(ofSize: 15, weight: .semibold)
            button.accessibilityLabel = item.accessibility
            button.addAction(UIAction { [weak self] _ in
                self?.onAction?(item.action)
            }, for: .touchUpInside)
            stack.addArrangedSubview(button)
            buttons.append(button)
        }
        // 顶部发丝线：与正文区分隔。
        let divider = UIView()
        divider.backgroundColor = .separator
        divider.translatesAutoresizingMaskIntoConstraints = false
        addSubview(divider)
        NSLayoutConstraint.activate([
            heightAnchor.constraint(equalToConstant: 44),
            stack.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 4),
            stack.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -4),
            stack.topAnchor.constraint(equalTo: topAnchor),
            stack.bottomAnchor.constraint(equalTo: bottomAnchor),
            divider.leadingAnchor.constraint(equalTo: leadingAnchor),
            divider.trailingAnchor.constraint(equalTo: trailingAnchor),
            divider.topAnchor.constraint(equalTo: topAnchor),
            divider.heightAnchor.constraint(equalToConstant: 0.5),
        ])
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("快捷栏不走 IB")
    }
}

// MARK: - 动作落点

// 快捷栏动作到文本编辑的落地：全部经 UITextView 的原生路径（selectedRange +
// insertText）操作。EditorTextView 覆写的 insertText 会自动记账撤销并按既有规则
// 收组件；源码纯文本视图走系统实现，两模式共用同一套语义。
enum MarkdownSnippet {
    // 前缀类：插入到光标所在行行首。
    static let prefixes: [MarkdownSnippetAction: String] = [
        .heading1: "# ",
        .heading2: "## ",
        .bullet: "- ",
        .task: "- [ ] ",
        .quote: "> ",
    ]

    // 包裹类：有选区包裹选区，无选区插占位对、光标落在对中。
    static let wraps: [MarkdownSnippetAction: String] = [
        .bold: "**",
        .code: "`",
    ]

    static func apply(_ action: MarkdownSnippetAction, to textView: UITextView) {
        if let prefix = prefixes[action] {
            insertPrefix(prefix, into: textView)
        } else if let marker = wraps[action] {
            wrapSelection(marker, into: textView)
        } else if action == .insertTime {
            textView.insertText(timeTokenText())
        }
    }

    // 插入时间入口：生成“当前时刻 + 1 天”的展示文本（显示层 DateFormatter 格式化，
    // 不是时间令牌解析——令牌识别红线只约束识别逻辑，仍全部在 core 绑定侧）。
    // 文本落在实时编辑器时按 M2-1 既有规则自动收为组件，全程无弹层。
    static func timeTokenText(now: Date = Date(), calendar: Calendar = .current) -> String {
        let tomorrow = calendar.date(byAdding: .day, value: 1, to: now) ?? now
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        // 格式化的日历与时区都显式跟随调用方：否则各自回落系统值，跨时区机器输出不一致
        //（DateFormatter.calendar 赋值不继承时区，timeZone 是独立属性）。
        formatter.calendar = calendar
        formatter.timeZone = calendar.timeZone
        formatter.dateFormat = "'@'yyyy-MM-dd HH:mm"
        return formatter.string(from: tomorrow) + " "
    }

    // 前缀插入：记住原光标/选区 → 移到行首插入 → 平移回原位（前缀只加在光标之前）。
    private static func insertPrefix(_ prefix: String, into textView: UITextView) {
        let selection = textView.selectedRange
        let lineStart = lineStart(ofCaret: selection.location, in: textView.text ?? "")
        textView.selectedRange = NSRange(location: lineStart, length: 0)
        textView.insertText(prefix)
        let shift = (prefix as NSString).length
        textView.selectedRange = NSRange(location: selection.location + shift, length: selection.length)
    }

    // 光标所在行行首：光标前最近换行符之后；无换行符为文档首。
    static func lineStart(ofCaret caret: Int, in text: String) -> Int {
        let ns = text as NSString
        let clamped = min(max(caret, 0), ns.length)
        let range = ns.rangeOfCharacter(from: .newlines, options: [.backwards], range: NSRange(location: 0, length: clamped))
        guard range.location != NSNotFound else { return 0 }
        return range.location + range.length
    }

    // 包裹插入：两端分别插入标记（选区内文本原样保留，含组件字符——不跨组件
    // 替换），随后重新选中包裹内容便于继续输入；无选区插占位对、光标居中。
    private static func wrapSelection(_ marker: String, into textView: UITextView) {
        let selection = textView.selectedRange
        let markerLength = (marker as NSString).length
        if selection.length > 0 {
            let start = selection.location
            textView.selectedRange = NSRange(location: start, length: 0)
            textView.insertText(marker)
            let end = start + markerLength + selection.length
            textView.selectedRange = NSRange(location: end, length: 0)
            textView.insertText(marker)
            textView.selectedRange = NSRange(location: start + markerLength, length: selection.length)
        } else {
            let caret = selection.location
            textView.insertText(marker + marker)
            textView.selectedRange = NSRange(location: caret + markerLength, length: 0)
        }
    }
}

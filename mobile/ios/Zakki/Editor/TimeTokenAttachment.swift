import UIKit

// 承载组件原始 Markdown 的文本附件（D1）：附件属性随缓冲字符（U+FFFC）走，
// 撤销/重做恢复属性即恢复 tokenRaw，文档内核从属性读回组件内容。
final class TimeTokenAttachment: NSTextAttachment {
    let tokenRaw: String
    private let info: TimeTokenInfo

    init(tokenRaw: String) {
        self.tokenRaw = tokenRaw
        // 显示信息经绑定重扫 raw 得到（suffix/ParseRule 投影），客户端不做时间解析。
        self.info = EditorDocument.findTimeToken(in: tokenRaw, protectedJSON: "").map(TimeTokenInfo.init)
            ?? TimeTokenInfo(due: tokenRaw) // 防御：扫描异常时原样展示 raw，不臆造信息
        super.init(data: nil, ofType: nil)
        applyAppearance(traits: .current)
    }

    required init?(coder: NSCoder) {
        fatalError("编辑器附件不走归档")
    }

    // 胶囊是位图附件，动态色不生效：明暗外观切换后由宿主按新 traits 重画
    //（原地换图，不动文本、不进撤销栈）。
    func refreshAppearance(traits: UITraitCollection) {
        applyAppearance(traits: traits)
    }

    private func applyAppearance(traits: UITraitCollection) {
        let image = TimePill.image(for: info, traits: traits)
        self.image = image
        // 挂在基线上（descender 为负），高度用 body 字体行盒。
        let font = Theme.Typography.bodyFont
        bounds = CGRect(x: 0, y: font.descender, width: image.size.width, height: image.size.height)
    }
}

// 片段→富文本的统一渲染入口：文本用主题基础属性，组件渲染为单个附件字符。
enum EditorRender {
    // 基础属性取自主题（桌面 sonnet 正文字号/行高 + 正文语义色）；前景色是
    // 动态 UIColor，明暗切换时既有文本自动跟随。标题/任务/引用/代码的排版
    // 令牌已在 Theme.Typography 备好，随块级渲染接入时消费。
    static var baseAttributes: [NSAttributedString.Key: Any] {
        [
            .font: Theme.Typography.bodyFont,
            .foregroundColor: Theme.Semantic.text,
            .paragraphStyle: Theme.Typography.bodyParagraph,
        ]
    }

    static func piece(_ piece: EditorPiece, base: [NSAttributedString.Key: Any] = baseAttributes) -> NSAttributedString {
        switch piece {
        case .text(let s):
            return NSAttributedString(string: s, attributes: base)
        case .token(let raw):
            let out = NSMutableAttributedString(attachment: TimeTokenAttachment(tokenRaw: raw))
            out.addAttributes(base, range: NSRange(location: 0, length: 1))
            return out
        }
    }

    static func pieces(_ pieces: [EditorPiece], base: [NSAttributedString.Key: Any] = baseAttributes) -> NSAttributedString {
        let out = NSMutableAttributedString()
        for piece in pieces {
            out.append(Self.piece(piece, base: base))
        }
        return out
    }
}

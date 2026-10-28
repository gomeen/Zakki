import UIKit

// 承载组件原始 Markdown 的文本附件（D1）：附件属性随缓冲字符（U+FFFC）走，
// 撤销/重做恢复属性即恢复 tokenRaw，文档内核从属性读回组件内容。
final class TimeTokenAttachment: NSTextAttachment {
    let tokenRaw: String

    init(tokenRaw: String) {
        self.tokenRaw = tokenRaw
        super.init(data: nil, ofType: nil)
        // 显示信息经绑定重扫 raw 得到（suffix/ParseRule 投影），客户端不做时间解析。
        let info = EditorDocument.findTimeToken(in: tokenRaw, protectedJSON: "").map(TimeTokenInfo.init)
            ?? TimeTokenInfo(due: tokenRaw) // 防御：扫描异常时原样展示 raw，不臆造信息
        let image = TimePill.image(for: info)
        self.image = image
        // 挂在基线上（descender 为负），高度用 body 字体行盒。
        let font = UIFont.preferredFont(forTextStyle: .body)
        bounds = CGRect(x: 0, y: font.descender, width: image.size.width, height: image.size.height)
    }

    required init?(coder: NSCoder) {
        fatalError("编辑器附件不走归档")
    }
}

// 片段→富文本的统一渲染入口：文本用基础属性，组件渲染为单个附件字符。
enum EditorRender {
    static var baseAttributes: [NSAttributedString.Key: Any] {
        [.font: UIFont.preferredFont(forTextStyle: .body), .foregroundColor: UIColor.label]
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
            out.append(piece(piece, base: base))
        }
        return out
    }
}

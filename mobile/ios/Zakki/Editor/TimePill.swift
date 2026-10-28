import UIKit
import Zakkicore

// ParseRule 结果投影（{"every","unit","weekdays?","end?"}，unit: d/w/mo/y）。
struct RecurProjection: Equatable {
    var every: Int
    var unit: String
    var weekdays: [Int]
    var end: String?

    // 解析失败（理论上 core 已校验过 recur 合法性）返回 nil，由调用方回退显示原文。
    static func parse(_ raw: String) -> RecurProjection? {
        var err: NSError?
        let out = ZakkicoreParseRule(raw, &err)
        if let err {
            assertionFailure("ParseRule 失败: \(err)")
            return nil
        }
        struct Rule: Decodable {
            var every: Int
            var unit: String
            var weekdays: [Int]?
            var end: String?
        }
        guard let data = out.data(using: .utf8), let rule = try? JSONDecoder().decode(Rule.self, from: data) else { return nil }
        return RecurProjection(every: rule.every, unit: rule.unit, weekdays: rule.weekdays ?? [], end: rule.end)
    }
}

// 时间组件的显示信息：全部来自 core 的 suffix 与 ParseRule 投影，客户端不做时间解析。
struct TimeTokenInfo: Equatable {
    var due: String
    var hasAlert: Bool
    var recur: RecurProjection?
    var recurRaw: String?

    init(due: String, hasAlert: Bool = false, recur: RecurProjection? = nil, recurRaw: String? = nil) {
        self.due = due
        self.hasAlert = hasAlert
        self.recur = recur
        self.recurRaw = recurRaw
    }

    init(hit: TimeTokenHit) {
        // recur 空串表示无重复（core 的 suffix JSON 字段全出），回退为 nil。
        let recurRaw = hit.suffix.recur.flatMap { $0.isEmpty ? nil : $0 }
        self.init(
            due: hit.suffix.due,
            hasAlert: hit.suffix.hasAlert,
            recur: recurRaw.flatMap { RecurProjection.parse($0) },
            recurRaw: recurRaw
        )
    }

    // M2-1 临时文案（M2-3 按桌面主题重做）；档位用语对齐桌面 locales。
    var displayText: String {
        var parts: [String] = [due]
        if hasAlert { parts.append(Strings.pillAlert) }
        if let recur {
            parts.append(Self.recurText(recur))
        } else if let recurRaw {
            parts.append(recurRaw)
        }
        return parts.joined(separator: " · ")
    }

    private static func recurText(_ r: RecurProjection) -> String {
        var text: String
        switch r.unit {
        case "d":
            text = r.every <= 1 ? Strings.pillDaily : Strings.pillInterval(r.every, Strings.pillUnitDay)
        case "w":
            if !r.weekdays.isEmpty {
                let names = r.weekdays.map { Self.weekdayName($0) }.joined(separator: Strings.pillListSeparator)
                text = Strings.pillWeeklyOn(names)
            } else {
                text = r.every <= 1 ? Strings.pillWeekly : Strings.pillInterval(r.every, Strings.pillUnitWeek)
            }
        case "mo":
            text = r.every <= 1 ? Strings.pillMonthly : Strings.pillInterval(r.every, Strings.pillUnitMonth)
        case "y":
            text = r.every <= 1 ? Strings.pillYearly : Strings.pillInterval(r.every, Strings.pillUnitYear)
        default:
            text = r.every <= 1 ? r.unit : Strings.pillInterval(r.every, r.unit)
        }
        if let end = r.end {
            text += " " + Strings.pillUntil(end)
        }
        return text
    }

    // core 的 weekday 为 1-7、周一起始（对齐桌面 timeWeekday1..7）。
    private static func weekdayName(_ iso: Int) -> String {
        let en = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"]
        let zh = ["一", "二", "三", "四", "五", "六", "日"]
        guard (1...7).contains(iso) else { return "??" }
        return Strings.t(en[iso - 1], zh[iso - 1])
    }
}

// 时间组件胶囊（NSTextAttachment 位图）。M2-1 临时样式：静态渲染、不支持动态色
//（深浅色切换沿用旧位图），主题与交互属 M2-3/M3。
enum TimePill {
    static func image(for info: TimeTokenInfo) -> UIImage {
        let label = UILabel()
        label.text = info.displayText
        label.font = .systemFont(ofSize: 12, weight: info.recur == nil ? .regular : .semibold)
        label.textColor = info.recur == nil ? .secondaryLabel : UIColor(red: 0.70, green: 0.33, blue: 0.12, alpha: 1)
        label.sizeToFit()
        let pad: CGFloat = 8
        let height: CGFloat = 22
        let size = CGSize(width: ceil(label.frame.width) + pad * 2, height: height)
        return UIGraphicsImageRenderer(size: size).image { _ in
            let rect = CGRect(origin: .zero, size: size)
            let path = UIBezierPath(roundedRect: rect, cornerRadius: height / 2)
            let recurring = info.recur != nil
            (recurring ? UIColor(red: 0.99, green: 0.95, blue: 0.92, alpha: 1) : UIColor.secondarySystemBackground).setFill()
            path.fill()
            (recurring ? UIColor(red: 0.70, green: 0.33, blue: 0.12, alpha: 1) : UIColor.separator).setStroke()
            path.lineWidth = 1
            path.stroke()
            label.drawText(in: rect.insetBy(dx: pad, dy: (height - label.frame.height) / 2))
        }
    }
}

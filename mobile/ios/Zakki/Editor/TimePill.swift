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

    // 档位文案对齐桌面 locales（双语集中 Strings.swift）。
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

// 时间组件胶囊（NSTextAttachment 位图）。颜色从主题语义色推导（浅深两套，
// 随 traits 解析；桌面无独立 pill 主题色，spike 的朱红/反演占位值不作依据）；
// 几何沿用组件规格（高 22 · 圆角 11 · 水平内边距 8 · 字号 12 · 边框 1）。
// 位图不支持动态色，明暗切换由 TimeTokenAttachment.refreshAppearance 重画。
enum TimePill {
    static func image(for info: TimeTokenInfo, traits: UITraitCollection) -> UIImage {
        let colors = Theme.pillColors(recurring: info.recur != nil, dark: traits.userInterfaceStyle == .dark)
        let label = UILabel()
        label.text = info.displayText
        label.font = .systemFont(ofSize: Theme.Typography.pillSize, weight: info.recur == nil ? .regular : .semibold)
        label.textColor = colors.text.uiColor
        label.sizeToFit()
        let pad = Theme.Typography.pillPadH
        let height = Theme.Typography.pillHeight
        let size = CGSize(width: ceil(label.frame.width) + pad * 2, height: height)
        return UIGraphicsImageRenderer(size: size).image { _ in
            let rect = CGRect(origin: .zero, size: size)
            let path = UIBezierPath(roundedRect: rect, cornerRadius: Theme.Typography.pillRadius)
            colors.background.uiColor.setFill()
            path.fill()
            if let border = colors.borderColor {
                border.uiColor.setStroke()
                path.lineWidth = Theme.Typography.pillBorder
                path.stroke()
            }
            label.drawText(in: rect.insetBy(dx: pad, dy: (height - label.frame.height) / 2))
        }
    }
}

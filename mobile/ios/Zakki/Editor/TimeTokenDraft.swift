import Foundation
import Zakkicore

// MARK: - 时间弹层草稿（桌面 timePopoverState/timeRecurDraft 移植）

// 弹层编辑的会话草稿：打开时从令牌现状播种，「完成」才写回文档；写回经
// ReplaceTimeToken 绑定走既有编辑管线并进撤销。语义对齐桌面 live_timepopover：
// 未改保留 `^` 原文；preset=none 才清除重复段；自定义非法时保留原段。
struct RecurDraft: Equatable {
    var preset: String // none / ^1d / ^w@1-5 / ^1w / ^1mo / ^1y / custom
    var every: Int
    var unit: String // d / w / mo / y
    var weekdays: Set<Int> // core 口径 1-7（周一起始）
    var hasEnd: Bool
    var endDay: Date // 仅日期部分有意义
    var original: String

    // 与桌面 timeRecurPresets 同序。
    static let presets = ["none", "^1d", "^w@1-5", "^1w", "^1mo", "^1y", "custom"]

    static func label(for preset: String) -> String {
        switch preset {
        case "none": return Strings.recurNone
        case "^1d": return Strings.recurDaily
        case "^w@1-5": return Strings.recurWorkdays
        case "^1w": return Strings.recurWeekly
        case "^1mo": return Strings.recurMonthly
        case "^1y": return Strings.recurYearly
        default: return Strings.recurCustom
        }
    }

    // 从令牌现状播种：解析失败（未知规则整簇）回落 none，但 original 保留原文，
    // 完成时按「parsed 相等则保留原文」的桌面口径原样写回。
    init(raw: String, due: Date) {
        self.init(preset: "none", every: 1, unit: "d", weekdays: [], hasEnd: false, endDay: due, original: raw)
        guard !raw.isEmpty, let canonical = ReminderPlanner.ruleJSON(of: raw) else { return }
        struct Rule: Decodable {
            var every: Int
            var unit: String
            var weekdays: [Int]?
            var end: String?
        }
        guard let data = canonical.data(using: .utf8),
              let rule = try? JSONDecoder().decode(Rule.self, from: data) else { return }
        preset = "custom"
        every = max(1, rule.every)
        unit = rule.unit
        weekdays = Set(rule.weekdays ?? [])
        if let end = rule.end {
            hasEnd = true
            endDay = Self.dayDate(from: end) ?? due
        }
        // 与预设等价的规则回种为预设（桌面 newTimeRecurDraft 同口径）。
        for candidate in Self.presets where candidate != "none" && candidate != "custom" {
            if ReminderPlanner.ruleJSON(of: candidate) == canonical {
                preset = candidate
                break
            }
        }
    }

    init(preset: String, every: Int, unit: String, weekdays: Set<Int>, hasEnd: Bool, endDay: Date, original: String) {
        self.preset = preset
        self.every = every
        self.unit = unit
        self.weekdays = weekdays
        self.hasEnd = hasEnd
        self.endDay = endDay
        self.original = original
    }

    // 草稿 → 规则原文。桌面 timeRecurDraft.token 同构：preset=none 返回空；
    // custom 组装并校验，非法返回空（调用方保留 original）；canonical 相等时
    // 保留 original 原文（未改保留 `^` 原文）。
    func token() -> String {
        if preset == "none" || preset.isEmpty { return "" }
        var token = preset
        if preset == "custom" {
            let clamped = min(max(every, 1), 999)
            token = "^\(clamped)\(unit)"
            if unit == "w", clamped == 1, !weekdays.isEmpty {
                let days = weekdays.sorted().map(String.init).joined(separator: ",")
                token += "@" + days
            }
            if hasEnd {
                token += "<" + Self.dayString(from: endDay)
            }
        }
        guard let canonical = ReminderPlanner.ruleJSON(of: token) else { return "" }
        if let originalCanonical = ReminderPlanner.ruleJSON(of: original), originalCanonical == canonical {
            return original
        }
        return token
    }

    // MARK: 日期字符串（yyyy-MM-dd，与 core 规则 end 段同一格式）

    static let dayFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd"
        f.locale = Locale(identifier: "en_US_POSIX")
        return f
    }()

    static func dayString(from date: Date) -> String {
        dayFormatter.string(from: date)
    }

    static func dayDate(from string: String) -> Date? {
        dayFormatter.date(from: string)
    }
}

// MARK: - 令牌编辑草稿（时刻 / 提醒 / 重复）

struct TimeTokenDraft: Equatable {
    var due: Date // 分钟精度（秒数归零）
    var hasAlert: Bool
    var alertMs: Int64
    var recur: RecurDraft

    // 提醒档位（AlertTiers 绑定，ms 白名单）；0 表示准点。
    static let alertTiers: [(ms: Int64, label: String)] = {
        var err: NSError?
        let out = ZakkicoreAlertTiers(&err)
        struct Tier: Decodable { var suffix: String; var ms: Int64 }
        guard err == nil, let data = out.data(using: .utf8),
              let tiers = try? JSONDecoder().decode([Tier].self, from: data) else { return [] }
        return tiers.map { ($0.ms, Self.alertLabel(ms: $0.ms)) }
    }()

    // 档位展示名（对齐桌面 timeAlertLabel 与 locales 用语）。
    static func alertLabel(ms: Int64) -> String {
        switch ms {
        case 0: return Strings.alertAtTime
        case 3_600_000: return Strings.alertHour
        case 86_400_000: return Strings.alertDay
        default: return Strings.alertMinutes(Int(ms / 60_000))
        }
    }

    // 从令牌现状播种草稿（打开弹层的唯一入口）。
    init(hit: TimeTokenHit, now: Date = Date()) {
        let suffix = hit.suffix
        due = Self.parseDue(suffix.due) ?? now
        hasAlert = suffix.hasAlert
        alertMs = suffix.hasAlert ? (suffix.alertMs ?? 0) : 0
        let recurRaw = suffix.recur ?? ""
        recur = RecurDraft(raw: recurRaw, due: due)
    }

    // 成员初始化（弹层可变盒转回值类型用）。
    init(due: Date, hasAlert: Bool, alertMs: Int64, recur: RecurDraft) {
        self.due = due
        self.hasAlert = hasAlert
        self.alertMs = alertMs
        self.recur = recur
    }

    static func parseDue(_ text: String) -> Date? {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd HH:mm"
        f.locale = Locale(identifier: "en_US_POSIX")
        return f.date(from: text)
    }

    static func formatDue(_ date: Date) -> String {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd HH:mm"
        f.locale = Locale(identifier: "en_US_POSIX")
        return f.string(from: date)
    }

    // 写回的 suffix JSON；dueMs 仅输出、忽略输入，写回由 core 按 due + 时区重算
    //（规格 §3.2）。
    func suffixJSON() -> String {
        let payload: [String: Any] = [
            "due": Self.formatDue(due),
            "dueMs": 0,
            "alertMs": hasAlert ? alertMs : 0,
            "hasAlert": hasAlert,
            "recur": recur.token(),
        ]
        guard let data = try? JSONSerialization.data(withJSONObject: payload),
              let json = String(data: data, encoding: .utf8) else { return "" }
        return json
    }
}

import Foundation

// 用户可见文案集中在这一处：英文为源，系统首选语言为中文时取中文
//（#26 i18n 口径：随票新增文案即时双语化，#30 收口统一核对；键位与措辞对齐桌面 locales）。
enum Strings {
    static func t(_ en: String, _ zh: String) -> String {
        if let first = Locale.preferredLanguages.first, first.lowercased().hasPrefix("zh") {
            return zh
        }
        return en
    }

    // MARK: M1 既有文案（双语化随 M2 装配票补齐）

    static let importButton = "Import zakki.json"
    static let importFailedTitle = "Import Failed"
    static let ok = "OK"
    static let welcomeTitle = "No notes yet"
    static let welcomeMessage = "Import a zakki.json file to view your notes. M1 is read-only."
    static let noTabsTitle = "Empty file"
    static let noTabsYet = "This file has no tabs."
    static let untitledTab = "Untitled"
    static let emptyPreview = "(empty)"
    static let currentBadge = "Current"
    static let bannerTitle = "Read-only"

    // 写保护横幅正文，按 DecodeNotes 的 reason 给对应文案；legacy 提示先用桌面打开升级。
    static func bannerMessage(for reason: String) -> String {
        switch WriteProtectReason(rawValue: reason) {
        case .versionTooNew:
            return "This file uses a newer data version than this app supports and stays write-protected."
        case .tooManyTabs:
            return "This file has more than 9 tabs and stays write-protected."
        case .legacyVersion:
            return "This file uses an older data version. Open it in the Zakki desktop app first to upgrade."
        case .invalid:
            return "This file is not valid zakki.json and cannot be read."
        case nil:
            return "This file stays write-protected."
        }
    }

    // MARK: 时间组件胶囊（M2-1 临时样式，档位用语对齐桌面 locales）

    static var pillAlert: String { t("Reminder", "提醒") }
    static var pillListSeparator: String { t(", ", "、") }
    static var pillDaily: String { t("Every day", "每天") }
    static var pillWeekly: String { t("Every week", "每周") }
    static var pillMonthly: String { t("Every month", "每月") }
    static var pillYearly: String { t("Every year", "每年") }
    static var pillUnitDay: String { t("days", "天") }
    static var pillUnitWeek: String { t("weeks", "周") }
    static var pillUnitMonth: String { t("months", "月") }
    static var pillUnitYear: String { t("years", "年") }
    static var pillAccessibilityLabel: String { t("Time reminder", "时间提醒") }

    static func pillInterval(_ n: Int, _ unit: String) -> String {
        t("Every \(n) \(unit)", "每 \(n) \(unit)")
    }

    static func pillWeeklyOn(_ names: String) -> String {
        t("Weekly \(names)", "每周\(names)")
    }

    static func pillUntil(_ date: String) -> String {
        t("until \(date)", "至 \(date)")
    }
}

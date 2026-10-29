import Foundation

// 用户可见文案集中在这一处：英文为源，系统首选语言为中文时取中文
//（#26 i18n 口径：随票新增文案即时双语化，#30 收口统一核对；用语对齐桌面 locales）。
enum Strings {
    static func t(_ en: String, _ zh: String) -> String {
        if let first = Locale.preferredLanguages.first, first.lowercased().hasPrefix("zh") {
            return zh
        }
        return en
    }

    // MARK: M1 既有文案（随 M2-1 双语化）

    static var importButton: String { t("Import zakki.json", "导入 zakki.json") }
    static var importFailedTitle: String { t("Import Failed", "导入失败") }
    static let ok = "OK"
    static var welcomeTitle: String { t("No notes yet", "还没有笔记") }
    static var welcomeMessage: String { t("Import a zakki.json file to view your notes. M1 is read-only.", "导入一个 zakki.json 即可查看笔记。M1 为只读。") }
    static var noTabsTitle: String { t("Empty file", "空文件") }
    static var noTabsYet: String { t("This file has no tabs.", "这个文件没有标签页。") }
    static var untitledTab: String { t("Untitled", "未命名") }
    static var emptyPreview: String { t("(empty)", "（空）") }
    static var currentBadge: String { t("Current", "当前") }
    static var bannerTitle: String { t("Read-only", "只读") }

    // 写保护横幅正文，按 DecodeNotes 的 reason 给对应文案；legacy 提示先用桌面打开升级。
    static func bannerMessage(for reason: String) -> String {
        switch WriteProtectReason(rawValue: reason) {
        case .versionTooNew:
            return t("This file uses a newer data version than this app supports and stays write-protected.",
                     "这个文件的数据版本比当前应用支持的新，保持写保护。")
        case .tooManyTabs:
            return t("This file has more than 9 tabs and stays write-protected.",
                     "这个文件的标签页超过 9 个，保持写保护。")
        case .legacyVersion:
            return t("This file uses an older data version. Open it in the Zakki desktop app first to upgrade.",
                     "这个文件的数据版本较旧，请先用 Zakki 桌面端打开升级。")
        case .invalid:
            return t("This file is not valid zakki.json and cannot be read.",
                     "这个文件不是有效的 zakki.json，无法读取。")
        case nil:
            return t("This file stays write-protected.", "这个文件保持写保护。")
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

    // MARK: 编辑器与写回

    static var saveFailedTitle: String { t("Save Failed", "保存失败") }

    static func saveFailedMessage(code: String) -> String {
        t("Your changes could not be saved (\(code)). They stay pending and will retry.",
          "改动暂未能保存（\(code)），将保持待写并重试。")
    }

    static var editorDone: String { t("Done", "完成") }

    // MARK: M2-2 模式切换

    // 浮动切换钮的无障碍文案（图标指向要切去的模式）。
    static var modeToggleToSource: String { t("Switch to source", "切换到源码") }
    static var modeToggleToLive: String { t("Switch to live rendering", "切换到实时渲染") }
}

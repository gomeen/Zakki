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
    static var ok: String { t("OK", "确定") }
    // 空态产品标语（#41）：泛化定位一句话作主行，功能说明 welcomeMessage 为副行。
    static var tagline: String { t("Every thought has a home.", "每个念头，都有归处。") }
    static var welcomeMessage: String { t("Import a zakki.json to get started.", "导入一个 zakki.json 即可开始。") }
    static var noTabsTitle: String { t("Empty file", "空文件") }
    static var noTabsYet: String { t("This file has no tabs.", "这个文件没有标签。") }
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
                     "这个文件的标签超过 9 个，保持写保护。")
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

    static var pillAlert: String { t("Alert", "提醒") }
    static var pillListSeparator: String { t(", ", "、") }
    static var pillDaily: String { t("Every day", "每天") }
    static var pillWeekly: String { t("Every week", "每周") }
    static var pillMonthly: String { t("Every month", "每月") }
    static var pillYearly: String { t("Every year", "每年") }
    static var pillUnitDay: String { t("days", "天") }
    static var pillUnitWeek: String { t("weeks", "周") }
    static var pillUnitMonth: String { t("months", "月") }
    static var pillUnitYear: String { t("years", "年") }
    static var pillAccessibilityLabel: String { t("Time alert", "时间提醒") }

    static func taskCheckbox(_ checked: Bool) -> String {
        checked ? t("Task completed", "已勾选任务") : t("Task, not completed", "未勾选任务")
    }

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

    // MARK: 标签管理错误上报（M2-4，模型层）

    static func tabChangeFailedMessage(code: String) -> String {
        t("Could not update tabs (\(code)).", "标签更新未能保存（\(code)）。")
    }

    static var menuAccessibility: String { t("Menu", "菜单") }

    // MARK: 首启种子内容（桌面 initDefaults 同构；文案按移动端交互改写）

    static var seedWelcomeText: String { t("""
        # Zakki

        7 colorful tabs for quick notes. Tap any text to edit — Markdown markers stay hidden.

        - [ ] Try checking me
        **Bold**, *italic*, `inline code`, [links](https://github.com/egoist/mygo) and quotes are supported
        Type @2026-11-06 10:00 anywhere and it becomes a reminder pill

        Use the ⋯ menu (top left) to export a backup or import zakki.json from desktop.
        """, """
        # Zakki

        7 个彩色标签，随手记。点击任意文字即可编辑，标记会自动隐藏。

        - [ ] 试试勾选我
        支持**加粗**、*斜体*、`行内代码`、[链接](https://github.com/egoist/mygo) 与引用
        在文字里写下 @2026-11-06 10:00 会变成提醒胶囊

        左上角 ⋯ 菜单可以导出备份，或导入桌面端的 zakki.json。
        """) }

    static var seedTimeGuideTabName: String { t("Time & Reminders", "时间与提醒") }

    static var seedTimeGuideText: String { t("""
        ## Time & Reminders

        Write a time anywhere and a reminder pill appears:

        - @2026-11-06 10:00 — reminds on time
        - @2026-11-06 10:00 ~30m — 30 minutes early
        - @2026-11-06 10:00 ^1d — repeats daily

        Example: Friday @2026-11-06 10:00 ~30m sync the notes
        """, """
        ## 时间与提醒

        在任意行写下时间，就会出现提醒胶囊：

        - @2026-11-06 10:00 —— 到点提醒
        - @2026-11-06 10:00 ~30m —— 提前 30 分钟
        - @2026-11-06 10:00 ^1d —— 每天重复

        示例：周五 @2026-11-06 10:00 ~30m 记得同步进展
        """) }

    // MARK: M2-4 标签管理与导出

    static var edit: String { t("Edit", "编辑") }
    static var done: String { t("Done", "完成") }
    static var cancel: String { t("Cancel", "取消") }
    static var addTab: String { t("New Tab", "新建标签") }
    static var exportFile: String { t("Export zakki.json", "导出 zakki.json") }
    static var renameTab: String { t("Rename Tab", "重命名标签") }
    static var renameTabTitle: String { t("Rename Tab", "重命名标签") }
    static var renameTabMessage: String { t("Give this tab a name. Leave it blank to show Untitled.", "给这个标签起个名字，留空则显示未命名。") }
    static var tabColor: String { t("Tab Color", "标签颜色") }

    // MARK: M2-2 模式切换

    // 浮动切换钮的无障碍文案（图标指向要切去的模式）。
    static var modeToggleToSource: String { t("Switch to source", "切换到源码") }
    static var modeToggleToLive: String { t("Switch to preview", "切换到实时预览") }

    // MARK: M2-2 Markdown 快捷栏

    // 按钮无障碍标签（按钮字形为 Markdown 记号符号，不本地化）。
    static var toolbarHeading1: String { t("Heading 1", "标题 1") }
    static var toolbarHeading2: String { t("Heading 2", "标题 2") }
    static var toolbarBulletList: String { t("Bullet List", "无序列表") }
    static var toolbarTaskList: String { t("Task List", "任务列表") }
    static var toolbarQuote: String { t("Quote", "引用") }
    static var toolbarBold: String { t("Bold", "加粗") }
    static var toolbarCode: String { t("Inline Code", "行内代码") }
    static var toolbarInsertTime: String { t("Insert time", "插入时间") }

    // MARK: M3 时间弹层（用语对齐桌面 locales 的 time popover）

    static var popoverTitle: String { t("Time", "时间") }
    static var popoverDue: String { t("Date & time", "日期与时刻") }
    static var popoverDate: String { t("Date", "日期") }
    static var popoverClock: String { t("Time", "时刻") }
    static var popoverAlert: String { t("Alert", "提醒") }
    static var popoverRecur: String { t("Repeat", "重复") }
    static var popoverClear: String { t("Clear Time", "清除时间") }
    static var popoverDone: String { t("Done", "完成") }
    static var alertNone: String { t("None", "无提醒") }
    static var alertAtTime: String { t("On time", "准点") }
    static var alertHour: String { t("1h early", "提前 1 小时") }
    static var alertDay: String { t("1d early", "提前 1 天") }

    static func alertMinutes(_ n: Int) -> String {
        t("\(n)m early", "提前 \(n) 分钟")
    }

    static var recurNone: String { t("No repeat", "不重复") }
    static var recurDaily: String { t("Daily", "每天") }
    static var recurWorkdays: String { t("Workdays", "工作日") }
    static var recurWeekly: String { t("Weekly", "每周") }
    static var recurMonthly: String { t("Monthly", "每月") }
    static var recurYearly: String { t("Yearly", "每年") }
    static var recurCustom: String { t("Custom", "自定义") }
    static var recurHasEnd: String { t("Until a date", "设结束日期") }
    static var recurEnd: String { t("End date", "结束日期") }
    static var recurUnitDay: String { t("Days", "天") }
    static var recurUnitWeek: String { t("Weeks", "周") }
    static var recurUnitMonth: String { t("Months", "月") }
    static var recurUnitYear: String { t("Years", "年") }

    static func recurInterval(_ n: Int, _ unit: String) -> String {
        t("Every \(n) \(unit)", "每 \(n) \(unit)")
    }

    static func weekdayShort(_ iso: Int) -> String {
        let en = ["M", "T", "W", "T", "F", "S", "S"]
        let zh = ["一", "二", "三", "四", "五", "六", "日"]
        guard (1...7).contains(iso) else { return "?" }
        return Strings.t(en[iso - 1], zh[iso - 1])
    }

    // MARK: M3 提醒状态（四态：已排定／待同步／待续排／权限受限）

    static func statusScheduled(_ count: Int) -> String {
        t("\(count) scheduled", "已排定 \(count) 条")
    }

    static var statusNone: String { t("No reminders scheduled", "暂无已排定提醒") }
    static var statusPendingSync: String { t("Changes pending save", "改动待写入") }
    static var statusNeedsRenewal: String { t("More pending — open app to continue", "有提醒待续排，打开应用继续") }
    static var statusLimited: String { t("Notifications denied", "通知权限受限") }
    static var renewalNoticeBody: String {
        t("More reminders are waiting. Open Zakki to schedule them.",
          "还有提醒未排入系统，打开 Zakki 继续排定。")
    }
}

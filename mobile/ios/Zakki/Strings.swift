// M1 最小做法：用户可见文案集中在这一处英文源字符串，M2 再接 i18n
// （键位与措辞对齐桌面 locales/en.json 的风格）。
enum Strings {
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
}

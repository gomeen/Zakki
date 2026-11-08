import SwiftUI

// MARK: - 提醒状态条（规格 §3.4：已排定／待同步／待续排／权限受限）

// 状态条放在标签列表底部：调度跨全部标签，状态属于应用而非单个编辑器。
// 不把写盘成功等同于系统已接受——待同步与权限受限都是独立展示位。
struct ReminderStatusBar: View {
    @ObservedObject var scheduler = ReminderScheduler.shared
    @Environment(\.openURL) private var openURL

    var body: some View {
        let status = scheduler.status
        HStack(spacing: 8) {
            Image(systemName: iconName(status))
                .font(.system(size: 12))
                .foregroundStyle(Color(uiColor: iconColor(status)))
            Text(primaryText(status))
                .font(.system(size: 12))
                .foregroundStyle(Color(uiColor: Theme.Semantic.text))
            if let next = nextText(status) {
                Text(next)
                    .font(.system(size: 12))
                    .foregroundStyle(Color(uiColor: Theme.Semantic.textMuted))
            }
            Spacer()
            if status.isLimited {
                Button(Strings.ok) {
                    if let url = URL(string: UIApplication.openSettingsURLString) {
                        openURL(url)
                    }
                }
                .font(.system(size: 12))
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 8)
        .background(Color(uiColor: Theme.Semantic.surface))
        .accessibilityElement(children: .combine)
    }

    private func iconName(_ status: ReminderStatus) -> String {
        if status.isLimited { return "bell.slash" }
        if status.needsRenewal { return "bell.badge" }
        if status.scheduledCount > 0 { return "bell.fill" }
        return "bell"
    }

    private func iconColor(_ status: ReminderStatus) -> UIColor {
        if status.isLimited { return .systemOrange }
        if status.needsRenewal { return .systemOrange }
        return Theme.Semantic.accent
    }

    private func primaryText(_ status: ReminderStatus) -> String {
        if status.isLimited { return Strings.statusLimited }
        if status.pendingSync { return Strings.statusPendingSync }
        if status.needsRenewal { return Strings.statusNeedsRenewal }
        if status.scheduledCount > 0 { return Strings.statusScheduled(status.scheduledCount) }
        return Strings.statusNone
    }

    // 最早触发时刻（覆盖到的时刻展示；MM-dd HH:mm 本地时区）。
    private func nextText(_ status: ReminderStatus) -> String? {
        guard !status.isLimited, let ms = status.nextFireAtMs else { return nil }
        let f = DateFormatter()
        f.dateFormat = "MM-dd HH:mm"
        return f.string(from: Date(timeIntervalSince1970: TimeInterval(ms) / 1000))
    }
}

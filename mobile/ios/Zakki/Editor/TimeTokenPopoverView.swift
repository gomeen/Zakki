import SwiftUI

// MARK: - 时间弹层（D17：点时间组件打开，编辑时刻/提醒/重复）

// 组件交互定案（规格 §4 D17）：点时间组件（任务行与非任务行同样）打开时间
// 弹层；任务只能通过勾选框完成，弹层里没有「完成」勾选操作。写回走
// ReplaceTimeToken 绑定 + 既有编辑管线（可撤销），清除时间移除令牌。
struct TimeTokenPopoverView: View {
    @ObservedObject var draft: TimeTokenDraftBox
    let onDone: () -> Void
    let onClear: () -> Void
    let onCancel: () -> Void

    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    dueSection
                    alertSection
                    recurSection
                    actions
                }
                .padding(16)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .background(Color(uiColor: Theme.Semantic.background))
            .navigationTitle(Strings.popoverTitle)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(Strings.cancel) {
                        onCancel()
                        dismiss()
                    }
                }
            }
        }
        .presentationDetents([.medium, .large])
    }

    // MARK: 时刻

    private var dueSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            sectionLabel(Strings.popoverDue)
            DatePicker(Strings.popoverDate, selection: $draft.due, displayedComponents: .date)
                .labelsHidden()
            DatePicker(Strings.popoverClock, selection: $draft.due, displayedComponents: .hourAndMinute)
                .labelsHidden()
        }
    }

    // MARK: 提醒档位（预设白名单）

    private var alertSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            sectionLabel(Strings.popoverAlert)
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 82), spacing: 8)], spacing: 8) {
                tierPill(label: Strings.alertNone, selected: !draft.hasAlert) {
                    draft.hasAlert = false
                }
                ForEach(Array(TimeTokenDraft.alertTiers.enumerated()), id: \.offset) { _, tier in
                    tierPill(label: tier.label, selected: draft.hasAlert && draft.alertMs == tier.ms) {
                        draft.hasAlert = true
                        draft.alertMs = tier.ms
                    }
                }
            }
        }
    }

    private func tierPill(label: String, selected: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(label)
                .font(.system(size: 12))
                .padding(.vertical, 5)
                .padding(.horizontal, 12)
                .frame(maxWidth: .infinity)
                .background(selected ? Color(uiColor: Theme.Semantic.accent) : Color(uiColor: Theme.Semantic.surface))
                .foregroundStyle(selected ? Color.white : Color(uiColor: Theme.Semantic.text))
                .clipShape(Capsule())
        }
        .buttonStyle(.plain)
    }

    // MARK: 重复（预设 + 自定义）

    private var recurSection: some View {
        VStack(alignment: .leading, spacing: 8) {
            sectionLabel(Strings.popoverRecur)
            Menu {
                ForEach(RecurDraft.presets, id: \.self) { preset in
                    Button(RecurDraft.label(for: preset)) {
                        draft.recur.preset = preset
                    }
                }
            } label: {
                HStack {
                    Text(RecurDraft.label(for: draft.recur.preset))
                        .font(.system(size: 13))
                    Spacer()
                    Image(systemName: "chevron.up.chevron.down")
                        .font(.system(size: 11))
                }
                .padding(.vertical, 7)
                .padding(.horizontal, 10)
                .background(Color(uiColor: Theme.Semantic.surface))
                .clipShape(RoundedRectangle(cornerRadius: 6))
            }
            if draft.recur.preset == "custom" {
                customRecur
            }
        }
    }

    private var customRecur: some View {
        VStack(alignment: .leading, spacing: 10) {
            if draft.recur.unit != "w" || draft.recur.every != 1 {
                Stepper(value: $draft.recur.every, in: 1...999) {
                    Text(Strings.recurInterval(draft.recur.every, unitLabel(draft.recur.unit)))
                        .font(.system(size: 13))
                }
            }
            unitPicker
            if draft.recur.unit == "w", draft.recur.every == 1 {
                weekdayPicker
            }
            Toggle(isOn: $draft.recur.hasEnd) {
                Text(Strings.recurHasEnd)
                    .font(.system(size: 13))
            }
            .font(.system(size: 13))
            if draft.recur.hasEnd {
                DatePicker(Strings.recurEnd, selection: $draft.recur.endDay, displayedComponents: .date)
                    .font(.system(size: 13))
            }
        }
        .padding(10)
        .background(Color(uiColor: Theme.Semantic.surface).opacity(0.5))
        .clipShape(RoundedRectangle(cornerRadius: 8))
    }

    private var unitPicker: some View {
        Picker(Strings.popoverRecur, selection: $draft.recur.unit) {
            Text(Strings.recurUnitDay).tag("d")
            Text(Strings.recurUnitWeek).tag("w")
            Text(Strings.recurUnitMonth).tag("mo")
            Text(Strings.recurUnitYear).tag("y")
        }
        .pickerStyle(.segmented)
        .font(.system(size: 12))
    }

    private var weekdayPicker: some View {
        HStack(spacing: 6) {
            ForEach(1...7, id: \.self) { day in
                let selected = draft.recur.weekdays.contains(day)
                Button {
                    if selected {
                        draft.recur.weekdays.remove(day)
                    } else {
                        draft.recur.weekdays.insert(day)
                    }
                } label: {
                    Text(Strings.weekdayShort(day))
                        .font(.system(size: 12, weight: .medium))
                        .frame(maxWidth: .infinity, minHeight: 30)
                        .background(selected ? Color(uiColor: Theme.Semantic.accent) : Color(uiColor: Theme.Semantic.surface))
                        .foregroundStyle(selected ? Color.white : Color(uiColor: Theme.Semantic.text))
                        .clipShape(RoundedRectangle(cornerRadius: 6))
                }
                .buttonStyle(.plain)
            }
        }
    }

    private func unitLabel(_ unit: String) -> String {
        switch unit {
        case "d": return Strings.recurUnitDay
        case "w": return Strings.recurUnitWeek
        case "mo": return Strings.recurUnitMonth
        default: return Strings.recurUnitYear
        }
    }

    // MARK: 底部操作（清除 / 完成）

    private var actions: some View {
        HStack(spacing: 10) {
            Button {
                onClear()
                dismiss()
            } label: {
                Text(Strings.popoverClear)
                    .font(.system(size: 13))
                    .padding(.vertical, 8)
                    .padding(.horizontal, 14)
                    .foregroundStyle(Color(uiColor: .systemRed))
                    .background(Color(uiColor: Theme.Semantic.surface))
                    .clipShape(RoundedRectangle(cornerRadius: 6))
            }
            .buttonStyle(.plain)
            Spacer()
            Button {
                onDone()
                dismiss()
            } label: {
                Text(Strings.popoverDone)
                    .font(.system(size: 13, weight: .medium))
                    .padding(.vertical, 8)
                    .padding(.horizontal, 18)
                    .foregroundStyle(Color.white)
                    .background(Color(uiColor: Theme.Semantic.accent))
                    .clipShape(RoundedRectangle(cornerRadius: 6))
            }
            .buttonStyle(.plain)
        }
    }

    private func sectionLabel(_ text: String) -> some View {
        Text(text)
            .font(.system(size: 12))
            .foregroundStyle(Color(uiColor: Theme.Semantic.textMuted))
    }
}

// 弹层草稿的可变盒（SwiftUI 绑定需要 class；字段与 TimeTokenDraft 对齐，
// 由打开方从令牌现状播种）。
@MainActor
final class TimeTokenDraftBox: ObservableObject {
    @Published var due: Date
    @Published var hasAlert: Bool
    @Published var alertMs: Int64
    @Published var recur: RecurDraft

    init(draft: TimeTokenDraft) {
        due = draft.due
        hasAlert = draft.hasAlert
        alertMs = draft.alertMs
        recur = draft.recur
    }

    // 当前草稿的快照（写回时转回值类型走 suffixJSON）。
    func draft() -> TimeTokenDraft {
        TimeTokenDraft(due: due, hasAlert: hasAlert, alertMs: alertMs, recur: recur)
    }
}

extension TimeTokenDraft {
    // 值草稿 → 弹层可变盒（UI 层调用，主线程）。
    @MainActor
    func box() -> TimeTokenDraftBox {
        TimeTokenDraftBox(draft: self)
    }
}

import SwiftUI

struct ContentView: View {
    @EnvironmentObject private var model: NotesModel
    @State private var showingPicker = false

    var body: some View {
        NavigationStack {
            mainContent
                .navigationTitle("Zakki")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .primaryAction) {
                        Button(Strings.importButton) {
                            showingPicker = true
                        }
                    }
                }
                .sheet(isPresented: $showingPicker) {
                    DocumentPicker { url in
                        model.importFile(at: url)
                    }
                }
                .alert(
                    Strings.importFailedTitle,
                    isPresented: Binding(
                        get: { model.importError != nil },
                        set: { if !$0 { model.importError = nil } }
                    )
                ) {
                    Button(Strings.ok, role: .cancel) {}
                } message: {
                    Text(model.importError ?? "")
                }
        }
    }

    @ViewBuilder
    private var mainContent: some View {
        if let snapshot = model.snapshot {
            VStack(spacing: 0) {
                if !snapshot.writable {
                    WriteProtectBanner(message: Strings.bannerMessage(for: snapshot.reason))
                }
                tabList(snapshot)
            }
        } else {
            ContentUnavailableView(
                Strings.welcomeTitle,
                systemImage: "square.and.arrow.down",
                description: Text(Strings.welcomeMessage)
            )
        }
    }

    // active 是磁盘里的当前标签下标，M1 仅做展示高亮，不做打开态记忆。
    private func tabList(_ snapshot: NotesSnapshot) -> some View {
        let tabs = snapshot.tabs ?? []
        return Group {
            if tabs.isEmpty {
                ContentUnavailableView(
                    Strings.noTabsTitle,
                    systemImage: "tray",
                    description: Text(Strings.noTabsYet)
                )
            } else {
                List {
                    ForEach(Array(tabs.enumerated()), id: \.offset) { index, tab in
                        TabRow(tab: tab, active: index == snapshot.active)
                    }
                }
                .listStyle(.plain)
            }
        }
    }
}

struct TabRow: View {
    let tab: NotesTab
    let active: Bool

    var body: some View {
        HStack(spacing: 12) {
            Circle()
                .fill(tab.displayColor)
                .frame(width: 12, height: 12)
            VStack(alignment: .leading, spacing: 2) {
                Text(tab.name.isEmpty ? Strings.untitledTab : tab.name)
                    .font(.headline)
                Text(tab.firstLine.isEmpty ? Strings.emptyPreview : tab.firstLine)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            Spacer()
            if active {
                Text(Strings.currentBadge)
                    .font(.caption2.bold())
                    .padding(.horizontal, 8)
                    .padding(.vertical, 3)
                    .background(Color.accentColor.opacity(0.15), in: Capsule())
                    .foregroundStyle(Color.accentColor)
            }
        }
        .padding(.vertical, 2)
    }
}

struct WriteProtectBanner: View {
    let message: String

    var body: some View {
        HStack(alignment: .top, spacing: 10) {
            Image(systemName: "lock.fill")
            VStack(alignment: .leading, spacing: 2) {
                Text(Strings.bannerTitle)
                    .font(.footnote.bold())
                Text(message)
                    .font(.footnote)
            }
        }
        .foregroundStyle(.white)
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(12)
        .background(Color.orange)
    }
}

extension NotesTab {
    // 正文首行：截掉换行后的第一行（可能为空，展示层给占位文案）。
    var firstLine: String {
        text.split(separator: "\n", omittingEmptySubsequences: false).first.map(String.init) ?? ""
    }

    // 桌面调色板的 #rrggbb（小写十六进制）；空串或非法值给中性占位色。
    var displayColor: Color {
        var hex = color.trimmingCharacters(in: .whitespacesAndNewlines)
        if hex.hasPrefix("#") {
            hex.removeFirst()
        }
        guard hex.count == 6, let value = UInt64(hex, radix: 16) else {
            return Color(uiColor: .systemGray)
        }
        return Color(
            red: Double((value >> 16) & 0xFF) / 255,
            green: Double((value >> 8) & 0xFF) / 255,
            blue: Double(value & 0xFF) / 255
        )
    }
}

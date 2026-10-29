import SwiftUI

struct ContentView: View {
    @EnvironmentObject private var model: NotesModel
    @State private var showingPicker = false
    // 重排模式：List 进编辑态只为拖拽排序（onMove）；导航与左滑操作在非编辑态。
    @State private var reordering = false
    // 换色面板与重命名弹窗的目标标签。
    @State private var colorTarget: NotesTab?
    @State private var renameTarget: NotesTab?
    @State private var renameText = ""

    var body: some View {
        NavigationStack {
            mainContent
                .navigationTitle("Zakki")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar { toolbarContent }
                .navigationDestination(for: UInt64.self) { tabID in
                    editorDestination(tabID: tabID)
                }
                .sheet(isPresented: $showingPicker) {
                    DocumentPicker { url in
                        model.importFile(at: url)
                    }
                }
                .sheet(item: $colorTarget) { tab in
                    TabColorPickerSheet(tab: tab)
                }
                .alert(
                    Strings.renameTabTitle,
                    isPresented: Binding(
                        get: { renameTarget != nil },
                        set: { if !$0 { renameTarget = nil } }
                    )
                ) {
                    TextField(Strings.untitledTab, text: $renameText)
                    Button(Strings.renameTab) {
                        if let target = renameTarget {
                            model.renameTab(id: target.id, name: renameText)
                        }
                        renameTarget = nil
                    }
                    Button(Strings.cancel, role: .cancel) {
                        renameTarget = nil
                    }
                } message: {
                    Text(Strings.renameTabMessage)
                }
                .alert(
                    Strings.saveFailedTitle,
                    isPresented: Binding(
                        get: { model.saveError != nil },
                        set: { if !$0 { model.clearSaveError() } }
                    )
                ) {
                    Button(Strings.ok, role: .cancel) {}
                } message: {
                    Text(model.saveError ?? "")
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

    // 导出=交出沙盒 zakki.json 的 URL（写保护文件也允许备份）；标签管理入口
    // 只在 writable 时出现（模型层同样拒绝，UI 隐藏是第一道门）。
    @ToolbarContentBuilder
    private var toolbarContent: some ToolbarContent {
        ToolbarItemGroup(placement: .primaryAction) {
            if model.snapshot != nil {
                ShareLink(item: model.fileURL) {
                    Image(systemName: "square.and.arrow.up")
                }
                .accessibilityLabel(Strings.exportFile)
            }
            if let snapshot = model.snapshot, snapshot.writable {
                Button {
                    model.addTab(name: "", color: NotesModel.unusedTabColor(for: snapshot.tabs ?? []))
                } label: {
                    Image(systemName: "plus")
                }
                .accessibilityLabel(Strings.addTab)
                .disabled((snapshot.tabs?.count ?? 0) >= 9)
            }
            Button(Strings.importButton) {
                showingPicker = true
            }
        }
        ToolbarItem(placement: .navigationBarLeading) {
            if let snapshot = model.snapshot, snapshot.writable,
               (snapshot.tabs?.count ?? 0) >= 2 {
                Button(reordering ? Strings.done : Strings.edit) {
                    reordering.toggle()
                }
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

    // active 是磁盘里的当前标签下标，M1 语义保持仅展示高亮；点行走 NavigationLink
    // 进编辑器（设备本地打开，不写 shared active）。
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
                    ForEach(Array(tabs.enumerated()), id: \.element.id) { index, tab in
                        NavigationLink(value: tab.id) {
                            TabRow(tab: tab, active: index == snapshot.active, slot: index)
                        }
                        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                            // 只剩一个标签时不给删除入口（桌面「至少保留一个」parity）。
                            if snapshot.writable, tabs.count >= 2 {
                                Button(role: .destructive) {
                                    model.deleteTab(id: tab.id)
                                } label: {
                                    Image(systemName: "trash")
                                }
                            }
                        }
                        .swipeActions(edge: .leading, allowsFullSwipe: false) {
                            if snapshot.writable {
                                Button {
                                    renameTarget = tab
                                    renameText = tab.name
                                } label: {
                                    Label(Strings.renameTab, systemImage: "pencil")
                                }
                                .tint(Color(uiColor: Theme.Semantic.accent))
                                Button {
                                    colorTarget = tab
                                } label: {
                                    Label(Strings.tabColor, systemImage: "paintpalette")
                                }
                                .tint(Color(uiColor: Theme.Semantic.accent))
                            }
                        }
                    }
                    .onMove { source, destination in
                        var ids = tabs.map(\.id)
                        ids.move(fromOffsets: source, toOffset: destination)
                        model.reorderTabs(ids: ids)
                    }
                }
                .listStyle(.plain)
                .scrollContentBackground(.hidden)
                .background(Color(uiColor: Theme.Semantic.background))
                .environment(\.editMode, .constant(reordering ? .active : .inactive))
            }
        }
    }

    // 编辑器目的地：按 id 取最新快照中的标签（写回后正文随之更新）。
    @ViewBuilder
    private func editorDestination(tabID: UInt64) -> some View {
        if let snapshot = model.snapshot, let tab = snapshot.tabs?.first(where: { $0.id == tabID }) {
            EditorHostView(tab: tab, writable: snapshot.writable, reason: snapshot.reason)
                .navigationTitle(tab.name.isEmpty ? Strings.untitledTab : tab.name)
                .navigationBarTitleDisplayMode(.inline)
        } else {
            ContentUnavailableView(Strings.noTabsTitle, systemImage: "tray")
        }
    }
}

extension NotesTab: Identifiable {}

struct TabRow: View {
    let tab: NotesTab
    let active: Bool
    let slot: Int

    // 「当前」徽标 = 标签胶囊：底色/文字色按桌面收敛规则派生
    //（目标 5:1、底线 4.5:1、向黑白混合 ≤12%），浅深外观同一套（同桌面）。
    private var pill: TabPill.Palette {
        let base = ThemeTagPalette.parse(tab.color) ?? ThemeTagPalette.color(at: slot)
        return TabPill.palette(base: base)
    }

    var body: some View {
        HStack(spacing: 12) {
            Circle()
                .fill(tab.displayColor(fallbackSlot: slot))
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
                    .background(Color(uiColor: pill.background.uiColor), in: Capsule())
                    .foregroundStyle(Color(uiColor: pill.text.uiColor))
            }
        }
        .padding(.vertical, 2)
    }
}

// 换色面板：桌面 12 色板（M2-3 Theme），选中勾的显色消费胶囊收敛色保证对比。
struct TabColorPickerSheet: View {
    let tab: NotesTab
    @EnvironmentObject private var model: NotesModel
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 48), spacing: 20)], spacing: 20) {
                ForEach(Array(ThemeTagPalette.colors.enumerated()), id: \.offset) { _, hex in
                    swatch(hex)
                }
            }
            .padding(24)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
            .background(Color(uiColor: Theme.Semantic.background))
            .navigationTitle(Strings.tabColor)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button(Strings.editorDone) { dismiss() }
                }
            }
        }
        .presentationDetents([.medium])
    }

    private func swatch(_ hex: String) -> some View {
        let selected = tab.color.caseInsensitiveCompare(hex) == .orderedSame
        let base = ThemeTagPalette.parse(hex)
        return Button {
            // 落盘成功才收起面板；失败由模型层上报，面板留在原地可重试。
            if model.setTabColor(id: tab.id, color: hex) {
                dismiss()
            }
        } label: {
            ZStack {
                Circle()
                    .fill(Color(uiColor: base?.uiColor ?? .clear))
                if selected {
                    let pill = TabPill.palette(base: base ?? ThemeRGB(0, 0, 0))
                    Image(systemName: "checkmark")
                        .font(.system(size: 15, weight: .bold))
                        .foregroundStyle(Color(uiColor: pill.text.uiColor))
                }
            }
            .frame(width: 44, height: 44)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(Strings.tabColor)
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

    // 桌面 tabColor：记录颜色优先（#rrggbb），无效/缺失按槽位回退内置调色板。
    func displayColor(fallbackSlot: Int) -> Color {
        if let rgb = ThemeTagPalette.parse(color) {
            return Color(uiColor: rgb.uiColor)
        }
        return Color(uiColor: ThemeTagPalette.color(at: fallbackSlot).uiColor)
    }
}

import SwiftUI

// MARK: - 设置页：同步节（后端选择 / 连接 / 断开 / 状态 / 冲突留档管理）

struct SettingsView: View {
    @EnvironmentObject private var sync: SyncEngine
    @Environment(\.dismiss) private var dismiss
    @State private var clientIDText = ""
    @State private var ownerText = ""
    @State private var repoText = ""
    @State private var isSyncingNow = false

    var body: some View {
        Form {
            backendSection
            if sync.selectedBackend == .github {
                githubSection
            }
            archiveSection
        }
        .navigationTitle(Strings.settingsTitle)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button(Strings.editorDone) { dismiss() }
            }
        }
        .onAppear {
            clientIDText = sync.clientID
            ownerText = sync.repoOwner
            repoText = sync.repoName
        }
        .sheet(item: Binding(
            get: { sync.deviceFlow },
            set: { if $0 == nil { sync.cancelDeviceFlow() } }
        )) { flow in
            DeviceFlowSheet(presentation: flow)
        }
    }

    // 后端选择：GitHub 可用；云盘（后端 B，#53）占位灰显——协议抽象为 B 留位。
    private var backendSection: some View {
        Section(Strings.syncBackendSection) {
            row(SyncBackendKind.github)
            row(SyncBackendKind.cloudFolder)
        }
    }

    private func row(_ kind: SyncBackendKind) -> some View {
        Button {
            if kind == .github {
                sync.selectBackend(kind)
            }
        } label: {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(kind == .github ? Strings.syncBackendGitHub : Strings.syncBackendCloudFolder)
                        .foregroundStyle(kind == .github ? Color.primary : Color.secondary)
                    if kind == .cloudFolder {
                        Text(Strings.syncBackendCloudFolderHint)
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
                Spacer()
                if sync.selectedBackend == kind {
                    Image(systemName: "checkmark")
                        .foregroundStyle(Color.accentColor)
                }
            }
        }
        .disabled(kind != .github)
    }

    @ViewBuilder
    private var githubSection: some View {
        Section {
            TextField(Strings.syncFieldClientID, text: $clientIDText)
            TextField(Strings.syncFieldOwner, text: $ownerText)
            TextField(Strings.syncFieldRepo, text: $repoText)
            if sync.isConnected {
                connectionRows
            } else {
                connectRows
            }
        } header: {
            Text(Strings.syncGitHubSection)
        } footer: {
            Text(Strings.syncHelperText)
        }
    }

    // 已连接：状态 + 立即同步 + 断开。
    private var connectionRows: some View {
        Group {
            LabeledContent {
                Text(Strings.syncStateConnected)
            } label: {
                Text("\(sync.repoOwner)/\(sync.repoName)")
            }
            LabeledContent {
                Text(Strings.syncOutcomeText(sync.lastSummary?.outcome))
                    .foregroundStyle(sync.lastSummary?.outcome.isFailure == true ? Color.red : Color.primary)
            } label: {
                Text(Strings.syncLastSync)
            }
            Button {
                guard !isSyncingNow else { return }
                isSyncingNow = true
                Task {
                    await sync.syncNow()
                    isSyncingNow = false
                }
            } label: {
                if isSyncingNow {
                    HStack { ProgressView(); Text(Strings.syncSyncing) }
                } else {
                    Text(Strings.syncSyncNow)
                }
            }
            Button(Strings.syncDisconnect, role: .destructive) {
                sync.disconnect()
            }
        }
    }

    // 未连接：配置 + 连接入口。
    private var connectRows: some View {
        Group {
            Button(Strings.syncConnect) {
                sync.clientID = clientIDText
                sync.repoOwner = ownerText
                sync.repoName = repoText
                Task { await sync.startDeviceFlow() }
            }
            .disabled(clientIDText.isEmpty || ownerText.isEmpty || repoText.isEmpty)
            if !sync.isGitHubConfigured {
                Text(Strings.syncConfigIncomplete)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
    }

    private var archiveSection: some View {
        Section(Strings.archiveSectionTitle) {
            let entries = sync.archiveEntries
            if entries.isEmpty {
                Text(Strings.archiveEmpty)
                    .foregroundStyle(.secondary)
            } else {
                NavigationLink(Strings.archiveOpen(count: entries.count)) {
                    ConflictArchiveListView()
                }
            }
        }
    }
}

// MARK: - 冲突对话框宿主（D13：保留本机/保留远端）

// 挂在 ContentView 外层的全屏遮罩卡片：不用系统 alert——同一视图树里 alert 与
// 设置 sheet 的 presentation 相互竞争（alert 会挤掉 sheet 且不可复原），overlay
// 无 presentation 语义，覆盖在任意界面（设置页/列表页/编辑器）之上且不干扰。
struct ConflictAlertModifier: ViewModifier {
    // 修饰器位于 environmentObject 注入点外侧，读不到注入值，由装配层显式传入；
    // @ObservedObject 订阅引擎变化，pendingConflict 置位即重渲染出遮罩卡片。
    @ObservedObject var sync: SyncEngine

    func body(content: Content) -> some View {
        content.overlay {
            if let conflict = sync.pendingConflict {
                ConflictResolutionCard(candidate: conflict, sync: sync)
                    .zIndex(10)
                    .transaction { $0.disablesAnimations = true }
            }
        }
    }
}

// 冲突二选一卡片：视觉对齐系统 alert（居中圆角卡 + 按钮列），保留本机 /
// 保留远端 二选一，稍后处理关闭（下一轮同步重新判定再弹，远端可能已变）。
struct ConflictResolutionCard: View {
    let candidate: ConflictCandidate
    let sync: SyncEngine

    var body: some View {
        ZStack {
            Color.black.opacity(0.35)
                .ignoresSafeArea()
                .accessibilityHidden(true)
                .onTapGesture { sync.dismissConflict() }
            VStack(spacing: 14) {
                Text(Strings.syncConflictTitle)
                    .font(.headline)
                Text(Strings.syncConflictMessage)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
                VStack(spacing: 10) {
                    Button(Strings.syncConflictKeepLocal) {
                        Task { await sync.resolveConflict(keepLocal: true) }
                    }
                    .buttonStyle(.borderedProminent)
                    Button(Strings.syncConflictKeepRemote) {
                        Task { await sync.resolveConflict(keepLocal: false) }
                    }
                    .buttonStyle(.bordered)
                    Button(Strings.syncConflictLater, role: .cancel) {
                        sync.dismissConflict()
                    }
                }
            }
            .padding(20)
            .frame(maxWidth: 300)
            .background(
                RoundedRectangle(cornerRadius: 14)
                    .fill(Color(uiColor: .systemBackground))
                    .shadow(radius: 20, y: 4)
            )
            .padding(32)
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier("conflict-card")
        }
    }
}

// MARK: - Device Flow 弹层

struct DeviceFlowSheet: View {
    let presentation: DeviceFlowPresentation
    @EnvironmentObject private var sync: SyncEngine
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(spacing: 20) {
            switch presentation.phase {
            case .waiting:
                Text(Strings.deviceFlowUserCodeHint)
                    .font(.headline)
                    .multilineTextAlignment(.center)
                Text(presentation.userCode)
                    .font(.system(size: 34, weight: .bold, design: .monospaced))
                    .padding(14)
                    .background(Color(uiColor: Theme.Semantic.background))
                    .clipShape(RoundedRectangle(cornerRadius: 12))
                    .textSelection(.enabled)
                Link(Strings.deviceFlowOpen, destination: presentation.verificationURL)
                ProgressView()
                Text(Strings.deviceFlowWaiting)
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            case .connected:
                Image(systemName: "checkmark.circle.fill")
                    .font(.system(size: 48))
                    .foregroundStyle(Color.green)
                Text(Strings.deviceFlowConnected)
                    .font(.headline)
                Button(Strings.deviceFlowDone) {
                    dismiss()
                }
                .buttonStyle(.borderedProminent)
            case .failed(let code):
                Image(systemName: "xmark.octagon.fill")
                    .font(.system(size: 48))
                    .foregroundStyle(Color.red)
                Text(Strings.deviceFlowFailed(code))
                    .font(.headline)
                    .multilineTextAlignment(.center)
                Button(Strings.deviceFlowCancel) {
                    dismiss()
                }
            }
        }
        .padding(28)
        .presentationDetents([.medium])
    }
}

// MARK: - 冲突留档列表与查看

struct ConflictArchiveListView: View {
    @EnvironmentObject private var sync: SyncEngine
    @State private var viewer: ConflictArchiveEntry?

    var body: some View {
        List {
            let entries = sync.archiveEntries
            if entries.isEmpty {
                Text(Strings.archiveEmpty)
                    .foregroundStyle(.secondary)
            }
            ForEach(entries) { entry in
                row(entry)
            }
        }
        .navigationTitle(Strings.archiveSectionTitle)
        .navigationBarTitleDisplayMode(.inline)
        .sheet(item: $viewer) { entry in
            ConflictArchiveViewer(entry: entry)
        }
    }

    private func row(_ entry: ConflictArchiveEntry) -> some View {
        Button {
            viewer = entry
        } label: {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(entry.fileName)
                        .font(.footnote.monospaced())
                        .lineLimit(1)
                    Text(Strings.archiveEntryDetail(entry))
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                if sync.pendingArchiveNames.contains(entry.fileName) {
                    Text(Strings.archivePendingBadge)
                        .font(.caption2)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(Color.orange.opacity(0.2), in: Capsule())
                }
            }
        }
        .swipeActions(edge: .trailing) {
            Button(role: .destructive) {
                sync.deleteArchive(entry)
            } label: {
                Label(Strings.archiveDelete, systemImage: "trash")
            }
        }
    }
}

// 留档全文查看（只读；不解析内容，按原始字节展示——留档即落选版本的完整拷贝）。
struct ConflictArchiveViewer: View {
    let entry: ConflictArchiveEntry
    @EnvironmentObject private var sync: SyncEngine
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            ScrollView {
                Text(sync.archiveContent(of: entry) ?? "")
                    .font(.system(size: 12, design: .monospaced))
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(16)
                    .textSelection(.enabled)
            }
            .navigationTitle(Strings.archiveViewerTitle)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button(Strings.editorDone) { dismiss() }
                }
            }
        }
    }
}

// MARK: - Strings 之外的展示辅助

extension SyncOutcome {
    var isFailure: Bool {
        switch self {
        case .failed, .blockedWriteProtected: return true
        default: return false
        }
    }
}

extension SyncEngine {
    // 留档 pending 徽标的数据源（设置页）。
    var pendingArchiveNames: Set<String> {
        // pending 集合在引擎内部；经公开计算属性桥出，避免暴露可变状态。
        pendingArchiveFileNames
    }
}

import SwiftUI

// MARK: - 设置页：同步节（后端选择 / 连接 / 断开 / 状态 / 冲突留档管理）

struct SettingsView: View {
    @EnvironmentObject private var sync: SyncEngine
    @Environment(\.dismiss) private var dismiss
    @State private var clientIDText = ""
    @State private var ownerText = ""
    @State private var repoText = ""
    @State private var isSyncingNow = false
    // 后端 B（#53）：文件选择器弹层开关（文件夹/文件两种授权形态各一个 sheet）。
    @State private var pickingCloudFolder = false
    @State private var pickingCloudFile = false

    var body: some View {
        Form {
            backendSection
            if sync.selectedBackend == .github {
                githubSection
            } else {
                cloudConnectSection
                if sync.isCloudFolderConnected, sync.cloudScope == .folder {
                    cloudCopiesSection
                }
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
        .sheet(isPresented: $pickingCloudFolder) {
            SecurityScopedPicker(contentTypes: [.folder]) { url in
                Task { await sync.connectCloudFolder(at: url) }
            }
        }
        .sheet(isPresented: $pickingCloudFile) {
            SecurityScopedPicker(contentTypes: [.json]) { url in
                Task { await sync.connectCloudFolder(at: url) }
            }
        }
        .sheet(item: Binding(
            get: { sync.deviceFlow },
            set: { if $0 == nil { sync.cancelDeviceFlow() } }
        )) { flow in
            DeviceFlowSheet(presentation: flow)
        }
    }

    // 后端选择：任一时刻只激活一个（§2.2）；切换即清基线重新按 D13 加入。
    private var backendSection: some View {
        Section(Strings.syncBackendSection) {
            row(SyncBackendKind.github)
            row(SyncBackendKind.cloudFolder)
        }
    }

    private func row(_ kind: SyncBackendKind) -> some View {
        Button {
            sync.selectBackend(kind)
        } label: {
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(kind == .github ? Strings.syncBackendGitHub : Strings.syncBackendCloudFolder)
                        .foregroundStyle(Color.primary)
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
        // Form 内默认 borderless 按钮样式会把整个标签染成 accent 色（蓝色链接观感）；
        // plain 保持内容原色，选中态由勾号表达。
        .buttonStyle(.plain)
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
            syncNowRow
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

    // MARK: 后端 B（云盘文件夹）：授权 / 状态 / 失效重选（探针 §5 错误形态）

    // 书签失效类错误码：目标被删/挪走（bookmark-invalid）或授权被系统撤销（access-denied）。
    // 任一出现都引导重新选位置，不自动重试（书签无复用价值）。
    private var isBookmarkInvalidated: Bool {
        switch sync.lastSummary?.outcome {
        case .failed(let code): return code == "bookmark-invalid" || code == "access-denied"
        default: return false
        }
    }

    @ViewBuilder
    private var cloudConnectSection: some View {
        Section {
            if sync.isCloudFolderConnected {
                LabeledContent {
                    Text(sync.cloudScope == .folder ? Strings.cloudScopeFolder : Strings.cloudScopeFile)
                } label: {
                    Text(sync.cloudTargetName)
                }
                LabeledContent {
                    Text(Strings.syncOutcomeText(sync.lastSummary?.outcome))
                        .foregroundStyle(isSyncFailure ? Color.red : Color.primary)
                } label: {
                    Text(Strings.syncLastSync)
                }
                syncNowRow
                if isBookmarkInvalidated {
                    Text(Strings.cloudBookmarkInvalidHint)
                        .font(.footnote)
                        .foregroundStyle(.red)
                    Button(Strings.cloudReselect) {
                        if sync.cloudScope == .folder {
                            pickingCloudFolder = true
                        } else {
                            pickingCloudFile = true
                        }
                    }
                }
                if sync.cloudScope == .file {
                    Text(Strings.cloudArchiveLocalHint)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                Button(Strings.syncDisconnect, role: .destructive) {
                    sync.disconnect()
                }
            } else {
                cloudPickButtons
            }
        } header: {
            Text(Strings.syncCloudSection)
        } footer: {
            if !sync.isCloudFolderConnected {
                Text(Strings.cloudGuideFooter)
            }
        }
    }

    private var cloudPickButtons: some View {
        Group {
            Button {
                pickingCloudFolder = true
            } label: {
                Label(Strings.cloudPickFolder, systemImage: "folder")
            }
            Button {
                pickingCloudFile = true
            } label: {
                Label(Strings.cloudPickFile, systemImage: "doc")
            }
        }
    }

    private var isSyncFailure: Bool {
        sync.lastSummary?.outcome.isFailure == true
    }

    // 立即同步（两种后端共用；弹冲突卡片时设置页由 ContentView 收起）。
    private var syncNowRow: some View {
        Button {
            guard !isSyncingNow else { return }
            isSyncingNow = true
            Task {
                await sync.syncNow()
                if sync.selectedBackend == .cloudFolder {
                    await sync.refreshCloudCopies()
                }
                isSyncingNow = false
            }
        } label: {
            if isSyncingNow {
                HStack { ProgressView(); Text(Strings.syncSyncing) }
            } else {
                Text(Strings.syncSyncNow)
            }
        }
    }

    // 云端冲突副本（双通道枚举）：仅文件夹授权可用（仅文件授权列不了父目录）。
    private var cloudCopiesSection: some View {
        Section(Strings.cloudCopySection) {
            if let error = sync.cloudCopiesError {
                Text(Strings.cloudCopiesFailedText(error))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            if sync.cloudCopies.isEmpty {
                Text(Strings.cloudCopyEmpty)
                    .foregroundStyle(.secondary)
            } else {
                NavigationLink(Strings.cloudCopiesOpen(count: sync.cloudCopies.count)) {
                    CloudCopiesListView()
                }
            }
        }
        .onAppear {
            Task { await sync.refreshCloudCopies() }
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

// MARK: - 云端冲突副本列表与查看（双通道枚举结果；采用走同一 D13 对话框）

struct CloudCopiesListView: View {
    @EnvironmentObject private var sync: SyncEngine
    @State private var viewing: CloudConflictEnumerator.Copy?

    var body: some View {
        List {
            if sync.cloudCopies.isEmpty {
                Text(Strings.cloudCopyEmpty)
                    .foregroundStyle(.secondary)
            }
            ForEach(sync.cloudCopies) { copy in
                row(copy)
            }
        }
        .navigationTitle(Strings.cloudCopySection)
        .navigationBarTitleDisplayMode(.inline)
        .onAppear {
            Task { await sync.refreshCloudCopies() }
        }
        .sheet(item: $viewing) { copy in
            CloudCopyViewer(copy: copy)
        }
    }

    private func row(_ copy: CloudConflictEnumerator.Copy) -> some View {
        Button {
            viewing = copy
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text(copy.fileName)
                    .font(.footnote.monospaced())
                    .lineLimit(1)
                Text(Self.detail(copy))
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
        }
    }

    private static func detail(_ copy: CloudConflictEnumerator.Copy) -> String {
        let source = copy.source == .namedCopy ? Strings.cloudCopySourceNamed : Strings.cloudCopySourceVersion
        return "\(source) · \(dateText(copy.modifiedAtMs))"
    }

    private static let detailDateFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd HH:mm"
        return f
    }()

    private static func dateText(_ ms: Int64) -> String {
        guard ms > 0 else { return "--" }
        return detailDateFormatter.string(from: Date(timeIntervalSince1970: Double(ms) / 1000))
    }
}

// 副本全文查看：只读展示 + 采用入口；采用后关闭自身，冲突卡片在根层弹出
//（ContentView onChange 收起设置 sheet，与同步判定冲突同一通路）。
struct CloudCopyViewer: View {
    let copy: CloudConflictEnumerator.Copy
    @EnvironmentObject private var sync: SyncEngine
    @Environment(\.dismiss) private var dismiss
    @State private var isAdopting = false

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    Text(Strings.cloudCopyAdoptHint)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    Text(copy.content)
                        .font(.system(size: 12, design: .monospaced))
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .textSelection(.enabled)
                }
                .padding(16)
            }
            .navigationTitle(Strings.cloudCopyViewerTitle)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(Strings.editorDone) { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button {
                        guard !isAdopting else { return }
                        isAdopting = true
                        Task {
                            dismiss()
                            await sync.adoptCloudCopy(copy)
                        }
                    } label: {
                        Text(Strings.cloudCopyAdopt)
                    }
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

import Combine
import Foundation
import SwiftUI
import UIKit

// MARK: - 同步引擎（§2.2：前台/聚焦拉推；每轮先 pull → 处理本地待写与冲突 → 再 commit）

// 串行编排（D12）：同一时刻至多一轮在跑；本地落盘与上传不混跑两条提交链。
// 基线与摘要存设备本地独立文件（Application Support/zakki-sync-state.json），
// 不写进 zakki.json、偏好文件或 Git；凭据只进 Keychain（R7）。
@MainActor
final class SyncEngine: ObservableObject {
    // MARK: 装配

    private weak var model: NotesModel?
    private let tokens: TokenStoring
    private let defaults: UserDefaults
    private let stateURL: URL
    private let archiveStore: ConflictArchiveStore
    private let authService: GitHubAuthService
    private let clock: () -> Int64
    private let transport: GitHubTransporting
    private let githubBase: URL
    private let apiBase: URL
    // 成功应用远端后的串联点：M3-1 对账管线（§2.2）。测试注入 spy。
    var onRemoteAdopted: (() -> Void)?
    // 测试注入后端（生产路径走 makeBackend 构建 GitHub 后端）。
    var testBackend: SyncBackend?

    // MARK: 状态

    @Published private(set) var lastSummary: SyncSummary?
    @Published var pendingConflict: ConflictCandidate?
    @Published private(set) var deviceFlow: DeviceFlowPresentation?
    @Published private(set) var isSyncing = false
    @Published private(set) var selectedBackend: SyncBackendKind
    // 后端 B（#53）：云端冲突副本列表（双通道枚举）与枚举错误码；连接/副本方法在同文件下方。
    @Published private(set) var cloudCopies: [CloudConflictEnumerator.Copy] = []
    @Published private(set) var cloudCopiesError: String?
    private var state = SyncPersistedState(baseline: nil)
    private var roundTask: Task<Void, Never>?
    private var foregroundObserver: NSObjectProtocol?

    // MARK: 配置（非凭据项，进 UserDefaults；token 永不进来，R7）

    private enum ConfigKey {
        static let backend = "sync.backend"
        static let clientID = "sync.github.client-id"
        static let owner = "sync.github.owner"
        static let repo = "sync.github.repo"
        // 后端 B（#53）：目标展示名与范围（非敏感；书签本体在 Keychain）。
        static let cloudName = "sync.cloud.name"
        static let cloudScope = "sync.cloud.scope"
    }

    var backendSelection: SyncBackendKind {
        get { selectedBackend }
        set { selectBackend(newValue) }
    }

    // 后端切换：任一时刻只激活一个（§2.2）；同时换基线体系——A 的 blob sha 与 B 的
    // 身份+修改时间不可互比，保留旧基线会让首轮误判。清空后按 D13「无基线」重新加入。
    func selectBackend(_ kind: SyncBackendKind) {
        guard kind != selectedBackend else { return }
        selectedBackend = kind
        defaults.set(kind.rawValue, forKey: ConfigKey.backend)
        state.baseline = nil
        saveState()
        objectWillChange.send()
    }

    var clientID: String {
        get { defaults.string(forKey: ConfigKey.clientID) ?? "" }
        set { defaults.set(newValue.trimmingCharacters(in: .whitespaces), forKey: ConfigKey.clientID) }
    }

    var repoOwner: String {
        get { defaults.string(forKey: ConfigKey.owner) ?? "" }
        set { defaults.set(newValue.trimmingCharacters(in: .whitespaces), forKey: ConfigKey.owner) }
    }

    var repoName: String {
        get { defaults.string(forKey: ConfigKey.repo) ?? "" }
        set { defaults.set(newValue.trimmingCharacters(in: .whitespaces), forKey: ConfigKey.repo) }
    }

    var isGitHubConfigured: Bool {
        !clientID.isEmpty && !repoOwner.isEmpty && !repoName.isEmpty
    }

    // 连接态：按当前后端判定。A=配置齐全且凭据在 Keychain；B=书签已持久化
    //（书签是否可解析由每轮同步实际验证，失效如实报错并引导重选）。
    var isConnected: Bool {
        switch backendSelection {
        case .github:
            return isGitHubConfigured && authService.hasToken
        case .cloudFolder:
            return cloudBookmarkData != nil
        }
    }

    // MARK: 初始化（全部依赖可注入；绝不触真实用户目录/真实 Keychain 之外的路径由调用方保证）

    init(tokens: TokenStoring = KeychainStore(),
         transport: GitHubTransporting = URLSessionTransport(),
         defaults: UserDefaults = .standard,
         stateURL: URL? = nil,
         archiveDir: URL? = nil,
         githubBase: URL = URL(string: "https://github.com")!,
         apiBase: URL = URL(string: "https://api.github.com")!,
         clock: @escaping () -> Int64 = { Int64(Date().timeIntervalSince1970 * 1000) },
         deviceName: (() -> String)? = nil)
    {
        self.tokens = tokens
        self.transport = transport
        self.defaults = defaults
        self.stateURL = stateURL ?? SyncEngine.defaultStateURL
        self.archiveStore = ConflictArchiveStore(
            dir: archiveDir ?? ConflictArchiveStore.directory(forZakkiURL: NotesModel.defaultSandboxURL),
            clock: clock,
            deviceName: deviceName ?? { UIDevice.current.name })
        self.clock = clock
        self.githubBase = githubBase
        self.apiBase = apiBase
        self.selectedBackend = SyncBackendKind(rawValue: defaults.string(forKey: ConfigKey.backend) ?? "") ?? .github
        let auth = GitHubAuthService(transport: transport, tokens: tokens, base: githubBase)
        self.authService = auth
        // 全部存储属性就绪后才允许逃逸闭包捕获 self。
        auth.clientIDProvider = { [weak self] in
            let id = self?.clientID
            return (id?.isEmpty ?? true) ? nil : id
        }
        loadState()
    }

    static var defaultStateURL: URL {
        let dir = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        return dir.appendingPathComponent("zakki-sync-state.json", isDirectory: false)
    }

    // App 装配入口：生产接真实 GitHub/云盘；-ZakkiSyncStub 启动参数下接进程内 stub
    //（内存凭据 + 注入传输 + 独立 suite），端到端实测不碰真实网络与真实 Keychain。
    // -ZakkiCloudGrant <path>（仅 stub 模式生效）：预授权文件夹/文件，供模拟器端到端
    // 实测后端 B——真实授权走文件选择器（跨进程 picker 无法自动化，探针 §4）。
    static func forLaunch() -> SyncEngine {
        guard SyncStubMode.isActive else { return SyncEngine() }
        let defaults = UserDefaults(suiteName: SyncStubMode.defaultsSuite) ?? .standard
        if defaults.string(forKey: "sync.github.client-id") == nil {
            defaults.set("stub-client-id", forKey: "sync.github.client-id")
            defaults.set("stub-owner", forKey: "sync.github.owner")
            defaults.set("stub-repo", forKey: "sync.github.repo")
        }
        // 每次启动重置 stub 世界与本地同步痕迹：端到端脚本从全新状态开始。
        SyncStubState.shared.reset(remoteContent: nil)
        try? FileManager.default.removeItem(at: defaultStateURL)
        let conflictsDir = ConflictArchiveStore.directory(forZakkiURL: NotesModel.defaultSandboxURL)
        try? FileManager.default.removeItem(at: conflictsDir)
        let tokens = InMemoryTokenStore()
        // 后端 B 预授权（stub 专用）：App 容器内临时目录建书签，机制与真实 provider
        // 一致（书签/会话/读写），模拟器无 iCloud 登录也不碰真实 iCloud（探针 §1）。
        SyncStubCloudSeed.apply(defaults: defaults, tokens: tokens)
        return SyncEngine(
            tokens: tokens,
            transport: URLSessionTransport(session: SyncStubMode.stubbedSession()),
            defaults: defaults,
            githubBase: SyncStubMode.githubBase,
            apiBase: SyncStubMode.apiBase)
    }

    // 接入模型与前台时机（幂等）；回前台立即拉推（§2.2 同步时机）。
    func attach(model: NotesModel) {
        self.model = model
        guard foregroundObserver == nil else { return }
        foregroundObserver = NotificationCenter.default.addObserver(
            forName: UIApplication.willEnterForegroundNotification, object: nil, queue: .main
        ) { [weak self] _ in
            Task { @MainActor in await self?.syncNow() }
        }
    }

    // MARK: 每轮同步（先 pull、处理本地待保存与冲突、再 commit；CAS 失败重拉重判定）

    func syncNow() async {
        guard let model else { return }
        // 串行：已有轮次在进行时忽略并发触发（D12：不混跑两条提交链）。
        guard !isSyncing else { return }
        guard isConnected, let backend = makeBackend() else {
            publish(.notConnected)
            return
        }
        isSyncing = true
        defer { isSyncing = false }
        // 1. 先冲刷本地防抖待写：判定基于落盘后的字节（「无未保存修改」由此保证）。
        model.flushPendingSave()
        // 2. 写保护拦截（§2.1：禁止上传与自动覆盖）。
        guard let snapshot = model.snapshot, snapshot.writable, let localRaw = model.raw else {
            publish(.blockedWriteProtected)
            return
        }
        // 3. CAS 失败重新 pull 重判定，不盲重试旧 sha；上限 3 次，仍冲突报远端持续变化。
        for _ in 0..<3 {
            do {
                let remote = try await backend.pullMain()
                let decision = ConflictDecision.evaluate(
                    ConflictDecisionInput(baseline: state.baseline, localRaw: localRaw,
                                          localWritable: true, remote: remote),
                    sha256Hex: SyncHash.sha256Hex)
                switch decision {
                case .none:
                    publish(.idle)
                    await uploadPendingArchives(backend: backend)
                    return
                case .adoptRemote:
                    guard let remoteContent = remote.content, let version = remote.version else {
                        publish(.failed("remote-shape"))
                        return
                    }
                    installRemote(kind: backend.kind, content: remoteContent, version: version)
                    publish(.adoptedRemote)
                    await uploadPendingArchives(backend: backend)
                    return
                case .pushLocal:
                    let newVersion = try await backend.pushMain(content: localRaw, expectedVersion: remote.version)
                    state.baseline = SyncBaseline(backendKind: backend.kind.rawValue,
                                                  contentSHA256: SyncHash.sha256Hex(localRaw),
                                                  remoteVersion: newVersion)
                    saveState()
                    publish(.pushedLocal)
                    await uploadPendingArchives(backend: backend)
                    return
                case .baselineOnly:
                    state.baseline = SyncBaseline(backendKind: backend.kind.rawValue,
                                                  contentSHA256: SyncHash.sha256Hex(localRaw),
                                                  remoteVersion: remote.version)
                    saveState()
                    publish(.baselineUpdated)
                    await uploadPendingArchives(backend: backend)
                    return
                case .conflict:
                    // 冲突按整个文件处理：暂存双方内容弹对话框，等待用户二选一。
                    guard let remoteContent = remote.content, let version = remote.version else {
                        publish(.failed("remote-shape"))
                        return
                    }
                    pendingConflict = ConflictCandidate(remoteContent: remoteContent,
                                                        remoteVersion: version, localRaw: localRaw)
                    publish(.conflictPending)
                    return
                case .blockedWriteProtected:
                    publish(.blockedWriteProtected)
                    return
                }
            } catch SyncError.casConflict {
                continue // 重拉重判定
            } catch SyncError.needsReauth {
                // 刷新失败＝过期/被撤销：凭据已清，设置页回到未连接态。
                publish(.failed("needs-reauth"))
                return
            } catch SyncError.notConfigured {
                publish(.notConnected)
                return
            } catch {
                publish(.failed(Self.errorCode(error)))
                return
            }
        }
        publish(.failed("cas-contention"))
    }

    // MARK: 冲突裁决（D13：保留本机或保留远端；落选版本完整留档）

    func resolveConflict(keepLocal: Bool) async {
        guard let candidate = pendingConflict, let model else {
            pendingConflict = nil
            return
        }
        pendingConflict = nil
        guard let backend = makeBackend() else {
            publish(.notConnected)
            return
        }
        model.flushPendingSave()
        guard model.snapshot?.writable == true, let localRaw = model.raw else {
            publish(.blockedWriteProtected)
            return
        }
        if keepLocal {
            // 远端落选：完整留档远端内容后上传本地（CAS 以主文件当前版本为基准；
            // 后端 B 采用副本候选时同样是主文件版本，不是副本自身的版本号）。
            guard archiveLoser(candidate.remoteContent) else { return }
            do {
                let newVersion = try await backend.pushMain(content: localRaw, expectedVersion: candidate.casBaseVersion)
                state.baseline = SyncBaseline(backendKind: backend.kind.rawValue,
                                              contentSHA256: SyncHash.sha256Hex(localRaw),
                                              remoteVersion: newVersion)
                saveState()
                publish(.pushedLocal)
            } catch SyncError.casConflict {
                // 远端期间又前进：远端留档已保留，基线不动，下一轮重新判定。
                publish(.failed("cas-contention"))
            } catch {
                publish(.failed(Self.errorCode(error)))
            }
        } else {
            // 本地落选：完整留档本地内容，再安装远端（active 随远端字节原样往返）。
            guard archiveLoser(localRaw) else { return }
            var adoptedVersion = candidate.casBaseVersion ?? candidate.remoteVersion
            do {
                if candidate.remoteVersion != candidate.casBaseVersion {
                    // 后端 B 副本候选：候选内容尚不在主文件里，先写回主文件再安装本地
                    //（后端 A 的候选即主文件内容，版本号相等，跳过这次无意义写回）。
                    // 若写回 CAS 失败：本地留档已保留、基线未动，下一轮重新判定。
                    adoptedVersion = try await backend.pushMain(
                        content: candidate.remoteContent, expectedVersion: candidate.casBaseVersion)
                }
                installRemote(kind: backend.kind, content: candidate.remoteContent, version: adoptedVersion)
                publish(.adoptedRemote)
            } catch SyncError.casConflict {
                publish(.failed("cas-contention"))
            } catch {
                publish(.failed(Self.errorCode(error)))
            }
        }
        await uploadPendingArchives(backend: backend)
        // 后端 B：裁决后尽力清除 NSFileVersion 冲突标记并刷新副本列表。
        await finishCloudConflictResolution()
    }

    // 用户取消对话框：本轮以冲突待决收场，下次前台同步重新判定再弹（远端可能已变）。
    func dismissConflict() {
        pendingConflict = nil
    }

    // 落选内容写 conflicts/ 并登记待上传；失败如实上报（引擎层不吞）。
    private func archiveLoser(_ content: String) -> Bool {
        do {
            let name = try archiveStore.write(content: content)
            if !state.pendingArchives.contains(name) {
                state.pendingArchives.append(name)
            }
            saveState()
            return true
        } catch {
            publish(.failed("archive-io"))
            return false
        }
    }

    // MARK: 留档随同步一起保存（失败保持 pending，下轮重试；已存在于远端视为已保存）

    private func uploadPendingArchives(backend: SyncBackend) async {
        guard !state.pendingArchives.isEmpty else { return }
        var confirmed: [String] = []
        for name in state.pendingArchives {
            guard let content = archiveStore.content(ofName: name) else {
                confirmed.append(name) // 本地文件已被用户删除：登记项随之移除
                continue
            }
            do {
                try await backend.pushArchive(name: name, content: content)
                confirmed.append(name)
            } catch SyncError.casConflict {
                // 同名文件已存在于远端（秒级碰撞/前轮已传成功但确认丢失）：
                // 视作已保存，不无限重试。
                confirmed.append(name)
            } catch SyncError.archiveNotSupported {
                // 后端 B 仅文件授权：写不进同目录 conflicts/。留档已完整保存在设备
                // 本地 conflicts/（设置页可查），不再重试上传。
                confirmed.append(name)
            } catch {
                break // 保持 pending，下一轮随同步重试
            }
        }
        if !confirmed.isEmpty {
            state.pendingArchives.removeAll { confirmed.contains($0) }
            saveState()
        }
    }

    // MARK: 远端安装与基线

    // 应用远端内容：整文件替换本地字节 → 刷快照 → 更新基线 → 全量对账提醒（串联点）。
    private func installRemote(kind: SyncBackendKind, content: String, version: String) {
        guard let model else { return }
        model.installSyncedContent(Data(content.utf8))
        state.baseline = SyncBaseline(backendKind: kind.rawValue,
                                      contentSHA256: SyncHash.sha256Hex(content),
                                      remoteVersion: version)
        saveState()
        onRemoteAdopted?()
    }

    // MARK: Device Flow（连接）

    func startDeviceFlow() async {
        guard isGitHubConfigured else { return }
        do {
            let start = try await authService.startDeviceFlow(clientID: clientID)
            deviceFlow = DeviceFlowPresentation(userCode: start.userCode,
                                               verificationURL: start.verificationURL,
                                               phase: .waiting)
            pollDeviceFlow(start: start)
        } catch {
            deviceFlow = DeviceFlowPresentation(userCode: "", verificationURL: githubBase,
                                               phase: .failed(Self.errorCode(error)))
        }
    }

    // 轮询循环：按 interval（slow_down 反馈 +5s）轮到授权/过期/拒绝为止；可取消。
    private func pollDeviceFlow(start: DeviceFlowStart) {
        roundTask?.cancel()
        roundTask = Task { [weak self] in
            guard let self else { return }
            var interval = max(1, start.intervalSeconds)
            while !Task.isCancelled {
                do { try await Task.sleep(nanoseconds: UInt64(interval) * 1_000_000_000) } catch { return }
                do {
                    switch try await self.authService.poll(clientID: self.clientID, deviceCode: start.deviceCode) {
                    case .authorized:
                        self.deviceFlow?.phase = .connected
                        self.objectWillChange.send()
                        return
                    case .pending:
                        continue
                    case .slowDown(let extra):
                        interval += extra // RFC 8628：间隔 +5s
                        continue
                    case .expired:
                        self.deviceFlow?.phase = .failed("device-flow-expired")
                        return
                    case .denied:
                        self.deviceFlow?.phase = .failed("device-flow-denied")
                        return
                    }
                } catch {
                    if Task.isCancelled { return }
                    self.deviceFlow?.phase = .failed(SyncEngine.errorCode(error))
                    return
                }
            }
        }
    }

    // 取消连接流程（关闭弹层）：撤销轮询；已换来的 token 不回收（下次同步直接可用）。
    func cancelDeviceFlow() {
        roundTask?.cancel()
        roundTask = nil
        deviceFlow = nil
    }

    // 断开连接：按当前后端分发——A 清 Keychain 凭据（配置保留，便于重连）；
    // B 清书签与目标信息（书签失效后无复用价值，重新走选择流程）。
    func disconnect() {
        switch selectedBackend {
        case .github:
            authService.disconnect()
        case .cloudFolder:
            disconnectCloudFolder()
        }
        objectWillChange.send()
    }

    // MARK: 后端 B（云盘文件夹）接入：授权生命周期 + 副本候选接入对话框

    // 书签数据：Keychain（TokenKey.cloudFolderBookmark，base64，不含敏感数据，R7）。
    var cloudBookmarkData: Data? {
        guard let b64 = tokens.token(forKey: TokenKey.cloudFolderBookmark), !b64.isEmpty else { return nil }
        return Data(base64Encoded: b64)
    }

    var cloudTargetName: String {
        defaults.string(forKey: ConfigKey.cloudName) ?? ""
    }

    var cloudScope: CloudGrantScope {
        CloudGrantScope(rawValue: defaults.string(forKey: ConfigKey.cloudScope) ?? "") ?? .folder
    }

    var isCloudFolderConnected: Bool {
        selectedBackend == .cloudFolder && cloudBookmarkData != nil
    }

    // 文件选择器授权后建立连接：picker 返回的是安全作用域 URL，按探针端到端顺序——
    // 先 startAccessing 再建书签（真机 provider 下书签创建依赖访问会话），建完立即解析
    // 验证（失效尽早暴露）→ 持久化。换位置即换文件身份：清空基线，按 D13「无基线」重新
    // 加入；副本列表随之作废。
    func connectCloudFolder(at pickedURL: URL) async -> Bool {
        // 生产顺序是先在设置页选后端再选位置；stub 预授权与直连路径可能跳过选择，
        // 统一在此确保后端 B 激活（任一时刻只激活一个，§2.2）。
        if selectedBackend != .cloudFolder {
            selectBackend(.cloudFolder)
        }
        do {
            var granted = CloudFolderAccess(
                rootURL: pickedURL,
                scope: pickedURL.hasDirectoryPath ? .folder : .file)
            try granted.startAccessing()
            defer { granted.stopAccessing() }
            let bookmark = try CloudFolderAccess.makeBookmarkData(for: pickedURL)
            _ = try CloudFolderAccess.resolve(bookmarkData: bookmark)
            tokens.setToken(bookmark.base64EncodedString(), forKey: TokenKey.cloudFolderBookmark)
            defaults.set(pickedURL.lastPathComponent, forKey: ConfigKey.cloudName)
            defaults.set(pickedURL.hasDirectoryPath ? CloudGrantScope.folder.rawValue : CloudGrantScope.file.rawValue,
                         forKey: ConfigKey.cloudScope)
            state.baseline = nil
            saveState()
            cloudCopies = []
            cloudCopiesError = nil
            publish(.idle)
            objectWillChange.send()
            await refreshCloudCopies()
            return true
        } catch let error as CloudFolderError {
            publish(.failed(Self.errorCode(CloudFolderBackend.map(error))))
            return false
        } catch {
            publish(.failed("bookmark-create"))
            return false
        }
    }

    // 断开后端 B：清书签与目标信息（授权失效后无复用价值）。
    private func disconnectCloudFolder() {
        tokens.setToken(nil, forKey: TokenKey.cloudFolderBookmark)
        defaults.removeObject(forKey: ConfigKey.cloudName)
        defaults.removeObject(forKey: ConfigKey.cloudScope)
        cloudCopies = []
        cloudCopiesError = nil
        state.baseline = nil
        saveState()
        publish(.notConnected)
    }

    // 云端冲突副本双通道枚举（命名扫描 + NSFileVersion）：设置页列表数据源；
    // 连接后与每轮同步后的 UI 时机调用。失败不清空旧列表，如实报码。
    func refreshCloudCopies() async {
        guard selectedBackend == .cloudFolder, let bookmark = cloudBookmarkData else {
            cloudCopies = []
            cloudCopiesError = nil
            return
        }
        switch await CloudConflictEnumerator.enumerate(bookmarkData: bookmark) {
        case .success(let copies):
            cloudCopies = copies.sorted { $0.modifiedAtMs > $1.modifiedAtMs }
            cloudCopiesError = nil
        case .failure(let error):
            cloudCopiesError = Self.errorCode(error)
        }
        objectWillChange.send()
    }

    // 把副本作为远端候选送进与后端 A 相同的 D13 对话框：casBaseVersion 取主文件
    // 当前版本（落盘目标是主文件；副本枚举与同步轮之间主文件可能已被其他设备写过）。
    func adoptCloudCopy(_ copy: CloudConflictEnumerator.Copy) async {
        guard let model else { return }
        guard selectedBackend == .cloudFolder, let bookmark = cloudBookmarkData else {
            publish(.notConnected)
            return
        }
        guard !isSyncing else { return }
        isSyncing = true
        defer { isSyncing = false }
        model.flushPendingSave()
        guard model.snapshot?.writable == true, let localRaw = model.raw else {
            publish(.blockedWriteProtected)
            return
        }
        do {
            let remote = try await CloudFolderBackend(bookmarkData: bookmark).pullMain()
            pendingConflict = ConflictCandidate(
                remoteContent: copy.content,
                remoteVersion: copy.version,
                localRaw: localRaw,
                casBaseVersion: remote.exists ? remote.version : nil)
            publish(.conflictPending)
        } catch {
            publish(.failed(Self.errorCode(error)))
        }
    }

    // 后端 B 冲突裁决收尾：尽力清除 NSFileVersion 冲突标记（保留版本文件；失败静默，
    // 残留条目会在下次枚举如实再现）并刷新副本列表。后端 A 直接空转返回。
    private func finishCloudConflictResolution() async {
        guard selectedBackend == .cloudFolder, let bookmark = cloudBookmarkData else { return }
        CloudConflictEnumerator.resolveVersionMarkers(bookmarkData: bookmark)
        await refreshCloudCopies()
    }

    // 留档 pending 徽标的数据源（设置页；不暴露可变内部状态）。
    var pendingArchiveFileNames: Set<String> {
        Set(state.pendingArchives)
    }

    // MARK: 冲突留档管理（设置页）

    var archiveEntries: [ConflictArchiveEntry] {
        archiveStore.list()
    }

    func archiveContent(of entry: ConflictArchiveEntry) -> String? {
        archiveStore.content(of: entry)
    }

    func deleteArchive(_ entry: ConflictArchiveEntry) {
        archiveStore.delete(entry)
        state.pendingArchives.removeAll { $0 == entry.fileName }
        saveState()
        objectWillChange.send()
    }

    // MARK: 私有

    private func makeBackend() -> SyncBackend? {
        if let testBackend { return testBackend }
        switch selectedBackend {
        case .github:
            guard isGitHubConfigured else { return nil }
            return GitHubContentsBackend(
                transport: transport, auth: authService,
                apiBase: apiBase,
                owner: { [weak self] in self?.repoOwner },
                repo: { [weak self] in self?.repoName })
        case .cloudFolder:
            guard let bookmark = cloudBookmarkData else { return nil }
            return CloudFolderBackend(bookmarkData: bookmark)
        }
    }

    private func publish(_ outcome: SyncOutcome) {
        lastSummary = SyncSummary(outcome: outcome, atMs: clock())
        objectWillChange.send()
    }

    private func loadState() {
        guard let data = try? Data(contentsOf: stateURL) else { return }
        state = (try? JSONDecoder().decode(SyncPersistedState.self, from: data))
            ?? SyncPersistedState(baseline: nil)
    }

    private func saveState() {
        do {
            let dir = stateURL.deletingLastPathComponent()
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            try JSONEncoder().encode(state).write(to: stateURL, options: .atomic)
        } catch {
            // 基线持久化失败必须如实上报，不得吞掉：无基线会导致下一轮误判冲突，
            // 用户可见失败原因后可手动重试。
            publish(.failed("state-io"))
        }
    }

    // 错误 → 状态展示码（不夹带任何响应体/token）。
    static func errorCode(_ error: Error) -> String {
        switch error as? SyncError {
        case .casConflict: return "cas-conflict"
        case .needsReauth: return "needs-reauth"
        case .notConfigured: return "not-configured"
        case .api(let code): return code
        case .deviceFlow(let code): return code
        case .bookmarkInvalid: return "bookmark-invalid"
        case .accessDenied: return "access-denied"
        case .archiveNotSupported: return "archive-local-only"
        case nil: return String(describing: error)
        }
    }
}

// MARK: - Device Flow 弹层呈现

struct DeviceFlowPresentation: Identifiable {
    enum Phase: Equatable {
        case waiting
        case connected
        case failed(String)
    }

    let id = UUID()
    var userCode: String
    var verificationURL: URL
    var phase: Phase
}

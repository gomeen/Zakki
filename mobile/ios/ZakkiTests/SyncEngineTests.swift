import XCTest
@testable import Zakki

// MARK: - Spy 后端（记录调用、按脚本返回；引擎编排测试不走 HTTP）

@MainActor
private final class SpyBackend: SyncBackend {
    let kind: SyncBackendKind = .github
    var pullCount = 0
    var pullResults: [Result<RemoteFile, Error>] = []
    var pushes: [(content: String, expected: String?)] = []
    var pushResults: [Result<String, Error>] = []
    var archives: [(name: String, content: String)] = []
    // 拉取时人工挂起（串行测试需要让出 actor）；置位后 pushArchive 抛错（留档失败路径）。
    var pullDelayNanos: UInt64 = 0
    var archiveError: Error?

    func pullMain() async throws -> RemoteFile {
        pullCount += 1
        if pullDelayNanos > 0 {
            try? await Task.sleep(nanoseconds: pullDelayNanos)
        }
        return try pullResults.removeFirst().get()
    }

    @discardableResult
    func pushMain(content: String, expectedVersion: String?) async throws -> String {
        pushes.append((content, expectedVersion))
        return try pushResults.removeFirst().get()
    }

    func pushArchive(name: String, content: String) async throws {
        if let archiveError {
            throw archiveError
        }
        archives.append((name, content))
    }
}

// MARK: - 同步引擎编排测试（D12 串行轮次、CAS 重拉、写保护、留档随同步）

@MainActor
final class SyncEngineTests: XCTestCase {
    private var tokens: InMemoryTokenStore!
    private var defaults: UserDefaults!
    private var model: NotesModel!
    private var backend: SpyBackend!
    private var engine: SyncEngine!
    private var reconcileCalls = 0
    private var sandboxDir: URL!

    override func setUp() {
        super.setUp()
        NotesCore.setLocation(TimeZone.current.identifier)
        tokens = InMemoryTokenStore()
        tokens.setToken("access", forKey: TokenKey.accessToken)
        defaults = UserDefaults(suiteName: "zakki-sync-tests-\(UUID().uuidString)") ?? .standard
        sandboxDir = FileManager.default.temporaryDirectory
            .appendingPathComponent("zakki-sync-engine-\(UUID().uuidString)", isDirectory: true)
        try? FileManager.default.createDirectory(at: sandboxDir, withIntermediateDirectories: true)
        // 连接态前提：配置齐全 + token 在存储中。
        defaults.set("client-id", forKey: "sync.github.client-id")
        defaults.set("owner", forKey: "sync.github.owner")
        defaults.set("repo", forKey: "sync.github.repo")
        backend = SpyBackend()
        model = NotesModel(sandboxURL: sandboxDir.appendingPathComponent("zakki.json"))
        engine = SyncEngine(tokens: tokens, transport: URLSessionTransport(session: ScriptedURLProtocol.session()),
                            defaults: defaults,
                            stateURL: sandboxDir.appendingPathComponent("zakki-sync-state.json"),
                            archiveDir: sandboxDir.appendingPathComponent("conflicts"),
                            clock: { 1_700_000_000_000 },
                            deviceName: { "Test Device" })
        engine.testBackend = backend
        engine.attach(model: model)
        reconcileCalls = 0
        engine.onRemoteAdopted = { [weak self] in self?.reconcileCalls += 1 }
    }

    override func tearDown() {
        if let dir = sandboxDir {
            try? FileManager.default.removeItem(at: dir)
        }
        super.tearDown()
    }

    // v3 JSON 组装（引擎走真实 core Decode/Encode 管道）。
    private func payload(text: String, active: Int = 0) -> String {
        let obj: [String: Any] = [
            "version": 3,
            "active": active,
            "tabs": [["id": 1, "name": "A", "text": text, "color": "#e5484d"]],
        ]
        let data = (try? JSONSerialization.data(withJSONObject: obj)) ?? Data()
        return String(decoding: data, as: UTF8.self)
    }

    private func remote(_ content: String, version: String) -> RemoteFile {
        RemoteFile(exists: true, content: content, version: version)
    }

    // 首次同步、远端为空 → 创建上传，基线记录内容 sha + 新版本号。
    func test_firstSync_remoteMissing_uploadsAndSetsBaseline() async {
        model.installSyncedContent(Data(payload(text: "hello").utf8))
        backend.pullResults = [.success(.missing)]
        backend.pushResults = [.success("v-created")]
        await engine.syncNow()
        XCTAssertEqual(backend.pullCount, 1)
        XCTAssertEqual(backend.pushes.count, 1)
        XCTAssertEqual(backend.pushes[0].expected, nil)
        XCTAssertEqual(engine.lastSummary?.outcome, .pushedLocal)
        XCTAssertEqual(engine.pendingArchiveFileNames, [])
        XCTAssertEqual(stateBaseline()?.contentSHA256, SyncHash.sha256Hex(model.syncRawContent ?? ""))
        XCTAssertEqual(stateBaseline()?.remoteVersion, "v-created")
    }

    // 情况 1：本地等于基线、远端已变 → 安装远端 + 基线更新 + 对账串联点被调。
    func test_case1_adoptsRemote_andReconciles() async {
        let remoteContent = payload(text: "remote text", active: 1)
        model.installSyncedContent(Data(payload(text: "hello").utf8))
        // 先构造基线（上一轮同步成功）：本地内容 + v1。
        backend.pullResults = [.success(.missing)]
        backend.pushResults = [.success("v1")]
        await engine.syncNow()
        // 远端变化：内容不同、版本 v2。
        backend.pullResults = [.success(remote(remoteContent, version: "v2"))]
        await engine.syncNow()
        XCTAssertEqual(engine.lastSummary?.outcome, .adoptedRemote)
        XCTAssertEqual(model.snapshot?.tabs?.first?.text, "remote text")
        XCTAssertEqual(model.snapshot?.active, 1, "active 字段随远端字节原样往返")
        XCTAssertEqual(reconcileCalls, 1)
        XCTAssertEqual(stateBaseline()?.contentSHA256, SyncHash.sha256Hex(remoteContent))
        XCTAssertEqual(stateBaseline()?.remoteVersion, "v2")
        XCTAssertEqual(backend.pushes.count, 1, "采用远端不上传")
    }

    // 情况 2：本地已变、远端未变 → 上传本地（CAS 基准 = 远端版本号）。
    func test_case2_pushesLocalWithRemoteVersionAsCASBase() async {
        model.installSyncedContent(Data(payload(text: "hello").utf8))
        backend.pullResults = [.success(.missing)]
        backend.pushResults = [.success("v1")]
        await engine.syncNow()
        // 本地编辑后远端未变。
        model.updateTabText("edited locally", tabID: 1)
        model.flushPendingSave()
        backend.pullResults = [.success(remote(payload(text: "hello"), version: "v1"))]
        backend.pushResults = [.success("v2")]
        await engine.syncNow()
        XCTAssertEqual(engine.lastSummary?.outcome, .pushedLocal)
        XCTAssertEqual(backend.pushes.last?.expected, "v1")
        XCTAssertEqual(stateBaseline()?.remoteVersion, "v2")
        XCTAssertEqual(reconcileCalls, 0, "上传不触发对账串联点")
    }

    // 情况 3：双方已变但内容相同（字节一致）→ 只更新基线，不安装不上传。
    func test_case3_sameContent_updatesBaselineOnly() async {
        model.installSyncedContent(Data(payload(text: "hello").utf8))
        backend.pullResults = [.success(.missing)]
        backend.pushResults = [.success("v1")]
        await engine.syncNow()
        model.updateTabText("same", tabID: 1)
        model.flushPendingSave()
        // 远端内容取 flush 后的真实字节（EncodeNotes 输出），保证字节一致。
        let actualLocal = try! XCTUnwrap(model.syncRawContent)
        backend.pullResults = [.success(remote(actualLocal, version: "v2"))]
        await engine.syncNow()
        XCTAssertEqual(engine.lastSummary?.outcome, .baselineUpdated)
        XCTAssertEqual(backend.pushes.count, 1, "无上传")
        XCTAssertEqual(reconcileCalls, 0)
        XCTAssertEqual(stateBaseline()?.remoteVersion, "v2")
        XCTAssertEqual(stateBaseline()?.contentSHA256, SyncHash.sha256Hex(actualLocal))
    }

    // 情况 4：双方已变且不同 → 冲突挂起；保留本机 = 留档远端 + CAS 上传本地。
    func test_case4_keepLocal_archivesRemoteAndPushes() async {
        let remoteEdit = payload(text: "remote edit")
        model.installSyncedContent(Data(payload(text: "hello").utf8))
        backend.pullResults = [.success(.missing)]
        backend.pushResults = [.success("v1")]
        await engine.syncNow()
        model.updateTabText("local edit", tabID: 1)
        model.flushPendingSave()
        let localActual = model.syncRawContent
        backend.pullResults = [.success(remote(remoteEdit, version: "v2"))]
        await engine.syncNow()
        XCTAssertEqual(engine.lastSummary?.outcome, .conflictPending)
        XCTAssertEqual(engine.pendingConflict?.remoteVersion, "v2")
        XCTAssertEqual(engine.pendingConflict?.localRaw, localActual)
        XCTAssertEqual(reconcileCalls, 0, "冲突挂起不触发对账")
        // 裁决：保留本机。
        backend.pushResults = [.success("v3")]
        await engine.resolveConflict(keepLocal: true)
        XCTAssertEqual(engine.lastSummary?.outcome, .pushedLocal)
        XCTAssertNil(engine.pendingConflict)
        XCTAssertEqual(model.snapshot?.tabs?.first?.text, "local edit")
        // 落选的远端版本已留档，且随本轮上传。
        let archives = engine.archiveEntries
        XCTAssertEqual(archives.count, 1)
        XCTAssertEqual(engine.archiveContent(of: archives[0]), remoteEdit)
        XCTAssertEqual(backend.archives.count, 1)
        XCTAssertTrue(backend.archives[0].name.hasPrefix("zakki-"))
        XCTAssertEqual(backend.archives[0].content, remoteEdit)
        XCTAssertEqual(stateBaseline()?.remoteVersion, "v3")
        XCTAssertEqual(engine.pendingArchiveFileNames, [], "上传成功后清 pending")
    }

    // 情况 4：保留远端 = 留档本地 + 安装远端 + 基线更新 + 对账。
    func test_case4_keepRemote_archivesLocalAndInstallsRemote() async {
        let remoteEdit = payload(text: "remote edit")
        model.installSyncedContent(Data(payload(text: "hello").utf8))
        backend.pullResults = [.success(.missing)]
        backend.pushResults = [.success("v1")]
        await engine.syncNow()
        model.updateTabText("local edit", tabID: 1)
        model.flushPendingSave()
        let localBefore = model.syncRawContent
        backend.pullResults = [.success(remote(remoteEdit, version: "v2"))]
        await engine.syncNow()
        await engine.resolveConflict(keepLocal: false)
        XCTAssertEqual(engine.lastSummary?.outcome, .adoptedRemote)
        XCTAssertEqual(model.snapshot?.tabs?.first?.text, "remote edit")
        XCTAssertEqual(reconcileCalls, 1)
        let archives = engine.archiveEntries
        XCTAssertEqual(archives.count, 1)
        XCTAssertEqual(engine.archiveContent(of: archives[0]), localBefore, "落选的本机版本完整留档")
        XCTAssertEqual(stateBaseline()?.contentSHA256, SyncHash.sha256Hex(remoteEdit))
        XCTAssertEqual(stateBaseline()?.remoteVersion, "v2")
    }

    // CAS 冲突：PUT 拒绝后重新 pull 重判定（不盲重试旧 sha），第二次按新远端走。
    func test_casConflict_repullsAndRedecides() async {
        model.installSyncedContent(Data(payload(text: "hello").utf8))
        backend.pullResults = [.success(.missing)]
        backend.pushResults = [.success("v1")]
        await engine.syncNow()
        model.updateTabText("edited", tabID: 1)
        model.flushPendingSave()
        // 第一轮 pull：远端仍 v1（基线未变）；PUT 拒绝（并发写入者已前进）；
        // 第二轮 pull：远端已是 v2（内容不同）→ 重判定为冲突挂起。
        backend.pullResults = [
            .success(remote(payload(text: "hello"), version: "v1")),
            .success(remote(payload(text: "changed elsewhere"), version: "v2")),
        ]
        backend.pushResults = [.failure(SyncError.casConflict)]
        await engine.syncNow()
        // 首轮基线各占 1 次：重试轮内 pull 2 次、push 1 次（不盲重试旧 sha）。
        XCTAssertEqual(backend.pullCount, 3, "CAS 拒绝后重新 pull")
        XCTAssertEqual(backend.pushes.count, 2, "不盲重试旧 sha")
        XCTAssertEqual(engine.lastSummary?.outcome, .conflictPending, "新远端与本地不同 → 冲突")
        XCTAssertEqual(engine.pendingConflict?.remoteVersion, "v2")
    }

    // 连续 CAS 竞争超上限 → 上报远端持续变化（安全阀，不无限循环）。
    func test_casContention_reportsFailureAfterLimit() async {
        model.installSyncedContent(Data(payload(text: "hello").utf8))
        backend.pullResults = [.success(.missing)]
        backend.pushResults = [.success("v1")]
        await engine.syncNow()
        // 本地已变；三轮 pull 都显示远端未变（决策为上传），但 PUT 每次 CAS 拒绝。
        model.updateTabText("edited", tabID: 1)
        model.flushPendingSave()
        for _ in 0..<3 {
            backend.pullResults.append(.success(remote(payload(text: "hello"), version: "v1")))
        }
        backend.pushResults = [.failure(SyncError.casConflict), .failure(SyncError.casConflict), .failure(SyncError.casConflict)]
        await engine.syncNow()
        // 首轮基线各占 1 次：重试轮内再 pull 3 次、push 3 次。
        XCTAssertEqual(backend.pullCount, 4)
        XCTAssertEqual(backend.pushes.count, 4)
        XCTAssertEqual(engine.lastSummary?.outcome, .failed("cas-contention"))
    }

    // 写保护：高版本文件整轮拦截，零网络调用（禁止上传与自动覆盖，§2.1）。
    func test_writeProtected_blocksRoundWithNoBackendCalls() async {
        let tooNew = ##"{"version":99,"active":0,"tabs":[{"id":1,"name":"A","text":"x","color":"#000000"}]}"##
        model.installSyncedContent(Data(tooNew.utf8))
        XCTAssertFalse(model.snapshot?.writable ?? true)
        await engine.syncNow()
        XCTAssertEqual(engine.lastSummary?.outcome, .blockedWriteProtected)
        XCTAssertEqual(backend.pullCount, 0)
        XCTAssertEqual(backend.pushes.count, 0)
    }

    // 防抖待写先冲刷再判定：未落盘的编辑算本地已变 → 上传而非被远端覆盖。
    func test_flushesPendingWritesBeforeDeciding() async {
        model.installSyncedContent(Data(payload(text: "hello").utf8))
        backend.pullResults = [.success(.missing)]
        backend.pushResults = [.success("v1")]
        await engine.syncNow()
        // 编辑不 flush（防抖 1s 未到）；远端版本未变（等于基线）→ flush 后本地已变走上传。
        model.updateTabText("typed but unsaved", tabID: 1)
        backend.pullResults = [.success(remote(payload(text: "hello"), version: "v1"))]
        backend.pushResults = [.success("v3")]
        await engine.syncNow()
        XCTAssertEqual(engine.lastSummary?.outcome, .pushedLocal, "flush 后本地已变，走上传")
        XCTAssertEqual(model.snapshot?.tabs?.first?.text, "typed but unsaved")
    }

    // needsReauth：后端抛断连 → 状态如实呈现（真实路径下凭据已被认证服务清空）。
    func test_needsReauth_reportsFailure() async {
        model.installSyncedContent(Data(payload(text: "hello").utf8))
        backend.pullResults = [.failure(SyncError.needsReauth)]
        await engine.syncNow()
        XCTAssertEqual(engine.lastSummary?.outcome, .failed("needs-reauth"))
    }

    // 串行：一轮进行中忽略并发触发（D12：不混跑两条提交链）。
    func test_concurrentSyncRequests_serializeToOneRound() async {
        model.installSyncedContent(Data(payload(text: "hello").utf8))
        backend.pullResults = [.success(.missing)]
        backend.pushResults = [.success("v1")]
        // 拉取时挂起，让第二个并发触发落在轮次中间（isSyncing=true）。
        backend.pullDelayNanos = 100_000_000
        async let a: Void = engine.syncNow()
        async let b: Void = engine.syncNow()
        await a
        await b
        XCTAssertEqual(backend.pullCount, 1, "并发触发只跑一轮")
        XCTAssertEqual(backend.pushes.count, 1)
    }

    // 留档 pending：上传失败保持 pending，下一轮随同步重试成功后清除。
    func test_pendingArchive_retriesNextRound() async {
        let remoteEdit = payload(text: "remote edit")
        model.installSyncedContent(Data(payload(text: "hello").utf8))
        backend.pullResults = [.success(.missing)]
        backend.pushResults = [.success("v1")]
        await engine.syncNow()
        model.updateTabText("local edit", tabID: 1)
        model.flushPendingSave()
        backend.pullResults = [.success(remote(remoteEdit, version: "v2"))]
        await engine.syncNow()
        // 保留远端：本地留档；本轮留档上传失败（网络错误脚本）。
        backend.archiveError = SyncError.api("network")
        await engine.resolveConflict(keepLocal: false)
        XCTAssertEqual(engine.pendingArchiveFileNames.count, 1, "上传失败保持 pending")
        XCTAssertEqual(backend.archives.count, 0)
        // 下一轮（无变化）留档重试成功。
        backend.archiveError = nil
        backend.pullResults = [.success(remote(remoteEdit, version: "v2"))]
        await engine.syncNow()
        XCTAssertEqual(engine.lastSummary?.outcome, .idle)
        XCTAssertEqual(backend.archives.count, 1, "pending 留档重试")
        XCTAssertEqual(engine.pendingArchiveFileNames, [], "成功后清除")
    }

    // 状态文件持久化：重建引擎后基线仍在（设备本地，独立于 zakki.json）。
    func test_stateFile_persistsBaselineAcrossEngineInstances() async {
        model.installSyncedContent(Data(payload(text: "hello").utf8))
        backend.pullResults = [.success(.missing)]
        backend.pushResults = [.success("v1")]
        await engine.syncNow()
        XCTAssertEqual(stateBaseline()?.remoteVersion, "v1")
        let reborn = SyncEngine(tokens: tokens, transport: URLSessionTransport(session: ScriptedURLProtocol.session()),
                                defaults: defaults,
                                stateURL: sandboxDir.appendingPathComponent("zakki-sync-state.json"),
                                archiveDir: sandboxDir.appendingPathComponent("conflicts"),
                                clock: { 1_700_000_001_000 },
                                deviceName: { "Test Device" })
        reborn.testBackend = backend
        reborn.attach(model: model)
        // 新引擎继续：本地未变、远端未变 → idle（基线生效，不重复上传）。
        backend.pullResults = [.success(remote(payload(text: "hello"), version: "v1"))]
        await reborn.syncNow()
        XCTAssertEqual(reborn.lastSummary?.outcome, .idle)
        XCTAssertEqual(backend.pushes.count, 1)
    }

    // MARK: 辅助

    private func stateBaseline() -> SyncBaseline? {
        guard let data = try? Data(contentsOf: sandboxDir.appendingPathComponent("zakki-sync-state.json")) else {
            return nil
        }
        return (try? JSONDecoder().decode(SyncPersistedState.self, from: data))?.baseline
    }
}

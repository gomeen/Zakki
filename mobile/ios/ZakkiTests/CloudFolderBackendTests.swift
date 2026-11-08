import XCTest
@testable import Zakki

// MARK: - 后端 B（云盘文件夹）语义：CAS 比对 / 留档 / 书签失效 / D13 引擎集成

// 全部走 App 容器内临时目录（自容器书签机制与真实 provider 一致，探针 §1），
// 不碰真实 iCloud 与真实用户目录。内容一律合法 v3 payload（引擎会走真实解码）。
@MainActor
final class CloudFolderBackendTests: XCTestCase {
    private var root: URL!

    override func setUpWithError() throws {
        root = FileManager.default.temporaryDirectory
            .appendingPathComponent("zakki-cloud-backend-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: root)
    }

    // 合法 v3 zakki.json（单标签）；文本可区分本机/远端/副本。
    private func payload(_ text: String) -> String {
        let obj: [String: Any] = [
            "version": 3,
            "active": 0,
            "tabs": [["id": 1, "name": name, "text": text, "color": "#30a46c"]],
        ]
        let data = try! JSONSerialization.data(withJSONObject: obj, options: [.sortedKeys])
        return String(decoding: data, as: UTF8.self)
    }

    // MARK: pull / push / 留档（后端语义，内容不进解码）

    private func makeBackend(scope: CloudGrantScope = .folder,
                             timeout: TimeInterval = 2) throws -> CloudFolderBackend {
        guard let root else { throw NSError(domain: "test", code: 0) }
        let target = scope == .folder ? root : root.appendingPathComponent("zakki.json", isDirectory: false)
        if scope == .file {
            try Data("{}".utf8).write(to: target)
        }
        let bookmark = try CloudFolderAccess.makeBookmarkData(for: target)
        return CloudFolderBackend(bookmarkData: bookmark, downloadTimeout: timeout, pollInterval: 0.05)
    }

    func testPullMissingReturnsMissing() async throws {
        let backend = try makeBackend()
        let remote = try await backend.pullMain()
        XCTAssertEqual(remote, .missing)
    }

    func testPullExistingReturnsContentAndVersion() async throws {
        try Data("{\"a\":1}".utf8).write(to: root.appendingPathComponent("zakki.json"))
        let backend = try makeBackend()
        let remote = try await backend.pullMain()
        XCTAssertTrue(remote.exists)
        XCTAssertEqual(remote.content, "{\"a\":1}")
        XCTAssertNotNil(remote.version)
    }

    // 书签目标被删：解析抛错 → bookmarkInvalid（探针 §5：不是 stale 标志）。
    func testPullWithDeletedTargetThrowsBookmarkInvalid() async throws {
        let target = root.appendingPathComponent("zakki.json")
        try Data("{}".utf8).write(to: target)
        let bookmark = try CloudFolderAccess.makeBookmarkData(for: target)
        try FileManager.default.removeItem(at: target)
        let backend = CloudFolderBackend(bookmarkData: bookmark)
        do {
            _ = try await backend.pullMain()
            XCTFail("目标已删应抛 bookmarkInvalid")
        } catch let error as SyncError {
            XCTAssertEqual(error, .bookmarkInvalid)
        }
    }

    // MARK: push（写前版本比对 ≈ CAS）

    func testPushCreateConflictsWhenFileExists() async throws {
        try Data("remote".utf8).write(to: root.appendingPathComponent("zakki.json"))
        let backend = try makeBackend()
        do {
            _ = try await backend.pushMain(content: "local", expectedVersion: nil)
            XCTFail("新建语义遇已存在文件应 casConflict")
        } catch let error as SyncError {
            XCTAssertEqual(error, .casConflict)
        }
    }

    func testPushUpdateConflictsOnVersionMismatch() async throws {
        try Data("remote".utf8).write(to: root.appendingPathComponent("zakki.json"))
        let backend = try makeBackend()
        do {
            _ = try await backend.pushMain(content: "local", expectedVersion: "stale-version")
            XCTFail("版本不一致应 casConflict")
        } catch let error as SyncError {
            XCTAssertEqual(error, .casConflict)
        }
    }

    func testPushUpdateWritesContentAndReturnsNewVersion() async throws {
        let main = root.appendingPathComponent("zakki.json")
        try Data("remote".utf8).write(to: main)
        let backend = try makeBackend()
        let first = try await backend.pullMain()
        let newVersion = try await backend.pushMain(content: "local-new", expectedVersion: first.version)
        XCTAssertEqual(try String(contentsOf: main, encoding: .utf8), "local-new")
        // 写后版本号必须与写前不同（基线记录写后稳定值）。
        XCTAssertNotEqual(newVersion, first.version)
        // 新版本号可用于下一轮比对。
        let again = try await backend.pullMain()
        XCTAssertEqual(again.version, newVersion)
    }

    func testPushCreateWritesWhenMissing() async throws {
        let backend = try makeBackend()
        _ = try await backend.pushMain(content: "seed", expectedVersion: nil)
        let main = root.appendingPathComponent("zakki.json")
        XCTAssertEqual(try String(contentsOf: main, encoding: .utf8), "seed")
    }

    // MARK: 留档（conflicts/）

    func testPushArchiveWritesIntoConflictsDirectory() async throws {
        let backend = try makeBackend()
        try await backend.pushArchive(name: "zakki-1.json", content: "loser")
        let archived = root.appendingPathComponent("conflicts/zakki-1.json")
        XCTAssertEqual(try String(contentsOf: archived, encoding: .utf8), "loser")
    }

    // 仅文件授权写不进同目录 conflicts/：archiveNotSupported（引擎按「留在设备本地」处理）。
    func testPushArchiveFileScopeThrowsNotSupported() async throws {
        let backend = try makeBackend(scope: .file)
        do {
            try await backend.pushArchive(name: "zakki-1.json", content: "loser")
            XCTFail("仅文件授权应 archiveNotSupported")
        } catch let error as SyncError {
            XCTAssertEqual(error, .archiveNotSupported)
        }
    }

    // MARK: 引擎集成：后端 B 走同一 D13 判定表（真实临时文件系统当 mock FS）

    private var tokens: InMemoryTokenStore!
    private var defaults: UserDefaults!
    private var engine: SyncEngine!
    private var model: NotesModel!
    private var sandboxDir: URL!

    private func makeEngine(remoteText: String, copyText: String? = nil) throws {
        let remoteJSON = payload(remoteText)
        try Data(remoteJSON.utf8).write(to: root.appendingPathComponent("zakki.json"))
        if let copyText {
            try Data(payload(copyText).utf8)
                .write(to: root.appendingPathComponent("zakki (1).json"))
        }
        let bookmark = try CloudFolderAccess.makeBookmarkData(for: root)
        tokens = InMemoryTokenStore()
        tokens.setToken(bookmark.base64EncodedString(), forKey: TokenKey.cloudFolderBookmark)
        defaults = UserDefaults(suiteName: "zakki-cloud-engine-\(UUID().uuidString)") ?? .standard
        defaults.set(SyncBackendKind.cloudFolder.rawValue, forKey: "sync.backend")
        defaults.set(root.lastPathComponent, forKey: "sync.cloud.name")
        sandboxDir = FileManager.default.temporaryDirectory
            .appendingPathComponent("zakki-cloud-engine-sb-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: sandboxDir, withIntermediateDirectories: true)
        model = NotesModel(sandboxURL: sandboxDir.appendingPathComponent("zakki.json"))
        engine = SyncEngine(tokens: tokens,
                            transport: URLSessionTransport(session: ScriptedURLProtocol.session()),
                            defaults: defaults,
                            stateURL: sandboxDir.appendingPathComponent("zakki-sync-state.json"),
                            archiveDir: sandboxDir.appendingPathComponent("conflicts"),
                            clock: { 1_700_000_000_000 },
                            deviceName: { "Test Device" })
        engine.attach(model: model)
    }

    private func installLocal(_ text: String) {
        XCTAssertTrue(model.install(Data(payload(text).utf8)))
    }

    private func mainFileText() throws -> String? {
        try String(contentsOf: root.appendingPathComponent("zakki.json"), encoding: .utf8)
    }

    // 无基线 + 双方内容不同 → 冲突对话框（候选 casBaseVersion=主文件版本）；
    // 保留本机 → 远端被本地覆盖 + 基线落地 + 留档写进云盘 conflicts/。
    func testEngineRoundConflictKeepLocalArchivesToCloud() async throws {
        try makeEngine(remoteText: "远端首行")
        installLocal("本机首行")
        await engine.syncNow()
        guard let conflict = engine.pendingConflict else {
            return XCTFail("无基线且双方不同应弹冲突，实际 \(String(describing: engine.lastSummary))")
        }
        let mainVersion = try await CloudFolderBackend(
            bookmarkData: CloudFolderAccess.makeBookmarkData(for: root)).pullMain().version
        XCTAssertEqual(conflict.casBaseVersion, mainVersion)
        XCTAssertTrue(engine.archiveEntries.isEmpty, "裁决前不应有留档")

        await engine.resolveConflict(keepLocal: true)
        XCTAssertNil(engine.pendingConflict)
        // 主文件被本地内容覆盖。
        XCTAssertTrue(try mainFileText()?.contains("本机首行") == true)
        // 落选的远端内容完整留档：本地 conflicts/ 与云盘 conflicts/ 同名各一份。
        XCTAssertEqual(engine.archiveEntries.count, 1)
        let cloudArchived = root.appendingPathComponent("conflicts")
        let names = (try? FileManager.default.contentsOfDirectory(atPath: cloudArchived.path)) ?? []
        XCTAssertEqual(names.count, 1, "留档应随同步上传到云盘 conflicts/")

        // 下一轮：本地==基线、远端==基线 → none（已是最新）。
        await engine.syncNow()
        XCTAssertEqual(engine.lastSummary?.outcome, .idle)
    }

    // 保留远端（主文件候选）：本地落选留档 + 远端内容安装 + 基线更新；
    // 候选与主文件版本相等，不发生额外写回。
    func testEngineRoundConflictKeepRemoteAdoptsMainFile() async throws {
        try makeEngine(remoteText: "远端首行")
        installLocal("本机首行")
        await engine.syncNow()
        guard engine.pendingConflict != nil else { return XCTFail("应弹冲突") }
        await engine.resolveConflict(keepLocal: false)
        XCTAssertEqual(model.raw, payload("远端首行"))
        XCTAssertEqual(engine.lastSummary?.outcome, .adoptedRemote)
        XCTAssertEqual(engine.archiveEntries.count, 1)
        await engine.syncNow()
        XCTAssertEqual(engine.lastSummary?.outcome, .idle)
    }

    // 远端被外部设备改写（mtime 变化）→ 本地等于基线 → adoptRemote。
    func testEngineAdoptsExternalRemoteChange() async throws {
        try makeEngine(remoteText: "首行")
        installLocal("首行")
        await engine.syncNow()
        // 无基线且双方内容相同：只建基线（本地无变化、不写盘、不弹冲突）。
        XCTAssertEqual(engine.lastSummary?.outcome, .baselineUpdated)

        // 外部改写远端（就地覆写推进 mtime）。
        Thread.sleep(forTimeInterval: 1.1)
        try Data(payload("改后首行").utf8).write(to: root.appendingPathComponent("zakki.json"))
        await engine.syncNow()
        XCTAssertEqual(engine.lastSummary?.outcome, .adoptedRemote, "本地等于基线、远端已变 → 采用远端")
        XCTAssertEqual(model.raw, payload("改后首行"))
    }

    // 本地已变、远端等于基线 → pushLocal（上传）。
    func testEnginePushesLocalWhenRemoteUnchanged() async throws {
        try makeEngine(remoteText: "首行")
        installLocal("首行")
        await engine.syncNow()
        XCTAssertEqual(engine.lastSummary?.outcome, .baselineUpdated)

        installLocal("本机新首行")
        await engine.syncNow()
        XCTAssertEqual(engine.lastSummary?.outcome, .pushedLocal)
        XCTAssertTrue(try mainFileText()?.contains("本机新首行") == true)
        await engine.syncNow()
        XCTAssertEqual(engine.lastSummary?.outcome, .idle)
    }

    // 双方已变但内容相同 → 只更新基线，不写盘不弹冲突（本地已变但未上传、
    // 远端被另一设备写成同一内容）。
    func testEngineBaselineOnlyWhenContentsIdentical() async throws {
        try makeEngine(remoteText: "首行")
        installLocal("首行")
        await engine.syncNow()
        XCTAssertEqual(engine.lastSummary?.outcome, .baselineUpdated)

        // 本地改内容但不推（下一轮才判定）；远端外部写成同一内容。
        installLocal("同改首行")
        Thread.sleep(forTimeInterval: 1.1)
        try Data(payload("同改首行").utf8).write(to: root.appendingPathComponent("zakki.json"))
        await engine.syncNow()
        XCTAssertEqual(engine.lastSummary?.outcome, .baselineUpdated)
        XCTAssertNil(engine.pendingConflict)
    }

    // 采用云端副本：走同一 D13 对话框，casBaseVersion 取主文件当前版本（不是副本版本）；
    // 保留远端后副本内容写回主文件。
    func testAdoptCloudCopyUsesMainFileCASBase() async throws {
        try makeEngine(remoteText: "主文件首行", copyText: "副本首行")
        installLocal("本机首行")
        await engine.syncNow()
        XCTAssertNotNil(engine.pendingConflict)
        await engine.resolveConflict(keepLocal: false) // 先建立主文件基线
        XCTAssertEqual(model.raw, payload("主文件首行"))

        await engine.refreshCloudCopies()
        XCTAssertEqual(engine.cloudCopies.count, 1)
        installLocal("本机编辑后") // 本地再变，制造「双方已变」局面
        await engine.adoptCloudCopy(engine.cloudCopies[0])
        guard let candidate = engine.pendingConflict else {
            return XCTFail("采用副本应弹同一冲突对话框")
        }
        XCTAssertEqual(candidate.remoteContent, payload("副本首行"))
        XCTAssertNotEqual(candidate.remoteVersion, candidate.casBaseVersion,
                          "副本自身版本 ≠ 主文件 CAS 基准")
        await engine.resolveConflict(keepLocal: false)
        XCTAssertEqual(try mainFileText(), payload("副本首行"), "副本内容应写回主文件")
        XCTAssertEqual(engine.lastSummary?.outcome, .adoptedRemote)
    }
}

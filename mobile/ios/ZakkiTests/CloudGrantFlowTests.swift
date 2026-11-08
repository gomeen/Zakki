import XCTest
@testable import Zakki

// MARK: - 后端 B 授权生命周期：连接 / 断开 / 书签失效→重选状态机

// 授权形态（探针 §5）：书签失效（目标被删）与授权被撤销都必须引导重选；
// 换位置即换文件身份，基线作废按「无基线」重新加入。
@MainActor
final class CloudGrantFlowTests: XCTestCase {
    private var tokens: InMemoryTokenStore!
    private var defaults: UserDefaults!
    private var engine: SyncEngine!
    private var model: NotesModel!
    private var root: URL!
    private var sandboxDir: URL!

    override func setUpWithError() throws {
        root = FileManager.default.temporaryDirectory
            .appendingPathComponent("zakki-grant-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        sandboxDir = FileManager.default.temporaryDirectory
            .appendingPathComponent("zakki-grant-sb-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: sandboxDir, withIntermediateDirectories: true)
        tokens = InMemoryTokenStore()
        defaults = UserDefaults(suiteName: "zakki-grant-\(UUID().uuidString)") ?? .standard
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

    // 合法 v3 zakki.json（单标签）；文本可区分不同位置内容。
    private func payload(_ text: String) -> String {
        let obj: [String: Any] = [
            "version": 3,
            "active": 0,
            "tabs": [["id": 1, "name": "标签", "text": text, "color": "#30a46c"]],
        ]
        let data = try! JSONSerialization.data(withJSONObject: obj, options: [.sortedKeys])
        return String(decoding: data, as: UTF8.self)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: root)
        try? FileManager.default.removeItem(at: sandboxDir)
    }

    // 连接：书签持久化进 token 存储、目标信息进 defaults、基线清空、连接态成立。
    func testConnectPersistsBookmarkAndClearsBaseline() async throws {
        let ok = await engine.connectCloudFolder(at: root)
        XCTAssertTrue(ok)
        XCTAssertNotNil(tokens.token(forKey: TokenKey.cloudFolderBookmark))
        XCTAssertEqual(engine.cloudTargetName, root.lastPathComponent)
        XCTAssertEqual(engine.cloudScope, .folder)
        XCTAssertTrue(engine.isConnected)
        XCTAssertEqual(engine.backendSelection, .cloudFolder)
        XCTAssertTrue(engine.isCloudFolderConnected)
    }

    // 断开：书签与目标信息清空，回到未连接。
    func testDisconnectClearsGrant() async throws {
        _ = await engine.connectCloudFolder(at: root)
        engine.disconnect()
        XCTAssertNil(tokens.token(forKey: TokenKey.cloudFolderBookmark))
        XCTAssertNil(defaults.string(forKey: "sync.cloud.name"))
        XCTAssertNil(defaults.string(forKey: "sync.cloud.scope"))
        XCTAssertFalse(engine.isCloudFolderConnected)
        XCTAssertFalse(engine.isConnected)
    }

    // 书签失效→重选状态机：目标被删后同步如实报 bookmark-invalid，重选新位置后恢复。
    func testBookmarkInvalidThenReselectRecovers() async throws {
        try Data(payload("远端首行").utf8).write(to: root.appendingPathComponent("zakki.json"))
        _ = await engine.connectCloudFolder(at: root)
        XCTAssertTrue(engine.isConnected)

        // 目标删除（真机对应 iCloud 文件被删/挪走）。
        try FileManager.default.removeItem(at: root)
        await engine.syncNow()
        XCTAssertEqual(engine.lastSummary?.outcome, .failed("bookmark-invalid"))
        // 连接态仍成立（书签数据在），由设置页按错误码引导重选。
        XCTAssertTrue(engine.isConnected)

        // 重选：新位置连接成功，基线作废重新加入，同步恢复正常。
        let fresh = FileManager.default.temporaryDirectory
            .appendingPathComponent("zakki-grant-new-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: fresh, withIntermediateDirectories: true)
        try Data(payload("新位置首行").utf8).write(to: fresh.appendingPathComponent("zakki.json"))
        defer { try? FileManager.default.removeItem(at: fresh) }
        let ok = await engine.connectCloudFolder(at: fresh)
        XCTAssertTrue(ok)
        await engine.syncNow()
        XCTAssertNotEqual(engine.lastSummary?.outcome, .failed("bookmark-invalid"))
    }

    // 连接到不可解析的路径：失败如实上报，不产生半连接态。
    func testConnectToMissingPathFailsCleanly() async throws {
        let ghost = FileManager.default.temporaryDirectory
            .appendingPathComponent("zakki-ghost-\(UUID().uuidString)", isDirectory: true)
        let ok = await engine.connectCloudFolder(at: ghost)
        XCTAssertFalse(ok)
        XCTAssertNil(tokens.token(forKey: TokenKey.cloudFolderBookmark))
        XCTAssertFalse(engine.isConnected)
    }

    // 换位置清基线：先在 A 建立基线，重连 B 后基线为空（按 D13「无基线」重新加入）。
    func testSwitchingLocationClearsBaseline() async throws {
        try Data(payload("A 位置首行").utf8).write(to: root.appendingPathComponent("zakki.json"))
        _ = await engine.connectCloudFolder(at: root)
        await engine.syncNow()
        guard engine.pendingConflict != nil else {
            return XCTFail("无基线且内容不同应弹冲突：\(String(describing: engine.lastSummary))")
        }
        await engine.resolveConflict(keepLocal: false)
        XCTAssertEqual(engine.lastSummary?.outcome, .adoptedRemote)

        let other = FileManager.default.temporaryDirectory
            .appendingPathComponent("zakki-grant-other-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: other, withIntermediateDirectories: true)
        try Data(payload("B 位置首行").utf8).write(to: other.appendingPathComponent("zakki.json"))
        defer { try? FileManager.default.removeItem(at: other) }
        // 重连后本地字节未变：若基线未清，判定会是 adoptRemote（误认为远端变了）；
        // 基线已清则按「无基线双方不同」走冲突。
        _ = await engine.connectCloudFolder(at: other)
        await engine.syncNow()
        XCTAssertNotNil(engine.pendingConflict, "重选位置后基线应作废，按无基线重新判定")
        await engine.resolveConflict(keepLocal: false)
        XCTAssertEqual(model.raw, payload("B 位置首行"))
    }
}

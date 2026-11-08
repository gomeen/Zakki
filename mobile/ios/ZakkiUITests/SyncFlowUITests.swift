import XCTest

// M4-1 同步端到端（模拟器 + 进程内 GitHub stub，-ZakkiSyncStub）：
// 连接（Device Flow 码）→ 首轮拉推（远端为空 → 创建上传）→ 本地编辑 +
// stub 注入远端变化 → 冲突对话框 → 保留远端 → conflicts/ 留档生成与查看。
// 全程不碰真实网络与真实 Keychain；截图以 keepAlways 附件留存归档。
final class SyncFlowUITests: XCTestCase {
    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    private func launchApp() throws -> XCUIApplication {
        let payload = try payload(tabs: [
            ("同步演示", "# 同步演示\n\n- 本机首行内容\n", "#e5484d"),
            ("第二标签", "普通内容\n", "#30a46c"),
        ])
        let url = FileManager.default.temporaryDirectory
            .appendingPathComponent("zakki-sync-ui-\(UUID().uuidString).json")
        try payload.write(to: url)
        let app = XCUIApplication()
        app.launchArguments = ["-ZakkiImportPath", url.path, "-ZakkiSyncStub", "-ZakkiProvisionalNotifications"]
        app.launch()
        return app
    }

    private func payload(tabs: [(name: String, text: String, color: String)]) throws -> Data {
        let obj: [String: Any] = [
            "version": 3,
            "active": 0,
            "tabs": tabs.enumerated().map { i, tab in
                ["id": i + 1, "name": tab.name, "text": tab.text, "color": tab.color]
            },
        ]
        return try JSONSerialization.data(withJSONObject: obj, options: [.sortedKeys])
    }

    // MARK: 元素定位（验证机中文环境为主，英文回退）

    private func menuButton(_ app: XCUIApplication) -> XCUIElement {
        let zh = app.buttons["菜单"]
        return zh.exists ? zh : app.buttons["Menu"]
    }

    private func settingsItem(_ app: XCUIApplication) -> XCUIElement {
        let zh = app.buttons["设置"]
        return zh.exists ? zh : app.buttons["Settings"]
    }

    private func button(_ app: XCUIApplication, zh: String, en: String) -> XCUIElement {
        let b = app.buttons[zh]
        return b.exists ? b : app.buttons[en]
    }

    private func text(_ app: XCUIApplication, zh: String, en: String) -> XCUIElement {
        let t = app.staticTexts[zh]
        return t.exists ? t : app.staticTexts[en]
    }

    private func sourceModeFab(_ app: XCUIApplication) -> XCUIElement {
        let zh = app.images["切换到源码"]
        if zh.exists { return zh }
        let en = app.images["Switch to source"]
        if en.exists { return en }
        return app.descendants(matching: .any)
            .matching(NSPredicate(format: "label == %@ OR label == %@", "切换到源码", "Switch to source")).firstMatch
    }

    private func shot(_ app: XCUIApplication, _ name: String) {
        let attachment = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    // 连接 GitHub（stub 预填配置）→ Device Flow 弹层 → 等待授权 → 完成。
    private func connectThroughStub(_ app: XCUIApplication) throws {
        menuButton(app).tap()
        settingsItem(app).tap()
        let connect = try waitFor(app, zh: "连接 GitHub", en: "Connect GitHub", timeout: 8)
        shot(app, "01-settings-before-connect")
        connect.tap()
        // Device Flow 弹层：stub 返回固定 user_code。
        let code = anyElement(app, zh: "TEST-CODE", en: "TEST-CODE")
        XCTAssertTrue(code.waitForExistence(timeout: 8), "Device Flow user code 可见")
        shot(app, "02-device-flow-code")
        // stub 第一次轮询 pending、第二次授权（interval 1s），12s 足够。
        let connected = anyElement(app, zh: "已连接 GitHub", en: "Connected to GitHub")
        XCTAssertTrue(connected.waitForExistence(timeout: 12), "Device Flow 授权完成")
        shot(app, "03-device-flow-connected")
        // 弹层内完成钮（下半屏；导航栏同名钮在上半屏，需按位置区分）。
        let candidates = app.buttons.matching(NSPredicate(format: "label == %@ OR label == %@", "完成", "Done")).allElementsBoundByIndex
        let done = candidates.first { $0.frame.minY > 400 }
        XCTAssertNotNil(done, "弹层完成按钮可见")
        done?.tap()
    }

    private func syncNow(_ app: XCUIApplication) throws {
        let button = try waitFor(app, zh: "立即同步", en: "Sync Now", timeout: 8)
        button.tap()
    }

    private func anyElement(_ app: XCUIApplication, zh: String, en: String) -> XCUIElement {
        app.descendants(matching: .any)
            .matching(NSPredicate(format: "label == %@ OR label == %@", zh, en)).firstMatch
    }

    private func waitFor(_ app: XCUIApplication, zh: String, en: String, timeout: TimeInterval) throws -> XCUIElement {
        // CONTAINS 匹配：Form/NavigationLink 的可访问 label 常带计数后缀（如 查看留档（1））。
        let el = app.descendants(matching: .any)
            .matching(NSPredicate(format: "label CONTAINS %@ OR label CONTAINS %@", zh, en)).firstMatch
        XCTAssertTrue(el.waitForExistence(timeout: timeout), "\(zh)/\(en) 可见")
        return el
    }

    private func statusText(_ app: XCUIApplication, zh: String, en: String, timeout: TimeInterval) throws {
        let el = app.descendants(matching: .any)
            .matching(NSPredicate(format: "label CONTAINS %@ OR label CONTAINS %@", zh, en)).firstMatch
        XCTAssertTrue(el.waitForExistence(timeout: timeout), "状态文案含 \(zh)/\(en)")
    }

    // MARK: 全流程

    func testConnectPullPushConflictAndArchive() throws {
        let app = try launchApp()
        try connectThroughStub(app)

        // 首轮同步：远端为空 → 创建上传。
        try syncNow(app)
        try statusText(app, zh: "已上传本地内容", en: "Uploaded local content", timeout: 10)
        shot(app, "04-first-sync-pushed")

        // 本地编辑：关设置 → 进标签 → 源码模式追加文字（防抖落盘由引擎 flush 兜底）。
        button(app, zh: "完成", en: "Done").tap()
        let row = app.staticTexts["同步演示"]
        XCTAssertTrue(row.waitForExistence(timeout: 8), "回到标签列表")
        row.tap()
        let fab = sourceModeFab(app)
        XCTAssertTrue(fab.waitForExistence(timeout: 8), "模式切换钮可见")
        fab.tap()
        let source = app.textViews.firstMatch
        XCTAssertTrue(source.waitForExistence(timeout: 8), "源码视图可见")
        source.tap()
        source.typeText("本机未同步改动")
        app.navigationBars.buttons.element(boundBy: 0).tap() // 返回列表（编辑器关闭冲刷待写）
        XCTAssertTrue(row.waitForExistence(timeout: 8))

        // 第二轮同步：stub 注入远端变化 + 本地已变 → 冲突对话框。
        menuButton(app).tap()
        settingsItem(app).tap()
        try syncNow(app)
        let keepRemote = anyElement(app, zh: "保留远端", en: "Keep Remote")
        XCTAssertTrue(keepRemote.waitForExistence(timeout: 10), "冲突卡片出现")
        shot(app, "05-conflict-dialog")
        keepRemote.tap()

        // 冲突出现时设置页自动收起；裁决后重开设置核对状态与留档入口。
        menuButton(app).tap()
        settingsItem(app).tap()
        try statusText(app, zh: "已采用远端内容", en: "Applied remote content", timeout: 10)
        shot(app, "06-kept-remote")

        // 留档管理：查看列表与内容。
        let archiveLink = try waitFor(app, zh: "查看留档", en: "View Archives", timeout: 8)
        archiveLink.tap()
        let entry = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "zakki-")).firstMatch
        XCTAssertTrue(entry.waitForExistence(timeout: 8), "留档条目可见")
        shot(app, "07-archive-list")
        entry.tap()
        let viewer = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "本机未同步改动")).firstMatch
        XCTAssertTrue(viewer.waitForExistence(timeout: 8), "留档内容包含落选的本机版本全文")
        shot(app, "08-archive-content")
    }
}

import XCTest

// M4-2 后端 B（云盘文件夹）模拟器端到端（-ZakkiSyncStub + -ZakkiCloudGrant 预授权）：
// 授权态 → 云端冲突副本双通道枚举 → D13 冲突对话框 → 留档；仅文件授权提示；
// 书签失效（目标被删）→ 失败码与重选引导；真实文件选择器授权（探针 §4 已验证的
// 自容器文件路径）。模拟器未登录 iCloud，书签目标在 App 容器内，不碰真实 iCloud。
final class CloudFolderFlowUITests: XCTestCase {
    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    // MARK: 启动装配

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

    private func writeTemp(_ data: Data, name: String) throws -> URL {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(name)
        try data.write(to: url)
        return url
    }

    private func launch(grant: String? = nil,
                        importPath: URL? = nil,
                        seedPath: URL? = nil,
                        copyPath: URL? = nil) throws -> XCUIApplication {
        var args = ["-ZakkiSyncStub", "-ZakkiProvisionalNotifications"]
        if let importPath { args += ["-ZakkiImportPath", importPath.path] }
        if let grant { args += ["-ZakkiCloudGrant", grant] }
        if let seedPath { args += ["-ZakkiCloudSeedPath", seedPath.path] }
        if let copyPath { args += ["-ZakkiCloudCopyPath", copyPath.path] }
        let app = XCUIApplication()
        app.launchArguments = args
        app.launch()
        return app
    }

    // MARK: 元素定位（验证机中文环境为主，英文回退；沿用 m4-1 约定）

    private func menuButton(_ app: XCUIApplication) -> XCUIElement {
        let zh = app.buttons["菜单"]
        return zh.exists ? zh : app.buttons["Menu"]
    }

    private func settingsItem(_ app: XCUIApplication) -> XCUIElement {
        let zh = app.buttons["设置"]
        return zh.exists ? zh : app.buttons["Settings"]
    }

    private func anyElement(_ app: XCUIApplication, zh: String, en: String) -> XCUIElement {
        app.descendants(matching: .any)
            .matching(NSPredicate(format: "label == %@ OR label == %@", zh, en)).firstMatch
    }

    private func waitFor(_ app: XCUIApplication, zh: String, en: String,
                         timeout: TimeInterval) throws -> XCUIElement {
        let el = app.descendants(matching: .any)
            .matching(NSPredicate(format: "label CONTAINS %@ OR label CONTAINS %@", zh, en)).firstMatch
        XCTAssertTrue(el.waitForExistence(timeout: timeout), "\(zh)/\(en) 可见")
        return el
    }

    private func shot(_ app: XCUIApplication, _ name: String) {
        let attachment = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    private func openSettings(_ app: XCUIApplication) throws {
        menuButton(app).tap()
        settingsItem(app).tap()
        _ = try waitFor(app, zh: "同步后端", en: "Sync Backend", timeout: 8)
    }

    private func syncNow(_ app: XCUIApplication) throws {
        let button = try waitFor(app, zh: "立即同步", en: "Sync Now", timeout: 8)
        button.tap()
    }

    // MARK: 场景一：文件夹授权 → 副本列表 → 冲突 → 保留远端 → 留档

    func testFolderGrantConflictCopiesAndArchive() throws {
        let local = try payload(tabs: [("同步演示", "本机首行\n", "#e5484d")])
        let seed = try payload(tabs: [("云端标签", "云端首行\n", "#30a46c")])
        let copy = try payload(tabs: [("副本标签", "副本首行\n", "#8d44ad")])
        let app = try launch(grant: "auto-folder",
                             importPath: try writeTemp(local, name: "cf-local.json"),
                             seedPath: try writeTemp(seed, name: "cf-seed.json"),
                             copyPath: try writeTemp(copy, name: "cf-copy.json"))
        try openSettings(app)
        // 已连接：目标行（stub 预授权文件夹）与授权形态徽标。
        _ = try waitFor(app, zh: "zakki-cloud-stub", en: "zakki-cloud-stub", timeout: 8)
        _ = try waitFor(app, zh: "文件夹", en: "Folder", timeout: 8)
        shot(app, "cf-01-connected")

        // 云端冲突副本（命名扫描通道）。
        let copiesLink = try waitFor(app, zh: "查看副本", en: "View Copies", timeout: 8)
        copiesLink.tap()
        let entry = app.staticTexts
            .matching(NSPredicate(format: "label CONTAINS %@", "zakki (1).json")).firstMatch
        XCTAssertTrue(entry.waitForExistence(timeout: 8), "命名副本条目可见")
        shot(app, "cf-02-copies")
        entry.tap()
        _ = try waitFor(app, zh: "采用此版本", en: "Use This Version", timeout: 8)
        _ = try waitFor(app, zh: "副本首行", en: "copy line", timeout: 8)
        shot(app, "cf-03-copy-viewer")
        // 关闭查看器（工具栏完成钮；同名按钮可能多层，取最上层 sheet 的）。
        let doneButtons = app.buttons.matching(NSPredicate(format: "label == %@ OR label == %@", "完成", "Done"))
            .allElementsBoundByIndex
        doneButtons.last?.tap()
        app.navigationBars.buttons.element(boundBy: 0).tap() // 返回设置主表单

        // 首轮同步：无基线 + 双方不同 → 冲突对话框；保留远端。
        try syncNow(app)
        let keepRemote = anyElement(app, zh: "保留远端", en: "Keep Remote")
        XCTAssertTrue(keepRemote.waitForExistence(timeout: 15), "冲突卡片出现")
        shot(app, "cf-04-conflict")
        keepRemote.tap()

        // 冲突出现时设置页自动收起；重开设置核对状态。
        try openSettings(app)
        _ = try waitFor(app, zh: "已采用远端内容", en: "Applied remote content", timeout: 10)
        shot(app, "cf-05-adopted")

        // 留档：本地落选版本完整入档。
        let archiveLink = try waitFor(app, zh: "查看留档", en: "View Archives", timeout: 8)
        archiveLink.tap()
        _ = try waitFor(app, zh: "zakki-", en: "zakki-", timeout: 8)
        shot(app, "cf-06-archive")
    }

    // MARK: 场景二：仅文件授权 → 留档留在本机的提示

    func testFileScopeArchiveLocalHint() throws {
        let local = try payload(tabs: [("同步演示", "本机首行\n", "#e5484d")])
        let seed = try payload(tabs: [("云端标签", "云端首行\n", "#30a46c")])
        let app = try launch(grant: "auto-file",
                             importPath: try writeTemp(local, name: "cf-file-local.json"),
                             seedPath: try writeTemp(seed, name: "cf-file-seed.json"))
        try openSettings(app)
        _ = try waitFor(app, zh: "zakki.json", en: "zakki.json", timeout: 8)
        _ = try waitFor(app, zh: "仅文件", en: "File only", timeout: 8)
        _ = try waitFor(app, zh: "冲突留档保留在本机", en: "stay on this device", timeout: 8)
        shot(app, "cf-07-file-scope")

        try syncNow(app)
        let keepRemote = anyElement(app, zh: "保留远端", en: "Keep Remote")
        XCTAssertTrue(keepRemote.waitForExistence(timeout: 15), "冲突卡片出现")
        keepRemote.tap()
        try openSettings(app)
        _ = try waitFor(app, zh: "已采用远端内容", en: "Applied remote content", timeout: 10)
        // 留档仍完整保存在设备本地（仅文件授权写不进同目录 conflicts/）。
        _ = try waitFor(app, zh: "查看留档", en: "View Archives", timeout: 8)
        shot(app, "cf-08-file-adopted")
    }

    // MARK: 场景三：书签失效（目标被删）→ 失败码与重选引导

    func testBookmarkInvalidShowsReselectHint() throws {
        let app = try launch(grant: "missing",
                             importPath: try writeTemp(
                                payload(tabs: [("同步演示", "内容\n", "#e5484d")]),
                                name: "cf-missing.json"))
        try openSettings(app)
        try syncNow(app)
        _ = try waitFor(app, zh: "bookmark-invalid", en: "bookmark-invalid", timeout: 15)
        _ = try waitFor(app, zh: "所选位置已不可用", en: "unavailable", timeout: 8)
        _ = try waitFor(app, zh: "重新选择位置", en: "Reselect Location", timeout: 8)
        shot(app, "cf-09-bookmark-invalid")
    }

    // MARK: 场景四：真实文件选择器授权（跨容器，探针 §4 已验证的可自动化路径）

    // 本 App 沙盒 Documents 未暴露给文件 App（不带 LSSupportsOpeningDocumentsInPlace：
    // 同步目标若可从文件 App 改写，会引入绕过同步引擎的外部修改面）。picker 测试授权
    // D21 探针 App（Probe）留在模拟器上的 Documents 文件——真跨容器授权，比自容器
    // 更接近真机 iCloud 场景。前置：本机装有探针 App（release icloud-probe-assets），
    // 无则 XCTSkip。
    // 跨进程 picker 内容冷启动渲染 15-30s 且偶发白屏（探针 §4），整体超时放宽；
    // 文件 Cell 必须点 Cell 容器（点内层文本不触发 didPickDocumentsAt）。
    func testPickerGrantFileRealFlow() throws {
        let app = try launch(importPath: try writeTemp(
            payload(tabs: [("同步演示", "本机首行\n", "#e5484d")]),
            name: "cf-picker-local.json"))
        try openSettings(app)
        // 未连接：切到云盘后端 → 授权入口（引导选择）。
        let cloudRow = try waitFor(app, zh: "云盘文件夹", en: "Cloud Folder", timeout: 8)
        cloudRow.tap()
        let pickFile = try waitFor(app, zh: "仅选择 zakki.json 文件", en: "Choose zakki.json Only", timeout: 8)
        _ = try waitFor(app, zh: "仅访问你明确选择的位置", en: "only accesses the location", timeout: 8)
        shot(app, "cf-10-pick-entry")
        pickFile.tap()

        // picker 冷启动：等 Cancel + 内容渲染两重信号（探针 §4）。
        let cancel = app.buttons.matching(NSPredicate(format: "label == %@ OR label == %@", "取消", "Cancel"))
            .firstMatch
        XCTAssertTrue(cancel.waitForExistence(timeout: 90), "文件选择器弹出")
        // 紧凑 sheet 无侧栏：底部「最近项目/共享/浏览」tab。picker 跨进程会记住上次
        // 浏览位置：可能恢复在 Probe 容器内部（只见文件）、「我的 iPhone」（见容器）
        // 或最近项目——以「能否看到 probe-note.json」为基准自适应导航。
        let browseTab = app.descendants(matching: .any)
            .matching(NSPredicate(format: "label == %@ OR label == %@", "浏览", "Browse")).firstMatch
        XCTAssertTrue(browseTab.waitForExistence(timeout: 20), "浏览 tab 可见")
        let onMyiPhone = app.descendants(matching: .any)
            .matching(NSPredicate(format: "label == %@ OR label CONTAINS %@", "我的 iPhone", "On My iPhone"))
            .firstMatch
        // 探针 App 容器（前置：本机装有 D21 探针 App）。
        let probeEntry = app.descendants(matching: .any)
            .matching(NSPredicate(format: "label == %@ OR label == %@", "Probe", "Probe")).firstMatch
        let probeNoteCell = app.cells
            .matching(NSPredicate(format: "label CONTAINS %@", "probe-note.json")).firstMatch
        if !probeNoteCell.waitForExistence(timeout: 10) {
            if !probeEntry.exists {
                browseTab.tap()
                if onMyiPhone.waitForExistence(timeout: 15) {
                    onMyiPhone.tap()
                }
                if !probeEntry.waitForExistence(timeout: 15) {
                    browseTab.tap() // 位置网格被恢复态盖住的备份路径
                }
            }
            if probeEntry.waitForExistence(timeout: 5) {
                probeEntry.tap()
            }
        }
        guard probeNoteCell.waitForExistence(timeout: 30) else {
            shot(app, "cf-pd-no-probe-container")
            throw XCTSkip("需要 D21 探针 App（icloud-probe-assets）提供文件共享容器；本机未安装，跳过 picker 实测")
        }
        shot(app, "cf-11-picker-list")
        probeNoteCell.tap()

        // 授权完成：目标行出现（连接态），授权入口消失。
        _ = try waitFor(app, zh: "probe-note.json", en: "probe-note.json", timeout: 15)
        _ = try waitFor(app, zh: "仅文件", en: "File only", timeout: 8)
        shot(app, "cf-12-picker-granted")
    }
}

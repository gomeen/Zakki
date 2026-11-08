import XCTest

// M3-1 提醒交互的模拟器端到端 UI 测试：点胶囊开弹层、弹层清除/完成、
// 勾选推进重复任务、状态条四态。启动参数注入测试数据（JSONSerialization
// 构造 zakki.json，换行经标准转义）；通知授权弹窗经 springboard 点允许。
final class ReminderFlowUITests: XCTestCase {
    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    private func launchApp(payload: Data) throws -> (app: XCUIApplication, dataURL: URL) {
        let url = FileManager.default.temporaryDirectory
            .appendingPathComponent("zakki-ui-\(UUID().uuidString).json")
        try payload.write(to: url)
        let app = XCUIApplication()
        app.launchArguments = ["-ZakkiImportPath", url.path, "-ZakkiProvisionalNotifications"]
        app.launch()
        dismissPermissionPromptIfPresented()
        return (app, url)
    }

    // zakki.json 组装（version 3 结构与桌面 schema 同源）。
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

    private func soonString() -> String {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd HH:mm"
        return f.string(from: Date().addingTimeInterval(300))
    }

    private func demoData() throws -> Data {
        let soon = soonString()
        return try payload(tabs: [
            ("提醒演示", "# 提醒演示\n\n- [ ] 到点响 @\(soon) ~0m\n\n普通行提醒 @\(soon) ~0m", "#e5484d"),
            ("重复任务", "# 重复任务\n\n- [ ] 每日站会 @2026-11-01 09:00 ~5m ^1d", "#30a46c"),
        ])
    }

    private func overflowData() throws -> Data {
        let base = Calendar.current.date(byAdding: .day, value: 2, to: Date())!
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd HH:mm"
        var lines: [String] = []
        for i in 1...70 {
            let t = Calendar.current.date(byAdding: .minute, value: i, to: base)!
            lines.append("- [ ] 条目\(i) @\(f.string(from: t)) ~0m")
        }
        return try payload(tabs: [("溢出", "# 槽位溢出\n\n" + lines.joined(separator: "\n"), "#0090ff")])
    }

    // 首启可能弹通知授权框：springboard 上点允许（双语兼容）。
    private func dismissPermissionPromptIfPresented() {
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for label in ["允许", "Allow"] {
            let button = springboard.buttons[label]
            if button.waitForExistence(timeout: 2) {
                button.tap()
                return
            }
        }
    }

    // 弹层操作按钮（验证机中文环境为主，英文回退）。
    private func doneButton(_ app: XCUIApplication) -> XCUIElement {
        app.buttons["完成"].exists ? app.buttons["完成"] : app.buttons["Done"]
    }

    private func clearButton(_ app: XCUIApplication) -> XCUIElement {
        app.buttons["清除时间"].exists ? app.buttons["清除时间"] : app.buttons["Clear Time"]
    }

    // 含提醒的渲染 TextView（无障碍 value 含令牌文本）。
    private func reminderTextView(_ app: XCUIApplication, containing needle: String) -> XCUIElement {
        app.textViews.matching(NSPredicate(format: "value CONTAINS %@", needle)).firstMatch
    }

    // 点胶囊：附件是图片位（附件字符只占一格），按可见文字宽度定位胶囊坐标
    //（普通行前缀约 5 字 ≈ 83pt，任务行约 3 字 ≈ 56pt；胶囊宽约 160pt，
    // +130 落在两种胶囊中部）。
    private func tapPill(in app: XCUIApplication, textView: XCUIElement) {
        let f = textView.frame
        app.coordinate(withNormalizedOffset: .zero)
            .withOffset(CGVector(dx: f.minX + 130, dy: f.minY + f.height * 0.4))
            .tap()
    }

    // 标签行：List 行的按钮 label 是组合串（名称, 预览, 徽标），用包含匹配。
    private func tabRow(_ app: XCUIApplication, name: String) -> XCUIElement {
        let exact = app.staticTexts[name].firstMatch
        if exact.exists { return exact }
        return app.buttons.matching(NSPredicate(format: "label CONTAINS %@", name)).firstMatch
    }

    private func uncheckedBox(_ app: XCUIApplication) -> XCUIElement {
        let zh = app.staticTexts["未勾选任务"].firstMatch
        return zh.exists ? zh : app.staticTexts["Task, not completed"].firstMatch
    }

    // 点胶囊开弹层（D17：任务行与非任务行同样）→ 完成关闭；编辑器仍在。
    func testPopoverOpensFromPillOnTaskLine() throws {
        let (app, _) = try launchApp(payload: demoData())

        let cell = tabRow(app, name: "提醒演示")
        XCTAssertTrue(cell.waitForExistence(timeout: 8), "标签列表就绪")
        cell.tap()

        let row = reminderTextView(app, containing: "到点响")
        XCTAssertTrue(row.waitForExistence(timeout: 8), "任务行可见")
        tapPill(in: app, textView: row)

        XCTAssertTrue(doneButton(app).waitForExistence(timeout: 5), "弹层完成按钮")
        XCTAssertTrue(clearButton(app).exists, "弹层清除按钮")
        doneButton(app).tap()
        XCTAssertTrue(reminderTextView(app, containing: "到点响").waitForExistence(timeout: 5),
                      "完成写回后胶囊仍在")
    }

    // 弹层清除时间：令牌从文档消失（源码模式读 raw 文档验证写回，端到端）。
    func testPopoverClearRemovesToken() throws {
        let (app, _) = try launchApp(payload: demoData())

        let cell = tabRow(app, name: "提醒演示")
        XCTAssertTrue(cell.waitForExistence(timeout: 8))
        cell.tap()
        let row = reminderTextView(app, containing: "普通行提醒")
        XCTAssertTrue(row.waitForExistence(timeout: 8))
        tapPill(in: app, textView: row)
        XCTAssertTrue(clearButton(app).waitForExistence(timeout: 5))
        clearButton(app).tap()
        // 切到源码模式读 raw：令牌已从文档移除，正文保留。
        let fab = sourceModeFab(app)
        XCTAssertTrue(fab.waitForExistence(timeout: 5), "模式切换钮可见")
        fab.tap()
        let source = app.textViews.firstMatch
        XCTAssertTrue(source.waitForExistence(timeout: 5), "源码视图可见")
        let raw = source.value as? String ?? ""
        XCTAssertFalse(raw.contains("@\(soonString())"), "清除后文档 raw 不再含该令牌")
        XCTAssertTrue(raw.contains("普通行提醒"), "正文保留")
    }

    // 右上模式切换钮（毛玻璃 FAB，无障碍树里由图标 Image 承载 label）。
    private func sourceModeFab(_ app: XCUIApplication) -> XCUIElement {
        let zh = app.images["切换到源码"]
        if zh.exists { return zh }
        let en = app.images["Switch to source"]
        if en.exists { return en }
        return app.descendants(matching: .any)
            .matching(NSPredicate(format: "label == %@ OR label == %@",
                                  "切换到源码", "Switch to source")).firstMatch
    }

    // 重复任务勾选即推进：锚点改写、保持未完成（D17/§3.3）。
    func testRecurringTaskAdvancesOnCheck() throws {
        let (app, _) = try launchApp(payload: demoData())

        let cell = tabRow(app, name: "重复任务")
        XCTAssertTrue(cell.waitForExistence(timeout: 8))
        cell.tap()

        let row = reminderTextView(app, containing: "每日站会")
        XCTAssertTrue(row.waitForExistence(timeout: 8))

        let checkbox = uncheckedBox(app)
        XCTAssertTrue(checkbox.waitForExistence(timeout: 5), "任务方框可见（无障碍元素）")
        checkbox.tap()

        // 推进后仍是未完成、锚点改写：源码模式读 raw 验证写回（端到端）。
        XCTAssertTrue(uncheckedBox(app).waitForExistence(timeout: 5), "推进后保持未完成")
        let fab = sourceModeFab(app)
        XCTAssertTrue(fab.waitForExistence(timeout: 5), "模式切换钮可见")
        fab.tap()
        let source = app.textViews.firstMatch
        XCTAssertTrue(source.waitForExistence(timeout: 5), "源码视图可见")
        let raw = source.value as? String ?? ""
        XCTAssertFalse(raw.contains("2026-11-01"), "原锚点已改写（原日期不再出现）")
        XCTAssertTrue(raw.contains("^1d"), "重复段原文保留")
        XCTAssertTrue(raw.contains("~5m"), "提醒段保留")
        XCTAssertFalse(raw.contains("- [x] "), "保持未完成（未标记 [x]）")
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd"
        let today = f.string(from: Date())
        let tomorrow = f.string(from: Calendar.current.date(byAdding: .day, value: 1, to: Date())!)
        XCTAssertTrue(raw.contains(today) || raw.contains(tomorrow),
                      "锚点推进到当前实例之后（今天或明天的实例）")
    }

    // 状态条：槽位溢出时显示待续排（§3.4）。
    func testStatusBarShowsRenewalState() throws {
        let (app, _) = try launchApp(payload: overflowData())
        XCTAssertTrue(tabRow(app, name: "溢出").waitForExistence(timeout: 8))
        let zh = app.staticTexts["有提醒待续排，打开应用继续"]
        let en = app.staticTexts["More pending — open app to continue"]
        XCTAssertTrue(zh.waitForExistence(timeout: 8) || en.exists, "状态条显示待续排")
    }
}

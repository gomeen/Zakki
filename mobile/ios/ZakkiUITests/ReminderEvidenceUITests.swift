import XCTest

// M3-1 模拟器实测截图取证：走通弹层/推进/状态条/到点横幅，逐屏截图存
// /tmp/zakki-m3-e2e/shots（release 资产用）。断言从简——流程正确性由
// ReminderFlowUITests 覆盖，这里只负责留档。
final class ReminderEvidenceUITests: XCTestCase {
    override func setUpWithError() throws {
        continueAfterFailure = true
    }

    private func payload(tabs: [(String, String, String)]) throws -> Data {
        let obj: [String: Any] = [
            "version": 3,
            "active": 0,
            "tabs": tabs.enumerated().map { i, tab in
                ["id": i + 1, "name": tab.0, "text": tab.1, "color": tab.2]
            },
        ]
        return try JSONSerialization.data(withJSONObject: obj, options: [.sortedKeys])
    }

    private func shot(_ name: String) {
        let png = XCUIScreen.main.screenshot().pngRepresentation
        let dir = URL(fileURLWithPath: "/tmp/zakki-m3-e2e/shots", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        try? png.write(to: dir.appendingPathComponent(name))
    }

    private func launch(_ payload: Data) -> XCUIApplication {
        let url = FileManager.default.temporaryDirectory
            .appendingPathComponent("zakki-shot-\(UUID().uuidString).json")
        try? payload.write(to: url)
        let app = XCUIApplication()
        app.launchArguments = ["-ZakkiImportPath", url.path, "-ZakkiProvisionalNotifications"]
        app.launch()
        // 授权弹窗（如出现）点允许。
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for label in ["允许", "Allow"] where springboard.buttons[label].waitForExistence(timeout: 2) {
            springboard.buttons[label].tap()
            break
        }
        return app
    }

    // 全流程：状态条（近未来提醒 → 已排定）→ 编辑器胶囊 → 弹层 → 完成回写
    // → 勾选推进 → 源码验证 → 到点前台横幅。
    func testCaptureEvidence() throws {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd HH:mm"
        let soon = f.string(from: Date().addingTimeInterval(70))
        let tab1 = ("提醒演示", "# 提醒演示\n\n- [ ] 到点响 @\(soon) ~0m\n\n普通行提醒 @\(soon) ~5m", "#e5484d")
        let tab2 = ("重复任务", "# 重复任务\n\n- [ ] 每日站会 @2026-11-01 09:00 ~5m ^1d", "#30a46c")
        let app = launch(try payload(tabs: [tab1, tab2]))

        // 1) 状态条：已排定 + 下次触发时刻。
        XCTAssertTrue(app.buttons["提醒演示"].waitForExistence(timeout: 10)
                      || app.staticTexts["提醒演示"].waitForExistence(timeout: 2))
        Thread.sleep(forTimeInterval: 1.5)
        shot("01-tablist-status-scheduled.png")

        // 2) 编辑器与胶囊。
        let row = app.staticTexts["提醒演示"].exists ? app.staticTexts["提醒演示"] : app.buttons["提醒演示"]
        row.tap()
        Thread.sleep(forTimeInterval: 1)
        shot("02-editor-pills.png")

        // 3) 弹层。
        let taskRow = app.textViews.matching(NSPredicate(format: "value CONTAINS %@", "到点响")).firstMatch
        XCTAssertTrue(taskRow.waitForExistence(timeout: 8))
        let frame = taskRow.frame
        app.coordinate(withNormalizedOffset: .zero)
            .withOffset(CGVector(dx: frame.minX + 130, dy: frame.minY + frame.height * 0.4))
            .tap()
        let done = app.buttons["完成"].exists ? app.buttons["完成"] : app.buttons["Done"]
        XCTAssertTrue(done.waitForExistence(timeout: 6))
        Thread.sleep(forTimeInterval: 0.6)
        shot("03-popover.png")
        done.tap()

        // 4) 勾选推进（切到重复任务标签）。
        let back = app.navigationBars.buttons.element(boundBy: 0)
        if back.exists { back.tap() }
        let tab2Row = app.staticTexts["重复任务"].exists ? app.staticTexts["重复任务"] : app.buttons["重复任务"]
        XCTAssertTrue(tab2Row.waitForExistence(timeout: 8))
        tab2Row.tap()
        let box = app.staticTexts["未勾选任务"].exists
            ? app.staticTexts["未勾选任务"] : app.staticTexts["Task, not completed"]
        XCTAssertTrue(box.waitForExistence(timeout: 8))
        box.tap()
        Thread.sleep(forTimeInterval: 1)
        shot("04-advance-checked.png")

        // 源码验证推进结果。
        let fab = app.images["切换到源码"].exists ? app.images["切换到源码"]
            : app.images["Switch to source"]
        if fab.waitForExistence(timeout: 5) {
            fab.tap()
            Thread.sleep(forTimeInterval: 0.8)
            shot("05-advance-source-raw.png")
        }

        // 5) 到点前台横幅：回实时模式等待 fire（约 70 秒后）。
        let backFab = app.images["切换到实时预览"].exists ? app.images["切换到实时预览"]
            : app.images["Switch to preview"]
        if backFab.waitForExistence(timeout: 5) {
            backFab.tap()
        }
        let wait = Date().addingTimeInterval(75)
        Thread.sleep(forTimeInterval: max(5, wait.timeIntervalSinceNow))
        shot("06-foreground-banner.png")
    }
}

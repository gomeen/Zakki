import XCTest
import UserNotifications
@testable import Zakki

// 通知中心 spy：记录排链/撤销/授权调用，供调度引擎断言。
private final class SpyNotificationCenter: NotificationScheduling {
    var added: [UNNotificationRequest] = []
    var removedAllPendingCount = 0
    var removedPendingIDs: [[String]] = []
    var removedDeliveredIDs: [[String]] = []
    var stubbedStatus: UNAuthorizationStatus = .authorized
    var requestAuthorizationGranted = true
    var requestAuthorizationCalls = 0

    func requestAuthorization(provisional: Bool, completion: @escaping (Bool) -> Void) {
        requestAuthorizationCalls += 1
        if requestAuthorizationGranted { stubbedStatus = provisional ? .provisional : .authorized }
        completion(requestAuthorizationGranted)
    }

    func fetchAuthorizationStatus(completion: @escaping (UNAuthorizationStatus) -> Void) {
        completion(stubbedStatus)
    }

    func removeAllPending() {
        removedAllPendingCount += 1
        added.removeAll()
    }

    func removePending(identifiers: [String]) {
        removedPendingIDs.append(identifiers)
    }

    func removeDelivered(identifiers: [String]) {
        removedDeliveredIDs.append(identifiers)
    }

    func add(_ request: UNNotificationRequest, completion: @escaping (Error?) -> Void) {
        added.append(request)
        completion(nil)
    }
}

// 调度引擎测试（M3-1）：对账重排、64 槽续排、权限受限清理、待同步状态、
// 请求内容（单次触发器 + 标题/正文）。时钟注入，通知中心注入 spy。
// 沙盒路径注入临时目录，绝不写真实用户目录。
@MainActor
final class ReminderSchedulerTests: XCTestCase {
    private var center: SpyNotificationCenter!
    private var model: NotesModel!
    private var scheduler: ReminderScheduler!
    private var sandboxURL: URL!

    override func setUp() {
        super.setUp()
        NotesCore.setLocation(TimeZone.current.identifier)
        center = SpyNotificationCenter()
        sandboxURL = FileManager.default.temporaryDirectory
            .appendingPathComponent("zakki-sched-tests-\(UUID().uuidString)")
            .appendingPathComponent("zakki.json")
        model = NotesModel(sandboxURL: sandboxURL)
        scheduler = ReminderScheduler(center: center, clock: { 1_800_000_000_000 })
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: sandboxURL.deletingLastPathComponent())
        super.tearDown()
    }

    // 对账流程经授权回调 + MainActor Task 两跳：排空主线程已入队工作再断言。
    private func settleMain(hops: Int = 2) {
        for _ in 0..<hops {
            let exp = expectation(description: "main settle")
            DispatchQueue.main.async { exp.fulfill() }
            wait(for: [exp], timeout: 2)
        }
    }

    private func installTab(_ text: String) {
        let payload: [String: Any] = [
            "version": 3, "active": 0,
            "tabs": [["id": 1, "name": "工作", "text": "", "color": "#ff0000"]],
        ]
        let data = try! JSONSerialization.data(withJSONObject: payload)
        _ = model.importFile(at: writeTemp(data))
        model.updateTabText(text, tabID: 1)
        model.flushPendingSave()
    }

    private func writeTemp(_ data: Data) -> URL {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("import-\(UUID().uuidString).json")
        try? data.write(to: url)
        return url
    }

    private var future: String { "2027-01-20 10:00" } // 相对固定时钟的未来时刻

    // MARK: 对账排链

    func testReconcileSchedulesFutureReminders() {
        installTab("买牛奶 @\(future) ~10m")
        scheduler.attach(model: model)
        settleMain()
        XCTAssertEqual(center.added.count, 1)
        let request = center.added[0]
        XCTAssertEqual(request.content.title, "Zakki")
        XCTAssertEqual(request.content.body, "工作 · 买牛奶", "标题 · 正文组装对齐桌面 reminderBody")
        XCTAssertTrue(request.identifier.hasPrefix("zakki-reminder-"), "稳定标识前缀")
        let trigger = request.trigger as? UNTimeIntervalNotificationTrigger
        XCTAssertNotNil(trigger, "单次实例触发器（D16 不用 repeating）")
        XCTAssertFalse(trigger?.repeats ?? true)
        XCTAssertEqual(scheduler.status.scheduledCount, 1)
        XCTAssertTrue(scheduler.status.authorized ?? false)
        XCTAssertFalse(scheduler.status.needsRenewal)
    }

    func testReconcileMergesAcrossTabs() {
        installTab("任务一 @\(future) ~10m")
        model.addTab(name: "B", color: "#00ff00")
        model.flushPendingSave()
        let secondID = model.snapshot?.tabs?.last?.id ?? 2
        model.updateTabText("任务二 @2027-01-20 10:05 ~1h", tabID: secondID)
        model.flushPendingSave()
        scheduler.attach(model: model)
        settleMain()
        XCTAssertEqual(center.added.count, 2, "跨标签合并后共同参与槽位分配")
    }

    func testReconcileOverflowAddsRenewalNotice() {
        // 70 条独立任务行（任务块单行成块，各自可调度）：63 排定 + 1 续排。
        var lines: [String] = []
        for i in 1...70 {
            lines.append(String(format: "- [ ] 条目%d @2027-02-01 %02d:%02d ~0m", i, i / 60, i % 60))
        }
        installTab(lines.joined(separator: "\n"))
        scheduler.attach(model: model)
        settleMain()
        XCTAssertEqual(center.added.count, 64)
        let renewal = center.added.first { $0.identifier == ReminderPlanner.renewalNotificationID }
        XCTAssertNotNil(renewal, "最后槽保留续排通知")
        XCTAssertEqual(renewal?.content.body, Strings.renewalNoticeBody)
        XCTAssertTrue(scheduler.status.needsRenewal)
    }

    func testReconcileClearsDeliveredRenewalNotice() {
        installTab("任务 @\(future) ~10m")
        scheduler.attach(model: model)
        settleMain()
        XCTAssertTrue(center.removedDeliveredIDs.contains([ReminderPlanner.renewalNotificationID]),
                      "打开时清理续排通知并补足")
    }

    // MARK: 授权状态

    func testDeniedClearsPendingAndShowsLimited() {
        center.stubbedStatus = .denied
        installTab("任务 @\(future) ~10m")
        scheduler.attach(model: model)
        settleMain()
        XCTAssertEqual(center.added.count, 0, "权限受限不排定")
        XCTAssertGreaterThanOrEqual(center.removedAllPendingCount, 1, "受限清空待发队列")
        XCTAssertEqual(scheduler.status.authorized, false)
        XCTAssertTrue(scheduler.status.isLimited)
    }

    func testProvisionalAuthorizationStillSchedules() {
        // 免费账号可用 provisional 静默授权：状态按已授权口径排链（e2e 依赖）。
        center.stubbedStatus = .provisional
        installTab("任务 @\(future) ~10m")
        scheduler.attach(model: model)
        settleMain()
        XCTAssertEqual(center.added.count, 1, "provisional 视作已授权")
        XCTAssertTrue(scheduler.status.authorized ?? false)
    }

    func testNotDeterminedRequestsAuthorizationThenReconciles() {
        center.stubbedStatus = .notDetermined
        installTab("任务 @\(future) ~10m")
        scheduler.attach(model: model)
        settleMain(hops: 3) // 授权请求 → 二次对账 → 应用
        XCTAssertEqual(center.requestAuthorizationCalls, 1)
        XCTAssertEqual(center.added.count, 1, "授权成功后完成本轮对账")
    }

    // MARK: 状态四态

    func testPendingSyncReflectsUnflushedWrites() {
        installTab("任务 @\(future) ~10m")
        scheduler.attach(model: model)
        settleMain()
        XCTAssertFalse(scheduler.status.pendingSync)
        // 有未落盘改动时状态进入待同步（写盘成功 ≠ 系统已接受）。
        model.updateTabText("任务 @\(future) ~30m", tabID: 1)
        scheduler.reconcile()
        settleMain()
        XCTAssertTrue(scheduler.status.pendingSync)
        model.flushPendingSave()
        scheduler.reconcile()
        settleMain()
        XCTAssertFalse(scheduler.status.pendingSync)
    }

    // MARK: 时间敏感与内容

    func testRequestUsesTimeSensitiveLevelAsAttempt() {
        let occurrence = ReminderOccurrence(tabID: 1, tabTitle: "工作", line: 0,
                                            dueMs: 1_800_000_600_000, alertMs: 600_000,
                                            fireAtMs: 1_800_000_000_000, body: "正文", recurRaw: "")
        let content = ReminderScheduler.content(for: occurrence)
        XCTAssertEqual(content.interruptionLevel, .timeSensitive, "尝试请求 Time Sensitive（R2，无 entitlement 由系统降级）")
        XCTAssertEqual(content.body, "工作 · 正文", "标题 · 正文组装对齐桌面 reminderBody")
        // 能力旗标：默认构建无 entitlement 声明，状态如实按未验证展示。
        XCTAssertFalse(ReminderScheduler.timeSensitiveActive)
    }

    func testTriggerClampsPastFireAt() {
        let trigger = ReminderScheduler.intervalTrigger(fireAtMs: 1) // 远过去的时刻
        let interval = (trigger as? UNTimeIntervalNotificationTrigger)?.timeInterval
        XCTAssertEqual(interval ?? 0, 0.2, accuracy: 0.001, "过去时刻钳到最小正值避免系统拒绝")
    }

    // MARK: 编辑防抖对账（attach 后快照变化触发重排）

    func testSnapshotChangeReconcilesAgain() {
        installTab("任务一 @\(future) ~10m")
        scheduler.attach(model: model)
        settleMain()
        XCTAssertEqual(center.added.count, 1)
        // 编辑取消提醒（去掉 ~ 档位）：对账后不再排定。防抖需真实等待。
        model.updateTabText("任务一 @\(future)", tabID: 1)
        model.flushPendingSave()
        let debounce = expectation(description: "debounce")
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.6) { debounce.fulfill() }
        wait(for: [debounce], timeout: 2)
        settleMain(hops: 2)
        XCTAssertEqual(center.added.count, 0, "无档位令牌不调度（D9）")
    }
}

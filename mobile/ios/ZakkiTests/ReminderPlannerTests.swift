import XCTest
import Zakkicore
@testable import Zakki

// 提醒计划纯逻辑测试（M3-1）：采集口径（~必带、勾选排除、未知规则不调度）、
// 重复展开、64 槽分配（最早 fireAt、平局稳定序、续排槽）、通知正文投影、
// 完成推进（Advance）与 due 格式化。时钟全部注入，不依赖真实当前时刻。
@MainActor
final class ReminderPlannerTests: XCTestCase {
    private let nowMs: Int64 = 1_800_000_000_000 // 2027-01-15 12:00 UTC 附近固定锚

    override func setUp() {
        super.setUp()
        // core 的时间语义依赖时区：测试进程内显式注入设备时区（生产由
        // TimeZoneSync 在启动时完成；单测不走 App 启动）。
        NotesCore.setLocation(TimeZone.current.identifier)
    }

    private func tab(_ text: String, id: UInt64 = 1, name: String = "工作") -> NotesTab {
        NotesTab(id: id, name: name, text: text, color: "#ff0000")
    }

    // MARK: 采集口径

    func testCollectRequiresAlertTier() {
        // 无 ~ 档位的令牌不调度（D9 语义）。
        let occurrences = ReminderPlanner.collect(tabs: [tab("明天 @2027-01-20 10:00")], nowMs: nowMs)
        XCTAssertTrue(occurrences.isEmpty)
    }

    func testCollectSingleFutureInstance() {
        let occurrences = ReminderPlanner.collect(tabs: [tab("周会 @2027-01-20 10:00 ~10m")], nowMs: nowMs)
        XCTAssertEqual(occurrences.count, 1)
        let o = occurrences[0]
        XCTAssertEqual(o.alertMs, 600_000)
        XCTAssertEqual(o.fireAtMs, o.dueMs - 600_000)
        XCTAssertGreaterThan(o.fireAtMs, nowMs, "只排未来触发（R1 首启不补发）")
        XCTAssertEqual(o.recurRaw, "")
    }

    func testCollectExcludesCheckedTasks() {
        let occurrences = ReminderPlanner.collect(tabs: [tab("- [x] 交付 @2027-01-20 10:00 ~10m")], nowMs: nowMs)
        XCTAssertTrue(occurrences.isEmpty, "已完成任务不再排定")
    }

    func testCollectUnknownRecurSkipped() {
        // 未知规则整簇不调度（ADR-0002 / 规格 §3.3）。
        let occurrences = ReminderPlanner.collect(tabs: [tab("任务 @2027-01-20 10:00 ~10m ^hello")], nowMs: nowMs)
        XCTAssertTrue(occurrences.isEmpty)
    }

    func testCollectRecurringExpandsIntoHorizon() {
        // ^1d 锚点在过去：窗口 (now, now+60d] 内应有每天实例。
        let occurrences = ReminderPlanner.collect(tabs: [tab("晨会 @2026-12-01 09:00 ~5m ^1d")], nowMs: nowMs)
        XCTAssertFalse(occurrences.isEmpty)
        XCTAssertTrue(occurrences.allSatisfy { $0.fireAtMs > nowMs })
        XCTAssertTrue(occurrences.allSatisfy { $0.recurRaw == "^1d" })
        // 实例升序且间隔恰为 1 天；窗口端点随时区钟面差异 ±1，总数预算放宽。
        let dues = occurrences.map(\.dueMs)
        XCTAssertEqual(dues, dues.sorted())
        if dues.count >= 2 {
            let day: Int64 = 24 * 60 * 60 * 1000
            XCTAssertEqual(dues[1] - dues[0], day)
        }
        XCTAssertTrue(occurrences.count >= 58 && occurrences.count <= 64,
                      "60 天地平线内每天一实例（时区钟面差异 ±1）: \(occurrences.count)")
    }

    func testCollectRecurringPastDueOutsideHorizon() {
        // ended 规则（结束日期已过）不产生实例。
        let occurrences = ReminderPlanner.collect(tabs: [tab("已结束 @2026-12-01 09:00 ~5m ^1d<2026-12-10")], nowMs: nowMs)
        XCTAssertTrue(occurrences.isEmpty)
    }

    // MARK: 64 槽分配

    private func occurrence(fireAtMs: Int64, tabID: UInt64 = 1, line: Int = 0) -> ReminderOccurrence {
        ReminderOccurrence(tabID: tabID, tabTitle: "", line: line,
                           dueMs: fireAtMs + 600_000, alertMs: 600_000,
                           fireAtMs: fireAtMs, body: "body", recurRaw: "")
    }

    func testAllocateAllFitNoRenewal() {
        let occurrences = (0..<64).map { occurrence(fireAtMs: nowMs + Int64($0 + 1) * 60_000) }
        let plan = ReminderPlanner.allocate(occurrences, nowMs: nowMs)
        XCTAssertFalse(plan.overflow)
        XCTAssertNil(plan.renewal)
        XCTAssertEqual(plan.scheduled.count, 64)
    }

    func testAllocateOverflowKeepsEarliest63PlusRenewal() {
        let occurrences = (0..<70).map { occurrence(fireAtMs: nowMs + Int64($0 + 1) * 60_000) }
        let plan = ReminderPlanner.allocate(occurrences, nowMs: nowMs)
        XCTAssertTrue(plan.overflow)
        XCTAssertEqual(plan.scheduled.count, 63, "溢出时给最早实例至多 63 槽")
        XCTAssertEqual(plan.scheduled.first?.fireAtMs, nowMs + 60_000, "最早触发优先")
        XCTAssertEqual(plan.scheduled.last?.fireAtMs, nowMs + 63 * 60_000)
        XCTAssertNotNil(plan.renewal, "最后槽保留续排通知")
        XCTAssertEqual(plan.renewal?.fireAtMs, nowMs + 63 * 60_000 + ReminderPlanner.renewalDelayMs)
    }

    func testAllocateTieBreaksByStableIdentity() {
        // 同一 fireAt 的多条：按 tabID→行→due 稳定排序，不让先扫描的条目独占。
        let occurrences = [
            occurrence(fireAtMs: nowMs + 60_000, tabID: 2, line: 1),
            occurrence(fireAtMs: nowMs + 60_000, tabID: 1, line: 5),
            occurrence(fireAtMs: nowMs + 60_000, tabID: 1, line: 2),
        ]
        let plan = ReminderPlanner.allocate(occurrences, nowMs: nowMs)
        XCTAssertEqual(plan.scheduled.map { ReminderPlanner.identityTieBreaker($0) },
                       plan.scheduled.map { ReminderPlanner.identityTieBreaker($0) }.sorted())
        XCTAssertEqual(plan.scheduled[0].tabID, 1)
        XCTAssertEqual(plan.scheduled[0].line, 2)
        XCTAssertEqual(plan.scheduled[1].tabID, 1)
        XCTAssertEqual(plan.scheduled[1].line, 5)
        XCTAssertEqual(plan.scheduled[2].tabID, 2)
    }

    // MARK: 通知正文投影

    func testBodyStripsTokenAndMarkdown() {
        let blocks = LiveBlocks.split("买 **牛奶** 和 `鸡蛋` [链接](https://example.com) @2027-01-20 10:00 ~10m 尾巴").blocks
        let body = ReminderPlanner.notificationBody(for: blocks[0])
        XCTAssertEqual(body, "买 牛奶 和 鸡蛋 链接 尾巴", "令牌与标记剥离、链接取显示文字、空白折叠")
    }

    func testBodyMultiLineParagraph() {
        let blocks = LiveBlocks.split("第一行 @2027-01-20 10:00 ~10m\n第二行").blocks
        let body = ReminderPlanner.notificationBody(for: blocks[0])
        XCTAssertEqual(body, "第一行 第二行", "软换行段合并为空格分隔的可读正文")
    }

    // MARK: 完成推进

    func testAdvanceReturnsNextInstance() {
        let anchorMs = nowMs - 3 * 24 * 60 * 60 * 1000 // 三天前锚点
        let next = ReminderPlanner.advance(recurRaw: "^1d", anchorMs: anchorMs, nowMs: nowMs)
        XCTAssertNotNil(next)
        // 下一实例应不晚于 now 之后一天内（当前实例之后的首次实例）。
        let day: Int64 = 24 * 60 * 60 * 1000
        XCTAssertLessThanOrEqual(next! - nowMs, day)
        XCTAssertGreaterThan(next!, nowMs - 1000, "Advance 结果不得仍在过去（当前实例之后）")
    }

    func testAdvanceEndedRuleReturnsNil() {
        let anchorMs = nowMs - 30 * 24 * 60 * 60 * 1000
        let next = ReminderPlanner.advance(recurRaw: "^1d<2026-12-31", anchorMs: anchorMs, nowMs: nowMs)
        XCTAssertNil(next, "越结束日期返回 nil（调用方标记完成）")
    }

    func testAdvanceInvalidRuleReturnsNil() {
        XCTAssertNil(ReminderPlanner.advance(recurRaw: "^hello", anchorMs: nowMs, nowMs: nowMs))
    }

    // MARK: due 格式化往返

    func testFormatDueMsRoundTrip() {
        let ms: Int64 = 1_800_123_600_000
        let formatted = ReminderPlanner.formatDueMs(ms)
        var err: NSError?
        // 写回格式经绑定校验（同 core decodeSuffix 的口径）。
        _ = ZakkicoreJoinTimeSuffix("任务",
            "{\"due\":\"\(formatted)\",\"dueMs\":0,\"alertMs\":0,\"hasAlert\":false,\"recur\":\"\"}", &err)
        XCTAssertNil(err, "formatDueMs 产出必须能通过绑定严格校验: \(formatted)")
    }
}

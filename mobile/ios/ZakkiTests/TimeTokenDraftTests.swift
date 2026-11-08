import XCTest
import Zakkicore
@testable import Zakki

// 时间弹层草稿测试（M3-1，桌面 live_timerecur 语义移植）：播种、预设识别、
// token()「未改保留 ^ 原文」、自定义组装与非法回落、suffix JSON 白名单写回。
@MainActor
final class TimeTokenDraftTests: XCTestCase {
    override func setUp() {
        super.setUp()
        NotesCore.setLocation(TimeZone.current.identifier)
    }

    private func hit(_ token: String) -> TimeTokenHit {
        guard let h = EditorDocument.findTimeToken(in: token, protectedJSON: "") else {
            return XCTFatalHit(token)
        }
        return h
    }

    private func XCTFatalHit(_ token: String) -> TimeTokenHit {
        // 测试输入都是合法令牌；识别失败直接失败用例。
        XCTFail("令牌识别失败: \(token)")
        return TimeTokenHit(range: NSRange(location: 0, length: 0), raw: token,
                            suffix: TimeSuffixInfo(due: "2027-01-20 10:00", dueMs: 0, hasAlert: false, alertMs: 0, recur: ""))
    }

    // MARK: 播种与预设识别

    func testSeedFromToken() {
        let draft = TimeTokenDraft(hit: hit("@2027-01-20 10:00 ~10m ^1d"))
        XCTAssertEqual(TimeTokenDraft.formatDue(draft.due), "2027-01-20 10:00")
        XCTAssertTrue(draft.hasAlert)
        XCTAssertEqual(draft.alertMs, 600_000)
        XCTAssertEqual(draft.recur.preset, "^1d", "标准预设识别")
    }

    func testSeedWorkdayPresetAndNoAlert() {
        let draft = TimeTokenDraft(hit: hit("@2027-01-20 10:00 ^w@1-5"))
        XCTAssertEqual(draft.recur.preset, "^w@1-5")
        XCTAssertFalse(draft.hasAlert)
    }

    func testSeedCustomRule() {
        let draft = TimeTokenDraft(hit: hit("@2027-01-20 10:00 ^2mo<2027-06-30"))
        XCTAssertEqual(draft.recur.preset, "custom")
        XCTAssertEqual(draft.recur.every, 2)
        XCTAssertEqual(draft.recur.unit, "mo")
        XCTAssertTrue(draft.recur.hasEnd)
        XCTAssertEqual(RecurDraft.dayString(from: draft.recur.endDay), "2027-06-30")
    }

    func testSeedCustomWeekdays() {
        let draft = TimeTokenDraft(hit: hit("@2027-01-20 10:00 ^1w@1,3"))
        XCTAssertEqual(draft.recur.preset, "custom", "带周几集的规则不在预设集内")
        XCTAssertEqual(draft.recur.weekdays, [1, 3])
    }

    func testUnknownRuleTokenStaysPlainText() {
        // 非法/未知 ^ 段整簇不识别（core 现行语义）：不收胶囊、无弹层入口。
        let text = "任务 @2027-01-20 10:00 ~10m ^hello"
        XCTAssertNil(EditorDocument.findTimeToken(in: text, protectedJSON: ""),
                     "未知规则整簇按普通文本保留")
        let blocks = LiveBlocks.split(text).blocks
        XCTAssertNil(blocks[0].timeTokenRaw, "无胶囊即无弹层入口")
    }

    // MARK: token()（桌面 timeRecurDraft.token 同构）

    func testTokenUnchangedKeepsOriginalRaw() {
        // 未改保留原文：非规范形式（周几乱序）canonical 相等时回写原文，
        // 桌面 timeRecurDraft.token 同口径。
        let original = "^1w@3,1<2027-06-30"
        let draft = TimeTokenDraft(hit: hit("@2027-01-20 10:00 \(original)"))
        XCTAssertEqual(draft.recur.token(), original, "canonical 相等时保留原文")
    }

    func testTokenNoneClears() {
        var draft = TimeTokenDraft(hit: hit("@2027-01-20 10:00 ~10m ^1d"))
        draft.recur.preset = "none"
        XCTAssertEqual(draft.recur.token(), "", "preset=none 清除重复段")
    }

    func testTokenCustomBuilds() {
        var draft = TimeTokenDraft(hit: hit("@2027-01-20 10:00 ^1d"))
        draft.recur.preset = "custom"
        draft.recur.every = 3
        draft.recur.unit = "mo"
        draft.recur.hasEnd = true
        draft.recur.endDay = RecurDraft.dayDate(from: "2027-12-31")!
        XCTAssertEqual(draft.recur.token(), "^3mo<2027-12-31")
    }

    func testTokenCustomWeekdaysOnlyWeeklyEvery1() {
        var draft = TimeTokenDraft(hit: hit("@2027-01-20 10:00 ^1d"))
        draft.recur.preset = "custom"
        draft.recur.every = 1
        draft.recur.unit = "w"
        draft.recur.weekdays = [5, 1]
        XCTAssertEqual(draft.recur.token(), "^1w@1,5", "周几集升序输出（桌面同款 ^1w 前缀）")
    }

    func testTokenCustomEveryAboveWeekdropsWeekdays() {
        // 桌面同构：every>1 时周几集不进 token（周几集仅支持每周，UI 只在
        // every==1 时展示周几开关；草稿层同桌面代码不做额外钳制）。
        var draft = TimeTokenDraft(hit: hit("@2027-01-20 10:00 ^1d"))
        draft.recur.preset = "custom"
        draft.recur.every = 2
        draft.recur.unit = "w"
        draft.recur.weekdays = [1, 3]
        XCTAssertEqual(draft.recur.token(), "^2w")
    }

    func testTokenInvalidCustomReturnsEmpty() {
        // 组装结果无法通过 core 校验时返回空（调用方保留原段，桌面 parity）。
        var draft = TimeTokenDraft(hit: hit("@2027-01-20 10:00 ^1d"))
        draft.recur.preset = "custom"
        draft.recur.every = 0
        draft.recur.unit = ""
        XCTAssertEqual(draft.recur.token(), "")
    }

    // MARK: suffix JSON 写回

    func testSuffixJSONRoundTrip() {
        let draft = TimeTokenDraft(hit: hit("@2027-01-20 10:00 ~30m ^1w"))
        let json = draft.suffixJSON()
        // 写回经绑定严格校验（白名单/开关矛盾/规则合法性）。
        var err: NSError?
        _ = ZakkicoreReplaceTimeToken("会议 @2027-01-20 10:00", json, "", &err)
        XCTAssertNil(err, "草稿 suffix JSON 必须通过绑定校验: \(json)")
    }

    func testAlertTierWhitelistComesFromBinding() {
        var err: NSError?
        let out = ZakkicoreAlertTiers(&err)
        XCTAssertNil(err)
        struct Tier: Decodable { let suffix: String; let ms: Int64 }
        let tiers = try! JSONDecoder().decode([Tier].self, from: out.data(using: .utf8)!)
        XCTAssertFalse(tiers.isEmpty)
        XCTAssertEqual(tiers[0].suffix, "0m", "档位形状含 suffix 键")
        // 弹层档位与绑定白名单一一对应。
        XCTAssertEqual(TimeTokenDraft.alertTiers.map(\.ms), tiers.map(\.ms))
    }

    func testAlertLabelsMatchDesktopWording() {
        XCTAssertEqual(TimeTokenDraft.alertLabel(ms: 0), Strings.alertAtTime)
        XCTAssertEqual(TimeTokenDraft.alertLabel(ms: 3_600_000), Strings.alertHour)
        XCTAssertEqual(TimeTokenDraft.alertLabel(ms: 86_400_000), Strings.alertDay)
        XCTAssertEqual(TimeTokenDraft.alertLabel(ms: 600_000), Strings.alertMinutes(10))
    }
}

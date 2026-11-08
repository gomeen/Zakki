import CryptoKit
import Foundation
import Zakkicore

// MARK: - 提醒计划（纯逻辑，可注入时钟测试）

// 一条可排定的提醒实例：跨全部标签合并后按 fireAt 参与槽位分配。
// 身份（identity）不含正文，对齐桌面 reminderOccurrence 的排定资格口径。
struct ReminderOccurrence: Equatable {
    var tabID: UInt64
    var tabTitle: String
    var line: Int // 块起始行（调试/稳定排序用）
    var dueMs: Int64
    var alertMs: Int64
    var fireAtMs: Int64
    var body: String
    var recurRaw: String

    var identity: String { "\(tabID)-\(dueMs)-\(alertMs)" }

    // 通知请求标识：同一实例幂等（对齐桌面 reminderKey 的组成要素）。
    var notificationID: String {
        let digest = body.sha256Hex.prefix(12)
        return "zakki-reminder-\(tabID)-\(dueMs)-\(alertMs)-\(digest)"
    }
}

// 64 槽分配结果：全部可容纳时不占续排槽；溢出时最后槽留给续排通知。
struct ReminderPlan: Equatable {
    var scheduled: [ReminderOccurrence]
    var renewal: ReminderOccurrence?
    var overflow: Bool
}

// 调度口径（规格 §3.3）：
//   - 只排带提醒（~档位）且未完成的任务/普通行；未知规则整簇不调度（ADR-0002）；
//   - 重复规则单次展开进窗口（D16），地平线按 due（桌面默认 60 天）；
//   - fireAt = due - 提前量，只排未来触发（首次启动不补发，R1/ADR-0001）。
enum ReminderPlanner {
    static let maxPending = 64
    static let horizonDays = 60
    // 每个块最多展开的实例数（对齐桌面 reminderDues 的 64）。
    static let maxPerBlock = 64

    // 采集全部标签的可调度实例。tabs 为不可变快照（值类型），nowMs 注入时钟。
    static func collect(tabs: [NotesTab], nowMs: Int64) -> [ReminderOccurrence] {
        var out: [ReminderOccurrence] = []
        for tab in tabs {
            let blocks = LiveBlocks.split(tab.text).blocks
            for block in blocks {
                guard let entry = occurrence(of: block, tab: tab, nowMs: nowMs) else { continue }
                out.append(contentsOf: entry)
            }
        }
        return out
    }

    // 单块 → 0..n 个实例。nil 表示块不可调度。
    static func occurrence(of block: LiveBlock, tab: NotesTab, nowMs: Int64) -> [ReminderOccurrence]? {
        guard let tokenRaw = block.timeTokenRaw, !block.checked,
              let hit = EditorDocument.findTimeToken(in: tokenRaw, protectedJSON: ""),
              hit.suffix.hasAlert else { return nil }
        let alertMs = hit.suffix.alertMs ?? 0
        let dueMs = dueMs(of: hit)
        guard dueMs > 0 else { return nil }
        let body = notificationBody(for: block)
        // 未知规则整簇不调度（ParseRule 失败即跳过；规格 §3.3）。
        if let recurRaw = hit.suffix.recur, !recurRaw.isEmpty {
            guard ruleJSON(of: recurRaw) != nil else { return nil }
            let dues = expand(ruleRaw: recurRaw, anchorMs: dueMs, nowMs: nowMs)
            return dues.map { due in
                ReminderOccurrence(tabID: tab.id, tabTitle: tab.displayTitle, line: block.startLine,
                                   dueMs: due, alertMs: alertMs, fireAtMs: due - alertMs,
                                   body: body, recurRaw: recurRaw)
            }
        }
        // 单次令牌：只在 due 仍处未来（或恰好现在）时排定。
        guard dueMs > nowMs else { return nil }
        return [ReminderOccurrence(tabID: tab.id, tabTitle: tab.displayTitle, line: block.startLine,
                                   dueMs: dueMs, alertMs: alertMs, fireAtMs: dueMs - alertMs,
                                   body: body, recurRaw: "")]
    }

    // 64 槽分配：fireAt 升序，平局以稳定条目身份（tabID→行→due）排序，
    // 不让先扫描的条目独占；溢出时保留 63 个最早实例，最后槽为续排通知。
    static func allocate(_ occurrences: [ReminderOccurrence], nowMs: Int64) -> ReminderPlan {
        let sorted = occurrences.sorted { a, b in
            if a.fireAtMs != b.fireAtMs { return a.fireAtMs < b.fireAtMs }
            return identityTieBreaker(a) < identityTieBreaker(b)
        }
        guard sorted.count > maxPending else {
            return ReminderPlan(scheduled: sorted, renewal: nil, overflow: false)
        }
        let kept = Array(sorted.prefix(maxPending - 1))
        let last = kept.last
        let renewal = ReminderOccurrence(
            tabID: 0, tabTitle: "", line: 0,
            dueMs: (last?.fireAtMs ?? nowMs) + renewalDelayMs,
            alertMs: 0, fireAtMs: (last?.fireAtMs ?? nowMs) + renewalDelayMs,
            body: "", recurRaw: "")
        return ReminderPlan(scheduled: kept, renewal: renewal, overflow: true)
    }

    // 续排通知在最后一个真实提醒之后片刻触发（提醒刚响完正是续排时机）。
    static let renewalDelayMs: Int64 = 60_000
    static let renewalNotificationID = "zakki-reminder-renewal"

    static func identityTieBreaker(_ o: ReminderOccurrence) -> String {
        "\(o.tabID)-\(o.line)-\(o.dueMs)"
    }

    // MARK: - 实例展开（只走 core 绑定）

    static func ruleJSON(of recurRaw: String) -> String? {
        var err: NSError?
        let out = ZakkicoreParseRule(recurRaw, &err)
        return err == nil ? out : nil
    }

    // 展开窗口 (now, now+horizonDays] 内实例（毫秒升序）；绑定失败按无实例处理。
    static func expand(ruleRaw: String, anchorMs: Int64, nowMs: Int64) -> [Int64] {
        guard let ruleJSON = ruleJSON(of: ruleRaw) else { return [] }
        var err: NSError?
        let out = ZakkicoreExpand(ruleJSON, anchorMs, nowMs, Int(horizonDays), Int(maxPerBlock), &err)
        guard err == nil, let data = out.data(using: .utf8),
              let millis = try? JSONDecoder().decode([Int64].self, from: data) else { return [] }
        return millis
    }

    // suffix.due 是分钟精度墙上时刻；秒数归零后的毫秒值以 dueMs 承载（仅输出）。
    static func dueMs(of hit: TimeTokenHit) -> Int64 {
        hit.suffix.dueMs ?? 0
    }

    // 实例毫秒 → 分钟精度墙上时刻串（core 写回格式，本端时区）。
    static func formatDueMs(_ ms: Int64) -> String {
        let f = DateFormatter()
        f.dateFormat = "yyyy-MM-dd HH:mm"
        f.locale = Locale(identifier: "en_US_POSIX")
        return f.string(from: Date(timeIntervalSince1970: TimeInterval(ms) / 1000))
    }

    // MARK: - 完成推进（重复任务勾选 → Advance）

    // 返回下一实例毫秒；规则非法或序列已结束返回 nil（调用方标记完成）。
    static func advance(recurRaw: String, anchorMs: Int64, nowMs: Int64) -> Int64? {
        guard let ruleJSON = ruleJSON(of: recurRaw) else { return nil }
        var err: NSError?
        let out = ZakkicoreAdvance(ruleJSON, anchorMs, nowMs, &err)
        guard err == nil, let data = out.data(using: .utf8) else { return nil }
        struct Result: Decodable { var dueMs: Int64; var ended: Bool }
        guard let result = try? JSONDecoder().decode(Result.self, from: data), !result.ended else { return nil }
        return result.dueMs > 0 ? result.dueMs : nil
    }

    // MARK: - 通知正文投影（规格 §3.1：组件前后全部可读正文，去令牌与标记）

    static func notificationBody(for block: LiveBlock) -> String {
        // 桌面 blk.text 先把段落行以空格相连再投影（令牌不跨行识别）；
        // withTimeObject 把令牌收成 U+FFFC 后剥除（桌面 stripTimeSuffix 对应物）。
        var flat = block
        flat.raw = block.raw.replacingOccurrences(of: "\n", with: " ")
        let text = LiveProjection.project(flat).withTimeObject().text
        let stripped = String(String.UnicodeScalarView(text.unicodeScalars.filter { $0.value != 0xfffc }))
        return stripped.split(whereSeparator: { $0.isWhitespace }).joined(separator: " ")
    }
}

// MARK: - 通知标题组装（对齐桌面 reminderBody：标题 · 正文）

extension ReminderOccurrence {
    var displayBody: String {
        let title = tabTitle
        if title.isEmpty { return body }
        if body.isEmpty { return title }
        return title + " · " + body
    }
}

extension String {
    // 通知标识用的正文摘要（桌面 reminderKey 含正文 hash 的同构需求）。
    var sha256Hex: String {
        SHA256.hash(data: Data(utf8)).map { String(format: "%02x", $0) }.joined()
    }
}

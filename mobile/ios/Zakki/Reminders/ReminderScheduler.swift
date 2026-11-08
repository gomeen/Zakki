import Combine
import Foundation
import UIKit
import UserNotifications
import os

// MARK: - 系统通知中心抽象（测试注入口）

// UNUserNotificationCenter 的可注入投影：排链/撤销/授权查询的契约面。
protocol NotificationScheduling: AnyObject {
    func requestAuthorization(provisional: Bool, completion: @escaping (Bool) -> Void)
    func fetchAuthorizationStatus(completion: @escaping (UNAuthorizationStatus) -> Void)
    func removeAllPending()
    func removePending(identifiers: [String])
    func removeDelivered(identifiers: [String])
    func add(_ request: UNNotificationRequest, completion: @escaping (Error?) -> Void)
}

// 系统实现：前台呈现横幅（到点响在打开状态下也可见）；Time Sensitive 按内容
// 级别尝试（R2：无 entitlement 时系统按普通通知呈现，真机实测为待验项）。
final class SystemNotificationCenter: NSObject, NotificationScheduling, UNUserNotificationCenterDelegate {
    override init() {
        super.init()
        UNUserNotificationCenter.current().delegate = self
    }

    func requestAuthorization(provisional: Bool, completion: @escaping (Bool) -> Void) {
        var options: UNAuthorizationOptions = [.alert, .sound]
        if provisional { options.insert(.provisional) }
        UNUserNotificationCenter.current().requestAuthorization(options: options) { granted, _ in
            completion(granted)
        }
    }

    func fetchAuthorizationStatus(completion: @escaping (UNAuthorizationStatus) -> Void) {
        UNUserNotificationCenter.current().getNotificationSettings { settings in
            completion(settings.authorizationStatus)
        }
    }

    func removeAllPending() {
        UNUserNotificationCenter.current().removeAllPendingNotificationRequests()
    }

    func removePending(identifiers: [String]) {
        UNUserNotificationCenter.current().removePendingNotificationRequests(withIdentifiers: identifiers)
    }

    func removeDelivered(identifiers: [String]) {
        UNUserNotificationCenter.current().removeDeliveredNotifications(withIdentifiers: identifiers)
    }

    func add(_ request: UNNotificationRequest, completion: @escaping (Error?) -> Void) {
        UNUserNotificationCenter.current().add(request, withCompletionHandler: completion)
    }

    // 前台到点：横幅 + 声音照常呈现（不做应用内自绘）；到点后自动对账
    // 刷新状态条（下次触发时刻前移）。
    func userNotificationCenter(_ center: UNUserNotificationCenter,
                                willPresent notification: UNNotification,
                                withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        // 端到端验证诊断（Swift String 不得直接进 NSLog 变参，会崩）。
        Logger(subsystem: "com.gomeen.zakki.ios", category: "reminders")
            .log("willPresent id=\(notification.request.identifier, privacy: .public)")
        NotificationCenter.default.post(name: Self.didDeliverNotification, object: nil)
        completionHandler([.banner, .sound])
    }

    // 到点投递事件（调度器监听后对账刷新状态）。
    static let didDeliverNotification = Notification.Name("zakki.reminderDidDeliver")
}

// MARK: - 状态（规格 §3.4：不把写盘成功等同于系统已接受）

// 四态的判定原料；展示文案由 Strings 组装：
//   已排定（scheduledCount/nextFireAtMs）· 待同步（pendingSync）·
//   待续排（needsRenewal）· 权限受限（authorized=false）。
struct ReminderStatus: Equatable {
    var scheduledCount: Int = 0
    var nextFireAtMs: Int64?
    var needsRenewal: Bool = false
    var pendingSync: Bool = false
    // nil = 尚未查询到授权结果。
    var authorized: Bool?
    // Time Sensitive 能力旗标（无 entitlement 的构建按普通通知呈现，真机待验）。
    var timeSensitiveActive: Bool = false

    var isLimited: Bool { authorized == false }
}

// MARK: - 调度引擎（打开／回前台／编辑后对账重排）

// 对账管线（规格 §3.3）：不可变条目快照 → core 展开 → 槽位分配 → 系统重排。
// 同步（M4）成功应用远端后调用 reconcile() 即复用本管线（§2.2 串联点接缝）。
@MainActor
final class ReminderScheduler: ObservableObject {
    static let provisionalAuthLaunchArg = "-ZakkiProvisionalNotifications"

    // 共享实例：App 装配层 attach 真实 NotesModel；测试直接构造独立实例。
    static let shared = ReminderScheduler(center: SystemNotificationCenter())

    @Published private(set) var status = ReminderStatus()

    private let center: NotificationScheduling
    private let clock: () -> Int64
    private weak var model: NotesModel?
    private var cancellables: Set<AnyCancellable> = []
    private var started = false
    // 排链任务序号：陈旧的后台计划完成后不得覆盖新状态。
    private var generation = 0

    init(center: NotificationScheduling, clock: @escaping () -> Int64 = { Int64(Date().timeIntervalSince1970 * 1000) }) {
        self.center = center
        self.clock = clock
    }

    // 接入模型并启动观察：快照变化（编辑/导入/推进）防抖重排，回前台/时区
    // 变化立即对账。attach 幂等，模型实例变化时重接。
    func attach(model: NotesModel) {
        guard !started || self.model !== model else { return }
        self.model = model
        started = true
        cancellables.removeAll()
        model.$snapshot
            .removeDuplicates()
            .dropFirst()
            .debounce(for: .milliseconds(250), scheduler: DispatchQueue.main)
            .sink { [weak self] _ in self?.reconcile() }
            .store(in: &cancellables)
        model.$hasPendingWrites
            .dropFirst()
            .debounce(for: .milliseconds(300), scheduler: DispatchQueue.main)
            .sink { [weak self] _ in self?.reconcile() }
            .store(in: &cancellables)
        NotificationCenter.default.addObserver(
            forName: UIApplication.willEnterForegroundNotification, object: nil, queue: .main
        ) { [weak self] _ in
            Task { @MainActor in self?.reconcile() }
        }
        NotificationCenter.default.addObserver(
            forName: NSNotification.Name.NSSystemTimeZoneDidChange, object: nil, queue: .main
        ) { [weak self] _ in
            // 时区切换成功后作废旧批次并重排（规格 §3.2）。
            Task { @MainActor in self?.reconcile() }
        }
        NotificationCenter.default.addObserver(
            forName: SystemNotificationCenter.didDeliverNotification, object: nil, queue: .main
        ) { [weak self] _ in
            // 到点后对账：状态条的已排定数与下次触发时刻随投递刷新。
            Task { @MainActor in self?.reconcile() }
        }
        reconcile()
    }

    // 全量对账：授权检查 → 计划 → 撤销旧请求 → 重排。陈旧代次静默丢弃。
    func reconcile() {
        guard let model else { return }
        generation += 1
        let gen = generation
        let nowMs = clock()
        let tabs = model.snapshot?.tabs ?? []
        center.fetchAuthorizationStatus { [weak self] authStatus in
            Task { @MainActor in
                guard let self, gen == self.generation else { return }
                if authStatus == .notDetermined {
                    self.requestAuthorization(gen: gen)
                    return
                }
                guard authStatus == .authorized || authStatus == .provisional || authStatus == .ephemeral else {
                    // 权限受限：清空待发队列，避免恢复授权后旧提醒集中轰炸；
                    // 状态如实展示受限而非成功。provisional/ephemeral 视作已授权。
                    self.center.removeAllPending()
                    self.publish(plan: ReminderPlan(scheduled: [], renewal: nil, overflow: false),
                                 nextFire: nil,
                                 pendingSync: self.model?.hasPendingWrites ?? false, authorized: false)
                    return
                }
                self.apply(self.plan(tabs: tabs, nowMs: nowMs), gen: gen, authorized: true)
            }
        }
    }

    private func requestAuthorization(gen: Int) {
        let provisional = ProcessInfo.processInfo.arguments.contains(Self.provisionalAuthLaunchArg)
        center.requestAuthorization(provisional: provisional) { [weak self] _ in
            Task { @MainActor in
                guard let self, gen == self.generation else { return }
                self.reconcile()
            }
        }
    }

    // MARK: 排链与状态

    private func plan(tabs: [NotesTab], nowMs: Int64) -> ReminderPlan {
        ReminderPlanner.allocate(ReminderPlanner.collect(tabs: tabs, nowMs: nowMs), nowMs: nowMs)
    }

    private func apply(_ plan: ReminderPlan, gen: Int, authorized: Bool) {
        guard gen == generation else { return }
        // 全撤全建：撤销失效实例的最简口径；打开时同时清理已投递的续排通知
        // 并补足（规格 §3.3 续排槽语义）。
        center.removeAllPending()
        center.removeDelivered(identifiers: [ReminderPlanner.renewalNotificationID])
        var nextFire: Int64?
        for occurrence in plan.scheduled {
            if nextFire == nil || occurrence.fireAtMs < nextFire! { nextFire = occurrence.fireAtMs }
            center.add(Self.request(for: occurrence), completion: { _ in })
        }
        if let renewal = plan.renewal {
            if nextFire == nil || renewal.fireAtMs < nextFire! { nextFire = renewal.fireAtMs }
            center.add(Self.renewalRequest(for: renewal), completion: { _ in })
        }
        publish(plan: plan, nextFire: nextFire,
                pendingSync: model?.hasPendingWrites ?? false, authorized: authorized)
    }

    private func publish(plan: ReminderPlan, nextFire: Int64?, pendingSync: Bool, authorized: Bool) {
        status = ReminderStatus(
            scheduledCount: plan.scheduled.count,
            nextFireAtMs: nextFire,
            needsRenewal: plan.overflow,
            pendingSync: pendingSync,
            authorized: authorized,
            timeSensitiveActive: Self.timeSensitiveActive
        )
        // 端到端验证与诊断：排链摘要进统一日志（Bool 显式转 0/1，不直接进 %d）。
        NSLog("ZAKKI-REM scheduled=%d renewal=%d pendingSync=%d authorized=%d next=%@",
              plan.scheduled.count, plan.overflow ? 1 : 0,
              pendingSync ? 1 : 0, authorized ? 1 : 0,
              nextFire.map { String($0) } ?? "-")
    }

    // MARK: 通知内容

    static func content(for occurrence: ReminderOccurrence) -> UNMutableNotificationContent {
        let content = UNMutableNotificationContent()
        content.title = "Zakki"
        content.body = occurrence.displayBody
        content.sound = .default
        // 尝试 Time Sensitive（R2/D19）：无 entitlement 的构建由系统降级为普通通知，
        // 不承诺绕过所有免打扰策略；真机实测列为待验项。
        content.interruptionLevel = .timeSensitive
        return content
    }

    static func request(for occurrence: ReminderOccurrence) -> UNNotificationRequest {
        let request = UNNotificationRequest(
            identifier: occurrence.notificationID,
            content: content(for: occurrence),
            trigger: intervalTrigger(fireAtMs: occurrence.fireAtMs))
        return request
    }

    static func renewalRequest(for occurrence: ReminderOccurrence) -> UNNotificationRequest {
        let content = UNMutableNotificationContent()
        content.title = "Zakki"
        content.body = Strings.renewalNoticeBody
        content.sound = .default
        content.interruptionLevel = .active
        return UNNotificationRequest(
            identifier: ReminderPlanner.renewalNotificationID,
            content: content,
            trigger: intervalTrigger(fireAtMs: occurrence.fireAtMs))
    }

    // 单次实例触发器（D16：不用原生 repeating trigger）；时刻必须在未来，
    // 间隔钳到最小正值避免系统拒绝。
    static func intervalTrigger(fireAtMs: Int64) -> UNNotificationTrigger {
        let interval = max(0.2, TimeInterval(fireAtMs) / 1000 - TimeInterval(Date().timeIntervalSince1970))
        return UNTimeIntervalNotificationTrigger(timeInterval: interval, repeats: false)
    }

    // Time Sensitive 能力旗标：宿主构建若接了 entitlement（Info.plist 显式声明）
    // 才视为能力可用；默认构建按普通通知口径展示状态（R2）。
    static var timeSensitiveActive: Bool {
        Bundle.main.object(forInfoDictionaryKey: "ZakkiTimeSensitiveEntitlement") as? Bool == true
    }
}

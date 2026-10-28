import SwiftUI

@main
struct ZakkiApp: App {
    // NotesModel.init 内同步完成启动加载/启动参数导入，首帧前状态就绪。
    @StateObject private var model = NotesModel()

    init() {
        TimeZoneSync.start()
    }

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(model)
        }
    }
}

// D9：core 的时间语义依赖时区，app 启动与系统时区变化时同步给绑定。
enum TimeZoneSync {
    private static var started = false
    private static let lock = NSLock()

    static func start() {
        lock.lock()
        defer { lock.unlock() }
        guard !started else { return }
        started = true
        sync()
        NotificationCenter.default.addObserver(
            forName: NSNotification.Name.NSSystemTimeZoneDidChange,
            object: nil,
            queue: .main
        ) { _ in
            sync()
        }
    }

    private static func sync() {
        NotesCore.setLocation(TimeZone.current.identifier)
    }
}

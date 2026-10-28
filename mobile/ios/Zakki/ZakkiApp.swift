import SwiftUI

@main
struct ZakkiApp: App {
    // NotesModel.init 内同步完成启动加载/启动参数导入，首帧前状态就绪。
    @StateObject private var model = NotesModel()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(model)
        }
    }
}

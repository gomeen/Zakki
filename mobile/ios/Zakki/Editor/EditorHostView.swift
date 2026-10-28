import SwiftUI

// SwiftUI 宿主：把指定标签页装配为编辑器视图控制器，桥接写回回调。
struct EditorHostView: UIViewControllerRepresentable {
    let tab: NotesTab
    let writable: Bool
    let reason: String

    @EnvironmentObject private var model: NotesModel

    func makeUIViewController(context: Context) -> EditorViewController {
        let vc = EditorViewController(raw: tab.text, writable: writable, reason: reason)
        vc.onTextChange = { [weak model] raw in
            model?.updateTabText(raw, tabID: tab.id)
        }
        vc.onEditingSessionEnd = { [weak model] in
            model?.flushPendingSave()
        }
        return vc
    }

    func updateUIViewController(_ uiViewController: EditorViewController, context: Context) {}
}

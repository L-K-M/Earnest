import AppKit
import SwiftUI
import EarnestKit

/// Single-key shortcuts act only when no text is being edited, as on the web
/// (WCAG 2.1.4). The router watches the key window's first responder after
/// every event and publishes whether a field or text view has focus; the
/// menus drop their unmodified-letter key equivalents while it does, so the
/// letters reach the field instead of running a command.
@MainActor final class KeyboardRouter: ObservableObject {
    static let shared = KeyboardRouter()

    @Published private(set) var editingText = false
    private weak var app: AppModel?
    private var openShortcuts: (() -> Void)?
    private var observer: NSObjectProtocol?
    private var monitor: Any?

    /// Whether a responder takes typed text: the shared field editor of a
    /// text field or search field, or an editable text view.
    static func isTextInput(_ responder: NSResponder?) -> Bool {
        guard let text = responder as? NSTextView else { return false }
        return text.isEditable || text.isFieldEditor
    }

    func start(app: AppModel, openShortcuts: @escaping () -> Void) {
        self.app = app
        self.openShortcuts = openShortcuts
        guard observer == nil else { return }
        observer = NotificationCenter.default.addObserver(forName: NSApplication.didUpdateNotification, object: nil, queue: .main) { _ in
            MainActor.assumeIsolated { KeyboardRouter.shared.refresh() }
        }
        // Aliases with no menu item of their own: V opens the original, as on
        // the web, and ? shows the keyboard shortcuts.
        monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { event in
            // Local monitors run on the main thread.
            let box = EventBox(event: event)
            let handled = MainActor.assumeIsolated { KeyboardRouter.shared.handle(box.event) }
            return handled ? nil : event
        }
    }

    private func refresh() {
        let editing = Self.isTextInput(NSApp.keyWindow?.firstResponder)
        if editing != editingText { editingText = editing }
    }

    private func handle(_ event: NSEvent) -> Bool {
        guard let app, app.loggedIn, app.readerSettings.singleKeyShortcuts,
              event.modifierFlags.intersection([.command, .control, .option]).isEmpty,
              !Self.isTextInput(NSApp.keyWindow?.firstResponder),
              let key = NSApp.keyWindow, key.attachedSheet == nil,
              let window = app.windows.first(where: { $0.nsWindow === key }) else { return false }
        switch event.charactersIgnoringModifiers {
        case "v":
            guard window.selectedURL != nil, window.canBrowse else { return false }
            window.openOriginal()
            return true
        case "?":
            openShortcuts?()
            return true
        default:
            return false
        }
    }
}

private struct EventBox: @unchecked Sendable { let event: NSEvent }

/// Help > Keyboard Shortcuts, also shown by pressing ?.
struct KeyboardShortcutsView: View {
    private struct Shortcut: Identifiable {
        var id: String { action }
        let keys: String
        let action: String
    }

    private let groups: [(String, [Shortcut])] = [
        ("Reading", [
            Shortcut(keys: "J", action: "Next article"),
            Shortcut(keys: "K", action: "Previous article"),
            Shortcut(keys: "E", action: "Move to Done"),
            Shortcut(keys: "S", action: "Move to Later, or remove from Later"),
            Shortcut(keys: "F", action: "Keep or unkeep"),
            Shortcut(keys: "M", action: "Mark as unread"),
            Shortcut(keys: "O, V", action: "Open in browser"),
            Shortcut(keys: "⌘Z", action: "Undo the last reading action"),
        ]),
        ("Going Places", [
            Shortcut(keys: "⌘1 – ⌘7", action: "Front page, All articles, Later, Kept, Done, Feeds, Activity"),
            Shortcut(keys: "⌥⌘F", action: "Search articles"),
            Shortcut(keys: "⌘K", action: "Command palette"),
            Shortcut(keys: "⌃⌘S", action: "Show or hide the sidebar"),
        ]),
        ("The Article", [
            Shortcut(keys: "⌘F", action: "Find in article"),
            Shortcut(keys: "⌘G  ⇧⌘G", action: "Find next, find previous"),
            Shortcut(keys: "⌘+  ⌘−  ⌘0", action: "Zoom in, zoom out, actual size"),
        ]),
        ("Your Account", [
            Shortcut(keys: "⌘N", action: "New subscription"),
            Shortcut(keys: "⌥⌘N", action: "New window"),
            Shortcut(keys: "⌘R", action: "Refresh"),
            Shortcut(keys: "⌘,", action: "Settings"),
        ]),
    ]

    var body: some View {
        Form {
            ForEach(groups, id: \.0) { group in
                SwiftUI.Section(group.0) {
                    ForEach(group.1) { shortcut in
                        LabeledContent(shortcut.action) {
                            Text(shortcut.keys)
                                .font(.body.monospaced())
                                .foregroundStyle(.secondary)
                        }
                    }
                }
            }
            SwiftUI.Section {
                Text("Single-key shortcuts work when you are not typing in a field. Turn them off in Settings.")
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }
        }
        .formStyle(.grouped)
        .frame(width: 460)
        .fixedSize(horizontal: false, vertical: true)
    }
}

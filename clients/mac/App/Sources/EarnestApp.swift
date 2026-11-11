import AppKit
import Foundation
import SwiftUI
import EarnestKit

@MainActor @main struct EarnestApp: App {
    @StateObject private var model: AppModel
    /// The account's reading preferences, edited by Settings and the feed
    /// detail; changes apply as they are made.
    @StateObject private var preferences: ReaderSettingsModel

    init() {
        let model = AppModel()
        let preferences = ReaderSettingsModel(app: model)
        preferences.autosaves = true
        _model = StateObject(wrappedValue: model)
        _preferences = StateObject(wrappedValue: preferences)
    }

    var body: some Scene {
        // A stable identifier keeps window frames and restored state across
        // builds; the default derives one from the view's type.
        WindowGroup("Earnest", id: "main") {
            WindowRootView(model: model)
                .environmentObject(preferences)
                .handlesExternalEvents(preferring: ["*"], allowing: ["*"])
        }
        .handlesExternalEvents(matching: ["*"])
        .defaultSize(width: 1180, height: 760)
        .commands {
            FileCommands(model: model)
            EditCommands()
            ViewCommands(model: model)
            GoCommands(model: model)
            ArticleCommands(model: model)
            AccountCommands(model: model)
            HelpCommands()
        }

        Settings {
            SettingsWindow(settings: preferences)
                .environmentObject(model)
        }

        Window("Keyboard Shortcuts", id: "shortcuts") {
            KeyboardShortcutsView()
        }
        .windowResizability(.contentSize)
        .handlesExternalEvents(matching: [])
        .restorationBehavior(.disabled)
    }
}

// MARK: - Menus
//
// Commands act on the key window's WindowModel; app-wide actions use the
// shared AppModel. Unmodified-letter key equivalents are attached only while
// the account's single-key preference is on and no text field has focus, so
// the menu always lists the shortcut but never steals typing.

private struct FileCommands: Commands {
    @ObservedObject var model: AppModel
    @FocusedObject private var window: WindowModel?
    @Environment(\.openWindow) private var openWindow

    var body: some Commands {
        CommandGroup(replacing: .newItem) {
            Button("New Subscription…") { window?.subscribing = true }
                .keyboardShortcut("n", modifiers: .command)
                .disabled(window == nil || !model.loggedIn)
            Button("New Window") { openWindow(id: "main") }
                .keyboardShortcut("n", modifiers: [.command, .option])
            Divider()
            Button("Import Subscriptions…") {
                NotificationCenter.default.post(name: .earnestImportSubscriptions, object: nil)
            }
            .disabled(window == nil || !model.loggedIn)
            Button("Import Starred Articles…") {
                NotificationCenter.default.post(name: .earnestImportStarredArticles, object: nil)
            }
            .disabled(window == nil || !model.loggedIn)
            Button("Export Subscriptions…") {
                NotificationCenter.default.post(name: .earnestExportSubscriptions, object: nil)
            }
            .disabled(window == nil || !model.loggedIn)
        }
    }
}

private struct EditCommands: Commands {
    @FocusedObject private var window: WindowModel?

    var body: some Commands {
        CommandGroup(after: .pasteboard) {
            Divider()
            Menu("Find") {
                Button("Search Articles") { window?.searchFocusRequest &+= 1 }
                    .keyboardShortcut("f", modifiers: [.command, .option])
                    .disabled(window == nil || !(window?.app.loggedIn ?? false))
                Divider()
                Button("Find in Article…") { window?.reader.showFind() }
                    .keyboardShortcut("f", modifiers: .command)
                    .disabled(!canFind)
                Button("Find Next") { window?.reader.findNext() }
                    .keyboardShortcut("g", modifiers: .command)
                    .disabled(!canFind)
                Button("Find Previous") { window?.reader.findPrevious() }
                    .keyboardShortcut("g", modifiers: [.command, .shift])
                    .disabled(!canFind)
            }
        }
    }

    private var canFind: Bool {
        guard let window else { return false }
        return window.section.showsReader && window.selected != nil && !window.html.isEmpty
    }
}

private struct ViewCommands: Commands {
    @ObservedObject var model: AppModel
    @FocusedObject private var window: WindowModel?
    @AppStorage(ReaderController.zoomKey) private var zoom = 1.0

    var body: some Commands {
        SidebarCommands()
        ToolbarCommands()
        CommandGroup(after: .toolbar) {
            Divider()
            Button("Refresh") { model.reloadLists() }
                .keyboardShortcut("r", modifiers: .command)
                .disabled(!model.loggedIn || model.syncing)
            Divider()
            Button("Actual Size") { ReaderController.resetZoom() }
                .keyboardShortcut("0", modifiers: .command)
                .disabled(abs(zoom - 1) < 0.001)
            // Follow the + character on every layout, as Safari does, rather
            // than the US key position.
            Button("Zoom In") { ReaderController.setZoom(1) }
                .keyboardShortcut("+", modifiers: .command, localization: .custom)
                .disabled(!ReaderController.canZoomIn)
            Button("Zoom Out") { ReaderController.setZoom(-1) }
                .keyboardShortcut("-", modifiers: .command)
                .disabled(!ReaderController.canZoomOut)
            Divider()
            Button("Command Palette…") { window?.palette = true }
                .keyboardShortcut("k", modifiers: .command)
                .disabled(window == nil || !model.loggedIn)
        }
    }
}

private struct GoCommands: Commands {
    @ObservedObject var model: AppModel
    @FocusedObject private var window: WindowModel?
    @ObservedObject private var keyboard = KeyboardRouter.shared

    var body: some Commands {
        CommandMenu("Go") {
            ForEach(Section.places) { target in
                place(target)
            }
            Divider()
            singleKey(Button("Next Article") { window?.move(1) }
                .disabled(window.map { !$0.canBrowse } ?? true), "j")
            singleKey(Button("Previous Article") { window?.move(-1) }
                .disabled(window.map { !$0.canBrowse } ?? true), "k")
        }
    }

    @ViewBuilder
    private func singleKey<V: View>(_ view: V, _ key: Character) -> some View {
        // The palette's field also stands them down, should it not yet be
        // first responder when a letter arrives.
        if (window?.singleKeyShortcuts ?? true) && !keyboard.editingText && !(window?.palette ?? false) {
            view.keyboardShortcut(KeyEquivalent(key), modifiers: [])
        } else {
            view
        }
    }

    @ViewBuilder
    private func place(_ target: Section) -> some View {
        let button = Button(target.title) { window?.go(to: target) }
            .disabled(!model.loggedIn || window == nil)
        if let key = target.keyEquivalent.flatMap(Character.init) {
            button.keyboardShortcut(KeyEquivalent(key), modifiers: .command)
        } else {
            button
        }
    }
}

private struct ArticleCommands: Commands {
    @ObservedObject var model: AppModel
    @FocusedObject private var window: WindowModel?
    @ObservedObject private var keyboard = KeyboardRouter.shared

    var body: some Commands {
        CommandMenu("Article") {
            singleKey(Button("Open in Browser") { window?.openOriginal() }
                .disabled(window?.selectedURL == nil), "o")
            Button("Copy Link") {
                guard let url = window?.selectedURL else { return }
                NSPasteboard.general.clearContents()
                NSPasteboard.general.writeObjects([url as NSURL])
                NSPasteboard.general.setString(url.absoluteString, forType: .string)
            }
            .disabled(window?.selectedURL == nil)
            if let url = window?.selectedURL {
                ShareLink("Share…", item: url, subject: Text(window?.selectedTitle ?? ""))
            } else {
                Button("Share…") {}.disabled(true)
            }
            Divider()
            singleKey(Button("Move to Done") { window?.command(.done) }
                .disabled(!canAct), "e")
            singleKey(Button(window?.selectedIsLater == true ? "Remove from Later" : "Move to Later") { window?.command(.later) }
                .disabled(!canAct), "s")
            singleKey(Button(window?.selectedIsKept == true ? "Unkeep" : "Keep") { window?.command(.keep) }
                .disabled(!canAct), "f")
            singleKey(Button("Mark as Unread") { window?.command(.unread) }
                .disabled(!canAct || window?.selectedIsUnread == true), "m")
            Divider()
            Button("Load Images") { window?.loadImages() }
                .disabled(window.map { $0.selected == nil || $0.imageMode != .click || $0.offlineCopy } ?? true)
        }
    }

    private var canAct: Bool { window?.canActOnEntry ?? false }

    /// Attach an unmodified-letter binding only while the account's
    /// single-key preference is on and no text field has focus. The menu
    /// action stays clickable either way, and the palette lists it.
    @ViewBuilder
    private func singleKey<V: View>(_ view: V, _ key: Character) -> some View {
        // The palette's field also stands them down, should it not yet be
        // first responder when a letter arrives.
        if (window?.singleKeyShortcuts ?? true) && !keyboard.editingText && !(window?.palette ?? false) {
            view.keyboardShortcut(KeyEquivalent(key), modifiers: [])
        } else {
            view
        }
    }
}

private struct AccountCommands: Commands {
    @ObservedObject var model: AppModel

    var body: some Commands {
        // After Settings…, before Services.
        CommandGroup(before: .systemServices) {
            Button("Sign Out…") { model.requestSignOut() }
                .disabled(!model.loggedIn)
            Divider()
        }
    }
}

private struct HelpCommands: Commands {
    @Environment(\.openWindow) private var openWindow

    var body: some Commands {
        CommandGroup(replacing: .help) {
            // Also opened by pressing ? outside text fields; ⇧⌘/ stays with
            // the system's Help search.
            Button("Keyboard Shortcuts") { openWindow(id: "shortcuts") }
        }
    }
}

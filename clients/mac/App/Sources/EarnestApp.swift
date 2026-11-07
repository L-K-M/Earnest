import AppKit
import Foundation
import SwiftUI
import EarnestKit

@MainActor @main struct EarnestApp: App {
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup {
            WindowRootView(model: model)
                .handlesExternalEvents(preferring: ["*"], allowing: ["*"])
        }
            .handlesExternalEvents(matching: ["*"])
            .commands {
                ArticleCommands(model: model)
                GoCommands(model: model)
            }
    }
}

/// Article and palette commands act on the key window's WindowModel;
/// app-wide actions use the shared AppModel.
private struct ArticleCommands: Commands {
    let model: AppModel
    @FocusedObject private var window: WindowModel?

    var body: some Commands {
        CommandMenu("Article") {
            // Unmodified-letter equivalents are suspended while the
            // palette is open so its filter field can type them.
            Button("Next Article") { window?.move(1) }
                .keyboardShortcut("j", modifiers: [])
                .disabled(window.map { $0.palette || !$0.canBrowse } ?? true)
            Button("Previous Article") { window?.move(-1) }
                .keyboardShortcut("k", modifiers: [])
                .disabled(window.map { $0.palette || !$0.canBrowse } ?? true)
            Divider()
            Button("Done") { window?.command(.done) }
                .keyboardShortcut("e", modifiers: [])
                .disabled(window.map { !$0.canActOnEntry || $0.palette } ?? true)
            Button("Later") { window?.command(.later) }
                .keyboardShortcut("s", modifiers: [])
                .disabled(window.map { !$0.canActOnEntry || $0.palette } ?? true)
            Button("Keep") { window?.command(.keep) }
                .keyboardShortcut("f", modifiers: [])
                .disabled(window.map { !$0.canActOnEntry || $0.palette } ?? true)
            Button("Mark Unread") { window?.command(.unread) }
                .keyboardShortcut("m", modifiers: [])
                .disabled(window.map { !$0.canActOnEntry || $0.palette } ?? true)
            Divider()
            Button("Open Original") { window?.openOriginal() }
                .keyboardShortcut("o", modifiers: [])
                .disabled(window.map { $0.selected == nil || $0.palette || !$0.canBrowse } ?? true)
            Button("View Original") { window?.openOriginal() }
                .keyboardShortcut("v", modifiers: [])
                .disabled(window.map { $0.selected == nil || $0.palette || !$0.canBrowse } ?? true)
            Divider()
            Button("Refresh") { model.scheduleSync() }
                .keyboardShortcut("r", modifiers: .command)
                .disabled(!model.loggedIn)
            Button("Command Palette") { window?.palette = true }
                .keyboardShortcut("k", modifiers: .command)
                .disabled(window == nil)
        }
    }
}

private struct GoCommands: Commands {
    let model: AppModel
    @FocusedObject private var window: WindowModel?

    var body: some Commands {
        CommandMenu("Go") {
            ForEach(Section.allCases) { target in
                Button("Go to \(target.rawValue)") { window?.section = target }
                    .keyboardShortcut(KeyEquivalent(Character(target.keyEquivalent)), modifiers: .command)
                    .disabled(!model.loggedIn || window == nil)
            }
            Divider()
            Button("Sign Out") { model.signOut() }
                .keyboardShortcut("q", modifiers: [.command, .shift])
                .disabled(!model.loggedIn)
        }
    }
}

/// Each window owns its WindowModel, so selection and reader are independent;
/// the shared AppModel is injected alongside it.
@MainActor private struct WindowRootView: View {
    let model: AppModel
    @StateObject private var window: WindowModel

    init(model: AppModel) {
        self.model = model
        _window = StateObject(wrappedValue: WindowModel(app: model))
    }

    var body: some View {
        ContentView()
            .environmentObject(model)
            .environmentObject(window)
            .focusedSceneObject(window)
    }
}

@MainActor struct ContentView: View {
    @EnvironmentObject private var model: AppModel
    @EnvironmentObject private var window: WindowModel

    var body: some View {
        Group {
            if model.loggedIn { mainView } else { loginView }
        }
        .frame(minWidth: 880, minHeight: 520)
        .onAppear {
            if !model.server.isEmpty && !model.connected { model.connect() }
            window.refreshSection()
        }
        .alert("Error", isPresented: Binding(get: { model.error != nil }, set: { if !$0 { model.error = nil } })) {
            Button("OK", role: .cancel) { model.error = nil }
        } message: {
            Text(model.error ?? "")
        }
        .sheet(isPresented: $window.palette) {
            PaletteView()
                .environmentObject(window)
        }
        .onOpenURL { window.openDeepLink($0) }
    }

    private var mainView: some View {
        NavigationSplitView {
            List(Section.allCases, selection: Binding(
                get: { window.section },
                set: { if let target = $0 { window.section = target } })) { target in
                Label(target.rawValue, systemImage: target.icon).tag(target)
            }
            .listStyle(.sidebar)
            .frame(minWidth: 170)
        } content: {
            contentColumn
                // Inset on the column, not the split view: macOS columns do not
                // yield to a split-view inset, so the note covered the end statement.
                .safeAreaInset(edge: .bottom) {
                    if let note = model.connectionNote {
                        Text(note).font(.caption).textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading).padding(8)
                            .background(.regularMaterial)
                    }
                }
                .frame(minWidth: 320)
        } detail: {
            if window.selected != nil {
                VStack(spacing: 0) {
                    if let entry = window.readingEntry {
                        ReaderHeader(entry: entry, feedTitle: window.readingFeedTitle, openOriginal: window.openOriginal)
                        Divider()
                    }
                    if window.imageMode == .click {
                        HStack {
                            Text("Images are blocked for externally opened articles.")
                                .font(.caption)
                            Button("Load Images") { window.loadImages() }
                        }
                        .padding(8)
                    }
                    if window.offlineCopy {
                        Text("Offline copy. Images are blocked.")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .padding(8)
                    }
                    if window.articleUnavailable {
                        ContentUnavailableView("Not available offline", systemImage: "wifi.slash",
                                               description: Text("Earnest keeps articles you have opened. This one loads when you are back online."))
                            .frame(maxWidth: .infinity, maxHeight: .infinity)
                    } else {
                        Reader(html: window.html, imageOrigin: window.imageOrigin)
                    }
                }
                .background(Color(nsColor: .textBackgroundColor))
            } else {
                ContentUnavailableView("Select an article", systemImage: "doc.text")
            }
        }
        .onChange(of: window.section) { window.refreshSection() }
        .toolbar {
            ToolbarItemGroup {
                if model.pendingOps > 0 {
                    Text("\(model.pendingOps) pending")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                if model.syncing {
                    ProgressView().controlSize(.small)
                }
                Button("Refresh", systemImage: "arrow.clockwise") { model.scheduleSync() }
                Button("Sign Out", systemImage: "rectangle.portrait.and.arrow.right") { model.signOut() }
            }
        }
    }

    private var entrySelection: Binding<Int64?> {
        Binding(get: { window.selected }, set: { window.selectFromList($0) })
    }

    @ViewBuilder private var contentColumn: some View {
        switch window.section {
        case .today:
            VStack(spacing: 0) {
                if !model.briefs.isEmpty {
                    BriefsView(briefs: model.briefs, names: model.principalNames)
                    Divider()
                }
                if window.rows.isEmpty && model.briefs.isEmpty {
                    Spacer()
                } else {
                    EntryTable(entries: window.rows, selection: entrySelection)
                }
                if !window.endTitle.isEmpty {
                    Divider()
                    VStack(alignment: .leading, spacing: 4) {
                        Text(window.endTitle).font(.callout)
                        if !window.endDetail.isEmpty {
                            Text(window.endDetail)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(10)
                }
            }
        case .edition:
            EditionListView(groups: window.editionGroups, selection: entrySelection)
        case .later, .kept, .done:
            if window.rows.isEmpty {
                ContentUnavailableView("Nothing here", systemImage: "tray")
            } else {
                EntryTable(entries: window.rows, selection: entrySelection)
            }
        case .feeds:
            FeedsView()
        case .activity:
            ActivityView()
        case .search:
            SearchView(search: window.search)
        }
    }

    private var loginView: some View {
        VStack(spacing: 16) {
            BrandLockup()
            TextField("Server URL", text: $model.server)
                .textFieldStyle(.roundedBorder)
                .frame(width: 320)
                .disabled(model.connected)
            if let code = model.code {
                Text("Enter \(code.userCode) at \(code.verificationURI)")
                    .textSelection(.enabled)
            }
            HStack {
                if model.connected {
                    Button("Sign In") { model.login() }
                        .disabled(model.loggingIn || model.server.isEmpty)
                    if model.loggingIn || model.code != nil {
                        Button("Cancel") { model.cancelLogin() }
                    }
                    Button("Change Server") { model.connected = false; model.signOut() }
                } else {
                    Button("Connect") { model.connect() }
                        .disabled(model.server.isEmpty)
                }
            }
            if let note = model.connectionNote {
                Text(note).font(.caption).foregroundStyle(.secondary)
            }
        }
        .padding(40)
    }
}

@MainActor struct PaletteView: View {
    @EnvironmentObject private var window: WindowModel
    @Environment(\.dismiss) private var dismiss
    @SwiftUI.State private var filter = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            TextField("Type a command", text: $filter)
                .textFieldStyle(.roundedBorder)
            List(window.paletteCommands().filter {
                filter.isEmpty || $0.title.localizedCaseInsensitiveContains(filter)
            }) { command in
                Button {
                    dismiss()
                    command.action()
                } label: {
                    HStack {
                        Text(command.title)
                        Spacer()
                        Text(command.key).foregroundStyle(.secondary)
                    }
                }
                .buttonStyle(.plain)
                .disabled(!command.enabled)
            }
        }
        .padding(12)
        .frame(width: 320, height: 380)
    }
}

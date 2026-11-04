import AppKit
import Foundation
import SwiftUI
import EarnestKit

@MainActor @main struct EarnestApp: App {
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup {
            ContentView().environmentObject(model)
                .handlesExternalEvents(preferring: ["*"], allowing: ["*"])
        }
            .handlesExternalEvents(matching: ["*"])
            .commands {
                CommandMenu("Article") {
                    // Unmodified-letter equivalents are suspended while the
                    // palette is open so its filter field can type them.
                    Button("Next Article") { model.move(1) }
                        .keyboardShortcut("j", modifiers: [])
                        .disabled(model.palette || !model.canBrowse)
                    Button("Previous Article") { model.move(-1) }
                        .keyboardShortcut("k", modifiers: [])
                        .disabled(model.palette || !model.canBrowse)
                    Divider()
                    Button("Done") { model.command(.done) }
                        .keyboardShortcut("e", modifiers: [])
                        .disabled(!model.canActOnEntry || model.palette)
                    Button("Later") { model.command(.later) }
                        .keyboardShortcut("s", modifiers: [])
                        .disabled(!model.canActOnEntry || model.palette)
                    Button("Keep") { model.command(.keep) }
                        .keyboardShortcut("f", modifiers: [])
                        .disabled(!model.canActOnEntry || model.palette)
                    Button("Mark Unread") { model.command(.unread) }
                        .keyboardShortcut("m", modifiers: [])
                        .disabled(!model.canActOnEntry || model.palette)
                    Divider()
                    Button("Open Original") { model.openOriginal() }
                        .keyboardShortcut("o", modifiers: [])
                        .disabled(model.selected == nil || model.palette || !model.canBrowse)
                    Button("View Original") { model.openOriginal() }
                        .keyboardShortcut("v", modifiers: [])
                        .disabled(model.selected == nil || model.palette || !model.canBrowse)
                    Divider()
                    Button("Refresh") { model.scheduleSync() }
                        .keyboardShortcut("r", modifiers: .command)
                        .disabled(!model.loggedIn)
                    Button("Command Palette") { model.palette = true }
                        .keyboardShortcut("k", modifiers: .command)
                }
                CommandMenu("Go") {
                    ForEach(Section.allCases) { target in
                        Button("Go to \(target.rawValue)") { model.section = target }
                            .keyboardShortcut(KeyEquivalent(Character(target.keyEquivalent)), modifiers: .command)
                            .disabled(!model.loggedIn)
                    }
                    Divider()
                    Button("Sign Out") { model.signOut() }
                        .keyboardShortcut("q", modifiers: [.command, .shift])
                        .disabled(!model.loggedIn)
                }
            }
    }
}

@MainActor struct ContentView: View {
    @EnvironmentObject private var model: AppModel

    var body: some View {
        Group {
            if model.loggedIn { mainView } else { loginView }
        }
        .frame(minWidth: 880, minHeight: 520)
        .onAppear {
            if !model.server.isEmpty && !model.connected { model.connect() }
        }
        .alert("Error", isPresented: Binding(get: { model.error != nil }, set: { if !$0 { model.error = nil } })) {
            Button("OK", role: .cancel) { model.error = nil }
        } message: {
            Text(model.error ?? "")
        }
        .sheet(isPresented: $model.palette) {
            PaletteView()
                .environmentObject(model)
        }
        .onOpenURL { model.openDeepLink($0) }
    }

    private var mainView: some View {
        NavigationSplitView {
            List(Section.allCases, selection: Binding(
                get: { model.section },
                set: { if let target = $0 { model.section = target } })) { target in
                Label(target.rawValue, systemImage: target.icon).tag(target)
            }
            .listStyle(.sidebar)
            .frame(minWidth: 170)
        } content: {
            contentColumn
                .frame(minWidth: 320)
        } detail: {
            if model.selected != nil {
                VStack(spacing: 0) {
                    if let entry = model.readingEntry {
                        ReaderHeader(entry: entry, feedTitle: model.readingFeedTitle, openOriginal: model.openOriginal)
                        Divider()
                    }
                    if model.imageMode == .click {
                        HStack {
                            Text("Images are blocked for externally opened articles.")
                                .font(.caption)
                            Button("Load Images") { model.loadImages() }
                        }
                        .padding(8)
                    }
                    Reader(html: model.html, imageOrigin: model.imageOrigin)
                }
                .background(Color(nsColor: .textBackgroundColor))
            } else {
                ContentUnavailableView("Select an article", systemImage: "doc.text")
            }
        }
        .safeAreaInset(edge: .bottom) {
            if let note = model.connectionNote {
                Text(note).font(.caption).textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading).padding(8)
                    .background(.regularMaterial)
            }
        }
        .onChange(of: model.section) { model.refreshSection() }
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
        Binding(get: { model.selected }, set: { model.selectFromList($0) })
    }

    @ViewBuilder private var contentColumn: some View {
        switch model.section {
        case .today:
            VStack(spacing: 0) {
                if !model.briefs.isEmpty {
                    BriefsView(briefs: model.briefs, names: model.principalNames)
                    Divider()
                }
                if model.rows.isEmpty && model.briefs.isEmpty {
                    Spacer()
                } else {
                    EntryTable(entries: model.rows, selection: entrySelection)
                }
                if !model.endTitle.isEmpty {
                    Divider()
                    VStack(alignment: .leading, spacing: 4) {
                        Text(model.endTitle).font(.callout)
                        if !model.endDetail.isEmpty {
                            Text(model.endDetail)
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
            EditionListView(groups: model.editionGroups, selection: entrySelection)
        case .later, .kept, .done:
            if model.rows.isEmpty {
                ContentUnavailableView("Nothing here", systemImage: "tray")
            } else {
                EntryTable(entries: model.rows, selection: entrySelection)
            }
        case .feeds:
            FeedsView()
        case .activity:
            ActivityView()
        }
    }

    private var loginView: some View {
        VStack(spacing: 16) {
            Text("Earnest").font(.title)
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
    @EnvironmentObject private var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @SwiftUI.State private var filter = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            TextField("Type a command", text: $filter)
                .textFieldStyle(.roundedBorder)
            List(model.paletteCommands().filter {
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

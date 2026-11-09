import AppKit
import Foundation
import SwiftUI
import UniformTypeIdentifiers
import EarnestKit

/// Each window owns its WindowModel, so selection and reader are independent;
/// the shared AppModel is injected alongside it.
@MainActor struct WindowRootView: View {
    let model: AppModel
    @StateObject private var window: WindowModel
    @SceneStorage("section") private var storedSection = Section.today.rawValue
    @SceneStorage("article") private var storedArticle = 0

    init(model: AppModel) {
        self.model = model
        _window = StateObject(wrappedValue: WindowModel(app: model))
    }

    var body: some View {
        ContentView()
            .environmentObject(model)
            .environmentObject(window)
            .focusedSceneObject(window)
            .onAppear {
                // Restore this window's place; Search is not restored because
                // its query is not kept.
                if let section = Section(rawValue: storedSection), section != .search, section != window.section {
                    window.section = section
                }
                if storedArticle > 0 { window.pendingRestoredSelection = Int64(storedArticle) }
            }
            .onChange(of: window.section) {
                if window.section != .search { storedSection = window.section.rawValue }
            }
            .onChange(of: window.selected) { storedArticle = Int(window.selected ?? 0) }
    }
}

@MainActor struct ContentView: View {
    @EnvironmentObject private var model: AppModel
    @EnvironmentObject private var window: WindowModel
    @Environment(\.undoManager) private var undoManager
    @Environment(\.openSettings) private var openSettings
    @Environment(\.openWindow) private var openWindow
    @SwiftUI.State private var importing = false
    @SwiftUI.State private var importingStarred = false
    @SwiftUI.State private var exportDocument: OPMLDocument?
    @SwiftUI.State private var pendingImport: PendingImport?
    @SwiftUI.State private var importReport: AppModel.ImportReport?
    @SwiftUI.State private var pendingStarredImport: PendingStarredImport?
    @SwiftUI.State private var keptImportReport: KeptImportReport?
    @SwiftUI.State private var starredSubscribeMissing = false

    // The body is split so each part type-checks quickly on every toolchain.
    var body: some View {
        withFiles(withAlerts(base))
    }

    private var base: some View {
        Group {
            if model.loggedIn { MainView() } else { LoginView() }
        }
        .frame(minWidth: 860, minHeight: 520)
        .background(WindowAccessor { window.nsWindow = $0 })
        .onAppear {
            window.undoManager = undoManager
            KeyboardRouter.shared.start(app: model) { openWindow(id: "shortcuts") }
            if !model.server.isEmpty && !model.connected { model.connect() }
            window.refreshSection()
        }
        .onChange(of: undoManager) { window.undoManager = undoManager }
        // A file staged before signing out never imports into the next account.
        .onChange(of: model.loggedIn) { wasSignedIn, signedIn in
            if wasSignedIn && !signedIn {
                pendingImport = nil
                importReport = nil
                pendingStarredImport = nil
                keptImportReport = nil
            }
        }
    }

    // MARK: Alerts

    /// App-wide messages appear once, in the front reading window.
    private var showsError: Binding<Bool> {
        Binding(get: { model.error != nil && window.presentsAppAlerts },
                set: { if !$0 { model.error = nil } })
    }

    private var showsSignOut: Binding<Bool> {
        Binding(get: { model.confirmingSignOut && window.presentsAppAlerts },
                set: { if !$0 { model.confirmingSignOut = false } })
    }

    /// The result belongs to the window whose import it was.
    private var showsImportReport: Binding<Bool> {
        Binding(get: { importReport != nil }, set: { if !$0 { importReport = nil } })
    }

    /// A file opened during launch waits until the account is signed in.
    private var showsImportConfirmation: Binding<Bool> {
        Binding(get: { pendingImport != nil && model.loggedIn }, set: { if !$0 { pendingImport = nil } })
    }

    /// The result belongs to the window whose import it was.
    private var showsKeptImportReport: Binding<Bool> {
        Binding(get: { keptImportReport != nil }, set: { if !$0 { keptImportReport = nil } })
    }

    /// A starred-articles file waits until the account is signed in.
    private var showsStarredImportConfirmation: Binding<Bool> {
        Binding(get: { pendingStarredImport != nil && model.loggedIn }, set: { if !$0 { pendingStarredImport = nil } })
    }

    private var signOutMessage: String {
        let waiting = model.pendingOps
        guard waiting > 0 else { return "You will need to approve this Mac again to sign in." }
        let changes = waiting == 1 ? "1 change" : "\(waiting) changes"
        return "\(changes) waiting to sync will stay on this Mac and sync when you sign in to this account again. "
            + "You will need to approve this Mac again to sign in."
    }

    private func withAlerts<Content: View>(_ content: Content) -> some View {
        content
            .alert("Earnest", isPresented: showsError) {
                Button("OK", role: .cancel) { model.error = nil }
            } message: {
                Text(model.error ?? "")
            }
            .alert(importReport?.title ?? "", isPresented: showsImportReport, presenting: importReport) { _ in
                Button("OK", role: .cancel) { importReport = nil }
            } message: { report in
                Text(report.detail)
            }
            .alert(keptImportReport?.title ?? "", isPresented: showsKeptImportReport, presenting: keptImportReport) { report in
                if report.retry != nil {
                    Button("Subscribe to these feeds and import again") {
                        if let retry = report.retry {
                            startStarredImport(retry.data, name: retry.name, subscribeMissing: true)
                        }
                        keptImportReport = nil
                    }
                    .keyboardShortcut(.defaultAction)
                }
                Button("OK", role: .cancel) { keptImportReport = nil }
            } message: { report in
                Text(report.detail)
            }
            .alert("Sign Out of Earnest?", isPresented: showsSignOut) {
                Button("Sign Out", role: .destructive) {
                    model.confirmingSignOut = false
                    model.signOut()
                }
                Button("Cancel", role: .cancel) { model.confirmingSignOut = false }
            } message: {
                Text(signOutMessage)
            }
            .alert("Import Subscriptions?", isPresented: showsImportConfirmation, presenting: pendingImport) { pending in
                Button("Import") {
                    model.importOPML(pending.data, name: pending.name) { importReport = $0 }
                    pendingImport = nil
                }
                .keyboardShortcut(.defaultAction)
                Button("Cancel", role: .cancel) { pendingImport = nil }
            } message: { pending in
                Text("Earnest will subscribe to the feeds in “\(pending.name)” and create its folders. Feeds you already follow are skipped.")
            }
    }

    // MARK: Sheets, files and links

    private var showsSubscribe: Binding<Bool> {
        Binding(get: { window.subscribing && model.loggedIn }, set: { window.subscribing = $0 })
    }

    private var showsExporter: Binding<Bool> {
        Binding(get: { exportDocument != nil }, set: { if !$0 { exportDocument = nil } })
    }

    private func withFiles<Content: View>(_ content: Content) -> some View {
        content
            .sheet(isPresented: $window.palette) {
                PaletteView().environmentObject(window)
            }
            .sheet(isPresented: showsSubscribe) {
                NewSubscriptionSheet().environmentObject(model).environmentObject(window)
            }
            .fileImporter(isPresented: $importing, allowedContentTypes: [.opml, .xml],
                          allowsMultipleSelection: false) { result in
                guard case .success(let urls) = result, let url = urls.first else { return }
                stage(url)
            }
            .fileImporter(isPresented: $importingStarred, allowedContentTypes: [.json],
                          allowsMultipleSelection: false) { result in
                guard case .success(let urls) = result, let url = urls.first else { return }
                stageStarred(url)
            }
            .sheet(isPresented: showsStarredImportConfirmation) {
                if let pending = pendingStarredImport {
                    StarredImportConfirmationSheet(name: pending.name, subscribeMissing: $starredSubscribeMissing) {
                        startStarredImport(pending.data, name: pending.name, subscribeMissing: starredSubscribeMissing)
                        pendingStarredImport = nil
                    } onCancel: {
                        pendingStarredImport = nil
                    }
                }
            }
            .fileExporter(isPresented: showsExporter, document: exportDocument, contentType: .opml,
                          defaultFilename: "Earnest Subscriptions") { result in
                if case .failure(let error) = result { model.report(error) }
                exportDocument = nil
            }
            .onOpenURL { open($0) }
            .dropDestination(for: URL.self) { urls, _ in _ = handleDrop(urls) }
            .onReceive(NotificationCenter.default.publisher(for: .earnestImportSubscriptions)) { _ in
                if window.presentsAppAlerts, model.loggedIn { importing = true }
            }
            .onReceive(NotificationCenter.default.publisher(for: .earnestImportStarredArticles)) { _ in
                if window.presentsAppAlerts, model.loggedIn { importingStarred = true }
            }
            .onReceive(NotificationCenter.default.publisher(for: .earnestExportSubscriptions)) { _ in
                export()
            }
            .onChange(of: model.openSettingsRequest) { if window.presentsAppAlerts { openSettings() } }
    }

    /// A dropped OPML file asks to import; a dropped web address or `feed:`
    /// link fills in New Subscription.
    private func handleDrop(_ urls: [URL]) -> Bool {
        guard model.loggedIn, let url = urls.first else { return false }
        if url.isFileURL {
            if url.pathExtension.lowercased() == "json" { stageStarred(url) } else { stage(url) }
            return true
        }
        let address: String
        switch url.scheme?.lowercased() {
        case "feed":
            guard let unwrapped = FeedLink.subscriptionURL(url) else { return false }
            address = unwrapped
        case "http", "https":
            guard url.user == nil, url.password == nil else { return false }
            address = url.absoluteString
        default:
            return false
        }
        window.subscribeURL = address
        window.subscribing = true
        return true
    }

    private func export() {
        guard window.presentsAppAlerts, model.loggedIn else { return }
        Task {
            do {
                if let data = try await model.exportOPML() { exportDocument = OPMLDocument(data: data) }
            }
            catch is CancellationError {}
            catch { model.report(error) }
        }
    }

    /// Opening an OPML file asks before importing; external `earnest:` links
    /// open articles and `feed:` links offer a subscription.
    private func open(_ url: URL) {
        if url.isFileURL {
            if url.pathExtension.lowercased() == "json" { stageStarred(url) } else { stage(url) }
        } else if let feed = FeedLink.subscriptionURL(url) {
            // Shown once the account is signed in, if it is not yet.
            window.subscribeURL = feed
            window.subscribing = true
        } else {
            window.openDeepLink(url)
        }
    }

    private func stage(_ url: URL) {
        let access = url.startAccessingSecurityScopedResource()
        defer { if access { url.stopAccessingSecurityScopedResource() } }
        do {
            let size = try url.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0
            guard size <= AppModel.opmlLimit else {
                model.error = "“\(url.lastPathComponent)” is too large to be a subscription list."
                return
            }
            pendingImport = PendingImport(name: url.lastPathComponent, data: try Data(contentsOf: url))
        } catch {
            model.error = "Earnest couldn't read “\(url.lastPathComponent)”. \(error.localizedDescription)"
        }
    }

    /// A starred-articles file asks before importing, like an OPML file.
    private func stageStarred(_ url: URL) {
        let access = url.startAccessingSecurityScopedResource()
        defer { if access { url.stopAccessingSecurityScopedResource() } }
        do {
            let size = try url.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0
            guard size <= AppModel.keptLimit else {
                model.error = "“\(url.lastPathComponent)” is too large to be a starred-articles export."
                return
            }
            pendingStarredImport = PendingStarredImport(name: url.lastPathComponent, data: try Data(contentsOf: url))
            // Each file asks afresh: an earlier file's choice to subscribe
            // must not carry over to this one.
            starredSubscribeMissing = false
        } catch {
            model.error = "Earnest couldn't read “\(url.lastPathComponent)”. \(error.localizedDescription)"
        }
    }

    private func startStarredImport(_ data: Data, name: String, subscribeMissing: Bool) {
        StarredImportCoordinator.shared.importStarred(model: model, document: data, name: name,
                                                      subscribeMissing: subscribeMissing) { keptImportReport = $0 }
    }

    private struct PendingImport: Identifiable {
        let id = UUID()
        let name: String
        let data: Data
    }

    private struct PendingStarredImport: Identifiable {
        let id = UUID()
        let name: String
        let data: Data
    }
}

extension Notification.Name {
    static let earnestImportSubscriptions = Notification.Name("EarnestImportSubscriptions")
    static let earnestExportSubscriptions = Notification.Name("EarnestExportSubscriptions")
}

extension UTType {
    /// OPML subscription lists, declared by the app as an XML subtype.
    static let opml = UTType(importedAs: "org.opml.opml", conformingTo: .xml)
}

/// An exported subscription list for the save panel.
struct OPMLDocument: FileDocument {
    static let readableContentTypes: [UTType] = [.opml, .xml]
    static let writableContentTypes: [UTType] = [.opml]
    let data: Data

    init(data: Data) { self.data = data }

    init(configuration: ReadConfiguration) throws {
        data = configuration.file.regularFileContents ?? Data()
    }

    func fileWrapper(configuration: WriteConfiguration) throws -> FileWrapper {
        FileWrapper(regularFileWithContents: data)
    }
}

/// Hands the hosting NSWindow to the model, for keyboard routing.
struct WindowAccessor: NSViewRepresentable {
    let found: (NSWindow?) -> Void

    func makeNSView(context: Context) -> NSView {
        let view = AccessorView()
        view.found = found
        return view
    }

    func updateNSView(_ view: NSView, context: Context) {}

    private final class AccessorView: NSView {
        var found: ((NSWindow?) -> Void)?
        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            found?(window)
        }
    }
}

// MARK: - Main window

@MainActor struct MainView: View {
    @EnvironmentObject private var model: AppModel
    @EnvironmentObject private var window: WindowModel
    @SwiftUI.State private var searchFocused = false

    var body: some View {
        NavigationSplitView {
            SidebarView()
                .navigationSplitViewColumnWidth(min: 180, ideal: 210, max: 300)
        } content: {
            contentColumn
                .navigationSplitViewColumnWidth(min: 300, ideal: 360, max: 520)
        } detail: {
            detailColumn
        }
        .navigationTitle(title)
        .navigationSubtitle(subtitle)
        .searchable(text: searchText, isPresented: $searchFocused, placement: .toolbar, prompt: "Search Articles")
        .onSubmit(of: .search) { window.submitSearch() }
        .onChange(of: window.searchFocusRequest) { searchFocused = true }
        .onChange(of: window.section) { window.refreshSection() }
        // Not customizable: AppKit can fail to restore a saved layout that
        // includes the split view's separators, which crashes at launch.
        .toolbar { toolbar }
    }

    private var searchText: Binding<String> {
        Binding(get: { window.search.query }, set: { text in
            window.search.query = text
            // Clearing the field leaves search, as it does in Mail.
            if text.isEmpty { window.endSearch() }
        })
    }

    private var title: String {
        if window.section == .search {
            return window.search.selectedSaved?.name ?? "Search"
        }
        return window.section.title
    }

    private var subtitle: String {
        switch window.section {
        case .today, .later, .kept, .done:
            let count = window.rows.count
            return count == 0 ? "" : "\(count) article\(count == 1 ? "" : "s")"
        case .edition:
            let count = window.editionGroups.reduce(0) { $0 + $1.rows.count }
            return count == 0 ? "" : "\(count) article\(count == 1 ? "" : "s")"
        case .feeds:
            let count = model.feeds.count
            return "\(count) subscription\(count == 1 ? "" : "s")"
        case .activity:
            return ""
        case .search:
            guard window.search.hasSubmittedQuery else { return "" }
            let count = window.search.matches.count
            return window.search.loading && count == 0 ? "Searching…" : "\(count) result\(count == 1 ? "" : "s")"
        }
    }

    private var entrySelection: Binding<Int64?> {
        Binding(get: { window.selected }, set: { window.selectFromList($0) })
    }

    @ViewBuilder private var contentColumn: some View {
        switch window.section {
        case .today:
            TodayColumn(selection: entrySelection)
        case .edition:
            if window.editionGroups.isEmpty {
                ContentUnavailableView("Nothing in This Week’s Edition", systemImage: "newspaper",
                                       description: Text("Feeds set to This Week collect here."))
            } else {
                EntryTable(groups: window.editionGroups, selection: entrySelection, actions: window.tableActions, takeFocusRequest: window.takeListFocusRequest)
            }
        case .later, .kept, .done:
            if window.rows.isEmpty {
                ContentUnavailableView(emptyTitle, systemImage: window.section.icon, description: Text(emptyDetail))
            } else {
                EntryTable(entries: window.rows, selection: entrySelection, actions: window.tableActions, takeFocusRequest: window.takeListFocusRequest)
            }
        case .feeds:
            FeedsView()
        case .activity:
            ActivityView()
        case .search:
            SearchView(search: window.search)
        }
    }

    private var emptyTitle: String {
        switch window.section {
        case .later: return "Nothing for Later"
        case .kept: return "Nothing Kept"
        default: return "Nothing Done Yet"
        }
    }

    private var emptyDetail: String {
        switch window.section {
        case .later: return "Press S on an article to save it for later."
        case .kept: return "Press F on an article to keep it."
        default: return "Articles you finish with appear here."
        }
    }

    @ViewBuilder private var detailColumn: some View {
        if window.section.showsReader {
            ReaderPane(reader: window.reader)
        } else {
            FeedDetailView()
        }
    }

    @ToolbarContentBuilder private var toolbar: some ToolbarContent {
        ToolbarItem(placement: .navigation) {
            Button {
                model.scheduleSync()
            } label: {
                Label("Refresh", systemImage: "arrow.clockwise")
            }
            .help("Refresh (⌘R)")
            .disabled(!model.loggedIn || model.syncing)
        }
        ToolbarItem(placement: .navigation) {
            Button {
                window.subscribing = true
            } label: {
                Label("New Subscription", systemImage: "plus")
            }
            .help("New Subscription (⌘N)")
        }
        ToolbarItem(placement: .primaryAction) {
            Button {
                window.command(.done)
            } label: {
                Label("Done", systemImage: "checkmark.circle")
            }
            .help("Move to Done (E)")
            .disabled(!window.canActOnEntry)
        }
        ToolbarItem(placement: .primaryAction) {
            Button {
                window.command(.later)
            } label: {
                Label(window.selectedIsLater ? "Remove from Later" : "Later",
                      systemImage: window.selectedIsLater ? "clock.badge.xmark" : "clock")
            }
            .help(window.selectedIsLater ? "Remove from Later (S)" : "Move to Later (S)")
            .disabled(!window.canActOnEntry)
        }
        ToolbarItem(placement: .primaryAction) {
            Button {
                window.command(.keep)
            } label: {
                Label(window.selectedIsKept ? "Unkeep" : "Keep",
                      systemImage: window.selectedIsKept ? "bookmark.fill" : "bookmark")
            }
            .help(window.selectedIsKept ? "Unkeep (F)" : "Keep (F)")
            .disabled(!window.canActOnEntry)
        }
        ToolbarItem(placement: .primaryAction) {
            Button {
                window.command(.unread)
            } label: {
                Label("Mark as Unread", systemImage: "circle.fill")
            }
            .help("Mark as Unread (M)")
            .disabled(!window.canActOnEntry || window.selectedIsUnread)
        }
        ToolbarSpacer(.fixed, placement: .primaryAction)
        ToolbarItem(placement: .primaryAction) {
            Button {
                window.openOriginal()
            } label: {
                Label("Open in Browser", systemImage: "safari")
            }
            .help("Open in Browser (O)")
            .disabled(window.selectedURL == nil)
        }
        ToolbarItem(placement: .primaryAction) {
            if let url = window.selectedURL {
                ShareLink(item: url, subject: Text(window.selectedTitle)) {
                    Label("Share", systemImage: "square.and.arrow.up")
                }
                .help("Share")
            } else {
                Button {} label: { Label("Share", systemImage: "square.and.arrow.up") }
                    .disabled(true)
            }
        }
    }
}

/// Today: agent briefs, the river, and its written end.
@MainActor private struct TodayColumn: View {
    @EnvironmentObject private var model: AppModel
    @EnvironmentObject private var window: WindowModel
    @Binding var selection: Int64?

    var body: some View {
        VStack(spacing: 0) {
            if !model.briefs.isEmpty {
                BriefsView(briefs: model.briefs, names: model.principalNames)
                Divider()
            }
            if window.rows.isEmpty {
                ContentUnavailableView {
                    Label(window.endTitle.isEmpty ? "Nothing New" : window.endTitle, systemImage: "sun.max")
                } description: {
                    if !window.endDetail.isEmpty { Text(window.endDetail) }
                }
            } else {
                EntryTable(entries: window.rows, selection: $selection, actions: window.tableActions, takeFocusRequest: window.takeListFocusRequest)
                if !window.endTitle.isEmpty {
                    EndStatement(title: window.endTitle, detail: window.endDetail)
                }
            }
        }
    }
}

/// "That is everything since Tuesday." The river's written end.
private struct EndStatement: View {
    let title: String
    let detail: String

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title).font(.callout)
            if !detail.isEmpty {
                Text(detail)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .background(.bar)
        .overlay(alignment: .top) { Divider() }
        .accessibilityElement(children: .combine)
    }
}

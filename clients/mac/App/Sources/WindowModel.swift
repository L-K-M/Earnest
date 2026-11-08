import Combine
import Foundation
import EarnestKit
#if canImport(SwiftUI)
import SwiftUI
#endif
#if canImport(AppKit)
import AppKit
#endif

/// One per window: the section, list, selection and reader this window
/// shows. Identity, sync and account data live in the shared AppModel,
/// which calls rebuildPresentation() on every window when they change, so
/// a write issued in one window appears in the others after the rebuild.
@MainActor final class WindowModel: ObservableObject {
    @Published var section: Section = .today
    @Published var entries: [APIEntry] = []
    @Published var rows: [EntryRow] = []
    @Published var editionGroups: [EditionGroup] = []
    @Published var selected: Int64? {
        didSet {
            guard oldValue != selected else { return }
            // The article read before stays in the river only while it is
            // open; drop it from the list as soon as the selection moves.
            if oldValue != nil, section == .today || section == .edition { rebuildPresentation() }
            open(selected)
        }
    }
    @Published var html = ""
    @Published private(set) var imageOrigin: URL?
    @Published private(set) var imageMode: ArticleImageMode = .proxied
    @Published private(set) var readingEntry: APIEntry?
    @Published private(set) var offlineCopy = false
    /// The selected article could not be fetched and has no offline copy.
    @Published private(set) var articleUnavailable = false
    /// Why the selected article failed to load, when it did; the reader
    /// offers Try Again instead of a spinner that never ends.
    @Published private(set) var readerFailure: String?
    @Published var palette = false
    /// Mirrors the account's single-key preference so commands can drop the
    /// unmodified-letter bindings without disabling the menu actions.
    @Published private(set) var singleKeyShortcuts = true
    /// The resolved typography for the open article, including its feed's
    /// override. Recomputed from the shared settings on every rebuild.
    @Published private(set) var readerStyle = ReaderStyle.default
    let search: SearchModel
    /// The live WKWebView behind this window's reader, for zoom and find.
    let reader = ReaderController()
    @Published var endTitle = ""
    @Published var endDetail = ""
    @Published var subscribeURL = ""
    @Published var subscribeCadence = "today"
    /// The New Subscription sheet is showing.
    @Published var subscribing = false
    /// The subscription selected in Feeds, shown in the detail column.
    @Published var selectedFeedID: Int64?
    /// A subscription waiting for Unsubscribe to be confirmed.
    @Published var unsubscribing: FeedInfo?
    /// The search result row selected, which may be an article or an agent note.
    @Published var selectedMatchID: String?
    /// The activity row selected in Activity.
    @Published var selectedActivityID: Int64?
    /// The window's undo manager, supplied by its view. Reading actions
    /// register their inverse here so Edit > Undo names and reverses them.
    weak var undoManager: UndoManager?
    /// The window this model drives, for routing plain-key shortcuts.
    weak var nsWindow: NSWindow?
    /// Whether this window presents app-wide alerts (errors, Sign Out): the
    /// key reading window, or the first one when Settings or nothing is key,
    /// so exactly one window asks.
    var presentsAppAlerts: Bool {
        #if canImport(AppKit)
        let windows = app.windows
        if let key = NSApp.keyWindow ?? NSApp.mainWindow {
            let owner = key.sheetParent ?? key
            if let front = windows.first(where: { $0.nsWindow === owner }) { return front === self }
        }
        return windows.first === self
        #else
        return true
        #endif
    }
    /// Where clearing the search field returns to.
    private(set) var sectionBeforeSearch: Section = .today
    /// An article restored with the window, opened once the account's cache is ready.
    var pendingRestoredSelection: Int64?
    /// An `earnest:` link that arrived before the account's cache was ready.
    private(set) var pendingDeepLink: Int64?

    /// Keep a pending link across a reconnect, which ends the old session.
    func deferDeepLink(_ id: Int64) {
        section = .today
        imageMode = .click
        pendingDeepLink = id
    }

    let app: AppModel
    private var tasks: [String: Task<Void, Never>] = [:]
    private var searchChanges: AnyCancellable?
    // Whether this window's candidates came from the cache or the server's
    // filtered river. Another window's successful fetch must not change it.
    private var cachedPresentation = false

    init(app: AppModel) {
        self.app = app
        search = SearchModel(app: app)
        singleKeyShortcuts = app.readerSettings.singleKeyShortcuts
        readerStyle = app.readerStyle(for: nil)
        if let sibling = app.windows.first(where: { $0.section == section && !$0.entries.isEmpty }) {
            entries = sibling.entries
            cachedPresentation = sibling.cachedPresentation
        } else {
            do { entries = try app.currentEntries() }
            catch { app.report(error) }
            cachedPresentation = true
        }
        app.register(self)
        rebuildPresentation()
        // The window's title, sidebar and commands read the search state, so
        // a change to it is a change to the window.
        searchChanges = search.objectWillChange.sink { [weak self] _ in self?.objectWillChange.send() }
    }

    func restoreCachedEntries(_ entries: [APIEntry]) {
        self.entries = keepingOpenArticle(entries)
        cachedPresentation = true
    }

    /// Put a cached article back into this window's entries.
    private func restoreEntryFromCache(_ id: Int64) {
        guard !entries.contains(where: { $0.id == id }), let engine = app.engine, app.principalID > 0,
              let cached = try? engine.entry(id), !cached.deleted, cached.hasBody,
              let entry = try? cachedEntry(cached, principal: app.principalID, engine: engine) else { return }
        entries.insert(entry, at: entries.firstIndex { $0.seq > entry.seq } ?? entries.endIndex)
    }

    /// The server's river omits an article once it is seen, so a refresh
    /// would drop the one you are reading, and with it the commands that act
    /// on it. Keep the open article until the selection moves on.
    private func keepingOpenArticle(_ fresh: [APIEntry]) -> [APIEntry] {
        guard let selected, !fresh.contains(where: { $0.id == selected }),
              let open = entries.first(where: { $0.id == selected }) ?? readingEntry.flatMap({ $0.id == selected ? $0 : nil })
        else { return fresh }
        var kept = fresh
        kept.insert(open, at: kept.firstIndex { $0.seq > open.seq } ?? kept.endIndex)
        return kept
    }

    /// Every chunk of async work is a named task so selection changes cancel
    /// superseded loads; the app session counter guards writes that resume
    /// after the shared session ended.
    private func runTask(_ key: String, _ work: @escaping @MainActor () async -> Void) {
        tasks[key]?.cancel()
        let expectedSession = app.session
        tasks[key] = Task { [weak self] in
            guard let self, self.app.session == expectedSession, !Task.isCancelled else { return }
            await work()
        }
    }

    /// The shared session ended: drop everything this window fetched or opened.
    func endSession() {
        tasks.values.forEach { $0.cancel() }
        tasks.removeAll()
        search.reset()
        singleKeyShortcuts = true
        readerStyle = .default
        subscribing = false
        subscribeURL = ""
        subscribeCadence = "today"
        readerFailure = nil
        unsubscribing = nil
        selectedMatchID = nil
        selectedFeedID = nil
        selectedActivityID = nil
        pendingRestoredSelection = nil
        pendingDeepLink = nil
        undoManager?.removeAllActions(withTarget: self)
        entries = []
        cachedPresentation = false
        rows = []
        editionGroups = []
        selected = nil
        html = ""
        imageOrigin = nil
        imageMode = .proxied
        readingEntry = nil
        offlineCopy = false
        articleUnavailable = false
        endTitle = ""
        endDetail = ""
    }

    // MARK: - Refresh

    /// Named-task wrapper so section changes cancel the previous fetch.
    func refreshSection() {
        runTask("list") { [weak self] in
            await self?.refresh()
        }
    }

    func refresh() async {
        guard app.loggedIn else { return }
        let expected = section
        switch expected {
        case .today:
            await app.refreshFeeds()
            async let briefsTask: Void = app.refreshBriefs()
            async let changesTask: Void = app.refreshAgentChanges()
            await refreshEntries()
            _ = await (briefsTask, changesTask)
        case .edition:
            await app.refreshFeeds()
            await refreshEntries()
        case .later, .kept, .done:
            await refreshEntries()
        case .feeds:
            await app.refreshFeeds()
        case .activity:
            await app.refreshActivity()
        case .search:
            search.loadMetadata()
        }
        guard self.section == expected, app.loggedIn, !Task.isCancelled else { return }
        app.rebuildPresentation()
    }

    func refreshEntries() async {
        // Keep the last fetched entries on non-entry sections; commands and
        // content loading still need them.
        guard let api = app.api, let engine = app.engine, let view = section.entryView,
              let generation = try? engine.snapshot().cursor?.generation else { return }
        let expected = section
        let expectedSession = app.session
        do {
            var all: [APIEntry] = []
            var cursor: String?
            var seenCursors = Set<String>()
            repeat {
                let page = try await api.entries(view: view, cursor: cursor)
                guard app.session == expectedSession, (try? engine.snapshot().cursor?.generation) == generation,
                      !Task.isCancelled else { return }
                all.append(contentsOf: page.entries ?? [])
                cursor = page.nextCursor
                if let cursor, !seenCursors.insert(cursor).inserted { throw APIError.invalidResponse }
            } while cursor != nil && !Task.isCancelled
            guard self.section == expected, app.loggedIn, app.session == expectedSession, !Task.isCancelled else { return }
            try engine.mergeEntries(all, generation: generation)
            cachedPresentation = false
            entries = keepingOpenArticle(all)
        } catch is CancellationError {
        } catch {
            guard app.session == expectedSession, (try? engine.snapshot().cursor?.generation) == generation else { return }
            if ErrorPresentation.isConnection(error) {
                do { try app.restoreCachedPresentation() }
                catch { app.report(error) }
            }
            app.report(error)
        }
    }

    // MARK: - Presentation

    /// Displayed entries never mutate the fetched APIEntry: each row's state
    /// is the fetched state merged with the engine's synced-plus-overlay state.
    private func effectiveState(of entry: APIEntry) -> EarnestKit.State {
        guard let engine = app.engine, app.principalID > 0 else { return entry.state.syncState }
        return (try? engine.state(principal: app.principalID, entryID: entry.id)) ?? entry.state.syncState
    }

    private func cadence(of entry: APIEntry) -> String {
        app.feedMap[entry.subscriptionID]?.cadence ?? "today"
    }

    private func row(_ entry: APIEntry, _ state: EarnestKit.State) -> EntryRow {
        // Source, then age, then author: truncation drops the least useful part first.
        var subtitle = app.feedMap[entry.subscriptionID]?.title ?? "Feed \(entry.subscriptionID)"
        let date = entry.publishedAt.flatMap(DateFormatting.parse) ?? DateFormatting.parse(entry.firstSeenAt)
        if let date { subtitle += " · \(DateFormatting.relative.localizedString(for: date, relativeTo: Date()))" }
        if !entry.author.isEmpty { subtitle += " · \(entry.author)" }
        // Kept and Later show as row markers; Done, which has none, stays in words.
        if state.shelf.value == .text("done") && section != .done { subtitle += " · Done" }
        let url = URL(string: entry.url).flatMap { ["http", "https"].contains($0.scheme?.lowercased() ?? "") ? $0 : nil }
        return EntryRow(id: entry.id, title: entry.title.isEmpty ? "Untitled" : entry.title,
                        subtitle: subtitle, unread: state.seen.value == .text("unseen"),
                        kept: state.kept.value == .flag(true), later: state.shelf.value == .text("later"),
                        url: url)
    }

    func rebuildPresentation() {
        search.invalidateAfterRestore()
        singleKeyShortcuts = app.readerSettings.singleKeyShortcuts
        readerStyle = app.readerStyle(for: readingEntry)
        let snapshot = try? app.engine?.snapshot()
        let knownIDs = Set(entries.map(\.id))
        let pendingIDs = Set(snapshot?.outbox.filter { $0.principal == app.principalID }.map(\.entryID) ?? [])
        let isRiver = section == .today || section == .edition
        let missing = (snapshot?.entries ?? []).filter {
            !$0.deleted && $0.hasBody && !knownIDs.contains($0.id) && section.entryView != nil
                && (!isRiver || cachedPresentation || pendingIDs.contains($0.id))
        }
        // Prefetch states for fetched candidates and possible additions once.
        // A shelf window may not have fetched an entry that another window
        // just moved there, especially while offline.
        let bulk: [Int64: EarnestKit.State]? = app.principalID > 0
            ? (try? app.engine?.states(principal: app.principalID, entryIDs: entries.map(\.id) + missing.map(\.id))) ?? nil
            : nil
        if let snapshot, let engine = app.engine {
            let subscriptions = Dictionary(snapshot.objects.filter { $0.kind == .subscription }.map { ($0.id, $0) }, uniquingKeysWith: { _, last in last })
            let resources = snapshot.objects.filter { $0.kind == .mute || $0.kind == .settings }
            let now = Date()
            do {
                let additions = try missing.compactMap { entry -> APIEntry? in
                    guard let state = bulk?[entry.id] else { return nil }
                    let belongs: Bool
                    switch section {
                    case .today, .edition:
                        belongs = River.contains(entry, state: state, subscription: subscriptions[entry.subscriptionID], resources: resources, now: now)
                    case .later: belongs = state.shelf.value == .text("later")
                    case .kept: belongs = state.kept.value == .flag(true)
                    case .done: belongs = state.shelf.value == .text("done")
                    case .feeds, .activity, .search: belongs = false
                    }
                    return belongs ? try cachedEntry(entry, principal: app.principalID, engine: engine, state: state) : nil
                }
                if !additions.isEmpty { entries = (entries + additions).sorted { $0.seq < $1.seq } }
            } catch { app.report(error) }
        }
        let goneIDs = Set(snapshot?.entries.filter(\.deleted).map(\.id) ?? [])
        let visible = entries.filter { !$0.deleted && !goneIDs.contains($0.id) }
        let presented = visible.map { ($0, bulk?[$0.id] ?? $0.state.syncState) }
        let riverIDs: Set<Int64>
        if cachedPresentation, let snapshot {
            let subscriptions = Dictionary(snapshot.objects.filter { $0.kind == .subscription }.map { ($0.id, $0) }, uniquingKeysWith: { _, last in last })
            let states = Dictionary(presented.map { ($0.0.id, $0.1) }, uniquingKeysWith: { _, last in last })
            let resources = snapshot.objects.filter { $0.kind == .mute || $0.kind == .settings }
            let now = Date()
            riverIDs = Set(snapshot.entries.filter { entry in
                guard let state = states[entry.id] else { return false }
                return River.contains(entry, state: state, subscription: subscriptions[entry.subscriptionID], resources: resources, now: now)
            }.map(\.id))
        } else {
            riverIDs = Set(presented.filter { $0.1.seen.value == .text("unseen") && $0.1.shelf.value == .text("none") }.map { $0.0.id })
        }
        // Opening an article marks it seen, which takes it out of the river.
        // Keep the open article in place until the selection moves on, as Mail
        // does in an unread view, so the list never shifts under the reader.
        // Explicit Done and Later still remove it at once.
        let sticky: Int64? = presented.first {
            $0.0.id == selected && $0.1.shelf.value == .text("none") && !riverIDs.contains($0.0.id)
        }?.0.id
        let inRiver = { (id: Int64) in riverIDs.contains(id) || id == sticky }
        switch section {
        case .today:
            rows = presented.filter { inRiver($0.0.id) && cadence(of: $0.0) != "week" }.map { row($0.0, $0.1) }
            editionGroups = []
        case .edition:
            let week = presented.filter { inRiver($0.0.id) && cadence(of: $0.0) == "week" }
            var groups: [Int64: EditionGroup] = [:]
            var order: [Int64] = []
            for (entry, state) in week {
                if groups[entry.subscriptionID] == nil {
                    order.append(entry.subscriptionID)
                    groups[entry.subscriptionID] = EditionGroup(
                        subscriptionID: entry.subscriptionID,
                        title: app.feedMap[entry.subscriptionID]?.title ?? "Feed \(entry.subscriptionID)",
                        rows: [])
                }
                groups[entry.subscriptionID]?.rows.append(row(entry, state))
            }
            editionGroups = order.sorted { (groups[$0]?.title ?? "") < (groups[$1]?.title ?? "") }.compactMap { groups[$0] }
            rows = []
        case .later:
            rows = presented.filter { $0.1.shelf.value == .text("later") }.map { row($0.0, $0.1) }
            editionGroups = []
        case .done:
            rows = presented.filter { $0.1.shelf.value == .text("done") }.map { row($0.0, $0.1) }
            editionGroups = []
        case .kept:
            rows = presented.filter { $0.1.kept.value == .flag(true) }.map { row($0.0, $0.1) }
            editionGroups = []
        case .feeds, .activity, .search:
            rows = []
            editionGroups = []
        }
        updateEndStatement(presented, riverIDs: riverIDs)
        if let link = pendingDeepLink, app.engine != nil, app.loggedIn {
            pendingDeepLink = nil
            pendingRestoredSelection = nil
            // External links never load images automatically.
            imageMode = .click
            if selected == link { open(link) } else { selected = link }
        } else if let pending = pendingRestoredSelection, app.engine != nil, app.loggedIn {
            pendingRestoredSelection = nil
            // A restored article that is no longer cached is not worth an error at launch.
            if entries.contains(where: { $0.id == pending }), selected == nil { selected = pending }
        }
    }

    private func updateEndStatement(_ presented: [(APIEntry, EarnestKit.State)], riverIDs: Set<Int64>) {
        guard section == .today else {
            endTitle = ""
            endDetail = ""
            return
        }
        let river = presented.filter { riverIDs.contains($0.0.id) && cadence(of: $0.0) != "week" }
        if river.isEmpty {
            endTitle = "Nothing new. A rare and beautiful thing."
        } else if let since = river.compactMap({ DateFormatting.parse($0.0.firstSeenAt) }).min() {
            endTitle = "That is everything since \(DateFormatting.endDay(since))."
        } else {
            endTitle = "That is everything."
        }
        var parts: [String] = []
        if !app.sessionActions.summary.isEmpty { parts.append(app.sessionActions.summary) }
        if !app.agentChanges.isEmpty {
            let names = Array(Set(app.agentChanges.map(\.principalName).filter { !$0.isEmpty })).sorted()
            let label = names.isEmpty ? "Your agents" : names.joined(separator: ", ")
            let count = app.agentChanges.count
            parts.append("While you were out: \(label) changed \(count) unread entr\(count == 1 ? "y" : "ies"). Review them in Activity.")
        }
        endDetail = parts.joined(separator: "\n")
    }

    private func visibleEntryIDs() -> [Int64] {
        switch section {
        case .edition: return editionGroups.flatMap(\.rowIDs)
        case .search:
            var seen = Set<Int64>()
            return search.matches.map(\.entryID).filter { seen.insert($0).inserted }
        case .activity:
            var seen = Set<Int64>()
            return app.activityGroups.flatMap(\.items).compactMap(\.entryID).filter { seen.insert($0).inserted }
        default: return rows.map(\.id)
        }
    }

    // MARK: - Commands

    var canBrowse: Bool {
        app.loggedIn && (section.entryView != nil || section == .search || section == .activity)
    }

    /// The effective state of the open article, for menu titles and toggles.
    var selectedState: EarnestKit.State? {
        guard let entry = entries.first(where: { $0.id == selected }) else { return nil }
        return effectiveState(of: entry)
    }

    var selectedIsKept: Bool { selectedState?.kept.value == .flag(true) }
    var selectedIsLater: Bool { selectedState?.shelf.value == .text("later") }
    var selectedIsUnread: Bool { selectedState?.seen.value == .text("unseen") }

    /// The article's link, when it is a web address the browser can open.
    var selectedURL: URL? {
        guard let entry = readingEntry ?? entries.first(where: { $0.id == selected }),
              let url = URL(string: entry.url), ["http", "https"].contains(url.scheme?.lowercased() ?? "") else { return nil }
        return url
    }

    var selectedTitle: String {
        let entry = readingEntry ?? entries.first(where: { $0.id == selected })
        return entry.map { $0.title.isEmpty ? "Untitled" : $0.title } ?? ""
    }
    var canActOnEntry: Bool {
        guard let selected, canBrowse, app.engine != nil, app.principalID > 0 else { return false }
        return entries.contains { $0.id == selected && !$0.deleted }
    }

    /// Apply a reading action to the open article, or to the row a context
    /// menu was opened on.
    func command(_ command: EntryCommand, entryID: Int64? = nil) {
        let target = entryID ?? selected
        guard let entry = entries.first(where: { $0.id == target && !$0.deleted }), let engine = app.engine,
              app.principalID > 0 else { return }
        guard let generation = try? engine.snapshot().cursor?.generation else {
            app.connectionNote = "Still syncing; try again in a moment."
            return
        }
        let state = effectiveState(of: entry)
        let register: Register
        let value: RegisterValue
        switch command {
        case .done: register = .shelf; value = .text("done")
        case .later:
            // Later toggles, as `s` does on the web: a Later article leaves the shelf.
            register = .shelf
            value = state.shelf.value == .text("later") ? .text("none") : .text("later")
        case .unread: register = .seen; value = .text("unseen")
        case .keep:
            register = .kept
            value = .flag(state.kept.value != .flag(true))
        }
        let write = Write(opID: "mac.\(UUID().uuidString.lowercased())",
                          principal: app.principalID, entryID: entry.id,
                          register: register, value: value,
                          base: Cursor(generation: generation, rev: state[register].rev),
                          occurredAt: Date())
        do {
            let index = visibleEntryIDs().firstIndex(of: entry.id)
            try engine.queue(write)
            let unkeep = command == .keep && value == .flag(false)
            let counted = !(command == .later && value == .text("none"))
            if counted { app.sessionActions.record(command, unkeep: unkeep) }
            registerUndo(entryID: entry.id, prior: Self.registers(of: state),
                         actionName: Self.actionName(command, value: value),
                         command: counted ? command : nil, unkeep: unkeep)
            app.rebuildPresentation()
            if entry.id == selected { keepSelection(near: index) }
            app.scheduleSync()
        } catch { app.report(error) }
    }

    /// Open a row's article in the default browser.
    func openInBrowser(entryID: Int64) {
        guard let entry = entries.first(where: { $0.id == entryID }), let url = URL(string: entry.url),
              ["http", "https"].contains(url.scheme?.lowercased() ?? "") else { return }
        #if canImport(AppKit)
        NSWorkspace.shared.open(url)
        #endif
    }

    /// Context-menu and double-click actions for this window's list.
    var tableActions: EntryTableActions {
        EntryTableActions(
            perform: { [weak self] command, id in self?.command(command, entryID: id) },
            openInBrowser: { [weak self] id in self?.openInBrowser(entryID: id) },
            canAct: app.loggedIn && app.engine != nil && app.principalID > 0)
    }

    static func actionName(_ command: EntryCommand, value: RegisterValue) -> String {
        switch command {
        case .done: return "Move to Done"
        case .later: return value == .text("none") ? "Remove from Later" : "Move to Later"
        case .keep: return value == .flag(false) ? "Unkeep" : "Keep"
        case .unread: return "Mark as Unread"
        }
    }

    private static func registers(of state: EarnestKit.State) -> [Register: RegisterValue] {
        [.seen: state.seen.value, .shelf: state.shelf.value, .kept: state.kept.value]
    }

    /// Undo writes back every register the action changed, including ones a
    /// server rule changed alongside it (Unread clears Done). Redo is the
    /// inverse of the undo, so the stack can go back and forth.
    private func registerUndo(entryID: Int64, prior: [Register: RegisterValue], actionName: String,
                              command: EntryCommand?, unkeep: Bool, undoing: Bool = false) {
        guard let undoManager else { return }
        let session = app.session
        undoManager.registerUndo(withTarget: self) { window in
            window.restore(entryID: entryID, to: prior, actionName: actionName, command: command,
                           unkeep: unkeep, undoing: !undoing, session: session)
        }
        undoManager.setActionName(actionName)
    }

    private func restore(entryID: Int64, to values: [Register: RegisterValue], actionName: String,
                         command: EntryCommand?, unkeep: Bool, undoing: Bool, session: Int) {
        guard app.session == session, app.loggedIn, let engine = app.engine, app.principalID > 0,
              let generation = try? engine.snapshot().cursor?.generation else { return }
        let state = (try? engine.state(principal: app.principalID, entryID: entryID))
            ?? EarnestKit.State(principal: app.principalID, entryID: entryID)
        let wasVisible = visibleEntryIDs().contains(entryID)
        let index = visibleEntryIDs().firstIndex(of: entryID)
        do {
            for register in [Register.shelf, .kept, .seen] {
                guard let value = values[register], state[register].value != value else { continue }
                try engine.queue(Write(opID: "mac.\(UUID().uuidString.lowercased())",
                                       principal: app.principalID, entryID: entryID,
                                       register: register, value: value,
                                       base: Cursor(generation: generation, rev: state[register].rev),
                                       occurredAt: Date()))
            }
        } catch {
            app.report(error)
            return
        }
        if let command {
            if undoing { app.sessionActions.unrecord(command, unkeep: unkeep) }
            else { app.sessionActions.record(command, unkeep: unkeep) }
        }
        registerUndo(entryID: entryID, prior: Self.registers(of: state), actionName: actionName,
                     command: command, unkeep: unkeep, undoing: undoing)
        app.rebuildPresentation()
        // Bring back an article the action had removed from this list, as
        // Mail reselects a message whose move is undone. Selecting it keeps a
        // read article in the river while it is open; a refresh since the
        // action may have dropped it from this window's entries, so restore
        // it from the cache first. If it still cannot be shown, stay put.
        if !wasVisible, selected != entryID {
            let previous = selected
            restoreEntryFromCache(entryID)
            selectFromList(entryID)
            if !visibleEntryIDs().contains(entryID) { selectFromList(previous) }
        } else if wasVisible, selected == entryID, !visibleEntryIDs().contains(entryID) {
            keepSelection(near: index)
        }
        app.scheduleSync()
    }

    private func keepSelection(near index: Int?) {
        guard let index else { return }
        let ids = visibleEntryIDs()
        if let selected, ids.contains(selected) { return }
        if !ids.isEmpty { selectFromList(ids[min(index, ids.count - 1)]) } else { selectFromList(nil) }
    }

    /// Opening an article is the desktop equivalent of scrolling past it: the
    /// seen register moves and the item leaves the river.
    private func markSeen(entryID: Int64, entry: APIEntry?) {
        guard let engine = app.engine, app.principalID > 0,
              let generation = try? engine.snapshot().cursor?.generation else { return }
        let state = entry.map { self.effectiveState(of: $0) }
            ?? (try? engine.state(principal: app.principalID, entryID: entryID))
            ?? EarnestKit.State(principal: app.principalID, entryID: entryID)
        guard state.seen.value != .text("seen") else { return }
        let write = Write(opID: "mac.\(UUID().uuidString.lowercased())",
                          principal: app.principalID, entryID: entryID,
                          register: .seen, value: .text("seen"),
                          base: Cursor(generation: generation, rev: state.seen.rev),
                          occurredAt: Date(), kind: .passive)
        try? engine.queue(write)
        app.rebuildPresentation()
        app.scheduleSync(delay: 1.5)
    }

    func move(_ offset: Int) {
        let ids = visibleEntryIDs()
        guard !ids.isEmpty else { return }
        let current = ids.firstIndex(of: selected ?? -1) ?? (offset > 0 ? -1 : ids.count)
        let id = ids[min(ids.count - 1, max(0, current + offset))]
        if section == .search, let match = search.matches.first(where: { $0.entryID == id }) {
            selectSearchMatch(match)
        } else {
            selectFromList(id)
        }
    }

    func openOriginal() {
        guard let url = selectedURL else { return }
        #if canImport(AppKit)
        NSWorkspace.shared.open(url)
        #endif
    }

    // MARK: - Navigation

    /// Navigate from the sidebar, Go menu or palette. The reader closes when
    /// the section changes, as switching mailboxes does in Mail, so a command
    /// never acts on an article the list no longer shows.
    func go(to target: Section) {
        guard target != section else { return }
        if target != .search { search.clearQuery() }
        selectFromList(nil)
        section = target
    }

    /// The toolbar search field submitted a query: show results in place of
    /// the current section, remembering where to return.
    func submitSearch() {
        guard search.canSearch else { return }
        if section != .search {
            sectionBeforeSearch = section
            selectFromList(nil)
            section = .search
        }
        search.selectSaved(nil)
        search.submit()
    }

    /// Open a saved search from the sidebar.
    func openSavedSearch(_ saved: APISavedSearch) {
        if section != .search {
            sectionBeforeSearch = section
            selectFromList(nil)
            section = .search
        }
        search.selectSaved(saved)
    }

    /// The search field was cleared: return to the section searched from.
    func endSearch() {
        guard section == .search else { return }
        search.clearQuery()
        selectFromList(nil)
        section = sectionBeforeSearch
        // Back to the list, as clearing Mail's search field does.
        requestListFocus()
    }

    /// Activity rows open the article an agent acted on. Agent-surfaced
    /// articles block automatic images, like agent-note search matches.
    func selectActivity(_ item: ActivityItem?) {
        selectedActivityID = item?.id
        guard let entryID = item?.entryID else {
            if item != nil { selectFromList(nil) }
            return
        }
        imageMode = .click
        if selected == entryID { open(entryID) } else { selected = entryID }
    }

    /// Change a feed's cadence with an undoable step.
    func setCadence(_ feed: FeedInfo, _ cadence: String) {
        let previous = feed.cadence
        guard previous != cadence else { return }
        app.setCadence(feed.id, cadence)
        guard let undoManager else { return }
        let session = app.session
        undoManager.registerUndo(withTarget: self) { window in
            guard window.app.session == session,
                  let current = window.app.feeds.first(where: { $0.id == feed.id }) else { return }
            window.setCadence(current, previous)
        }
        undoManager.setActionName("Change Cadence")
    }

    var readingFeedTitle: String {
        guard let entry = readingEntry else { return "Feed" }
        return app.feeds.first(where: { $0.id == entry.subscriptionID })?.title ?? "Feed"
    }

    func selectFromList(_ id: Int64?) {
        imageMode = .proxied
        selectedMatchID = nil
        if section == .activity, id == nil { selectedActivityID = nil }
        if selected == id { open(id) }
        else { selected = id }
    }

    func selectSearchMatch(_ match: APISearchMatch) {
        selectedMatchID = match.id
        imageMode = match.kind == "agent_note" || match.untrusted ? .click : .proxied
        if selected == match.entryID { open(match.entryID) }
        else { selected = match.entryID }
    }

    func openDeepLink(_ url: URL) {
        guard let id = DeepLink.entryID(url) else { return }
        section = .today
        imageMode = .click
        // A link that launched the app opens once the account's cache is ready.
        guard app.engine != nil else {
            pendingDeepLink = id
            return
        }
        if selected == id { open(id) }
        else { selected = id }
    }

    func loadImages() {
        guard selected != nil else { return }
        imageMode = .proxied
        open(selected)
    }

    func open(_ id: Int64?) {
        tasks["open"]?.cancel()
        html = ""
        imageOrigin = nil
        readingEntry = nil
        offlineCopy = false
        articleUnavailable = false
        readerFailure = nil
        guard let id, app.loggedIn else { return }
        let mode = imageMode
        runTask("open") { [weak self] in
            guard let self else { return }
            let app = self.app
            guard let api = app.api, let engine = app.engine, app.loggedIn,
                  let generation = try? engine.snapshot().cursor?.generation else { return }
            let expectedSession = app.session
            let cache = app.cache
            do {
                if !self.entries.contains(where: { $0.id == id }) {
                    let entry = try await api.entry(id: id)
                    try Task.checkCancellation()
                    guard app.loggedIn, app.session == expectedSession,
                          (try? engine.snapshot().cursor?.generation) == generation else { return }
                    try engine.mergeEntries([entry], generation: generation)
                    // A shared rebuild may have supplied it while the request waited.
                    if !self.entries.contains(where: { $0.id == id }) { self.entries.append(entry) }
                }
                let entry = self.entries.first(where: { $0.id == id })
                self.readingEntry = entry
                let article: String
                let origin: URL?
                var offline = false
                var contentError: Error?
                do {
                    let signed = try await api.signedContent(entryID: id, mode: mode)
                    article = signed.html
                    origin = signed.imageOrigin
                } catch is CancellationError {
                    return
                } catch {
                    if ErrorPresentation.isUnauthorized(error) { throw error }
                    do {
                        article = try await api.content(entryID: id).html
                    } catch {
                        guard ErrorPresentation.isConnection(error) else { throw error }
                        try Task.checkCancellation()
                        guard app.session == expectedSession,
                              (try? engine.snapshot().cursor?.generation) == generation else { return }
                        guard let saved = try cache?.article(entryID: id, generation: generation) else {
                            // Never opened before: say so, and retry after the next successful sync.
                            guard self.selected == id else { throw error }
                            self.articleUnavailable = true
                            app.connectionNote = ErrorPresentation.message(error)
                            return
                        }
                        article = saved
                        offline = true
                        contentError = error
                    }
                    origin = nil
                }
                guard self.selected == id, app.loggedIn, app.session == expectedSession,
                      (try? engine.snapshot().cursor?.generation) == generation, !Task.isCancelled else { return }
                // Persistence is best effort once the article is available to read.
                try? cache?.saveArticle(entryID: id, html: article, generation: generation)
                self.html = article
                self.imageOrigin = origin
                self.offlineCopy = offline
                if let contentError, ErrorPresentation.isConnection(contentError) {
                    app.connectionNote = ErrorPresentation.message(contentError)
                }
                self.markSeen(entryID: id, entry: self.entries.first(where: { $0.id == id }))
            } catch is CancellationError {
            } catch {
                guard app.loggedIn, app.session == expectedSession, self.selected == id, !Task.isCancelled,
                      (try? engine.snapshot().cursor?.generation) == generation else { return }
                var missing = (error as? APIProblem)?.status == 404
                if case APIError.httpStatus(404) = error { missing = true }
                if missing {
                    do {
                        try engine.markGone(entryID: id)
                        try cache?.removeArticle(entryID: id, generation: generation)
                    } catch {
                        app.report(error)
                        return
                    }
                    app.rebuildPresentation()
                }
                app.report(error)
                // Say so in the reader too, with Try Again, rather than
                // leaving a spinner; a sign-out has already cleared it.
                if app.loggedIn, self.selected == id { self.readerFailure = ErrorPresentation.message(error) }
            }
        }
    }

    /// Load the open article again after a failure.
    func retryOpen() {
        guard let selected else { return }
        open(selected)
    }

    // MARK: - Feeds

    /// Subscribe to the address in the New Subscription sheet. A bare address
    /// gets HTTPS and `feed:` links are unwrapped; `done` runs on success.
    func subscribe(done: @escaping () -> Void = {}) {
        let typed = subscribeURL.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !typed.isEmpty else { return }
        var url = typed
        if let link = URL(string: typed), let feed = FeedLink.subscriptionURL(link) { url = feed }
        else if !typed.contains("://") { url = "https://" + typed }
        guard let parsed = URL(string: url), let scheme = parsed.scheme?.lowercased(),
              ["http", "https"].contains(scheme), parsed.host?.isEmpty == false else {
            app.error = "Enter a feed address that starts with http:// or https://."
            return
        }
        app.subscribe(url: url, cadence: subscribeCadence) { [weak self] in
            self?.subscribeURL = ""
            done()
        }
    }

    // MARK: - Palette

    func paletteCommands() -> [PaletteCommand] {
        let canAct = canActOnEntry
        let hasRows = !visibleEntryIDs().isEmpty
        let hasURL = selectedURL != nil
        var commands: [PaletteCommand] = [
            PaletteCommand(title: "Next Article", key: "J", enabled: hasRows) { self.move(1) },
            PaletteCommand(title: "Previous Article", key: "K", enabled: hasRows) { self.move(-1) },
            PaletteCommand(title: "Move to Done", key: "E", enabled: canAct) { self.command(.done) },
            PaletteCommand(title: selectedIsLater ? "Remove from Later" : "Move to Later", key: "S", enabled: canAct) { self.command(.later) },
            PaletteCommand(title: selectedIsKept ? "Unkeep" : "Keep", key: "F", enabled: canAct) { self.command(.keep) },
            PaletteCommand(title: "Mark as Unread", key: "M", enabled: canAct) { self.command(.unread) },
            PaletteCommand(title: "Open in Browser", key: "O", enabled: hasURL) { self.openOriginal() },
            PaletteCommand(title: "Load Images", key: "", enabled: selected != nil && imageMode == .click) { self.loadImages() },
            PaletteCommand(title: "Refresh", key: "⌘R", enabled: app.loggedIn) { self.app.scheduleSync() },
            PaletteCommand(title: "New Subscription…", key: "⌘N", enabled: app.loggedIn) { self.subscribing = true },
        ]
        commands.append(contentsOf: Section.places.map { target in
            PaletteCommand(title: "Go to \(target.title)", key: target.keyEquivalent.map { "⌘\($0)" } ?? "", enabled: app.loggedIn) { self.go(to: target) }
        })
        commands.append(PaletteCommand(title: "Search Articles", key: "⌥⌘F", enabled: app.loggedIn) { self.searchFocusRequest &+= 1 })
        commands.append(PaletteCommand(title: "Settings…", key: "⌘,", enabled: true) { self.app.openSettingsRequest &+= 1 })
        commands.append(PaletteCommand(title: "Sign Out…", key: "", enabled: app.loggedIn) { self.app.requestSignOut() })
        return commands
    }

    /// Incremented to move keyboard focus into the toolbar search field.
    @Published var searchFocusRequest = 0
    /// Incremented to move keyboard focus to the article list; the list
    /// takes the pending request once, even if it is created afterwards.
    @Published private(set) var listFocusRequest = 0
    private var listFocusPending = false

    func requestListFocus() {
        listFocusPending = true
        listFocusRequest &+= 1
    }

    func takeListFocusRequest() -> Bool {
        defer { listFocusPending = false }
        return listFocusPending
    }
}

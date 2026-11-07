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
        didSet { if oldValue != selected { open(selected) } }
    }
    @Published var html = ""
    @Published private(set) var imageOrigin: URL?
    @Published private(set) var imageMode: ArticleImageMode = .proxied
    @Published private(set) var readingEntry: APIEntry?
    @Published private(set) var offlineCopy = false
    /// The selected article could not be fetched and has no offline copy.
    @Published private(set) var articleUnavailable = false
    @Published var palette = false
    @Published var endTitle = ""
    @Published var endDetail = ""
    @Published var subscribeURL = ""
    @Published var subscribeCadence = "today"

    let app: AppModel
    private var tasks: [String: Task<Void, Never>] = [:]

    /// A new window seeds its list from a sibling on the same section, else
    /// the restored cache, so it never opens blank before its first refresh.
    init(app: AppModel) {
        self.app = app
        entries = app.windows.first(where: { $0.section == section && !$0.entries.isEmpty })?.entries
            ?? app.cachedEntries
        app.register(self)
        rebuildPresentation()
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
        entries = []
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
            app.cachedPresentation = false
            entries = all
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
        var subtitle = app.feedMap[entry.subscriptionID]?.title ?? "Feed \(entry.subscriptionID)"
        if !entry.author.isEmpty { subtitle += " · \(entry.author)" }
        let date = entry.publishedAt.flatMap(DateFormatting.parse) ?? DateFormatting.parse(entry.firstSeenAt)
        if let date { subtitle += " · \(DateFormatting.relative.localizedString(for: date, relativeTo: Date()))" }
        var badges: [String] = []
        if state.kept.value == .flag(true) { badges.append("Kept") }
        if state.shelf.value == .text("later") { badges.append("Later") }
        if state.shelf.value == .text("done") { badges.append("Done") }
        if !badges.isEmpty { subtitle += " · " + badges.joined(separator: " · ") }
        return EntryRow(id: entry.id, title: entry.title.isEmpty ? "(untitled)" : entry.title,
                        subtitle: subtitle, unread: state.seen.value == .text("unseen"))
    }

    func rebuildPresentation() {
        let snapshot = try? app.engine?.snapshot()
        let visible = entries.filter { !$0.deleted }
        // One snapshot load for the whole list: state() per row cost a full
        // snapshot decode each and dominated rebuilds on large libraries.
        let bulk: [Int64: EarnestKit.State]? = app.principalID > 0
            ? (try? app.engine?.states(principal: app.principalID, entryIDs: visible.map(\.id))) ?? nil
            : nil
        let presented = visible.map { ($0, bulk?[$0.id] ?? $0.state.syncState) }
        let riverIDs: Set<Int64>
        if app.cachedPresentation, let snapshot {
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
        switch section {
        case .today:
            rows = presented.filter { riverIDs.contains($0.0.id) && cadence(of: $0.0) != "week" }.map { row($0.0, $0.1) }
            editionGroups = []
        case .edition:
            let week = presented.filter { riverIDs.contains($0.0.id) && cadence(of: $0.0) == "week" }
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
        case .feeds, .activity:
            rows = []
            editionGroups = []
        }
        updateEndStatement(presented, riverIDs: riverIDs)
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
        default: return rows.map(\.id)
        }
    }

    // MARK: - Commands

    var canBrowse: Bool { app.loggedIn && section.entryView != nil }
    var canActOnEntry: Bool { canBrowse && selected != nil && app.engine != nil && app.principalID > 0 }

    func command(_ command: EntryCommand) {
        guard let entry = entries.first(where: { $0.id == selected }), let engine = app.engine else { return }
        guard let generation = try? engine.snapshot().cursor?.generation else {
            app.connectionNote = "Still syncing; try again in a moment."
            return
        }
        let state = effectiveState(of: entry)
        let register: Register
        let value: RegisterValue
        switch command {
        case .done: register = .shelf; value = .text("done")
        case .later: register = .shelf; value = .text("later")
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
            app.sessionActions.record(command, unkeep: command == .keep && value == .flag(false))
            app.rebuildPresentation()
            keepSelection(near: index)
            app.scheduleSync()
        } catch { app.report(error) }
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
        selectFromList(ids[min(ids.count - 1, max(0, current + offset))])
    }

    func openOriginal() {
        guard let entry = readingEntry ?? entries.first(where: { $0.id == selected }),
              let url = URL(string: entry.url),
              ["http", "https"].contains(url.scheme?.lowercased() ?? "") else { return }
        #if canImport(AppKit)
        NSWorkspace.shared.open(url)
        #endif
    }

    var readingFeedTitle: String {
        guard let entry = readingEntry else { return "Feed" }
        return app.feeds.first(where: { $0.id == entry.subscriptionID })?.title ?? "Feed"
    }

    func selectFromList(_ id: Int64?) {
        imageMode = .proxied
        if selected == id { open(id) }
        else { selected = id }
    }

    func openDeepLink(_ url: URL) {
        guard let id = DeepLink.entryID(url) else { return }
        section = .today
        imageMode = .click
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
                    self.entries.append(entry)
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
            }
        }
    }

    // MARK: - Feeds

    func subscribe() {
        let url = subscribeURL.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !url.isEmpty else { return }
        guard let parsed = URL(string: url), let scheme = parsed.scheme?.lowercased(),
              ["http", "https"].contains(scheme), parsed.host != nil else {
            app.error = "Feed URL must be an http or https URL."
            return
        }
        app.subscribe(url: url, cadence: subscribeCadence) { [weak self] in
            self?.subscribeURL = ""
        }
    }

    // MARK: - Palette

    func paletteCommands() -> [PaletteCommand] {
        let canAct = canActOnEntry
        let hasRows = !visibleEntryIDs().isEmpty
        var commands: [PaletteCommand] = [
            PaletteCommand(title: "Next Article", key: "J", enabled: hasRows) { self.move(1) },
            PaletteCommand(title: "Previous Article", key: "K", enabled: hasRows) { self.move(-1) },
            PaletteCommand(title: "Done", key: "E", enabled: canAct) { self.command(.done) },
            PaletteCommand(title: "Later", key: "S", enabled: canAct) { self.command(.later) },
            PaletteCommand(title: "Keep", key: "F", enabled: canAct) { self.command(.keep) },
            PaletteCommand(title: "Mark Unread", key: "M", enabled: canAct) { self.command(.unread) },
            PaletteCommand(title: "Open Original", key: "O", enabled: self.selected != nil) { self.openOriginal() },
            PaletteCommand(title: "View Original", key: "V", enabled: self.selected != nil) { self.openOriginal() },
            PaletteCommand(title: "Refresh", key: "⌘R", enabled: self.app.loggedIn) { self.app.scheduleSync() },
        ]
        commands.append(contentsOf: Section.allCases.map { target in
            PaletteCommand(title: "Go to \(target.rawValue)", key: "⌘\(target.keyEquivalent)", enabled: true) { self.section = target }
        })
        commands.append(PaletteCommand(title: "Sign Out", key: "", enabled: app.loggedIn) { self.app.signOut() })
        return commands
    }
}

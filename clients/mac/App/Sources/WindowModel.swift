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
    @Published var section: Section = .frontPage {
        didSet { if oldValue != section { beginListVisit() } }
    }
    /// The front page's window. Changing it starts a new visit for that
    /// period's cached page.
    @Published var frontPagePeriod: FrontPagePeriod = .daily {
        didSet {
            guard oldValue != frontPagePeriod else { return }
            pendingRestoredSelection = nil
            beginListVisit()
            frontPage = nil
            frontPageUpdated = nil
            frontPageError = nil
            frontPageRequest += 1
        }
    }
    /// The page this visit is showing. The server owns ranking and selection;
    /// the client never reorders it.
    @Published private(set) var frontPage: FrontPage?
    @Published private(set) var frontPageLoading = false
    /// Why no page could be shown, when none could.
    @Published private(set) var frontPageError: String?
    /// A newer revision found by a background fetch. It waits behind an
    /// explicit refresh so the visit's order stays stable.
    @Published private(set) var frontPageAvailable: FrontPage?
    /// Why a background refresh failed while a page is visible.
    @Published private(set) var frontPageRefreshError: String?
    /// When the visible page last arrived, for the masthead's updated time.
    @Published private(set) var frontPageUpdated: Date?
    private var frontPageRequest = 0
    @Published var entries: [APIEntry] = []
    @Published var rows: [EntryRow] = []
    @Published var editionGroups: [EditionGroup] = []
    @Published var selected: Int64? {
        didSet {
            guard oldValue != selected else { return }
            // Reevaluate the unread-only list's open-article exception. An
            // unread-only visit instead retains every admitted article.
            if oldValue != nil, section.supportsUnreadFilter { rebuildPresentation() }
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
    private(set) var sectionBeforeSearch: Section = .frontPage
    /// An article restored with the window, opened once the account's cache is ready.
    var pendingRestoredSelection: Int64?
    /// An `earnest:` link that arrived before the account's cache was ready.
    private(set) var pendingDeepLink: Int64?

    /// Keep a pending link across a reconnect, which ends the old session.
    func deferDeepLink(_ id: Int64) {
        section = .frontPage
        frontPagePeriod = .daily
        imageMode = .click
        pendingDeepLink = id
    }

    let app: AppModel
    private var tasks: [String: Task<Void, Never>] = [:]
    private var searchChanges: AnyCancellable?
    // Whether this window's candidates came from the cache or the server's
    // filtered river. Another window's successful fetch must not change it.
    private var cachedPresentation = false
    private var admittedUnread = Set<Int64>()
    private var listVisit: UInt64 = 0
    private var pendingListReload: UInt64?

    init(app: AppModel, section: Section = .frontPage) {
        self.app = app
        self.section = section
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
        var restored = entries
        if section == .frontPage, let frontPage {
            let coverageIDs = Set(frontPage.sections.flatMap(\.stories).flatMap { $0.coverage.map(\.entry.id) })
            let restoredIDs = Set(restored.map(\.id))
            restored += self.entries.filter { coverageIDs.contains($0.id) && !restoredIDs.contains($0.id) && !isGone($0) }
        }
        self.entries = keepingVisitedArticles(restored)
        cachedPresentation = true
    }

    /// Whether selecting an article would show it in this list: it is listed,
    /// or it is a front-page or all-articles entry, where the open article
    /// stays while it is open.
    private func canShow(_ id: Int64) -> Bool {
        if visibleEntryIDs().contains(id) { return true }
        if section == .frontPage || section == .allArticles {
            return entries.contains { $0.id == id }
        }
        guard let entry = entries.first(where: { $0.id == id }) else { return false }
        let state = effectiveState(of: entry)
        switch section {
        case .later: return state.shelf.value == .text("later")
        case .kept: return state.kept.value == .flag(true)
        case .done: return state.shelf.value == .text("done")
        default: return false
        }
    }

    /// Put a cached article back into this window's entries.
    private func restoreEntryFromCache(_ id: Int64) {
        guard !entries.contains(where: { $0.id == id }), let engine = app.engine, app.principalID > 0,
              let cached = try? engine.entry(id), !cached.deleted, cached.hasMetadata,
              let entry = try? cachedEntry(cached, principal: app.principalID, engine: engine) else { return }
        // All articles reads newest first; every other list is chronological.
        let ahead = section == .allArticles
            ? entries.firstIndex { $0.seq < entry.seq }
            : entries.firstIndex { $0.seq > entry.seq }
        entries.insert(entry, at: ahead ?? entries.endIndex)
    }

    /// An unread-only list omits an article once it is seen, so a refresh
    /// would drop the one you are reading, and with it the commands that act
    /// on it. Keep the open article until the selection moves on.
    private func keepingOpenArticle(_ fresh: [APIEntry]) -> [APIEntry] {
        guard let selected, !fresh.contains(where: { $0.id == selected }),
              let open = entries.first(where: { $0.id == selected }) ?? readingEntry.flatMap({ $0.id == selected ? $0 : nil }),
              !isGone(open)
        else { return fresh }
        var kept = fresh
        // All articles reads newest first; every other list is chronological.
        let ahead = section == .allArticles
            ? kept.firstIndex { $0.seq < open.seq }
            : kept.firstIndex { $0.seq > open.seq }
        kept.insert(open, at: ahead ?? kept.endIndex)
        return kept
    }

    private func keepingVisitedArticles(_ fresh: [APIEntry]) -> [APIEntry] {
        let unreadVisit = app.readerSettings.showOnlyUnread && section.supportsUnreadFilter
        let combined: [APIEntry]
        if unreadVisit {
            let freshIDs = Set(fresh.map(\.id))
            let retained = entries.filter { admittedUnread.contains($0.id) && !freshIDs.contains($0.id) }
            combined = fresh + retained
        } else {
            combined = fresh
        }
        // All articles reads newest first; every other list is chronological.
        let ordered = section == .allArticles ? combined.sorted { $0.seq > $1.seq } : combined.sorted { $0.seq < $1.seq }
        return keepingOpenArticle(ordered)
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
        beginListVisit()
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
        frontPage = nil
        frontPageLoading = false
        frontPageError = nil
        frontPageAvailable = nil
        frontPageRefreshError = nil
        frontPageUpdated = nil
    }

    // MARK: - Refresh

    func beginListVisit() {
        listVisit += 1
        pendingListReload = nil
        admittedUnread.removeAll()
        frontPageAvailable = nil
        frontPageRefreshError = nil
    }

    func resetListAfterRestore() {
        beginListVisit()
        entries = []
        selected = nil
    }

    /// Keep the current visit until its requested load succeeds. A load that
    /// was already in flight cannot consume a newer refresh request.
    func requestListReload() {
        guard app.readerSettings.showOnlyUnread else { return }
        listVisit += 1
        pendingListReload = listVisit
    }

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
        case .frontPage:
            await app.refreshFeeds()
            async let briefsTask: Void = app.refreshBriefs()
            async let changesTask: Void = app.refreshAgentChanges()
            await refreshFrontPage()
            _ = await (briefsTask, changesTask)
        case .allArticles, .later, .kept, .done:
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

    // MARK: - Front page

    /// Fetch the front page for this window. A background fetch keeps the
    /// visit's page when the revision changed and offers the newer one; a
    /// forced fetch (the reader's Refresh control, or a new period) replaces
    /// it. A cached page is shown first so a cold launch is never a blank.
    func refreshFrontPage(force: Bool = false) async {
        guard app.loggedIn, let api = app.api else { return }
        frontPageRequest += 1
        let request = frontPageRequest
        let expected = section
        let expectedPeriod = frontPagePeriod
        let expectedSession = app.session
        if frontPage == nil, frontPagePeriod == expectedPeriod,
           let cached = try? app.cache?.frontPage(expectedPeriod) {
            frontPage = cached.page
            frontPageUpdated = cached.updated
            mergeFrontPageEntries(cached.page)
        }
        frontPageLoading = true
        frontPageError = nil
        defer {
            if request == frontPageRequest, section == expected, frontPagePeriod == expectedPeriod, app.session == expectedSession {
                frontPageLoading = false
            }
        }
        do {
            let page = try await api.frontPage(period: expectedPeriod)
            try Task.checkCancellation()
            guard request == frontPageRequest, section == expected, frontPagePeriod == expectedPeriod, app.session == expectedSession, app.loggedIn else { return }
            let changed = page.revision != frontPage?.revision
            // A different window's page is always a replacement, not an offer.
            let periodChanged = frontPage?.period != expectedPeriod
            if force || frontPage == nil || periodChanged || !changed {
                frontPage = page
                frontPageUpdated = Date()
                frontPageAvailable = nil
                frontPageRefreshError = nil
                try? app.cache?.saveFrontPage(page, period: expectedPeriod)
                mergeFrontPageEntries(page)
            } else {
                // The visit keeps its order; the newer revision waits behind
                // the explicit refresh offer.
                frontPageAvailable = page
                try? app.cache?.saveFrontPage(page, period: expectedPeriod)
            }
        } catch is CancellationError {
        } catch {
            guard request == frontPageRequest, section == expected, frontPagePeriod == expectedPeriod, app.session == expectedSession else { return }
            if frontPage == nil {
                frontPageError = ErrorPresentation.message(error)
            } else {
                frontPageRefreshError = ErrorPresentation.message(error)
            }
            if ErrorPresentation.isConnection(error) { app.connectionNote = ErrorPresentation.message(error) }
        }
    }

    /// Replace the visible page with the newer revision a background fetch
    /// found.
    func applyAvailableFrontPage() {
        guard let available = frontPageAvailable, frontPage?.revision != available.revision else { return }
        frontPage = available
        frontPageUpdated = Date()
        frontPageAvailable = nil
        frontPageRefreshError = nil
        try? app.cache?.saveFrontPage(available, period: frontPagePeriod)
        mergeFrontPageEntries(available)
    }

    /// Merge a page's entries and their states into the engine so the reader,
    /// commands and the read indicator all see the same state.
    private func mergeFrontPageEntries(_ page: FrontPage) {
        let entries = page.sections.flatMap(\.stories).flatMap { story in
            [story.entry] + story.coverage.map(\.entry)
        }
        if !entries.isEmpty, let engine = app.engine,
           let generation = try? engine.snapshot().cursor?.generation {
            try? engine.mergeEntries(entries, generation: generation)
        }
        // The window keeps the same entries so the reader and commands work
        // without another request.
        var merged = self.entries
        for entry in entries {
            if let index = merged.firstIndex(where: { $0.id == entry.id }) {
                if entry.rev >= merged[index].rev { merged[index] = entry }
            } else {
                merged.append(entry)
            }
        }
        self.entries = merged
        restorePendingSelection()
    }

    func refreshEntries() async {
        // Keep the last fetched entries on non-entry sections; commands and
        // content loading still need them.
        guard let api = app.api, let engine = app.engine, let view = section.entryView,
              let generation = try? engine.snapshot().cursor?.generation else { return }
        let expected = section
        let expectedSession = app.session
        let expectedVisit = listVisit
        let reload = pendingListReload
        do {
            var all: [APIEntry] = []
            var cursor: String?
            var seenCursors = Set<String>()
            let unreadOnly = app.readerSettings.showOnlyUnread && section.supportsUnreadFilter
            repeat {
                let page = try await api.entries(view: view, cursor: cursor, direction: section.entryViewDirection,
                                                 unreadOnly: unreadOnly)
                guard app.session == expectedSession, (try? engine.snapshot().cursor?.generation) == generation,
                      !Task.isCancelled else { return }
                all.append(contentsOf: page.entries ?? [])
                cursor = page.nextCursor
                if let cursor, !seenCursors.insert(cursor).inserted { throw APIError.invalidResponse }
            } while cursor != nil && !Task.isCancelled
            guard self.section == expected, listVisit == expectedVisit, app.loggedIn, app.session == expectedSession, !Task.isCancelled else { return }
            try engine.mergeEntries(all, generation: generation)
            if let reload, pendingListReload == reload {
                admittedUnread.removeAll()
                pendingListReload = nil
            }
            cachedPresentation = false
            entries = keepingVisitedArticles(all)
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
    func effectiveState(of entry: APIEntry) -> EarnestKit.State {
        guard let engine = app.engine, app.principalID > 0 else { return entry.state.syncState }
        return (try? engine.state(principal: app.principalID, entryID: entry.id)) ?? entry.state.syncState
    }

    /// A front-page story is read only when every article in its coverage has
    /// been seen or marked Done. A partially read story stays unread.
    func isStoryRead(_ story: FrontPageStory) -> Bool {
        let coverage = visibleCoverage(story)
        return !coverage.isEmpty && coverage.allSatisfy { isRead($0.entry) }
    }

    func isRead(_ entry: APIEntry) -> Bool {
        let state = effectiveState(of: entry)
        return state.seen.value == .text("seen") || state.shelf.value == .text("done")
    }

    /// Whether a mute resource hides an entry from a list the server would
    /// otherwise include. All articles applies the reader's mutes even when
    /// an entry arrives from the shared engine rather than a fetch.
    private func isMuted(_ entry: SyncEntry, resources: [SyncObject]) -> Bool {
        resources.contains { resource in
            guard resource.kind == .mute, !resource.deleted,
                  let value = resource.fields["value"]?.stringValue?.lowercased() else { return false }
            switch resource.fields["mute_kind"]?.stringValue {
            case "author": return entry.author.lowercased() == value
            case "keyword": return entry.title.lowercased().contains(value)
            default: return false
            }
        }
    }

    /// Coverage that sync still shows: a deleted or access-revoked article
    /// drops out, and the newest visible member becomes the headline link.
    func visibleCoverage(_ story: FrontPageStory) -> [FrontPageCoverage] {
        story.coverage.filter { !isGone($0.entry) }
    }

    func representative(_ story: FrontPageStory) -> FrontPageCoverage? {
        let coverage = visibleCoverage(story)
        return coverage.first(where: { $0.entry.id == story.entry.id }) ?? coverage.first
    }

    func visibleFrontPageSections(_ page: FrontPage) -> [(name: String, stories: [FrontPageStory])] {
        page.sections.compactMap { section in
            let stories = section.stories.filter { representative($0) != nil }
            return stories.isEmpty ? nil : (name: section.name, stories: stories)
        }
    }

    /// Whether the engine's snapshot has deleted the article or a sync has
    /// marked it gone, so the front page hides it without waiting for the
    /// server to drop the story.
    func isGone(_ entry: APIEntry) -> Bool {
        if entry.deleted { return true }
        guard let snapshot = try? app.engine?.snapshot() else { return false }
        return snapshot.entries.contains { $0.id == entry.id && $0.deleted }
    }

    private func row(_ entry: APIEntry, _ state: EarnestKit.State) -> EntryRow {
        // Source, then age, then author: truncation drops the least useful part first.
        var subtitle = app.feedMap[entry.subscriptionID]?.title ?? "Feed \(entry.subscriptionID)"
        let date = entry.publishedAt.flatMap(DateFormatting.parse) ?? DateFormatting.parse(entry.firstSeenAt)
        if let date { subtitle += " · \(DateFormatting.relative.localizedString(for: date, relativeTo: Date()))" }
        if !entry.author.isEmpty { subtitle += " · \(entry.author)" }
        // Kept and Later show as row markers; Done, which has none, stays in words.
        if state.shelf.value == .text("done") && section != .done { subtitle += " · Done" }
        let url = WebLink.url(entry.url)
        return EntryRow(id: entry.id, title: entry.title.isEmpty ? "Untitled" : entry.title,
                        subtitle: subtitle, unread: state.seen.value == .text("unseen"),
                        kept: state.kept.value == .flag(true), later: state.shelf.value == .text("later"),
                        url: url, thumbnail: entry.thumbnailURL,
                        icon: app.feedMap[entry.subscriptionID]?.iconURL)
    }

    func rebuildPresentation() {
        search.invalidateAfterRestore()
        singleKeyShortcuts = app.readerSettings.singleKeyShortcuts
        readerStyle = app.readerStyle(for: readingEntry)
        let snapshot = try? app.engine?.snapshot()
        let knownIDs = Set(entries.map(\.id))
        let pendingIDs = Set(snapshot?.outbox.filter { $0.principal == app.principalID }.map(\.entryID) ?? [])
        let isUnreadList = section.supportsUnreadFilter
        let missing = (snapshot?.entries ?? []).filter {
            !$0.deleted && $0.hasMetadata && !knownIDs.contains($0.id) && section.entryView != nil
                && (!isUnreadList || cachedPresentation || pendingIDs.contains($0.id))
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
                    case .allArticles: belongs = !isMuted(entry, resources: resources)
                    case .later: belongs = state.shelf.value == .text("later")
                    case .kept: belongs = state.kept.value == .flag(true)
                    case .done: belongs = state.shelf.value == .text("done")
                    case .frontPage, .feeds, .activity, .search: belongs = false
                    }
                    return belongs ? try cachedEntry(entry, principal: app.principalID, engine: engine, state: state) : nil
                }
                if !additions.isEmpty {
                    entries = entries + additions
                    entries.sort { section == .allArticles ? $0.seq > $1.seq : $0.seq < $1.seq }
                }
            } catch { app.report(error) }
        }
        let goneIDs = Set(snapshot?.entries.filter(\.deleted).map(\.id) ?? [])
        if section == .allArticles { entries.sort { $0.seq > $1.seq } }
        let visible = entries.filter { !$0.deleted && !goneIDs.contains($0.id) }
        let presented = visible.map { ($0, bulk?[$0.id] ?? $0.state.syncState) }
        let unreadOnly = app.readerSettings.showOnlyUnread && isUnreadList
        if unreadOnly { admittedUnread.formUnion(presented.filter { $0.1.seen.value == .text("unseen") }.map(\.0.id)) }
        switch section {
        case .frontPage:
            rows = []
            editionGroups = []
        case .allArticles:
            // An unread-only visit retains every article it admitted until the
            // list is reloaded; otherwise the fetched order is the list.
            rows = (unreadOnly ? presented.filter { admittedUnread.contains($0.0.id) } : presented).map { row($0.0, $0.1) }
            editionGroups = []
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
        if let link = pendingDeepLink, app.engine != nil, app.loggedIn {
            pendingDeepLink = nil
            pendingRestoredSelection = nil
            // External links never load images automatically.
            imageMode = .click
            if selected == link { open(link) } else { selected = link }
        } else {
            restorePendingSelection()
        }
    }

    private func restorePendingSelection() {
        guard let pending = pendingRestoredSelection, app.engine != nil, app.loggedIn else { return }
        let entry = entries.first { $0.id == pending }
        // The sync cache may arrive before the front page's cached coverage.
        if section == .frontPage, frontPage == nil, entry == nil { return }
        pendingRestoredSelection = nil
        if let entry, !isGone(entry), selected == nil { selected = pending }
    }

    func visibleEntryIDs() -> [Int64] {
        switch section {
        case .frontPage:
            if let frontPage { return frontPage.sections.flatMap(\.stories).compactMap { representative($0)?.entry.id } }
            return []
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
        app.loggedIn && (section.entryView != nil || section == .frontPage || section == .search || section == .activity)
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
        guard let entry = readingEntry ?? entries.first(where: { $0.id == selected }) else { return nil }
        return WebLink.url(entry.url)
    }

    var selectedTitle: String {
        let entry = readingEntry ?? entries.first(where: { $0.id == selected })
        return entry.map { $0.title.isEmpty ? "Untitled" : $0.title } ?? ""
    }
    var canActOnEntry: Bool {
        guard let selected, canBrowse, app.engine != nil, app.principalID > 0 else { return false }
        return entries.contains { $0.id == selected && !isGone($0) }
    }

    /// Apply a reading action to the open article, or to the row a context
    /// menu was opened on.
    func command(_ command: EntryCommand, entryID: Int64? = nil) {
        let target = entryID ?? selected
        guard let entry = entries.first(where: { $0.id == target && !isGone($0) }), let engine = app.engine,
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
            registerUndo(entryID: entry.id, prior: Self.registers(of: state, changedBy: command),
                         actionName: Self.actionName(command, value: value),
                         command: counted ? command : nil, unkeep: unkeep)
            app.rebuildPresentation()
            if entry.id == selected { keepSelection(near: index) }
            app.scheduleSync()
        } catch { app.report(error) }
    }

    /// Open a row's article in the default browser.
    func openInBrowser(entryID: Int64) {
        guard let entry = entries.first(where: { $0.id == entryID }), let url = WebLink.url(entry.url) else { return }
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

    /// The registers an action changes, with their values before it: its
    /// own register, plus the shelf for Mark as Unread, which the server
    /// clears when the article was Done. Registers changed elsewhere since
    /// are left alone by undo.
    private static func registers(of state: EarnestKit.State, changedBy command: EntryCommand) -> [Register: RegisterValue] {
        switch command {
        case .done, .later: return [.shelf: state.shelf.value]
        case .keep: return [.kept: state.kept.value]
        case .unread: return [.seen: state.seen.value, .shelf: state.shelf.value]
        }
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
        let index = visibleEntryIDs().firstIndex(of: entryID)
        let wasVisible = index != nil
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
        let current = Dictionary(uniqueKeysWithValues: values.keys.map { ($0, state[$0].value) })
        registerUndo(entryID: entryID, prior: current, actionName: actionName,
                     command: command, unkeep: unkeep, undoing: undoing)
        app.rebuildPresentation()
        // Bring back an article the action had removed from this list, as
        // Mail reselects a message whose move is undone. Selecting it keeps a
        // read article in the river while it is open; a refresh since the
        // action may have dropped it from this window's entries, so restore
        // it from the cache first. If it still cannot be shown, stay put.
        if !wasVisible, selected != entryID {
            restoreEntryFromCache(entryID)
            if canShow(entryID) { selectFromList(entryID) }
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
        if section == .search {
            moveInSearch(offset)
            return
        }
        let ids = visibleEntryIDs()
        guard !ids.isEmpty else { return }
        let current = ids.firstIndex(of: selected ?? -1) ?? (offset > 0 ? -1 : ids.count)
        selectFromList(ids[min(ids.count - 1, max(0, current + offset))])
    }

    /// Search results show articles, then agent notes; J and K follow that
    /// order, match by match, since a note and its article share an entry.
    private func moveInSearch(_ offset: Int) {
        let displayed = search.matches.filter { $0.kind == "entry" } + search.matches.filter { $0.kind == "agent_note" }
        guard !displayed.isEmpty else { return }
        let current = displayed.firstIndex { $0.id == selectedMatchID && $0.entryID == selected }
            ?? (offset > 0 ? -1 : displayed.count)
        selectSearchMatch(displayed[min(displayed.count - 1, max(0, current + offset))])
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
        guard target != section else {
            if app.readerSettings.showOnlyUnread {
                requestListReload()
                selectFromList(nil)
                app.scheduleSync()
            }
            return
        }
        if target != .search { search.clearQuery() }
        selectFromList(nil)
        section = target
    }

    /// The toolbar search field submitted a query: show results in place of
    /// the current section, remembering where to return.
    func submitSearch() {
        guard search.canSearch else { return }
        pendingRestoredSelection = nil
        // A new search keeps the keyboard in the search field.
        listFocusPending = false
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
        listFocusPending = false
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
        pendingRestoredSelection = nil
        guard let item else {
            selectFromList(nil)
            return
        }
        selectedActivityID = item.id
        guard let entryID = item.entryID else {
            // A row without an article, such as a notification, stays selected.
            selected = nil
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
        pendingRestoredSelection = nil
        // Articles reached from Activity were surfaced by an agent and never
        // load images automatically, however they were selected.
        imageMode = section == .activity ? .click : .proxied
        selectedMatchID = nil
        if section == .activity {
            selectedActivityID = id.flatMap { id in app.activityGroups.flatMap(\.items).first { $0.entryID == id }?.id }
        }
        if selected == id { open(id) }
        else { selected = id }
    }

    func selectSearchMatch(_ match: APISearchMatch) {
        pendingRestoredSelection = nil
        selectedMatchID = match.id
        imageMode = match.kind == "agent_note" || match.untrusted ? .click : .proxied
        if selected == match.entryID { open(match.entryID) }
        else { selected = match.entryID }
    }

    func openDeepLink(_ url: URL) {
        guard let id = DeepLink.entryID(url) else { return }
        section = .frontPage
        frontPagePeriod = .daily
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
                    if !self.entries.contains(where: { $0.id == id }) {
                        self.entries.insert(entry, at: self.entries.firstIndex { $0.seq > entry.seq } ?? self.entries.endIndex)
                    }
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
            PaletteCommand(title: "Refresh", key: "⌘R", enabled: app.loggedIn) { self.app.reloadLists() },
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

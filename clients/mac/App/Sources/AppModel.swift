import Foundation
import EarnestKit
#if canImport(SwiftUI)
import SwiftUI
#endif
#if canImport(AppKit)
import AppKit
#endif

@MainActor final class AppModel: ObservableObject {
    @Published var server = UserDefaults.standard.string(forKey: "server") ?? ""
    @Published var section: Section = .today
    @Published var entries: [APIEntry] = []
    @Published var rows: [EntryRow] = []
    @Published var editionGroups: [EditionGroup] = []
    @Published var feeds: [FeedInfo] = []
    @Published private(set) var activityActionPending = false
    @Published var activityGroups: [ActivityGroup] = []
    @Published var briefs: [BriefItem] = []
    @Published var agentChanges: [ActivityItem] = []
    @Published var principalNames: [Int64: String] = [:]
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
    @Published var error: String?
    @Published var connectionNote: String?
    @Published var code: DeviceAuthorization?
    @Published var loggingIn = false
    @Published var loggedIn = false
    @Published var connected = false
    @Published var palette = false
    @Published var syncing = false
    @Published var pendingOps = 0
    @Published var endTitle = ""
    @Published var endDetail = ""
    @Published var subscribeURL = ""
    @Published var subscribeCadence = "today"

    private var api: EarnestAPI?
    private var tokens: (any TokenStore)?
    private var serverURL: URL?
    private let identities: IdentityStore
    private let cacheDirectory: URL?
    private var identity: AccountIdentity?
    private var cache: Cache?
    private var engine: SyncEngine?
    private var coordinator: SyncCoordinator?
    private var principalID: Int64 = 0
    private var feedMap: [Int64: FeedInfo] = [:]
    private var cachedPresentation = false
    private var session = 0
    private var tasks: [String: Task<Void, Never>] = [:]
    private var sessionActions = SessionCounts()

    init(api: EarnestAPI? = nil, engine: SyncEngine? = nil, principalID: Int64 = 0,
         serverURL: URL? = nil, tokens: (any TokenStore)? = nil, identities: IdentityStore = IdentityStore(), cacheDirectory: URL? = nil) {
        self.serverURL = serverURL
        self.tokens = tokens
        self.identities = identities
        self.cacheDirectory = cacheDirectory
        self.api = api
        self.engine = engine
        self.principalID = principalID
    }

    // MARK: - Tasks and session lifecycle

    /// Every chunk of async work is a named task so sign-out and server
    /// changes can cancel them. A session counter additionally guards writes
    /// that resume after cancellation.
    private func runTask(_ key: String, _ work: @escaping @MainActor () async -> Void) {
        tasks[key]?.cancel()
        let expectedSession = session
        tasks[key] = Task { [weak self] in
            guard let self, self.session == expectedSession, !Task.isCancelled else { return }
            await work()
        }
    }

    private func endSession() {
        session &+= 1
        coordinator?.cancel()
        tasks.values.forEach { $0.cancel() }
        tasks.removeAll()
        engine = nil
        cache = nil
        identity = nil
        coordinator = nil
        principalID = 0
        entries = []
        rows = []
        editionGroups = []
        feeds = []
        feedMap = [:]
        activityGroups = []
        activityActionPending = false
        briefs = []
        agentChanges = []
        principalNames = [:]
        cachedPresentation = false
        selected = nil
        html = ""
        imageOrigin = nil
        imageMode = .proxied
        readingEntry = nil
        offlineCopy = false
        articleUnavailable = false
        code = nil
        loggingIn = false
        loggedIn = false
        syncing = false
        pendingOps = 0
        endTitle = ""
        endDetail = ""
        connectionNote = nil
        sessionActions = SessionCounts()
        error = nil
    }

    func connect() {
        do {
            let trimmed = server.trimmingCharacters(in: .whitespacesAndNewlines)
            guard let url = URL(string: trimmed), !trimmed.isEmpty else { throw APIError.invalidBaseURL }
            endSession()
            api = nil
            tokens = nil
            serverURL = nil
            connected = false
            let store = KeychainTokens(server: url)
            tokens = store
            serverURL = url
            api = try EarnestAPI(baseURL: url, tokenStore: store)
            connected = true
            UserDefaults.standard.set(trimmed, forKey: "server")
            if (try? store.load()) != nil {
                loggedIn = true
                startSession()
            }
        } catch { report(error) }
    }

    func signOut() {
        if let serverURL { identities.remove(server: serverURL) }
        endSession()
        do { try tokens?.delete() } catch { report(error) }
    }

    /// Retry identity verification without letting an offline credential send
    /// the remembered account's outbox under a different principal.
    private func startSession() {
        let s = session
        runTask("session") { [weak self] in
            while !Task.isCancelled {
                guard let self, self.session == s, self.loggedIn else { return }
                do {
                    if try await self.establishSession() {
                        guard self.session == s, !Task.isCancelled else { return }
                        self.startEvents()
                        self.scheduleSync()
                        return
                    }
                } catch is CancellationError {
                    return
                } catch {
                    guard self.session == s, !Task.isCancelled else { return }
                    // Transient failures keep retrying as a note; permanent
                    // ones surface and stop the loop.
                    if ErrorPresentation.isTransient(error) {
                        self.connectionNote = ErrorPresentation.message(error)
                    } else {
                        self.report(error)
                        return
                    }
                }
                try? await Task.sleep(for: .seconds(10))
            }
        }
    }

    /// Also used before reconnect sync. A verified replacement starts with
    /// its own empty presentation before any of its cache is attached.
    func establishSession() async throws -> Bool {
        guard let api, let url = serverURL else { return false }
        let expectedSession = session
        let result: Result<APIValue, Error>
        do { result = .success(try await api.me()) }
        catch { result = .failure(error) }
        try Task.checkCancellation()
        guard session == expectedSession, loggedIn else { return false }
        let remembered: AccountIdentity?
        if case .failure(let error) = result, ErrorPresentation.isTransient(error) {
            // A record that cannot be decoded is simply absent: it re-verifies online.
            remembered = try? identities.load(server: url)
        } else {
            remembered = nil
        }
        let resolution = try SessionResolution.resolve(server: url, result: result, remembered: remembered)
        let resolved = resolution.identity
        if case .verified = resolution { try identities.save(resolved) }
        if let identity, identity != resolved {
            endSession()
            loggedIn = true
            startSession()
            return false
        }
        if identity == nil {
            let cache = try makeCache(for: resolved)
            let engine = SyncEngine(store: cache)
            self.cache = cache
            self.engine = engine
            self.identity = resolved
            principalID = resolved.principalID
            coordinator = SyncCoordinator(engine: engine, api: api)
            do {
                try restoreCachedPresentation()
            } catch {
                // A cache that cannot be read or decoded is a cold cache, not
                // a fatal error: clear it and let the next sync rebuild it.
                cachedPresentation = false
                entries = []
                feeds = []
                feedMap = [:]
                principalNames = [:]
                briefs = []
                rebuildPresentation()
                try? cache.clear()
            }
        }
        if case .offline = resolution {
            connectionNote = "Offline. Showing what was last synced."
            return false
        }
        return true
    }

    /// A cache file that cannot be opened is a cold cache: remove it once and
    /// start empty. A second failure is a real problem and surfaces.
    private func makeCache(for identity: AccountIdentity) throws -> Cache {
        do { return try Cache(identity: identity, directory: cacheDirectory) }
        catch {
            let database = try Cache.databaseURL(in: cacheDirectory)
            for suffix in ["", "-wal", "-shm"] {
                try? FileManager.default.removeItem(at: URL(fileURLWithPath: database.path + suffix))
            }
            return try Cache(identity: identity, directory: cacheDirectory)
        }
    }

    private func restoreCachedPresentation() throws {
        guard let engine, let cache else { return }
        let snapshot = try engine.snapshot()
        cachedPresentation = true
        // Oldest first, like the server river, so offline reading order matches online.
        let cached = snapshot.entries.filter { !$0.deleted && $0.hasBody }.sorted { $0.seq < $1.seq }
        let prefetched = (try? engine.states(principal: principalID, entryIDs: cached.map(\.id))) ?? [:]
        entries = try cached.map { try cachedEntry($0, principal: principalID, engine: engine, state: prefetched[$0.id]) }
        let generation = snapshot.cursor?.generation ?? ""
        feeds = try cache.resource("feeds", generation: generation).compactMap(FeedInfo.init).filter { !$0.deleted }
        let subscriptions = snapshot.objects.filter { $0.kind == .subscription && !$0.deleted }
        if !subscriptions.isEmpty {
            feeds = subscriptions.compactMap { object in
                var fields = object.fields
                fields["id"] = .integer(object.id)
                fields["title"] = .string(object.name)
                return FeedInfo(.object(fields))
            }
        }
        principalNames = Dictionary(try cache.resource("principal_names", generation: generation).compactMap { value in
            guard let id = value["id"]?.int64Value, let name = value["name"]?.stringValue else { return nil }
            return (id, name)
        }, uniquingKeysWith: { _, last in last })
        feedMap = Dictionary(feeds.map { ($0.id, $0) }, uniquingKeysWith: { _, last in last })
        briefs = try cache.resource("briefs", generation: generation).compactMap(BriefItem.init)
        rebuildPresentation()
    }

    private func startEvents() {
        let s = session
        runTask("events") { [weak self] in
            while !Task.isCancelled {
                guard let self, let api = self.api, self.session == s else { return }
                do {
                    let cursor = try self.engine?.snapshot().cursor
                    let text = cursor.map { "\($0.generation):\($0.rev)" }
                    for try await _ in try api.eventHints(cursor: text) {
                        guard self.session == s, !Task.isCancelled else { return }
                        self.scheduleSync()
                    }
                    return
                } catch is CancellationError {
                    return
                } catch {
                    guard !Task.isCancelled, self.session == s else { return }
                    if ErrorPresentation.isUnauthorized(error) {
                        self.report(error)
                        return
                    }
                    self.connectionNote = "Live updates interrupted; retrying."
                    try? await Task.sleep(for: .seconds(10))
                }
            }
        }
        // Poll as well as following hints so a quiet or interrupted stream
        // cannot prevent retrying queued work.
        runTask("poll") { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(120))
                guard !Task.isCancelled, let self, self.session == s, self.loggedIn else { return }
                self.scheduleSync()
            }
        }
    }

    func scheduleSync() { scheduleSync(delay: 0) }

    /// Passive marks coalesce for `delay` so rapid j/k browsing costs one
    /// round trip; the named task cancels the previous pending sync.
    func scheduleSync(delay: Double) {
        guard coordinator != nil else { return }
        let s = session
        runTask("sync") { [weak self] in
            if delay > 0 { try? await Task.sleep(for: .milliseconds(Int(delay * 1000))) }
            guard let self, self.session == s, self.loggedIn, !Task.isCancelled else { return }
            self.syncing = true
            defer { if !Task.isCancelled, self.session == s { self.syncing = false } }
            do {
                if self.serverURL != nil {
                    guard try await self.establishSession(), self.session == s else { return }
                }
                guard let coordinator = self.coordinator else { return }
                let generation = try self.engine?.snapshot().cursor?.generation
                let report = try await coordinator.synchronize()
                guard !Task.isCancelled, self.session == s, self.loggedIn else { return }
                if (try self.engine?.snapshot().cursor?.generation) != generation {
                    self.selected = nil
                    try self.restoreCachedPresentation()
                }
                if !report.droppedOperationIDs.isEmpty {
                    self.connectionNote = "\(report.droppedOperationIDs.count) queued action(s) expired or no longer apply."
                } else {
                    self.connectionNote = nil
                }
                self.rebuildPresentation()
                await self.refresh()
                self.refreshPrincipalNames()
                if self.articleUnavailable, let id = self.selected { self.open(id) }
            } catch is CancellationError {
            } catch {
                guard self.session == s else { return }
                self.report(error)
            }
        }
    }

    private func report(_ error: Error) {
        if ErrorPresentation.isUnauthorized(error) { signOut() }
        let message = ErrorPresentation.message(error)
        if ErrorPresentation.isConnection(error) {
            connectionNote = message
            return
        }
        self.error = message
    }

    // MARK: - Device login

    func login() {
        guard let api, let loginTokens = tokens, !loggingIn else { return }
        let expectedSession = session
        loggingIn = true
        runTask("login") { [weak self] in
            guard let self else { return }
            defer { if self.session == expectedSession { self.loggingIn = false } }
            do {
                let authorization = try await api.startDevice(clientName: "Earnest for Mac")
                try Task.checkCancellation()
                guard self.session == expectedSession else { return }
                self.code = authorization
                #if canImport(AppKit)
                if let url = URL(string: authorization.verificationURI), ["http", "https"].contains(url.scheme?.lowercased() ?? "") {
                    NSWorkspace.shared.open(url)
                }
                #endif
                let deadline = Date().addingTimeInterval(TimeInterval(authorization.expiresIn))
                var interval = max(1, authorization.interval)
                while Date() < deadline {
                    try await Task.sleep(for: .seconds(interval))
                    do {
                        let token = try await api.pollDevice(deviceCode: authorization.deviceCode)
                        if Task.isCancelled || self.session != expectedSession {
                            // The poll already persisted a token; a cancelled
                            // login must not leave the app half signed in.
                            if (try? loginTokens.load()) == token.accessToken { try? loginTokens.delete() }
                            return
                        }
                        // New credentials must verify their own account before cache reuse.
                        if let serverURL = self.serverURL { self.identities.remove(server: serverURL) }
                        self.code = nil
                        self.loggedIn = true
                        self.startSession()
                        return
                    } catch let problem as APIProblem {
                        if problem.error == "authorization_pending" || problem.detail == "authorization_pending" { continue }
                        if problem.error == "slow_down" || problem.detail == "slow_down" { interval += 5; continue }
                        throw problem
                    }
                }
                self.code = nil
                self.error = "The device code expired. Start again."
            } catch is CancellationError {
                if self.session == expectedSession { self.code = nil }
            } catch {
                guard self.session == expectedSession else { return }
                self.code = nil
                self.report(error)
            }
        }
    }

    func cancelLogin() {
        tasks["login"]?.cancel()
        tasks["login"] = nil
        code = nil
        loggingIn = false
    }

    // MARK: - Refresh pipelines

    /// Named-task wrapper so section changes cancel the previous fetch.
    func refreshSection() {
        runTask("list") { [weak self] in
            await self?.refresh()
        }
    }

    func refresh() async {
        guard loggedIn else { return }
        let expected = section
        switch expected {
        case .today:
            await refreshFeeds()
            async let briefsTask: Void = refreshBriefs()
            async let changesTask: Void = refreshAgentChanges()
            await refreshEntries()
            _ = await (briefsTask, changesTask)
        case .edition:
            await refreshFeeds()
            await refreshEntries()
        case .later, .kept, .done:
            await refreshEntries()
        case .feeds:
            await refreshFeeds()
        case .activity:
            await refreshActivity()
        }
        guard self.section == expected, loggedIn, !Task.isCancelled else { return }
        rebuildPresentation()
    }

    private func refreshEntries() async {
        // Keep the last fetched entries on non-entry sections; commands and
        // content loading still need them.
        guard let api, let engine, let view = section.entryView,
              let generation = try? engine.snapshot().cursor?.generation else { return }
        let expected = section
        let expectedSession = session
        do {
            var all: [APIEntry] = []
            var cursor: String?
            var seenCursors = Set<String>()
            repeat {
                let page = try await api.entries(view: view, cursor: cursor)
                guard session == expectedSession, (try? engine.snapshot().cursor?.generation) == generation,
                      !Task.isCancelled else { return }
                all.append(contentsOf: page.entries ?? [])
                cursor = page.nextCursor
                if let cursor, !seenCursors.insert(cursor).inserted { throw APIError.invalidResponse }
            } while cursor != nil && !Task.isCancelled
            guard self.section == expected, loggedIn, session == expectedSession, !Task.isCancelled else { return }
            try engine.mergeEntries(all, generation: generation)
            cachedPresentation = false
            entries = all
        } catch is CancellationError {
        } catch {
            guard session == expectedSession, (try? engine.snapshot().cursor?.generation) == generation else { return }
            if ErrorPresentation.isConnection(error) {
                do { try restoreCachedPresentation() }
                catch { report(error) }
            }
            report(error)
        }
    }

    private func refreshFeeds() async {
        guard let api else { return }
        let expectedSession = session
        let generation = try? engine?.snapshot().cursor?.generation
        do {
            let values = try await api.subscriptions()
            guard loggedIn, session == expectedSession, !Task.isCancelled,
                  (try? engine?.snapshot().cursor?.generation) == generation else { return }
            if let generation {
                try cache?.saveResource("feeds", values: values, generation: generation)
            }
            let subs = values.compactMap(FeedInfo.init).filter { !$0.deleted }
            feeds = subs
            feedMap = Dictionary(subs.map { ($0.id, $0) }, uniquingKeysWith: { _, last in last })
        } catch is CancellationError {
        } catch { if session == expectedSession { report(error) } }
    }

    private func refreshBriefs() async {
        guard let api else { return }
        let expectedSession = session
        let generation = try? engine?.snapshot().cursor?.generation
        do {
            let values = try await api.todayBriefs()
            guard loggedIn, session == expectedSession, !Task.isCancelled,
                  (try? engine?.snapshot().cursor?.generation) == generation else { return }
            if let generation {
                try cache?.saveResource("briefs", values: values, generation: generation)
            }
            let items = values.compactMap(BriefItem.init)
            briefs = items
            if items.contains(where: { principalNames[$0.principalID] == nil }) { refreshPrincipalNames() }
        } catch is CancellationError {
        } catch { if session == expectedSession { report(error) } }
    }

    private func refreshAgentChanges() async {
        guard let api else { return }
        let expectedSession = session
        do {
            let items = try await api.agentChanges().map(ActivityItem.init)
            guard loggedIn, session == expectedSession, !Task.isCancelled else { return }
            agentChanges = items
            notePrincipalNames(items)
        } catch is CancellationError {
        } catch { if session == expectedSession { report(error) } }
    }

    /// Token metadata names every agent principal; missing names fall back to
    /// activity rows or a generic label.
    private func refreshPrincipalNames() {
        let expectedSession = session
        runTask("names") { [weak self] in
            guard let self, let names = try? await self.api?.agentNames() else { return }
            guard self.session == expectedSession, self.loggedIn, !Task.isCancelled else { return }
            self.principalNames.merge(names) { _, new in new }
            self.cachePrincipalNames()
        }
    }

    private func refreshActivity() async {
        guard let api else { return }
        let expectedSession = session
        do {
            // Tolerate both the flat page and the grouped page.
            let items = try await api.activity().flatMap { raw -> [ActivityItem] in
                if let nested = raw["activities"]?.arrayValue { return nested.map(ActivityItem.init) }
                return [ActivityItem(raw)]
            }
            guard loggedIn, session == expectedSession, !Task.isCancelled else { return }
            notePrincipalNames(items)
            var groups: [ActivityGroup] = []
            var index: [Int64: Int] = [:]
            for item in items {
                if let existing = index[item.principalID] {
                    groups[existing].items.append(item)
                } else {
                    index[item.principalID] = groups.count
                    let name = item.principalName.isEmpty ? "Agent \(item.principalID)" : item.principalName
                    groups.append(ActivityGroup(principalID: item.principalID, name: name, items: [item]))
                }
            }
            activityGroups = groups
        } catch is CancellationError {
        } catch { if session == expectedSession { report(error) } }
    }

    private func notePrincipalNames(_ items: [ActivityItem]) {
        for item in items where !item.principalName.isEmpty {
            principalNames[item.principalID] = item.principalName
        }
        cachePrincipalNames()
    }

    private func cachePrincipalNames() {
        guard let generation = try? engine?.snapshot().cursor?.generation else { return }
        let values = principalNames.sorted { $0.key < $1.key }.map {
            APIValue.object(["id": .integer($0.key), "name": .string($0.value)])
        }
        do { try cache?.saveResource("principal_names", values: values, generation: generation) }
        catch { report(error) }
    }

    // MARK: - Presentation

    /// Displayed entries never mutate the fetched APIEntry: each row's state
    /// is the fetched state merged with the engine's synced-plus-overlay state.
    private func effectiveState(of entry: APIEntry) -> EarnestKit.State {
        guard let engine, principalID > 0 else { return entry.state.syncState }
        return (try? engine.state(principal: principalID, entryID: entry.id)) ?? entry.state.syncState
    }

    private func cadence(of entry: APIEntry) -> String {
        feedMap[entry.subscriptionID]?.cadence ?? "today"
    }

    private func row(_ entry: APIEntry, _ state: EarnestKit.State) -> EntryRow {
        var subtitle = feedMap[entry.subscriptionID]?.title ?? "Feed \(entry.subscriptionID)"
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
        let snapshot = try? engine?.snapshot()
        let visible = entries.filter { !$0.deleted }
        // One snapshot load for the whole list: state() per row cost a full
        // snapshot decode each and dominated rebuilds on large libraries.
        let bulk: [Int64: EarnestKit.State]? = principalID > 0
            ? (try? engine?.states(principal: principalID, entryIDs: visible.map(\.id))) ?? nil
            : nil
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
                        title: feedMap[entry.subscriptionID]?.title ?? "Feed \(entry.subscriptionID)",
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
        pendingOps = snapshot?.outbox.count ?? 0
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
        if !sessionActions.summary.isEmpty { parts.append(sessionActions.summary) }
        if !agentChanges.isEmpty {
            let names = Array(Set(agentChanges.map(\.principalName).filter { !$0.isEmpty })).sorted()
            let label = names.isEmpty ? "Your agents" : names.joined(separator: ", ")
            let count = agentChanges.count
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

    func title(for entryID: Int64?) -> String? {
        guard let entryID else { return nil }
        if let title = entries.first(where: { $0.id == entryID })?.title, !title.isEmpty { return title }
        return try? engine?.entry(entryID)?.title
    }

    // MARK: - Commands

    var canBrowse: Bool { loggedIn && section.entryView != nil }
    var canActOnEntry: Bool { canBrowse && selected != nil && engine != nil && principalID > 0 }

    func command(_ command: EntryCommand) {
        guard let entry = entries.first(where: { $0.id == selected }), let engine else { return }
        guard let generation = try? engine.snapshot().cursor?.generation else {
            connectionNote = "Still syncing; try again in a moment."
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
                          principal: principalID, entryID: entry.id,
                          register: register, value: value,
                          base: Cursor(generation: generation, rev: state[register].rev),
                          occurredAt: Date())
        do {
            let index = visibleEntryIDs().firstIndex(of: entry.id)
            try engine.queue(write)
            sessionActions.record(command, unkeep: command == .keep && value == .flag(false))
            rebuildPresentation()
            keepSelection(near: index)
            scheduleSync()
        } catch { report(error) }
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
        guard let engine, principalID > 0,
              let generation = try? engine.snapshot().cursor?.generation else { return }
        let state = entry.map { self.effectiveState(of: $0) }
            ?? (try? engine.state(principal: principalID, entryID: entryID))
            ?? EarnestKit.State(principal: principalID, entryID: entryID)
        guard state.seen.value != .text("seen") else { return }
        let write = Write(opID: "mac.\(UUID().uuidString.lowercased())",
                          principal: principalID, entryID: entryID,
                          register: .seen, value: .text("seen"),
                          base: Cursor(generation: generation, rev: state.seen.rev),
                          occurredAt: Date(), kind: .passive)
        try? engine.queue(write)
        rebuildPresentation()
        scheduleSync(delay: 1.5)
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
        return feeds.first(where: { $0.id == entry.subscriptionID })?.title ?? "Feed"
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
        guard let id, loggedIn else { return }
        let mode = imageMode
        runTask("open") { [weak self] in
            guard let self, let api = self.api, let engine = self.engine, self.loggedIn,
                  let generation = try? engine.snapshot().cursor?.generation else { return }
            let expectedSession = self.session
            let cache = self.cache
            do {
                if !self.entries.contains(where: { $0.id == id }) {
                    let entry = try await api.entry(id: id)
                    try Task.checkCancellation()
                    guard self.loggedIn, self.session == expectedSession,
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
                        guard self.session == expectedSession,
                              (try? engine.snapshot().cursor?.generation) == generation else { return }
                        guard let saved = try cache?.article(entryID: id, generation: generation) else {
                            // Never opened before: say so, and retry after the next successful sync.
                            guard self.selected == id else { throw error }
                            self.articleUnavailable = true
                            self.connectionNote = ErrorPresentation.message(error)
                            return
                        }
                        article = saved
                        offline = true
                        contentError = error
                    }
                    origin = nil
                }
                guard self.selected == id, self.loggedIn, self.session == expectedSession,
                      (try? engine.snapshot().cursor?.generation) == generation, !Task.isCancelled else { return }
                // Persistence is best effort once the article is available to read.
                try? cache?.saveArticle(entryID: id, html: article, generation: generation)
                self.html = article
                self.imageOrigin = origin
                self.offlineCopy = offline
                if let contentError, ErrorPresentation.isConnection(contentError) {
                    self.connectionNote = ErrorPresentation.message(contentError)
                }
                self.markSeen(entryID: id, entry: self.entries.first(where: { $0.id == id }))
            } catch is CancellationError {
            } catch {
                guard self.loggedIn, self.session == expectedSession, self.selected == id, !Task.isCancelled,
                      (try? engine.snapshot().cursor?.generation) == generation else { return }
                var missing = (error as? APIProblem)?.status == 404
                if case APIError.httpStatus(404) = error { missing = true }
                if missing {
                    do {
                        try engine.markGone(entryID: id)
                        try cache?.removeArticle(entryID: id, generation: generation)
                    } catch {
                        self.report(error)
                        return
                    }
                    self.rebuildPresentation()
                }
                self.report(error)
            }
        }
    }

    // MARK: - Feeds

    func subscribe() {
        let url = subscribeURL.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let api, !url.isEmpty else { return }
        guard let parsed = URL(string: url), let scheme = parsed.scheme?.lowercased(),
              ["http", "https"].contains(scheme), parsed.host != nil else {
            error = "Feed URL must be an http or https URL."
            return
        }
        let cadence = subscribeCadence
        let expectedSession = session
        runTask("feeds") { [weak self] in
            do {
                _ = try await api.subscribe(url: url, cadence: cadence)
                guard let self, self.loggedIn, self.session == expectedSession, !Task.isCancelled else { return }
                self.subscribeURL = ""
                await self.refreshFeeds()
                self.rebuildPresentation()
                self.scheduleSync()
            } catch is CancellationError {
                return
            } catch { if self?.session == expectedSession { self?.report(error) } }
        }
    }

    func setCadence(_ subscriptionID: Int64, _ cadence: String) {
        guard let api, let index = feeds.firstIndex(where: { $0.id == subscriptionID }) else { return }
        feeds[index].cadence = cadence
        rebuildPresentation()
        let expectedSession = session
        runTask("feeds") { [weak self] in
            do {
                _ = try await api.setCadence(subscriptionID: subscriptionID, cadence: cadence)
            } catch is CancellationError {
                return
            } catch { if self?.session == expectedSession { self?.report(error) } }
            guard let self, self.session == expectedSession, self.loggedIn, !Task.isCancelled else { return }
            await self.refreshFeeds()
            self.rebuildPresentation()
            self.scheduleSync()
        }
    }

    func unsubscribe(_ subscriptionID: Int64) {
        guard let api else { return }
        let expectedSession = session
        runTask("feeds") { [weak self] in
            do {
                try await api.unsubscribe(subscriptionID: subscriptionID)
            } catch is CancellationError {
                return
            } catch { if self?.session == expectedSession { self?.report(error) } }
            guard let self, self.loggedIn, self.session == expectedSession, !Task.isCancelled else { return }
            await self.refreshFeeds()
            self.rebuildPresentation()
            self.scheduleSync()
        }
    }

    // MARK: - Activity

    func revert(_ item: ActivityItem) {
        guard item.nextAction == "revert" else { return }
        performActivityAction(item)
    }

    func cancelNotification(_ item: ActivityItem) {
        guard item.nextAction == "cancel", item.notificationID != nil else {
            error = "Notification ID is unavailable."
            return
        }
        performActivityAction(item)
    }

    private func performActivityAction(_ item: ActivityItem) {
        guard let api, !activityActionPending else { return }
        let expectedSession = session
        activityActionPending = true
        runTask("activity-action") { [weak self] in
            defer { if self?.session == expectedSession { self?.activityActionPending = false } }
            do {
                if let notificationID = item.notificationID, item.nextAction == "cancel" {
                    try await api.cancelNotification(id: notificationID)
                } else {
                    _ = try await api.revertActivity(id: item.id)
                }
            } catch is CancellationError {
                return
            } catch { if self?.session == expectedSession { self?.report(error) } }
            guard let self, self.loggedIn, self.session == expectedSession, !Task.isCancelled else { return }
            await self.refreshActivity()
            await self.refreshEntries()
            self.rebuildPresentation()
            self.scheduleSync()
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
            PaletteCommand(title: "Refresh", key: "⌘R", enabled: self.loggedIn) { self.scheduleSync() },
        ]
        commands.append(contentsOf: Section.allCases.map { target in
            PaletteCommand(title: "Go to \(target.rawValue)", key: "⌘\(target.keyEquivalent)", enabled: true) { self.section = target }
        })
        commands.append(PaletteCommand(title: "Sign Out", key: "", enabled: loggedIn) { self.signOut() })
        return commands
    }
}

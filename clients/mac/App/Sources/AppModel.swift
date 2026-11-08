import Foundation
import EarnestKit
#if canImport(SwiftUI)
import SwiftUI
#endif
#if canImport(AppKit)
import AppKit
#endif

/// One per app: server login, identity, connection and the shared sync
/// engine. What one window shows lives in WindowModel; AppModel keeps weak
/// references to the open windows so shared changes refresh every window's
/// presentation from the same engine snapshot.
@MainActor final class AppModel: ObservableObject {
    @Published var server = UserDefaults.standard.string(forKey: "server") ?? ""
    @Published var feeds: [FeedInfo] = []
    @Published private(set) var activityActionPending = false
    @Published var activityGroups: [ActivityGroup] = []
    @Published var briefs: [BriefItem] = []
    @Published var agentChanges: [ActivityItem] = []
    @Published var principalNames: [Int64: String] = [:]
    @Published var error: String?
    @Published var connectionNote: String?
    @Published var code: DeviceAuthorization?
    @Published var loggingIn = false
    @Published var loggedIn = false
    @Published var connected = false
    @Published var syncing = false
    @Published var pendingOps = 0
    /// The shared account reader settings, restored from the durable snapshot
    /// on cold launch and refreshed after every sync. Windows keep their own
    /// drafts; this value is the account truth.
    @Published private(set) var readerSettings = APIReaderSettings()
    /// Last successfully synced unread totals. nil means never synced or the
    /// last fetch failed; it is never treated as zero.
    @Published private(set) var unreadCounts: APIUnreadCounts?
    @Published private(set) var unreadCountsError: String?

    private(set) var api: EarnestAPI?
    private var tokens: (any TokenStore)?
    private var serverURL: URL?
    private let identities: IdentityStore
    private let cacheDirectory: URL?
    private var identity: AccountIdentity?
    private(set) var cache: Cache?
    private(set) var engine: SyncEngine?
    private var coordinator: SyncCoordinator?
    private(set) var principalID: Int64 = 0
    private(set) var feedMap: [Int64: FeedInfo] = [:]
    private(set) var session = 0
    private var tasks: [String: Task<Void, Never>] = [:]
    var sessionActions = SessionCounts()
    private var windowRefs: [WindowRef] = []

    private struct WindowRef {
        weak var value: WindowModel?
    }

    /// Open windows in registration order; dead references are pruned on read.
    var windows: [WindowModel] {
        windowRefs.removeAll { $0.value == nil }
        return windowRefs.compactMap(\.value)
    }

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

    // MARK: - Windows

    func register(_ window: WindowModel) {
        windowRefs.removeAll { $0.value == nil || $0.value === window }
        windowRefs.append(WindowRef(value: window))
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
        feeds = []
        feedMap = [:]
        activityGroups = []
        activityActionPending = false
        briefs = []
        agentChanges = []
        principalNames = [:]
        code = nil
        loggingIn = false
        loggedIn = false
        syncing = false
        pendingOps = 0
        connectionNote = nil
        sessionActions = SessionCounts()
        readerSettings = APIReaderSettings()
        unreadCounts = nil
        unreadCountsError = nil
        error = nil
        for window in windows { window.endSession() }
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
                for window in windows { window.restoreCachedEntries([]) }
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

    /// A cache that cannot be opened is only unrecoverable when the database
    /// file itself is corrupt or undecodable. Any other failure (permissions,
    /// disk full, SQLITE_BUSY, directory creation) is rethrown so the retry and
    /// offline paths handle it, and the shared cache file is left untouched.
    private func makeCache(for identity: AccountIdentity) throws -> Cache {
        do { return try Cache(identity: identity, directory: cacheDirectory) }
        catch {
            guard Cache.isUnrecoverableOpenFailure(error) else { throw error }
            try Cache.quarantineCorruptCache(in: cacheDirectory)
            return try Cache(identity: identity, directory: cacheDirectory)
        }
    }

    /// A newly opened window reads the current engine, not a list captured
    /// before the latest sync or the section another window happens to show.
    func currentEntries() throws -> [APIEntry] {
        guard let engine else { return [] }
        let snapshot = try engine.snapshot()
        let cached = snapshot.entries.filter { !$0.deleted && $0.hasBody }.sorted { $0.seq < $1.seq }
        let states = try engine.states(principal: principalID, entryIDs: cached.map(\.id))
        return try cached.map { try cachedEntry($0, principal: principalID, engine: engine, state: states[$0.id]) }
    }

    func restoreCachedPresentation() throws {
        guard let engine, let cache else { return }
        let snapshot = try engine.snapshot()
        let entries = try currentEntries()
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
        for window in windows { window.restoreCachedEntries(entries) }
        try restoreReaderPreferences()
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
                    for window in self.windows { window.selected = nil }
                    try self.restoreCachedPresentation()
                }
                try self.restoreReaderPreferences()
                if !report.droppedOperationIDs.isEmpty {
                    self.connectionNote = "\(report.droppedOperationIDs.count) queued action(s) expired or no longer apply."
                } else {
                    self.connectionNote = nil
                }
                self.rebuildPresentation()
                await self.refresh()
                self.refreshPrincipalNames()
                for window in self.windows where window.articleUnavailable {
                    if let id = window.selected { window.open(id) }
                }
            } catch is CancellationError {
            } catch {
                guard self.session == s else { return }
                self.report(error)
            }
        }
    }

    func report(_ error: Error) {
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

    /// After a sync, shared resources are fetched once for the union of the
    /// sections on screen and every window refreshes its own list.
    func refresh() async {
        guard loggedIn else { return }
        // Exact counts are only meaningful in the unread-count reading mode;
        // river mode never requests them.
        if readerSettings.readingMode == .unreadCounts {
            await refreshUnreadCounts()
        }
        let sections = Set(windows.map(\.section))
        if sections.contains(.today) || sections.contains(.edition) || sections.contains(.feeds) {
            await refreshFeeds()
        }
        if sections.contains(.today) {
            async let briefsTask: Void = refreshBriefs()
            async let changesTask: Void = refreshAgentChanges()
            _ = await (briefsTask, changesTask)
        }
        if sections.contains(.activity) { await refreshActivity() }
        for window in windows { await window.refreshEntries() }
        rebuildPresentation()
    }

    func refreshFeeds() async {
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

    func refreshBriefs() async {
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

    func refreshAgentChanges() async {
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

    func refreshActivity() async {
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

    /// Shared inputs changed: every window re-derives its list from the same
    /// engine snapshot, so a write in one window shows up in the others.
    func rebuildPresentation() {
        pendingOps = (try? engine?.snapshot().outbox.count) ?? 0
        for window in windows { window.rebuildPresentation() }
    }

    /// Read the durable settings and last-synced counts into the shared
    /// presentation. The engine's merge rules already reject older settings
    /// revisions; a cold launch restores the same values without a request.
    func restoreReaderPreferences() throws {
        guard let engine else { return }
        let settings = try engine.readerSettings() ?? APIReaderSettings()
        if settings.rev >= readerSettings.rev { readerSettings = settings }
        unreadCounts = try engine.snapshot().unreadCounts
    }

    /// An acknowledged save enters the durable snapshot and the shared
    /// presentation, then windows rebuild so an open article repaints.
    func adoptReaderSettings(_ settings: APIReaderSettings) {
        do { try engine?.mergeSettings(settings) }
        catch { report(error) }
        if settings.rev >= readerSettings.rev { readerSettings = settings }
        for window in windows { window.rebuildPresentation() }
        if settings.readingMode == .unreadCounts {
            Task { [weak self] in await self?.refreshUnreadCounts() }
        }
    }

    /// The reader typography for an open article, resolving its feed's
    /// override. App chrome stays platform-native; only the article HTML
    /// uses the synced palette.
    func readerStyle(for entry: APIEntry?) -> ReaderStyle {
        ReaderStyle.resolve(readerSettings, feedID: entry?.feedID)
    }

    /// Fetch exact unread totals. River mode never calls this; a failure keeps
    /// the last-synced values so an outage cannot masquerade as zero.
    func refreshUnreadCounts() async {
        guard let api, loggedIn, readerSettings.readingMode == .unreadCounts else { return }
        let expectedSession = session
        do {
            let counts = try await api.unreadCounts()
            guard session == expectedSession, !Task.isCancelled,
                  readerSettings.readingMode == .unreadCounts else { return }
            unreadCounts = counts
            unreadCountsError = nil
            try engine?.mergeUnreadCounts(counts)
        } catch is CancellationError {
        } catch {
            guard session == expectedSession, !Task.isCancelled else { return }
            unreadCountsError = ErrorPresentation.message(error)
        }
    }

    func title(for entryID: Int64?) -> String? {
        guard let entryID else { return nil }
        for window in windows {
            if let title = window.entries.first(where: { $0.id == entryID })?.title, !title.isEmpty { return title }
        }
        return try? engine?.entry(entryID)?.title
    }

    // MARK: - Feeds

    func subscribe(url: String, cadence: String, onSubscribed: @escaping () -> Void) {
        guard let api else { return }
        let expectedSession = session
        runTask("feeds") { [weak self] in
            do {
                _ = try await api.subscribe(url: url, cadence: cadence)
                guard let self, self.loggedIn, self.session == expectedSession, !Task.isCancelled else { return }
                onSubscribed()
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
            for window in self.windows { await window.refreshEntries() }
            self.rebuildPresentation()
            self.scheduleSync()
        }
    }
}

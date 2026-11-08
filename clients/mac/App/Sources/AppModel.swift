import Foundation
import EarnestKit
#if canImport(SwiftUI)
import SwiftUI
#endif
#if canImport(AppKit)
import AppKit
#endif

struct ReaderPreferencesScope: Equatable {
    let session: Int
    let generation: String
}

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
    /// When the last synchronization finished, for the sidebar status line.
    @Published private(set) var lastSyncedAt: Date?
    /// Incremented to ask the frontmost window to open the Settings window.
    @Published var openSettingsRequest = 0
    /// Sign Out was chosen; the active window asks for confirmation.
    @Published var confirmingSignOut = false
    /// A New Subscription request is in flight.
    @Published private(set) var subscribing = false
    /// A subscription import or export in progress, for the status line.
    @Published private(set) var transfer: String?
    private var readerSettingsGeneration: String?
    private var countsRequestID = 0

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
    private var preferenceRefs: [PreferencesRef] = []

    private struct WindowRef {
        weak var value: WindowModel?
    }

    private struct PreferencesRef {
        weak var value: ReaderSettingsModel?
    }

    struct ImportReport: Identifiable, Equatable {
        let id = UUID()
        let title: String
        let detail: String
    }

    /// Settings drafts over the shared account preferences: the Settings
    /// window's, and any a test creates.
    var preferenceModels: [ReaderSettingsModel] {
        preferenceRefs.removeAll { $0.value == nil }
        return preferenceRefs.compactMap(\.value)
    }

    func register(preferences: ReaderSettingsModel) {
        preferenceRefs.removeAll { $0.value == nil || $0.value === preferences }
        preferenceRefs.append(PreferencesRef(value: preferences))
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
        readerSettingsGeneration = nil
        countsRequestID &+= 1
        unreadCounts = nil
        unreadCountsError = nil
        lastSyncedAt = nil
        subscribing = false
        transfer = nil
        error = nil
        for window in windows { window.endSession() }
        for preferences in preferenceModels { preferences.endSession() }
    }

    func connect() {
        do {
            guard let trimmed = ServerAddress.normalize(server), let url = URL(string: trimmed) else {
                throw APIError.invalidBaseURL
            }
            server = trimmed
            // A link opened while signed out or launching waits for this session.
            let links = windows.compactMap { window in window.pendingDeepLink.map { (window, $0) } }
            endSession()
            for (window, link) in links { window.deferDeepLink(link) }
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
                self.lastSyncedAt = Date()
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
                // The sign-in page opens when you ask for it, not on its own.
                self.code = authorization
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

    /// Ask before signing out. With no reading window open there is nowhere
    /// to show a sheet, so ask in an alert of its own.
    func requestSignOut() {
        guard loggedIn else { return }
        #if canImport(AppKit)
        if !windows.contains(where: { $0.nsWindow?.isVisible == true }) {
            let alert = NSAlert()
            alert.messageText = "Sign Out of Earnest?"
            alert.informativeText = "You will need to approve this Mac again to sign in."
            alert.addButton(withTitle: "Sign Out").hasDestructiveAction = true
            alert.addButton(withTitle: "Cancel")
            if alert.runModal() == .alertFirstButtonReturn { signOut() }
            return
        }
        #endif
        confirmingSignOut = true
    }

    /// The device-approval page for the code on screen, if it is a web address.
    var signInPage: URL? {
        guard let code, let url = URL(string: code.verificationURI),
              ["http", "https"].contains(url.scheme?.lowercased() ?? "") else { return nil }
        return url
    }

    func openSignInPage() {
        #if canImport(AppKit)
        if let signInPage { NSWorkspace.shared.open(signInPage) }
        #endif
    }

    /// Return to the server field, forgetting the connection and any login.
    func changeServer() {
        connected = false
        signOut()
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
        // An optional counts endpoint must not delay the primary reading refresh.
        if readerSettings.readingMode == .unreadCounts {
            await refreshUnreadCounts()
        }
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
        for preferences in preferenceModels { preferences.update(from: readerSettings) }
    }

    /// Read the durable settings and last-synced counts into the shared
    /// presentation. The engine's merge rules already reject older settings
    /// revisions; a cold launch restores the same values without a request.
    func restoreReaderPreferences() throws {
        guard let engine else { return }
        let snapshot = try engine.snapshot()
        let settings = try engine.readerSettings() ?? APIReaderSettings()
        let generation = snapshot.objects.contains { $0.kind == .settings && !$0.deleted }
            ? snapshot.cursor?.generation : nil
        let changed = generation != readerSettingsGeneration
        if changed {
            readerSettings = settings
            readerSettingsGeneration = generation
            countsRequestID &+= 1
            unreadCountsError = nil
        } else if settings.rev >= readerSettings.rev {
            readerSettings = settings
        }
        unreadCounts = generation == nil ? nil : snapshot.unreadCounts
        if changed {
            for preferences in preferenceModels { preferences.update(from: readerSettings) }
        }
    }

    /// A complete account baseline must belong to the active snapshot. During
    /// restore, the engine can advance before its presentation is rebuilt.
    func readerPreferencesScope() -> ReaderPreferencesScope? {
        guard let generation = readerSettingsGeneration,
              let snapshot = try? engine?.snapshot(), snapshot.cursor?.generation == generation else { return nil }
        return ReaderPreferencesScope(session: session, generation: generation)
    }

    func ownsReaderPreferences(_ scope: ReaderPreferencesScope) -> Bool {
        loggedIn && readerPreferencesScope() == scope
    }

    /// Persist before confirming success. The stored object remains canonical
    /// when another window or sync has already committed a newer revision.
    func adoptReaderSettings(_ settings: APIReaderSettings, scope: ReaderPreferencesScope) throws -> APIReaderSettings? {
        guard ownsReaderPreferences(scope), let engine else { return nil }
        try engine.mergeSettings(settings, generation: scope.generation)
        guard ownsReaderPreferences(scope) else { return nil }
        guard let effective = try engine.readerSettings() else { throw SyncError.invalidPage }
        readerSettings = effective
        for window in windows { window.rebuildPresentation() }
        for preferences in preferenceModels { preferences.update(from: effective) }
        if effective.readingMode == .unreadCounts {
            Task { [weak self] in await self?.refreshUnreadCounts() }
        }
        return effective
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
        guard let api, loggedIn, readerSettings.readingMode == .unreadCounts,
              let scope = readerPreferencesScope() else { return }
        countsRequestID &+= 1
        let requestID = countsRequestID
        do {
            let counts = try await api.unreadCounts()
            guard countsRequestID == requestID, ownsReaderPreferences(scope), !Task.isCancelled,
                  readerSettings.readingMode == .unreadCounts, let engine else { return }
            guard try engine.mergeUnreadCounts(counts, generation: scope.generation) else { return }
            unreadCounts = counts
            unreadCountsError = nil
        } catch is CancellationError {
        } catch {
            guard countsRequestID == requestID, ownsReaderPreferences(scope), !Task.isCancelled,
                  readerSettings.readingMode == .unreadCounts else { return }
            if ErrorPresentation.isUnauthorized(error) { report(error); return }
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
        guard let api, !subscribing else { return }
        let expectedSession = session
        subscribing = true
        runTask("feeds") { [weak self] in
            defer { if self?.session == expectedSession { self?.subscribing = false } }
            do {
                _ = try await api.subscribe(url: url, cadence: cadence)
                guard let self, self.loggedIn, self.session == expectedSession, !Task.isCancelled else { return }
                self.subscribing = false
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

    /// Ask the server to fetch a feed on its next pass.
    func refreshFeed(_ subscriptionID: Int64) {
        guard let api else { return }
        let expectedSession = session
        runTask("feed-refresh-\(subscriptionID)") { [weak self] in
            do {
                try await api.refreshSubscription(id: subscriptionID)
            } catch is CancellationError {
                return
            } catch {
                if self?.session == expectedSession { self?.report(error) }
                return
            }
            guard let self, self.loggedIn, self.session == expectedSession else { return }
            self.connectionNote = nil
            await self.refreshFeeds()
            self.rebuildPresentation()
        }
    }

    // MARK: - Subscription files

    /// OPML larger than this is not a subscription list.
    static let opmlLimit = 16 << 20

    /// Import an OPML document's folders and feeds. The server skips feeds
    /// you already follow; the report, handed to the window that asked, says
    /// what happened, failures included.
    func importOPML(_ document: Data, name: String, reported: @escaping (ImportReport) -> Void) {
        guard let api, loggedIn, transfer == nil else { return }
        guard document.count <= Self.opmlLimit else {
            error = "“\(name)” is too large to be a subscription list."
            return
        }
        let expectedSession = session
        let followed = Set(feeds.map(\.id))
        transfer = "Importing subscriptions…"
        runTask("opml") { [weak self] in
            defer { if self?.session == expectedSession { self?.transfer = nil } }
            do {
                let summary = try await api.importOPML(document)
                guard let self, self.session == expectedSession, self.loggedIn else { return }
                reported(Self.report(for: summary, name: name, followed: followed))
                await self.refreshFeeds()
                self.rebuildPresentation()
                self.scheduleSync()
            } catch is CancellationError {
            } catch {
                guard self?.session == expectedSession else { return }
                self?.report(error)
            }
        }
    }

    static func report(for summary: OPMLImportSummary, name: String, followed: Set<Int64> = []) -> ImportReport {
        var parts: [String] = []
        let added = summary.subscriptionIDs.filter { !followed.contains($0) }.count
        let already = summary.subscriptionIDs.count - added
        parts.append(added == 1 ? "1 new subscription." : "\(added) new subscriptions.")
        if already > 0 {
            parts.append(already == 1 ? "1 feed you already follow." : "\(already) feeds you already follow.")
        }
        if summary.folders > 0 { parts.append(summary.folders == 1 ? "1 folder." : "\(summary.folders) folders.") }
        if summary.deferred > 0 {
            parts.append(summary.deferred == 1 ? "1 feed will be checked shortly." : "\(summary.deferred) feeds will be checked shortly.")
        }
        if !summary.failures.isEmpty {
            let count = summary.failures.count
            let listed = summary.failures.prefix(5).joined(separator: "\n")
            parts.append((count == 1 ? "1 entry could not be imported:" : "\(count) entries could not be imported:") + "\n" + listed)
            if count > 5 { parts.append("and \(count - 5) more.") }
        }
        let title = summary.failures.isEmpty ? "Imported “\(name)”" : "Imported “\(name)” with problems"
        return ImportReport(title: title, detail: parts.joined(separator: " "))
    }

    /// Fetch the account's subscriptions as OPML for a save panel.
    func exportOPML() async throws -> Data {
        guard let api, loggedIn else { throw APIError.invalidResponse }
        let expectedSession = session
        transfer = "Exporting subscriptions…"
        defer { if session == expectedSession { transfer = nil } }
        let data = try await api.exportOPML()
        guard session == expectedSession, loggedIn else { throw CancellationError() }
        return data
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

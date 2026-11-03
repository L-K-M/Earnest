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
    @Published var activityGroups: [ActivityGroup] = []
    @Published var briefs: [BriefItem] = []
    @Published var agentChanges: [ActivityItem] = []
    @Published var principalNames: [Int64: String] = [:]
    @Published var selected: Int64?
    @Published var html = ""
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
    private var tokens: KeychainTokens?
    private var serverURL: URL?
    private var engine: SyncEngine?
    private var coordinator: SyncCoordinator?
    private var principalID: Int64 = 0
    private var feedMap: [Int64: FeedInfo] = [:]
    private var session = 0
    private var tasks: [String: Task<Void, Never>] = [:]
    private var sessionActions = SessionCounts()

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
        coordinator = nil
        principalID = 0
        entries = []
        rows = []
        editionGroups = []
        feeds = []
        feedMap = [:]
        activityGroups = []
        briefs = []
        agentChanges = []
        principalNames = [:]
        selected = nil
        html = ""
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
        endSession()
        do { try tokens?.delete() } catch { report(error) }
    }

    /// A new session resolves the principal once, then builds the persisted
    /// cache, sync engine and network coordinator around it.
    private func startSession() {
        let s = session
        runTask("session") { [weak self] in
            guard let self, let api = self.api else { return }
            do {
                let me = try await api.me()
                guard self.session == s, self.loggedIn else { return }
                guard let principal = parsePrincipalID(me), let url = self.serverURL else { throw APIError.invalidResponse }
                self.principalID = principal
                let engine = SyncEngine(store: try Cache(server: url, principal: principal))
                self.engine = engine
                self.coordinator = SyncCoordinator(engine: engine, api: api)
                self.refreshPrincipalNames()
                await self.refresh()
                self.startEvents()
                self.scheduleSync()
            } catch is CancellationError {
            } catch { if self.session == s { self.report(error) } }
        }
    }

    private func startEvents() {
        let s = session
        runTask("events") { [weak self] in
            while !Task.isCancelled {
                guard let self, let coordinator = self.coordinator, self.session == s else { return }
                do {
                    try await coordinator.runEvents { [weak self] report in
                        // Every hint means the engine state moved; refresh the
                        // displayed list so agent changes arrive live.
                        guard let self, self.session == s else { return }
                        if !report.droppedOperationIDs.isEmpty {
                            self.connectionNote = "\(report.droppedOperationIDs.count) queued action(s) expired or no longer apply."
                        }
                        self.rebuildPresentation()
                        await self.refresh()
                    }
                    return
                } catch is CancellationError {
                    return
                } catch {
                    guard !Task.isCancelled, self.session == s else { return }
                    if let problem = error as? APIProblem, [401, 403].contains(problem.status) {
                        self.report(error)
                        return
                    }
                    self.connectionNote = "Live updates interrupted; retrying."
                    try? await Task.sleep(for: .seconds(10))
                }
            }
        }
        // The event stream keeps the engine fresh, but entry lists are API
        // fetches: poll on a slow cadence so new items arrive on their own.
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
        guard let coordinator else { return }
        let s = session
        runTask("sync") { [weak self] in
            if delay > 0 { try? await Task.sleep(for: .milliseconds(Int(delay * 1000))) }
            guard let self, self.session == s, self.loggedIn, !Task.isCancelled else { return }
            self.syncing = true
            defer { self.syncing = false }
            do {
                let report = try await coordinator.synchronize()
                guard !Task.isCancelled, self.session == s, self.loggedIn else { return }
                if !report.droppedOperationIDs.isEmpty {
                    self.connectionNote = "\(report.droppedOperationIDs.count) queued action(s) expired or no longer apply."
                } else {
                    self.connectionNote = nil
                }
                self.rebuildPresentation()
                await self.refresh()
            } catch is CancellationError {
            } catch {
                guard self.session == s else { return }
                self.connectionNote = "Sync failed: \(String(describing: error))"
            }
        }
    }

    private func report(_ error: Error) {
        if let problem = error as? APIProblem {
            if problem.status == 401 {
                signOut()
                self.error = "Your sign-in expired or was revoked. Sign in again."
                return
            }
            self.error = problem.detail ?? problem.title
        } else {
            self.error = String(describing: error)
        }
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
            entries = all
        } catch is CancellationError {
        } catch {
            guard session == expectedSession, (try? engine.snapshot().cursor?.generation) == generation else { return }
            report(error)
        }
    }

    private func refreshFeeds() async {
        guard let api else { return }
        let expectedSession = session
        do {
            let subs = try await api.subscriptions().compactMap(FeedInfo.init).filter { !$0.deleted }
            guard loggedIn, session == expectedSession, !Task.isCancelled else { return }
            feeds = subs
            feedMap = Dictionary(subs.map { ($0.id, $0) }, uniquingKeysWith: { _, last in last })
        } catch is CancellationError {
        } catch { if session == expectedSession { report(error) } }
    }

    private func refreshBriefs() async {
        guard let api else { return }
        let expectedSession = session
        do {
            let items = try await api.todayBriefs().compactMap(BriefItem.init)
            guard loggedIn, session == expectedSession, !Task.isCancelled else { return }
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
    }

    // MARK: - Presentation

    /// Displayed entries never mutate the fetched APIEntry: each row's state
    /// is the fetched state merged with the engine's synced-plus-overlay state.
    private func effectiveState(of entry: APIEntry) -> EarnestKit.State {
        guard let engine, principalID > 0 else { return entry.state.syncState }
        return (try? engine.state(principal: principalID, entryID: entry.id)) ?? entry.state.syncState
    }

    private func riverVisible(_ state: EarnestKit.State) -> Bool {
        state.seen.value == .text("unseen") && state.shelf.value == .text("none")
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
        let presented = entries.filter { !$0.deleted }.map { ($0, effectiveState(of: $0)) }
        switch section {
        case .today:
            rows = presented.filter { riverVisible($0.1) && cadence(of: $0.0) != "week" }.map { row($0.0, $0.1) }
            editionGroups = []
        case .edition:
            let week = presented.filter { riverVisible($0.1) && cadence(of: $0.0) == "week" }
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
        pendingOps = (try? engine?.snapshot().outbox.count) ?? 0
        updateEndStatement(presented)
    }

    private func updateEndStatement(_ presented: [(APIEntry, EarnestKit.State)]) {
        guard section == .today else {
            endTitle = ""
            endDetail = ""
            return
        }
        let river = presented.filter { riverVisible($0.1) && cadence(of: $0.0) != "week" }
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
        if !ids.isEmpty { selected = ids[min(index, ids.count - 1)] } else { selected = nil }
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
        selected = ids[min(ids.count - 1, max(0, current + offset))]
    }

    func openOriginal() {
        guard let entry = entries.first(where: { $0.id == selected }),
              let url = URL(string: entry.url),
              ["http", "https"].contains(url.scheme?.lowercased() ?? "") else { return }
        #if canImport(AppKit)
        NSWorkspace.shared.open(url)
        #endif
    }

    func open(_ id: Int64?) {
        guard let id, loggedIn else { html = ""; return }
        html = ""
        runTask("open") { [weak self] in
            guard let self, let api = self.api, let engine = self.engine, self.loggedIn,
                  let generation = try? engine.snapshot().cursor?.generation else { return }
            let expectedSession = self.session
            do {
                if !self.entries.contains(where: { $0.id == id }) {
                    let entry = try await api.entry(id: id)
                    try Task.checkCancellation()
                    guard self.loggedIn, self.session == expectedSession,
                          (try? engine.snapshot().cursor?.generation) == generation else { return }
                    try engine.mergeEntries([entry], generation: generation)
                    self.entries.append(entry)
                }
                let article: String
                do {
                    article = try await api.signedContent(entryID: id)
                } catch is CancellationError {
                    return
                } catch {
                    article = try await api.content(entryID: id).html
                }
                guard self.selected == id, self.loggedIn, self.session == expectedSession,
                      (try? engine.snapshot().cursor?.generation) == generation, !Task.isCancelled else { return }
                self.html = article
                self.markSeen(entryID: id, entry: self.entries.first(where: { $0.id == id }))
            } catch is CancellationError {
            } catch {
                guard self.loggedIn, self.session == expectedSession,
                      (try? engine.snapshot().cursor?.generation) == generation else { return }
                var missing = (error as? APIProblem)?.status == 404
                if case APIError.httpStatus(404) = error { missing = true }
                if missing {
                    try? self.engine?.markGone(entryID: id)
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
        guard let api, item.nextAction == "revert" else { return }
        let expectedSession = session
        runTask("revert") { [weak self] in
            do {
                _ = try await api.revertActivity(id: item.id)
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

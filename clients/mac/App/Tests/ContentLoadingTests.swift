import Foundation
import XCTest
import EarnestKit
@testable import Earnest

@MainActor final class ContentLoadingTests: XCTestCase {
    func testListAndKeyboardSelectionsUseProxiedImages() async throws {
        let (model, session) = try makeModel([
            .signed(mode: "proxied"), .article,
            .signed(mode: "proxied"), .article,
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        model.selectFromList(42)
        try await waitForArticle(model)
        XCTAssertEqual(model.imageOrigin?.absoluteString, "https://content.test:8081")
        XCTAssertEqual(model.readingEntry?.title, "Article title")
        model.rows = [EntryRow(id: 42, title: "Article title", subtitle: "", unread: true)]
        model.move(1)
        try await waitForArticle(model)
        XCTAssertEqual(model.imageMode, .proxied)
        ContentHTTP.assertFinished()
    }

    func testDeepLinkBlocksImagesUntilExplicitLoadIncludingSameEntry() async throws {
        let (model, session) = try makeModel([
            .signed(mode: "proxied"), .article,
            .signed(mode: "click"), .article,
            .signed(mode: "proxied"), .article,
            .signed(mode: "click"), .article,
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        model.selectFromList(42)
        try await waitForArticle(model)
        model.openDeepLink(URL(string: "earnest://entry/42")!)
        XCTAssertNil(model.imageOrigin)
        try await waitForArticle(model)
        XCTAssertEqual(model.imageMode, .click)
        XCTAssertNil(model.imageOrigin)
        model.loadImages()
        try await waitForArticle(model)
        XCTAssertEqual(model.imageOrigin?.absoluteString, "https://content.test:8081")
        model.openDeepLink(URL(string: "earnest://entry/42")!)
        try await waitForArticle(model)
        XCTAssertNil(model.imageOrigin)
        ContentHTTP.assertFinished()
    }

    func testFallbackBlocksOriginalImageURLs() async throws {
        let (model, session) = try makeModel([
            .init(path: "/v1/entries/42/content-url", status: 503, body: "{}", mode: "proxied"),
            .init(path: "/v1/entries/42/content", body: #"{"entry_id":42,"html":"<img src='https://publisher.test/tracker'>","text":"","sanitizer_version":1,"source":"feed"}"#),
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        model.selectFromList(42)
        try await waitForArticle(model)
        XCTAssertTrue(model.html.contains("publisher.test"))
        XCTAssertNil(model.imageOrigin)
        let scheme = ArticleScheme()
        scheme.html = model.html
        scheme.imageOrigin = model.imageOrigin
        XCTAssertTrue(scheme.page.contains("img-src 'none';"))
        ContentHTTP.assertFinished()
    }

    func testConnectionFailuresUseNoteInsteadOfModal() async throws {
        let (model, session) = try makeModel([
            .init(path: "/v1/entries/42/content-url", body: "", failure: .cannotConnectToHost),
            .init(path: "/v1/entries/42/content", body: "", failure: .cannotConnectToHost),
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        model.selectFromList(42)
        let deadline = Date().addingTimeInterval(5)
        while model.error == nil && model.connectionNote == nil && Date() < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertNil(model.error, "Connection failures must not open a modal")
        XCTAssertNotNil(model.connectionNote)
        ContentHTTP.assertFinished()
    }

    func testColdLaunchRestoresRowsBriefsReaderAndQueuesActionsOffline() async throws {
        let fixture = try offlineFixture()
        let (directory, defaults, store, identity, cache) = fixture
        defer { defaults.removePersistentDomain(forName: defaultsSuite) }
        let session = ContentHTTP.install([
            .init(path: "/v1/me", body: "", failure: .cannotConnectToHost),
            .init(path: "/v1/entries/42/content-url", body: "", failure: .notConnectedToInternet),
            .init(path: "/v1/entries/42/content", body: "", failure: .notConnectedToInternet),
            .init(path: "/v1/me", body: "", failure: .cannotConnectToHost),
        ])
        let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, serverURL: identity.server, identities: store, cacheDirectory: directory)
        defer { model.signOut(); session.invalidateAndCancel() }
        model.loggedIn = true
        let online = try await model.establishSession()
        XCTAssertFalse(online)
        XCTAssertNil(model.error)
        XCTAssertEqual(model.rows.map(\.id), [42])
        XCTAssertEqual(model.briefs.map(\.text), ["Cached brief"])
        XCTAssertEqual(model.feeds.map(\.title), ["Cached feed"])
        XCTAssertNotNil(model.connectionNote)
        model.selectFromList(42)
        try await waitForArticle(model)
        XCTAssertEqual(model.html, "<p>Saved article</p><img src='https://publisher.test/tracker'>")
        XCTAssertTrue(model.offlineCopy)
        XCTAssertNil(model.imageOrigin)
        XCTAssertEqual(model.readingEntry?.title, "Article title")
        let scheme = ArticleScheme()
        scheme.html = model.html
        scheme.imageOrigin = model.imageOrigin
        XCTAssertTrue(scheme.page.contains("img-src 'none';"))
        model.command(.keep)
        let deadline = Date().addingTimeInterval(5)
        while !ContentHTTP.finished && Date() < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertNil(model.error)
        XCTAssertEqual(try cache.load().outbox.map(\.register), [.seen, .kept])
        XCTAssertEqual(model.pendingOps, 2)
        ContentHTTP.assertFinished()
    }

    func testVerifiedDifferentAccountDoesNotAttachRememberedCache() async throws {
        let (directory, defaults, store, identity, oldCache) = try offlineFixture()
        defer { defaults.removePersistentDomain(forName: defaultsSuite) }
        let session = ContentHTTP.install([
            .init(path: "/v1/me", body: #"{"user_id":2,"principal_id":8,"kind":"human","scopes":[]}"#),
        ])
        let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, serverURL: identity.server, identities: store, cacheDirectory: directory)
        defer { model.signOut(); session.invalidateAndCancel() }
        model.loggedIn = true
        let online = try await model.establishSession()
        XCTAssertTrue(online)
        XCTAssertTrue(model.entries.isEmpty)
        XCTAssertTrue(model.briefs.isEmpty)
        XCTAssertTrue(model.feeds.isEmpty)
        XCTAssertEqual(try store.load(server: identity.server)?.principalID, 8)
        XCTAssertEqual(try oldCache.load().entries.count, 1)
        ContentHTTP.assertFinished()
    }

    func testPrincipalChangeFollowedByConnectionFailureKeepsOldCacheDetached() async throws {
        let (directory, defaults, store, identity, oldCache) = try offlineFixture()
        defer { defaults.removePersistentDomain(forName: defaultsSuite) }
        let engine = SyncEngine(store: oldCache)
        try engine.queue(Write(opID: "old-account", principal: 7, entryID: 42, register: .kept, value: .flag(true),
                               base: Cursor(generation: "g", rev: 0), occurredAt: Date()))
        let session = ContentHTTP.install([
            .init(path: "/v1/me", body: "", failure: .cannotConnectToHost),
            .init(path: "/v1/me", body: #"{"user_id":2,"principal_id":8,"kind":"human","scopes":[]}"#),
            .init(path: "/v1/me", body: "", failure: .cannotConnectToHost),
        ])
        let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, serverURL: identity.server, identities: store, cacheDirectory: directory)
        defer { model.signOut(); session.invalidateAndCancel() }
        model.loggedIn = true
        _ = try await model.establishSession()
        XCTAssertEqual(model.entries.map(\.id), [42])
        XCTAssertEqual(model.pendingOps, 1)
        _ = try await model.establishSession()
        let deadline = Date().addingTimeInterval(5)
        while (!ContentHTTP.finished || model.connectionNote == nil) && Date() < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertTrue(model.loggedIn)
        XCTAssertNil(model.error)
        XCTAssertTrue(model.entries.isEmpty)
        XCTAssertTrue(model.briefs.isEmpty)
        XCTAssertEqual(model.pendingOps, 0)
        XCTAssertEqual(try store.load(server: identity.server)?.principalID, 8)
        XCTAssertEqual(try oldCache.load().outbox.map(\.opID), ["old-account"])
        ContentHTTP.assertFinished()
    }

    func testUnauthorizedSyncSignsOutWithoutSendingOutbox() async throws {
        let (directory, defaults, store, identity, cache) = try offlineFixture()
        defer { defaults.removePersistentDomain(forName: defaultsSuite) }
        let session = ContentHTTP.install([
            .init(path: "/v1/me", body: "", failure: .cannotConnectToHost),
            .init(path: "/v1/me", status: 401, body: "{}"),
        ])
        let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, serverURL: identity.server, identities: store, cacheDirectory: directory)
        defer { model.signOut(); session.invalidateAndCancel() }
        model.loggedIn = true
        _ = try await model.establishSession()
        model.scheduleSync()
        let deadline = Date().addingTimeInterval(5)
        while model.loggedIn && Date() < deadline { try await Task.sleep(for: .milliseconds(10)) }
        XCTAssertFalse(model.loggedIn)
        XCTAssertTrue(model.rows.isEmpty)
        XCTAssertTrue(model.briefs.isEmpty)
        XCTAssertNil(try store.load(server: identity.server))
        XCTAssertEqual(model.error, "Your sign-in expired or was revoked. Sign in again.")
        XCTAssertEqual(try cache.load().entries.count, 1)
        ContentHTTP.assertFinished()
    }

    func testDisplayedSignedAndFallbackHTMLIsPersisted() async throws {
        let cases: [([ContentHTTP.Reply], String)] = [
            ([.signed(mode: "proxied"), .article], "<p>Article</p>"),
            ([
                .init(path: "/v1/entries/42/content-url", status: 503, body: "{}"),
                .init(path: "/v1/entries/42/content", body: #"{"entry_id":42,"html":"<p>Fallback copy</p>","text":"","sanitizer_version":1,"source":"feed"}"#),
            ], "<p>Fallback copy</p>"),
        ]
        for (replies, expected) in cases {
            let (directory, defaults, store, identity, _) = try offlineFixture()
            defer { defaults.removePersistentDomain(forName: defaultsSuite) }
            let session = ContentHTTP.install([
                .init(path: "/v1/me", body: #"{"user_id":1,"principal_id":7,"kind":"human","scopes":[]}"#),
            ] + replies)
            let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
            let model = AppModel(api: api, serverURL: identity.server, identities: store, cacheDirectory: directory)
            defer { model.signOut(); session.invalidateAndCancel() }
            model.loggedIn = true
            _ = try await model.establishSession()
            model.selectFromList(42)
            try await waitForArticle(model)
            XCTAssertEqual(model.html, expected)
            XCTAssertFalse(model.offlineCopy)
            let reopened = try Cache(identity: identity, directory: directory)
            XCTAssertEqual(try reopened.article(entryID: 42, generation: "g"), expected)
            ContentHTTP.assertFinished()
        }
    }

    func testReconnectVerifiesIdentitySendsOutboxAndClearsNote() async throws {
        let (directory, defaults, store, identity, cache) = try offlineFixture()
        defer { defaults.removePersistentDomain(forName: defaultsSuite) }
        let engine = SyncEngine(store: cache)
        try engine.queue(Write(opID: "offline", principal: 7, entryID: 42, register: .kept, value: .flag(true),
                               base: Cursor(generation: "g", rev: 0), occurredAt: Date()))
        var state = EarnestKit.State(principal: 7, entryID: 42)
        state.kept = Stamp(value: .flag(true), rev: 2)
        state.stateRev = 2
        let stateJSON = String(decoding: try JSONEncoder().encode(state), as: UTF8.self)
        let acknowledgement = "{\"op_id\":\"offline\",\"status\":\"applied\",\"state\":\(stateJSON),\"replay\":false}"
        let session = ContentHTTP.install([
            .init(path: "/v1/me", body: "", failure: .cannotConnectToHost),
            .init(path: "/v1/me", body: #"{"user_id":1,"principal_id":7,"kind":"human","scopes":[]}"#),
            .init(path: "/v1/changes", body: #"{"changes":[],"next":{"generation":"g","rev":1},"next_cursor":"g:1"}"#),
            .init(path: "/v1/state:batch", body: "[\(acknowledgement)]", method: "POST"),
            .init(path: "/v1/changes", body: "{\"changes\":[{\"rev\":2,\"type\":\"state.updated\",\"payload\":{\"results\":[\(acknowledgement)]}}],\"next\":{\"generation\":\"g\",\"rev\":2},\"next_cursor\":\"g:2\"}"),
            .init(path: "/v1/changes", body: #"{"changes":[],"next":{"generation":"g","rev":2},"next_cursor":"g:2"}"#),
            .init(path: "/v1/entries", body: #"{"entries":[]}"#),
        ])
        let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, serverURL: identity.server, identities: store, cacheDirectory: directory)
        defer { model.signOut(); session.invalidateAndCancel() }
        model.loggedIn = true
        _ = try await model.establishSession()
        XCTAssertNotNil(model.connectionNote)
        model.section = .later
        model.scheduleSync()
        let deadline = Date().addingTimeInterval(5)
        while (!ContentHTTP.finished || model.syncing || model.connectionNote != nil) && Date() < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertNil(model.error)
        XCTAssertNil(model.connectionNote)
        XCTAssertTrue(try cache.load().outbox.isEmpty)
        XCTAssertEqual(try engine.state(principal: 7, entryID: 42).kept.value, .flag(true))
        ContentHTTP.assertFinished()
    }

    private let defaultsSuite = "Earnest.ContentLoadingTests.Offline.\(UUID())"

    private func offlineFixture() throws -> (URL, UserDefaults, IdentityStore, AccountIdentity, Cache) {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        let defaults = UserDefaults(suiteName: defaultsSuite)!
        defaults.removePersistentDomain(forName: defaultsSuite)
        let store = IdentityStore(defaults: defaults)
        let identity = try AccountIdentity(server: URL(string: "https://api.test")!, userID: 1, principalID: 7)
        try store.save(identity)
        let cache = try Cache(identity: identity, directory: directory)
        let engine = SyncEngine(store: cache)
        let entry = try JSONDecoder().decode(APIEntry.self, from: Data(Self.entryJSON.utf8))
        try engine.bootstrap(Cursor(generation: "g", rev: 1))
        try engine.mergeEntries([entry], generation: "g")
        try engine.finishBootstrap()
        try cache.saveResource("feeds", values: [.object(["id": .integer(1), "title": .string("Cached feed")])], generation: "g")
        try cache.saveResource("briefs", values: [.object(["id": .integer(9), "value": .string("Cached brief")])], generation: "g")
        try cache.saveArticle(entryID: 42, html: "<p>Saved article</p><img src='https://publisher.test/tracker'>", generation: "g")
        return (directory, defaults, store, identity, cache)
    }

    func testCSPAllowsOnlySignedContentOrigin() {
        let scheme = ArticleScheme()
        scheme.html = "<img src='https://publisher.test/tracker'>"
        scheme.imageOrigin = URL(string: "https://content.test:8081")
        XCTAssertTrue(scheme.page.contains("default-src 'none'; img-src https://content.test:8081;"))
        XCTAssertTrue(scheme.page.contains("base-uri 'none'; form-action 'none'"))
    }

    func testNotificationCancellationUsesNotificationTargetAndRefreshesActivity() async throws {
        let (model, session) = try makeModel([
            .init(path: "/v1/notifications/19:cancel", body: "{}", method: "POST"),
            .init(path: "/v1/activity", body: #"{"activities":[]}"#),
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        let item = ActivityItem(.object([
            "id": .integer(8), "action": .string("notification.created"),
            "target": .object(["id": .integer(19)]), "next_action": .string("cancel")
        ]))
        model.section = .activity
        model.activityGroups = [ActivityGroup(principalID: 2, name: "Scout", items: [item])]
        model.cancelNotification(item)
        XCTAssertTrue(model.activityActionPending)
        let deadline = Date().addingTimeInterval(5)
        while model.activityActionPending && Date() < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertFalse(model.activityActionPending)
        XCTAssertNil(model.error)
        XCTAssertTrue(model.activityGroups.isEmpty)
        ContentHTTP.assertFinished()
    }

    private func waitForArticle(_ model: AppModel) async throws {
        let deadline = Date().addingTimeInterval(5)
        while model.html.isEmpty && model.error == nil && Date() < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertNil(model.error)
        XCTAssertFalse(model.html.isEmpty, "Article did not finish loading")
    }

    private func makeModel(_ replies: [ContentHTTP.Reply]) throws -> (AppModel, URLSession) {
        let entry = try JSONDecoder().decode(APIEntry.self, from: Data(Self.entryJSON.utf8))
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g", rev: 1))
        try engine.mergeEntries([entry], generation: "g")
        try engine.finishBootstrap()
        let session = ContentHTTP.install(replies)
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, engine: engine, principalID: 7)
        model.loggedIn = true
        model.entries = [entry]
        return (model, session)
    }

    static let entryJSON = #"{"id":42,"subscription_id":1,"feed_id":2,"seq":3,"title":"Article title","url":"https://publisher.test/article","author":"Ada","first_seen_at":"2026-11-04T08:00:00Z","rev":1,"deleted":false,"state":{"principal":7,"entry_id":42,"state_rev":0,"seen":{"value":"unseen","rev":0,"occurred_at":"","actor":0,"source":""},"shelf":{"value":"none","rev":0,"occurred_at":"","actor":0,"source":""},"kept":{"value":false,"rev":0,"occurred_at":"","actor":0,"source":""}}}"#
}

private struct ContentTokens: TokenStore {
    func load() throws -> String? { "test-token" }
    func save(_ token: String) throws {}
    func delete() throws {}
}

private final class ContentHTTP: URLProtocol {
    struct Reply {
        let path: String
        var status = 200
        let body: String
        var mode: String?
        var method = "GET"
        var failure: URLError.Code?

        static func signed(mode: String) -> Self {
            .init(path: "/v1/entries/42/content-url", body: #"{"url":"https://content.test:8081/article/42?signature=abc","expires_at":"2099-01-01T00:00:00Z"}"#, mode: mode)
        }
        static let article = Self(path: "/article/42", body: "<p>Article</p>")
    }
    private static let lock = NSLock()
    private static var replies: [Reply] = []

    static func install(_ replies: [Reply]) -> URLSession {
        lock.withLock { Self.replies = replies }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [Self.self]
        return URLSession(configuration: configuration)
    }

    static var finished: Bool { lock.withLock { replies.isEmpty } }
    static func assertFinished() { XCTAssertTrue(lock.withLock { replies.isEmpty }) }
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        guard let reply = Self.lock.withLock({ Self.replies.isEmpty ? nil : Self.replies.removeFirst() }) else {
            XCTFail("Unexpected request: \(request.url?.path ?? "")")
            client?.urlProtocol(self, didFailWithError: URLError(.badServerResponse))
            return
        }
        XCTAssertEqual(request.url?.path, reply.path)
        XCTAssertEqual(request.httpMethod, reply.method)
        if let mode = reply.mode { XCTAssertEqual(request.url?.query, "mode=\(mode)") }
        if let failure = reply.failure {
            client?.urlProtocol(self, didFailWithError: URLError(failure))
            return
        }
        let isContent = request.url?.host == "content.test"
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), isContent ? nil : "Bearer test-token")
        let response = HTTPURLResponse(url: request.url!, statusCode: reply.status, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(reply.body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

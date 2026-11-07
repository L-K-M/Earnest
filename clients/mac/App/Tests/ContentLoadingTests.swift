import Foundation
import XCTest
import EarnestKit
import GRDB
import Network
@testable import Earnest

@MainActor final class ContentLoadingTests: XCTestCase {
    func testFailedLoginPreservesIdentityForOfflineRelaunch() async throws {
        let (directory, defaults, store, identity, _) = try offlineFixture()
        defer { defaults.removePersistentDomain(forName: defaultsSuite) }
        let session = ContentHTTP.install([
            .init(path: "/v1/devices/start", status: 503, body: "{}", method: "POST"),
            .init(path: "/v1/me", body: "", failure: .cannotConnectToHost),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
        let login = AppModel(api: api, serverURL: identity.server, tokens: ContentTokens(), identities: store, cacheDirectory: directory)
        login.login()
        let deadline = Date().addingTimeInterval(5)
        while login.loggingIn && Date() < deadline { try await Task.sleep(for: .milliseconds(10)) }
        XCTAssertFalse(login.loggingIn)
        XCTAssertNotNil(login.error)
        XCTAssertEqual(try store.load(server: identity.server), identity)

        let relaunched = AppModel(api: api, serverURL: identity.server, identities: store, cacheDirectory: directory)
        defer { relaunched.signOut() }
        relaunched.loggedIn = true
        let online = try await relaunched.establishSession()
        XCTAssertFalse(online)
        XCTAssertEqual(try relaunched.currentEntries().map(\.id), [42])
        XCTAssertEqual(relaunched.briefs.map(\.text), ["Cached brief"])
        ContentHTTP.assertFinished()
    }

    func testNewCredentialsRequireVerificationBeforeOfflineCacheReuse() async throws {
        let (directory, defaults, store, identity, _) = try offlineFixture()
        defer { defaults.removePersistentDomain(forName: defaultsSuite) }
        let session = ContentHTTP.install([
            .init(path: "/v1/devices/start", body: #"{"device_code":"device","user_code":"user","verification_uri":"urn:earnest:test","interval":1,"expires_in":30}"#, method: "POST"),
            .init(path: "/v1/devices/poll", body: #"{"access_token":"test-token","token_type":"Bearer"}"#, method: "POST"),
            .init(path: "/v1/me", body: "", failure: .cannotConnectToHost),
        ])
        let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, serverURL: identity.server, tokens: ContentTokens(), identities: store, cacheDirectory: directory)
        defer { model.signOut(); session.invalidateAndCancel() }
        model.login()
        let deadline = Date().addingTimeInterval(5)
        while model.connectionNote == nil && Date() < deadline { try await Task.sleep(for: .milliseconds(10)) }
        XCTAssertTrue(model.loggedIn)
        XCTAssertNil(model.error)
        XCTAssertNotNil(model.connectionNote)
        XCTAssertTrue(try model.currentEntries().isEmpty)
        XCTAssertTrue(model.briefs.isEmpty)
        XCTAssertNil(try store.load(server: identity.server))
        ContentHTTP.assertFinished()
    }

    func testArticleCacheWriteFailureStillDisplaysFetchedAndOfflineHTML() async throws {
        let cases: [([ContentHTTP.Reply], String, Bool)] = [
            ([.signed(mode: "proxied"), .article], "<p>Article</p>", false),
            ([
                .init(path: "/v1/entries/42/content-url", status: 503, body: "{}"),
                .init(path: "/v1/entries/42/content", body: #"{"entry_id":42,"html":"<p>Fallback</p>","text":"","sanitizer_version":1,"source":"feed"}"#),
            ], "<p>Fallback</p>", false),
            ([
                .init(path: "/v1/entries/42/content-url", body: "", failure: .cannotConnectToHost),
                .init(path: "/v1/entries/42/content", body: "", failure: .cannotConnectToHost),
            ], "<p>Saved article</p><img src='https://publisher.test/tracker'>", true),
        ]
        for (replies, expected, offline) in cases {
            let (directory, defaults, store, identity, cache) = try offlineFixture()
            defer { defaults.removePersistentDomain(forName: defaultsSuite) }
            let db = try DatabaseQueue(path: directory.appendingPathComponent("cache.sqlite").path)
            try await db.write { db in
                try db.execute(sql: "CREATE TRIGGER fail_article_write BEFORE INSERT ON article_copies BEGIN SELECT RAISE(ABORT, 'article cache unavailable'); END")
            }
            XCTAssertThrowsError(try cache.saveArticle(entryID: 42, html: "New copy", generation: "g"))
            let session = ContentHTTP.install([
                .init(path: "/v1/me", body: "", failure: .cannotConnectToHost),
            ] + replies)
            let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
            let model = AppModel(api: api, serverURL: identity.server, identities: store, cacheDirectory: directory)
            let window = WindowModel(app: model)
            defer { model.signOut(); session.invalidateAndCancel() }
            model.loggedIn = true
            _ = try await model.establishSession()
            window.selectFromList(42)
            try await waitForArticle(window, model)
            XCTAssertEqual(window.html, expected)
            XCTAssertEqual(window.offlineCopy, offline)
            XCTAssertEqual(window.imageOrigin?.absoluteString, replies.first?.mode == "proxied" ? "https://content.test:8081" : nil)
            XCTAssertEqual(try cache.article(entryID: 42, generation: "g"), "<p>Saved article</p><img src='https://publisher.test/tracker'>")
            ContentHTTP.assertFinished()
        }
    }

    func testEventServerFailureUsesRetryNoteAndUnauthorizedSignsOut() async throws {
        for status in [500, 401] {
            let requested = expectation(description: "Event stream connected")
            let server = try EventFailureServer { requested.fulfill() }
            try await server.start()
            defer { server.stop() }
            let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
            addTeardownBlock { try FileManager.default.removeItem(at: directory) }
            let defaults = UserDefaults(suiteName: defaultsSuite)!
            defer { defaults.removePersistentDomain(forName: defaultsSuite) }
            let store = IdentityStore(defaults: defaults)
            let identity = try AccountIdentity(server: server.origin, userID: 1, principalID: 7)
            let cache = try Cache(identity: identity, directory: directory)
            let engine = SyncEngine(store: cache)
            try engine.bootstrap(Cursor(generation: "g", rev: 1))
            try engine.finishBootstrap()
            let session = ContentHTTP.install([
                .init(path: "/v1/devices/start", body: #"{"device_code":"device","user_code":"user","verification_uri":"urn:earnest:test","interval":1,"expires_in":30}"#, method: "POST"),
                .init(path: "/v1/devices/poll", body: #"{"access_token":"test-token","token_type":"Bearer"}"#, method: "POST"),
                .init(path: "/v1/me", body: #"{"user_id":1,"principal_id":7,"kind":"human","scopes":[]}"#),
                .init(path: "/v1/me", body: #"{"user_id":1,"principal_id":7,"kind":"human","scopes":[]}"#),
                .init(path: "/v1/mutes", body: "[]"),
                .init(path: "/v1/settings", body: #"{"reading_mode":"river","today_decay_days":2,"single_key_shortcuts":true,"theme":"system","measure":68,"font":"serif","feed_overrides":{},"rev":0}"#),
                .init(path: "/v1/changes", body: #"{"changes":[],"next":{"generation":"g","rev":1},"next_cursor":"g:1"}"#),
                .init(path: "/v1/changes", body: #"{"changes":[],"next":{"generation":"g","rev":1},"next_cursor":"g:1"}"#),
                .init(path: "/v1/entries", body: #"{"entries":[]}"#),
                .init(path: "/v1/tokens", body: "[]"),
            ])
            let api = try EarnestAPI(baseURL: server.origin, tokenStore: ContentTokens(), session: session)
            let model = AppModel(api: api, serverURL: server.origin, tokens: ContentTokens(), identities: store, cacheDirectory: directory)
            let window = WindowModel(app: model)
            defer { model.signOut(); session.invalidateAndCancel() }
            window.section = .later
            model.login()
            await fulfillment(of: [requested], timeout: 5)
            let syncDeadline = Date().addingTimeInterval(5)
            while (!ContentHTTP.finished || model.syncing) && Date() < syncDeadline { try await Task.sleep(for: .milliseconds(10)) }
            ContentHTTP.assertFinished()
            XCTAssertFalse(model.syncing)
            XCTAssertNil(model.error)
            server.respond(status: status)
            let deadline = Date().addingTimeInterval(5)
            while model.connectionNote == nil && model.error == nil && Date() < deadline { try await Task.sleep(for: .milliseconds(10)) }
            if status == 401 {
                XCTAssertFalse(model.loggedIn)
                XCTAssertEqual(model.error, "Your sign-in expired or was revoked. Sign in again.")
            } else {
                XCTAssertTrue(model.loggedIn)
                XCTAssertNil(model.error)
                XCTAssertEqual(model.connectionNote, "Live updates interrupted; retrying.")
            }
        }
    }

    /// A transient /v1/me failure attaches the remembered account's cache and
    /// the next attempt recovers, instead of killing the session.
    func testTransientMeFailureShowsCacheAndRecovers() async throws {
        let (directory, defaults, store, identity, _) = try offlineFixture()
        defer { defaults.removePersistentDomain(forName: defaultsSuite) }
        let session = ContentHTTP.install([
            .init(path: "/v1/me", status: 503, body: "{}"),
            .init(path: "/v1/me", body: #"{"user_id":1,"principal_id":7,"kind":"human","scopes":[]}"#),
        ])
        let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, serverURL: identity.server, identities: store, cacheDirectory: directory)
        let window = WindowModel(app: model)
        defer { model.signOut(); session.invalidateAndCancel() }
        model.loggedIn = true
        let offline = try await model.establishSession()
        XCTAssertFalse(offline)
        XCTAssertNil(model.error)
        XCTAssertEqual(window.rows.map(\.id), [42])
        XCTAssertEqual(model.connectionNote, "Offline. Showing what was last synced.")
        let recovered = try await model.establishSession()
        XCTAssertTrue(recovered)
        XCTAssertNil(model.error)
        XCTAssertTrue(model.loggedIn)
        ContentHTTP.assertFinished()
    }

    /// Permanent failures must still surface instead of retrying forever.
    func testUnauthorizedMeFailureStaysFatal() async throws {
        let (directory, defaults, store, identity, _) = try offlineFixture()
        defer { defaults.removePersistentDomain(forName: defaultsSuite) }
        let session = ContentHTTP.install([.init(path: "/v1/me", status: 401, body: "{}")])
        let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, serverURL: identity.server, identities: store, cacheDirectory: directory)
        defer { model.signOut(); session.invalidateAndCancel() }
        model.loggedIn = true
        do {
            _ = try await model.establishSession()
            XCTFail("A 401 from /v1/me must not resolve")
        } catch {
            XCTAssertTrue(ErrorPresentation.isUnauthorized(error))
            XCTAssertFalse(ErrorPresentation.isTransient(error))
        }
        ContentHTTP.assertFinished()
    }

    /// A snapshot blob that no longer decodes is a cold cache: the session
    /// still comes up and the next sync repopulates it.
    func testCorruptedCacheStartsColdAndSessionStillEstablishes() async throws {
        let (directory, defaults, store, identity, cache) = try offlineFixture()
        defer { defaults.removePersistentDomain(forName: defaultsSuite) }
        try await corruptStoredSnapshot(in: directory, identity: identity)
        let session = ContentHTTP.install([
            .init(path: "/v1/me", body: #"{"user_id":1,"principal_id":7,"kind":"human","scopes":[]}"#),
        ])
        let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, serverURL: identity.server, identities: store, cacheDirectory: directory)
        let window = WindowModel(app: model)
        defer { model.signOut(); session.invalidateAndCancel() }
        model.loggedIn = true
        let online = try await model.establishSession()
        XCTAssertTrue(online)
        XCTAssertNil(model.error)
        XCTAssertTrue(window.rows.isEmpty)
        XCTAssertEqual(try cache.load(), SyncSnapshot())
        XCTAssertNil(try cache.article(entryID: 42, generation: "g"))
        XCTAssertTrue(try cache.resource("briefs", generation: "g").isEmpty)
        ContentHTTP.assertFinished()
    }

    /// The same cold-cache rule applies to the offline resolution.
    func testCorruptedCacheStillAllowsOfflineSession() async throws {
        let (directory, defaults, store, identity, cache) = try offlineFixture()
        defer { defaults.removePersistentDomain(forName: defaultsSuite) }
        try await corruptStoredSnapshot(in: directory, identity: identity)
        let session = ContentHTTP.install([.init(path: "/v1/me", body: "", failure: .cannotConnectToHost)])
        let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, serverURL: identity.server, identities: store, cacheDirectory: directory)
        let window = WindowModel(app: model)
        defer { model.signOut(); session.invalidateAndCancel() }
        model.loggedIn = true
        let online = try await model.establishSession()
        XCTAssertFalse(online)
        XCTAssertNil(model.error)
        XCTAssertEqual(model.connectionNote, "Offline. Showing what was last synced.")
        XCTAssertTrue(window.rows.isEmpty)
        XCTAssertEqual(try cache.load(), SyncSnapshot())
        ContentHTTP.assertFinished()
    }

    /// A garbage database file is unrecoverable: it is quarantined, not
    /// deleted, and a fresh cache opens in its place. Older quarantine copies
    /// are removed.
    func testCorruptCacheFileIsQuarantinedAndReopened() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let garbage = Data("this is not a sqlite database".utf8)
        try garbage.write(to: directory.appendingPathComponent("cache.sqlite"))
        let staleQuarantine = directory.appendingPathComponent("cache.sqlite.corrupt-2000-01-01T00:00:00Z")
        try Data("older corrupt cache".utf8).write(to: staleQuarantine)
        let defaults = UserDefaults(suiteName: defaultsSuite)!
        defaults.removePersistentDomain(forName: defaultsSuite)
        defer { defaults.removePersistentDomain(forName: defaultsSuite) }
        let store = IdentityStore(defaults: defaults)
        let identity = try AccountIdentity(server: URL(string: "https://api.test")!, userID: 1, principalID: 7)
        let session = ContentHTTP.install([.init(path: "/v1/me", body: #"{"user_id":1,"principal_id":7,"kind":"human","scopes":[]}"#)])
        let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, serverURL: identity.server, identities: store, cacheDirectory: directory)
        defer { model.signOut(); session.invalidateAndCancel() }
        model.loggedIn = true
        let online = try await model.establishSession()
        XCTAssertTrue(online)
        XCTAssertNil(model.error)
        let files = try FileManager.default.contentsOfDirectory(atPath: directory.path)
        XCTAssertFalse(files.contains(staleQuarantine.lastPathComponent), "Older quarantine copies must be removed")
        let quarantine = try XCTUnwrap(files.first { $0.contains(".corrupt-") })
        XCTAssertTrue(quarantine.hasPrefix("cache.sqlite"))
        XCTAssertEqual(try Data(contentsOf: directory.appendingPathComponent(quarantine)), garbage)
        let reopened = try Cache(identity: identity, directory: directory)
        XCTAssertEqual(try reopened.load(), SyncSnapshot())
        var snapshot = SyncSnapshot()
        snapshot.cursor = Cursor(generation: "fresh", rev: 1)
        try reopened.save(snapshot)
        XCTAssertEqual(try reopened.load(), snapshot)
        ContentHTTP.assertFinished()
    }

    /// A non-corruption open failure must be rethrown without touching the
    /// cache path, so a transient problem cannot wipe every account's cache.
    func testNonCorruptionCacheFailureRethrowsAndLeavesCacheUntouched() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        // A directory where the database file belongs makes SQLite fail with
        // SQLITE_CANTOPEN, which is not corruption.
        let database = directory.appendingPathComponent("cache.sqlite")
        try FileManager.default.createDirectory(at: database, withIntermediateDirectories: false)
        let marker = database.appendingPathComponent("marker.txt")
        try Data("keep".utf8).write(to: marker)
        let defaults = UserDefaults(suiteName: defaultsSuite)!
        defaults.removePersistentDomain(forName: defaultsSuite)
        defer { defaults.removePersistentDomain(forName: defaultsSuite) }
        let store = IdentityStore(defaults: defaults)
        let identity = try AccountIdentity(server: URL(string: "https://api.test")!, userID: 1, principalID: 7)
        let session = ContentHTTP.install([.init(path: "/v1/me", body: #"{"user_id":1,"principal_id":7,"kind":"human","scopes":[]}"#)])
        let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, serverURL: identity.server, identities: store, cacheDirectory: directory)
        defer { model.signOut(); session.invalidateAndCancel() }
        model.loggedIn = true
        do {
            _ = try await model.establishSession()
            XCTFail("A directory at the cache path must not open a cache")
        } catch {
            XCTAssertFalse(Cache.isUnrecoverableOpenFailure(error), "The test premise must be a non-corruption failure")
        }
        XCTAssertTrue(FileManager.default.fileExists(atPath: database.path))
        XCTAssertEqual(try Data(contentsOf: marker), Data("keep".utf8))
        let files = try FileManager.default.contentsOfDirectory(atPath: directory.path)
        XCTAssertFalse(files.contains { $0.contains(".corrupt-") })
        ContentHTTP.assertFinished()
    }

    private func corruptStoredSnapshot(in directory: URL, identity: AccountIdentity) async throws {
        let db = try DatabaseQueue(path: directory.appendingPathComponent("cache.sqlite").path)
        try await db.write { db in
            try db.execute(sql: "UPDATE snapshots SET value = x'00ff' WHERE key = ?", arguments: [identity.cacheKey])
        }
    }

    func testListAndKeyboardSelectionsUseProxiedImages() async throws {
        let (model, window, session) = try makeModel([
            .signed(mode: "proxied"), .article,
            .signed(mode: "proxied"), .article,
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        window.selectFromList(42)
        try await waitForArticle(window, model)
        XCTAssertEqual(window.imageOrigin?.absoluteString, "https://content.test:8081")
        XCTAssertEqual(window.readingEntry?.title, "Article title")
        window.rows = [EntryRow(id: 42, title: "Article title", subtitle: "", unread: true)]
        window.move(1)
        try await waitForArticle(window, model)
        XCTAssertEqual(window.imageMode, .proxied)
        ContentHTTP.assertFinished()
    }

    func testDeepLinkBlocksImagesUntilExplicitLoadIncludingSameEntry() async throws {
        let (model, window, session) = try makeModel([
            .signed(mode: "proxied"), .article,
            .signed(mode: "click"), .article,
            .signed(mode: "proxied"), .article,
            .signed(mode: "click"), .article,
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        window.selectFromList(42)
        try await waitForArticle(window, model)
        window.openDeepLink(URL(string: "earnest://entry/42")!)
        XCTAssertNil(window.imageOrigin)
        try await waitForArticle(window, model)
        XCTAssertEqual(window.imageMode, .click)
        XCTAssertNil(window.imageOrigin)
        window.loadImages()
        try await waitForArticle(window, model)
        XCTAssertEqual(window.imageOrigin?.absoluteString, "https://content.test:8081")
        window.openDeepLink(URL(string: "earnest://entry/42")!)
        try await waitForArticle(window, model)
        XCTAssertNil(window.imageOrigin)
        ContentHTTP.assertFinished()
    }

    func testFallbackBlocksOriginalImageURLs() async throws {
        let (model, window, session) = try makeModel([
            .init(path: "/v1/entries/42/content-url", status: 503, body: "{}", mode: "proxied"),
            .init(path: "/v1/entries/42/content", body: #"{"entry_id":42,"html":"<img src='https://publisher.test/tracker'>","text":"","sanitizer_version":1,"source":"feed"}"#),
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        window.selectFromList(42)
        try await waitForArticle(window, model)
        XCTAssertTrue(window.html.contains("publisher.test"))
        XCTAssertNil(window.imageOrigin)
        let scheme = ArticleScheme()
        scheme.html = window.html
        scheme.imageOrigin = window.imageOrigin
        XCTAssertTrue(scheme.page.contains("img-src 'none';"))
        ContentHTTP.assertFinished()
    }

    func testConnectionFailuresUseNoteInsteadOfModal() async throws {
        let (model, window, session) = try makeModel([
            .init(path: "/v1/entries/42/content-url", body: "", failure: .cannotConnectToHost),
            .init(path: "/v1/entries/42/content", body: "", failure: .cannotConnectToHost),
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        window.selectFromList(42)
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
        let window = WindowModel(app: model)
        defer { model.signOut(); session.invalidateAndCancel() }
        model.loggedIn = true
        let online = try await model.establishSession()
        XCTAssertFalse(online)
        XCTAssertNil(model.error)
        XCTAssertEqual(window.rows.map(\.id), [42])
        XCTAssertEqual(model.briefs.map(\.text), ["Cached brief"])
        XCTAssertEqual(model.feeds.map(\.title), ["Cached feed"])
        XCTAssertNotNil(model.connectionNote)
        window.selectFromList(42)
        try await waitForArticle(window, model)
        XCTAssertEqual(window.html, "<p>Saved article</p><img src='https://publisher.test/tracker'>")
        XCTAssertTrue(window.offlineCopy)
        XCTAssertNil(window.imageOrigin)
        XCTAssertEqual(window.readingEntry?.title, "Article title")
        let scheme = ArticleScheme()
        scheme.html = window.html
        scheme.imageOrigin = window.imageOrigin
        XCTAssertTrue(scheme.page.contains("img-src 'none';"))
        window.command(.keep)
        let deadline = Date().addingTimeInterval(5)
        while !ContentHTTP.finished && Date() < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertNil(model.error)
        XCTAssertEqual(try cache.load().outbox.map(\.register), [.seen, .kept])
        XCTAssertEqual(model.pendingOps, 2)
        ContentHTTP.assertFinished()
    }

    func testAuthenticatedBodyNotFoundDiscardsSavedCopyAndMarksGone() async throws {
        // Both a problem response and a plain HTTP status are authoritative.
        for body in ["{}", #"{"type":"about:blank","title":"Not Found","status":404}"#] {
            try await assertSavedCopyRejected(status: 404, body: body)
        }
    }

    func testServerFailureDoesNotUseSavedCopyOrMarkGone() async throws {
        try await assertSavedCopyRejected(status: 503, body: "{}")
    }

    private func assertSavedCopyRejected(status: Int, body: String) async throws {
        let (directory, defaults, store, identity, cache) = try offlineFixture()
        defer { defaults.removePersistentDomain(forName: defaultsSuite) }
        let session = ContentHTTP.install([
            .init(path: "/v1/me", body: #"{"user_id":1,"principal_id":7,"kind":"human","scopes":[]}"#),
            .init(path: "/v1/entries/42/content-url", status: status, body: body),
            .init(path: "/v1/entries/42/content", status: status, body: body),
        ])
        let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, serverURL: identity.server, identities: store, cacheDirectory: directory)
        let window = WindowModel(app: model)
        defer { model.signOut(); session.invalidateAndCancel() }
        model.loggedIn = true
        let online = try await model.establishSession()
        XCTAssertTrue(online)
        XCTAssertNotNil(try cache.article(entryID: 42, generation: "g"))
        try cache.saveArticle(entryID: 43, html: "Other article", generation: "g")
        window.selectFromList(42)
        let deadline = Date().addingTimeInterval(5)
        while model.error == nil && window.html.isEmpty && Date() < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertNotNil(model.error)
        XCTAssertTrue(window.html.isEmpty)
        XCTAssertFalse(window.offlineCopy)
        XCTAssertFalse(window.articleUnavailable)
        let entry = try XCTUnwrap(cache.load().entries.first { $0.id == 42 })
        XCTAssertEqual(entry.deleted, status == 404)
        if status == 404 {
            XCTAssertNil(try cache.article(entryID: 42, generation: "g"))
            XCTAssertFalse(window.rows.contains { $0.id == 42 })
        } else {
            XCTAssertNotNil(try cache.article(entryID: 42, generation: "g"))
        }
        XCTAssertEqual(try cache.article(entryID: 43, generation: "g"), "Other article")
        XCTAssertTrue(try cache.load().outbox.isEmpty)
        ContentHTTP.assertFinished()
    }

    func testColdLaunchRejectsImportedBacklogAndRestoresBriefAuthor() async throws {
        let (directory, defaults, store, identity, cache) = try offlineFixture()
        defer { defaults.removePersistentDomain(forName: defaultsSuite) }
        let engine = SyncEngine(store: cache)
        let subscription: APIValue = .object([
            "id": .integer(1), "rev": .integer(2), "cadence": .string("now"), "title": .string("Cached feed"),
            "initial_since": .string("2026-11-01T00:00:00Z"), "initial_through_seq": .integer(100),
        ])
        try engine.mergeBootstrap(kind: "subscriptions", items: [subscription])
        try engine.mergeEntry(SyncEntry(id: 43, subscriptionID: 1, seq: 4, title: "Backlog", url: "https://news.test/old",
                                       publishedAt: DateFormatting.parse("2006-01-01T00:00:00Z"), firstSeenAt: Date()))
        try cache.saveResource("principal_names", values: [.object(["id": .integer(2), "name": .string("Morning Agent")])], generation: "g")
        let session = ContentHTTP.install([.init(path: "/v1/me", body: "", failure: .cannotConnectToHost)])
        let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, serverURL: identity.server, identities: store, cacheDirectory: directory)
        let window = WindowModel(app: model)
        defer { model.signOut(); session.invalidateAndCancel() }
        model.loggedIn = true
        _ = try await model.establishSession()
        XCTAssertEqual(window.rows.map(\.id), [42])
        let brief = try XCTUnwrap(model.briefs.first)
        XCTAssertEqual(model.principalNames[brief.principalID], "Morning Agent")
        ContentHTTP.assertFinished()
    }

    /// The server's river reads oldest first; the offline list must match it.
    func testColdLaunchListsCachedRiverInSequenceOrder() async throws {
        let (directory, defaults, store, identity, cache) = try offlineFixture()
        defer { defaults.removePersistentDomain(forName: defaultsSuite) }
        let engine = SyncEngine(store: cache)
        try engine.mergeEntry(SyncEntry(id: 44, subscriptionID: 1, seq: 5, title: "Later arrival", url: "https://news.test/new",
                                       publishedAt: nil, firstSeenAt: Date()))
        let session = ContentHTTP.install([.init(path: "/v1/me", body: "", failure: .cannotConnectToHost)])
        let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, serverURL: identity.server, identities: store, cacheDirectory: directory)
        let window = WindowModel(app: model)
        defer { model.signOut(); session.invalidateAndCancel() }
        model.loggedIn = true
        _ = try await model.establishSession()
        XCTAssertEqual(window.rows.map(\.id), [42, 44])
        ContentHTTP.assertFinished()
    }

    func testOnlineRefreshDoesNotRelaxAnotherWindowsCachedRiver() async throws {
        let (directory, defaults, store, identity, cache) = try offlineFixture()
        defer { defaults.removePersistentDomain(forName: defaultsSuite) }
        let engine = SyncEngine(store: cache)
        try engine.mergeBootstrap(kind: "subscriptions", items: [.object([
            "id": .integer(1), "rev": .integer(2), "title": .string("Cached feed"), "cadence": .string("now"),
            "initial_since": .string("2026-01-01T00:00:00Z"), "initial_through_seq": .integer(100),
        ])])
        try engine.mergeEntry(SyncEntry(id: 43, subscriptionID: 1, seq: 4, title: "Imported backlog",
                                       publishedAt: DateFormatting.parse("2006-01-01T00:00:00Z"), firstSeenAt: Date()))
        let session = ContentHTTP.install([
            .init(path: "/v1/me", body: "", failure: .cannotConnectToHost),
            .init(path: "/v1/entries", body: #"{"entries":[]}"#),
        ])
        let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, serverURL: identity.server, identities: store, cacheDirectory: directory)
        let today = WindowModel(app: model)
        let later = WindowModel(app: model)
        later.section = .later
        defer { model.signOut(); session.invalidateAndCancel() }
        model.loggedIn = true
        _ = try await model.establishSession()
        XCTAssertEqual(today.rows.map(\.id), [42])

        await later.refreshEntries()
        model.rebuildPresentation()

        XCTAssertEqual(today.rows.map(\.id), [42], "Another window's online fetch must not admit imported backlog")
        ContentHTTP.assertFinished()
    }

    /// An article never opened before has no offline copy; the reader must say
    /// so instead of staying blank.
    func testOfflineArticleWithoutSavedCopyIsMarkedUnavailable() async throws {
        let (directory, defaults, store, identity, cache) = try offlineFixture()
        defer { defaults.removePersistentDomain(forName: defaultsSuite) }
        let engine = SyncEngine(store: cache)
        try engine.mergeEntry(SyncEntry(id: 44, subscriptionID: 1, seq: 5, title: "Never opened", url: "https://news.test/new",
                                       publishedAt: nil, firstSeenAt: Date()))
        let session = ContentHTTP.install([
            .init(path: "/v1/me", body: "", failure: .cannotConnectToHost),
            .init(path: "/v1/entries/44/content-url", body: "", failure: .notConnectedToInternet),
            .init(path: "/v1/entries/44/content", body: "", failure: .notConnectedToInternet),
        ])
        let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, serverURL: identity.server, identities: store, cacheDirectory: directory)
        let window = WindowModel(app: model)
        defer { model.signOut(); session.invalidateAndCancel() }
        model.loggedIn = true
        _ = try await model.establishSession()
        window.selectFromList(44)
        let deadline = Date().addingTimeInterval(5)
        while !window.articleUnavailable && Date() < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertTrue(window.articleUnavailable)
        XCTAssertTrue(window.html.isEmpty)
        XCTAssertNil(model.error)
        ContentHTTP.assertFinished()
    }

    func testCachedMissingPublisherDateFallsBackToFirstSeen() throws {
        let engine = SyncEngine(store: MemorySyncStore())
        let firstSeen = try XCTUnwrap(DateFormatting.parse("2026-11-04T08:00:00Z"))
        let entry = SyncEntry(id: 42, firstSeenAt: firstSeen)
        let cached = try cachedEntry(entry, principal: 7, engine: engine)
        XCTAssertNil(cached.publishedAt)
        XCTAssertEqual(cached.publishedAt.flatMap(DateFormatting.parse) ?? DateFormatting.parse(cached.firstSeenAt), firstSeen)
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
        XCTAssertTrue(try model.currentEntries().isEmpty)
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
        XCTAssertEqual(try model.currentEntries().map(\.id), [42])
        XCTAssertEqual(model.pendingOps, 1)
        _ = try await model.establishSession()
        let deadline = Date().addingTimeInterval(5)
        while (!ContentHTTP.finished || model.connectionNote == nil) && Date() < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertTrue(model.loggedIn)
        XCTAssertNil(model.error)
        XCTAssertTrue(try model.currentEntries().isEmpty)
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
        let window = WindowModel(app: model)
        defer { model.signOut(); session.invalidateAndCancel() }
        model.loggedIn = true
        _ = try await model.establishSession()
        model.scheduleSync()
        let deadline = Date().addingTimeInterval(5)
        while model.loggedIn && Date() < deadline { try await Task.sleep(for: .milliseconds(10)) }
        XCTAssertFalse(model.loggedIn)
        XCTAssertTrue(window.rows.isEmpty)
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
            let window = WindowModel(app: model)
            defer { model.signOut(); session.invalidateAndCancel() }
            model.loggedIn = true
            _ = try await model.establishSession()
            window.selectFromList(42)
            try await waitForArticle(window, model)
            XCTAssertEqual(window.html, expected)
            XCTAssertFalse(window.offlineCopy)
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
            .init(path: "/v1/mutes", body: "[]"),
            .init(path: "/v1/settings", body: #"{"reading_mode":"river","today_decay_days":2,"single_key_shortcuts":true,"theme":"system","measure":68,"font":"serif","feed_overrides":{},"rev":0}"#),
            .init(path: "/v1/changes", body: #"{"changes":[],"next":{"generation":"g","rev":1},"next_cursor":"g:1"}"#),
            .init(path: "/v1/state:batch", body: "[\(acknowledgement)]", method: "POST"),
            .init(path: "/v1/changes", body: "{\"changes\":[{\"rev\":2,\"type\":\"state.updated\",\"payload\":{\"results\":[\(acknowledgement)]}}],\"next\":{\"generation\":\"g\",\"rev\":2},\"next_cursor\":\"g:2\"}"),
            .init(path: "/v1/changes", body: #"{"changes":[],"next":{"generation":"g","rev":2},"next_cursor":"g:2"}"#),
            .init(path: "/v1/entries", body: #"{"entries":[]}"#),
            .init(path: "/v1/tokens", body: #"[{"id":2,"principal_id":2,"kind":"agent","name":"Morning Agent","audience":"api","scopes":[],"daily_budget":100,"created_at":"2026-11-04T00:00:00Z"}]"#),
        ])
        let api = try EarnestAPI(baseURL: identity.server, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, serverURL: identity.server, identities: store, cacheDirectory: directory)
        let window = WindowModel(app: model)
        defer { model.signOut(); session.invalidateAndCancel() }
        model.loggedIn = true
        _ = try await model.establishSession()
        XCTAssertNotNil(model.connectionNote)
        window.section = .later
        model.scheduleSync()
        let deadline = Date().addingTimeInterval(5)
        while (!ContentHTTP.finished || model.syncing || model.connectionNote != nil || model.principalNames[2] == nil) && Date() < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertNil(model.error)
        XCTAssertNil(model.connectionNote)
        XCTAssertTrue(try cache.load().outbox.isEmpty)
        XCTAssertEqual(model.principalNames[2], "Morning Agent")
        XCTAssertEqual(try cache.resource("principal_names", generation: "g").first?["name"]?.stringValue, "Morning Agent")
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
        try engine.mergeBootstrap(kind: "subscriptions", items: [.object([
            "id": .integer(1), "title": .string("Cached feed"), "cadence": .string("now"),
            "initial_since": .string("2026-01-01T00:00:00Z"), "initial_through_seq": .integer(0),
        ])])
        try engine.finishBootstrap()
        try cache.saveResource("feeds", values: [.object(["id": .integer(1), "title": .string("Cached feed")])], generation: "g")
        try cache.saveResource("briefs", values: [.object(["id": .integer(9), "principal_id": .integer(2), "value": .string("Cached brief")])], generation: "g")
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
        let (model, window, session) = try makeModel([
            .init(path: "/v1/notifications/19:cancel", body: "{}", method: "POST"),
            .init(path: "/v1/activity", body: #"{"activities":[]}"#),
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        let item = ActivityItem(.object([
            "id": .integer(8), "action": .string("notification.created"),
            "target": .object(["id": .integer(19)]), "next_action": .string("cancel")
        ]))
        window.section = .activity
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

    private func waitForArticle(_ window: WindowModel, _ model: AppModel) async throws {
        let deadline = Date().addingTimeInterval(5)
        while window.html.isEmpty && model.error == nil && Date() < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertNil(model.error)
        XCTAssertFalse(window.html.isEmpty, "Article did not finish loading")
    }

    private func makeModel(_ replies: [ContentHTTP.Reply]) throws -> (AppModel, WindowModel, URLSession) {
        let entry = try JSONDecoder().decode(APIEntry.self, from: Data(Self.entryJSON.utf8))
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g", rev: 1))
        try engine.mergeEntries([entry], generation: "g")
        try engine.mergeBootstrap(kind: "subscriptions", items: [.object([
            "id": .integer(1), "cadence": .string("now"),
            "initial_since": .null, "initial_through_seq": .integer(0),
        ])])
        try engine.finishBootstrap()
        let session = ContentHTTP.install(replies)
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, engine: engine, principalID: 7)
        let window = WindowModel(app: model)
        model.loggedIn = true
        window.entries = [entry]
        return (model, window, session)
    }

    static let entryJSON = #"{"id":42,"subscription_id":1,"feed_id":2,"seq":3,"title":"Article title","url":"https://publisher.test/article","author":"Ada","first_seen_at":"2026-11-04T08:00:00Z","rev":1,"deleted":false,"state":{"principal":7,"entry_id":42,"state_rev":0,"seen":{"value":"unseen","rev":0,"occurred_at":"","actor":0,"source":""},"shelf":{"value":"none","rev":0,"occurred_at":"","actor":0,"source":""},"kept":{"value":false,"rev":0,"occurred_at":"","actor":0,"source":""}}}"#
}

struct ContentTokens: TokenStore {
    func load() throws -> String? { "test-token" }
    func save(_ token: String) throws {}
    func delete() throws {}
}

final class ContentHTTP: URLProtocol {
    struct Reply {
        let path: String
        var status = 200
        let body: String
        var mode: String?
        var method = "GET"
        var failure: URLError.Code?
        var beforeResponse: (@Sendable () -> Void)?
        var query: String?

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
        if let query = reply.query { XCTAssertEqual(request.url?.query, query) }
        if let failure = reply.failure {
            client?.urlProtocol(self, didFailWithError: URLError(failure))
            return
        }
        let isContent = request.url?.host == "content.test"
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), isContent || reply.path.hasPrefix("/v1/devices/") ? nil : "Bearer test-token")
        reply.beforeResponse?()
        let response = HTTPURLResponse(url: request.url!, statusCode: reply.status, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(reply.body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

/// Holds the hint response until ordinary sync completes, so error routing is deterministic.
private final class EventFailureServer: @unchecked Sendable {
    private let listener: NWListener
    private let queue = DispatchQueue(label: "EarnestEventFailureTest")
    private let lock = NSLock()
    private var connection: NWConnection?

    init(onRequest: @escaping @Sendable () -> Void) throws {
        listener = try NWListener(using: .tcp, on: .any)
        listener.newConnectionHandler = { [weak self] connection in
            guard let self else { connection.cancel(); return }
            self.lock.withLock { self.connection = connection }
            connection.start(queue: self.queue)
            connection.receive(minimumIncompleteLength: 1, maximumLength: 65536) { data, _, _, _ in
                guard let data else { connection.cancel(); return }
                XCTAssertTrue(String(decoding: data, as: UTF8.self).contains("/v1/events"), String(decoding: data, as: UTF8.self))
                onRequest()
            }
        }
    }

    var origin: URL { URL(string: "http://localhost:\(listener.port!.rawValue)")! }

    func start() async throws {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            listener.stateUpdateHandler = { state in
                switch state {
                case .ready: continuation.resume()
                case .failed(let error): continuation.resume(throwing: error)
                default: break
                }
            }
            listener.start(queue: queue)
        }
    }

    func respond(status: Int) {
        guard let connection = lock.withLock({ connection }) else { XCTFail("No event request"); return }
        let response = "HTTP/1.1 \(status) Test\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
        connection.send(content: Data(response.utf8), completion: .contentProcessed { _ in connection.cancel() })
    }

    func stop() {
        lock.withLock { connection?.cancel() }
        listener.cancel()
    }
}

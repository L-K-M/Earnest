import Foundation
import XCTest
import EarnestKit
@testable import Earnest

/// Reader preferences: explicit save and stable retries, account/restore
/// isolation, override inheritance, safe style application, the shortcut
/// toggle, and counts gating and failures. No GUI is launched.
@MainActor final class SettingsTests: XCTestCase {
    // MARK: - Scripted HTTP

    final class SettingsHTTP: URLProtocol {
        struct Reply {
            var method = "GET"
            let path: String
            var status = 200
            let body: String
            var failure: URLError.Code?
            var beforeResponse: (() -> Void)?
        }
        private static let lock = NSLock()
        private static var replies: [Reply] = []
        private static var bodies: [Data] = []

        static func install(_ replies: [Reply]) -> URLSession {
            lock.withLock { Self.replies = replies; Self.bodies = [] }
            let configuration = URLSessionConfiguration.ephemeral
            configuration.protocolClasses = [Self.self]
            return URLSession(configuration: configuration)
        }
        static func body(at index: Int) -> Data? {
            lock.withLock { bodies.indices.contains(index) ? bodies[index] : nil }
        }
        static var pending: Int { lock.withLock { replies.count } }
        static func assertFinished() { XCTAssertTrue(lock.withLock { replies.isEmpty }) }

        override class func canInit(with request: URLRequest) -> Bool { true }
        override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
        override func startLoading() {
            let reply = Self.lock.withLock { Self.replies.isEmpty ? nil : Self.replies.removeFirst() }
            guard let reply else {
                XCTFail("Unexpected request: \(request.url?.path ?? "")")
                client?.urlProtocol(self, didFailWithError: URLError(.badServerResponse))
                return
            }
            XCTAssertEqual(request.url?.path, reply.path)
            XCTAssertEqual(request.httpMethod, reply.method)
            if let body = Self.body(of: request) {
                Self.lock.withLock { Self.bodies.append(body) }
            }
            reply.beforeResponse?()
            if let failure = reply.failure {
                client?.urlProtocol(self, didFailWithError: URLError(failure))
                return
            }
            let response = HTTPURLResponse(url: request.url!, statusCode: reply.status,
                                           httpVersion: "HTTP/1.1",
                                           headerFields: ["Content-Type": "application/json"])!
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: Data(reply.body.utf8))
            client?.urlProtocolDidFinishLoading(self)
        }
        override func stopLoading() {}

        private static func body(of request: URLRequest) -> Data? {
            if let body = request.httpBody { return body }
            guard let stream = request.httpBodyStream else { return nil }
            stream.open()
            defer { stream.close() }
            var data = Data()
            var buffer = [UInt8](repeating: 0, count: 4096)
            while true {
                let count = stream.read(&buffer, maxLength: buffer.count)
                if count <= 0 { break }
                data.append(buffer, count: count)
            }
            return data
        }
    }

    private final class SettingsTokens: TokenStore, @unchecked Sendable {
        func load() throws -> String? { "test-token" }
        func save(_ token: String) throws {}
        func delete() throws {}
    }

    private func makeModel(api: EarnestAPI? = nil, engine: SyncEngine? = nil) -> AppModel {
        let engine = engine ?? SyncEngine(store: MemorySyncStore())
        do {
            if try engine.snapshot().cursor == nil { try engine.bootstrap(Cursor(generation: "g1", rev: 0)) }
            if try engine.readerSettings() == nil {
                let generation = try XCTUnwrap(engine.snapshot().cursor?.generation)
                try engine.mergeSettings(APIReaderSettings(), generation: generation)
            }
        } catch { XCTFail("Could not prepare settings fixture: \(error)") }
        let model = AppModel(api: api, engine: engine, principalID: 7)
        do { try model.restoreReaderPreferences() }
        catch { XCTFail("Could not restore settings fixture: \(error)") }
        return model
    }

    private func makeAPI(_ replies: [SettingsHTTP.Reply]) throws -> (EarnestAPI, URLSession) {
        let session = SettingsHTTP.install(replies)
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!,
                                 tokenStore: SettingsTokens(), session: session)
        return (api, session)
    }

    private func waitUntil(file: StaticString = #filePath, line: UInt = #line,
                           _ condition: @escaping () -> Bool) async throws {
        let deadline = Date().addingTimeInterval(5)
        while !condition() && Date() < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertTrue(condition(), "Timed out waiting for settings state", file: file, line: line)
    }

    // MARK: - Explicit save and stable retries

    func testExplicitSaveRetriesReuseTheSameBodyAndOpID() async throws {
        let (api, session) = try makeAPI([
            .init(method: "PUT", path: "/v1/settings", status: 500,
                  body: #"{"type":"about:blank","title":"Server","status":500,"error":"boom"}"#),
            .init(method: "PUT", path: "/v1/settings",
                  body: #"{"reading_mode":"unread_counts","today_decay_days":4,"single_key_shortcuts":false,"theme":"dark","measure":68,"font":"sans","feed_overrides":{},"rev":9}"#),
            .init(path: "/v1/entries:counts", body: #"{"by_subscription":{"1":2},"later":1,"kept":0,"total":3}"#),
        ])
        defer { session.invalidateAndCancel() }
        let engine = SyncEngine(store: MemorySyncStore())
        let model = makeModel(api: api, engine: engine)
        model.loggedIn = true
        let settings = ReaderSettingsModel(app: model)
        settings.draft = APIReaderSettings(readingMode: .unreadCounts, todayDecayDays: 4,
                                           singleKeyShortcuts: false, theme: .dark,
                                           measure: 68, font: .sans, rev: 3)

        settings.save()
        try await waitUntil { !settings.busy }
        XCTAssertNotNil(settings.error)
        XCTAssertFalse(settings.confirmed)
        let first = try XCTUnwrap(SettingsHTTP.body(at: 0))
        let firstFields = try JSONDecoder().decode(APIValue.self, from: first)
        let firstOp = try XCTUnwrap(firstFields["op_id"]?.stringValue)

        settings.save()
        try await waitUntil { !settings.busy }
        XCTAssertTrue(settings.confirmed)
        XCTAssertNil(settings.error)
        let second = try XCTUnwrap(SettingsHTTP.body(at: 1))
        XCTAssertEqual(second, first, "retry must freeze the full body and opID")
        let secondFields = try JSONDecoder().decode(APIValue.self, from: second)
        XCTAssertEqual(secondFields["op_id"]?.stringValue, firstOp)
        XCTAssertEqual(model.readerSettings.rev, 9)
        XCTAssertEqual(try engine.readerSettings()?.rev, 9)
        try await waitUntil { model.unreadCounts != nil }
        XCTAssertEqual(model.unreadCounts?.total, 3)
        SettingsHTTP.assertFinished()
    }

    func testEditingAfterAFailedSaveMintsANewOpID() async throws {
        let (api, session) = try makeAPI([
            .init(method: "PUT", path: "/v1/settings", status: 500,
                  body: #"{"type":"about:blank","title":"Server","status":500,"error":"boom"}"#),
            .init(method: "PUT", path: "/v1/settings",
                  body: #"{"reading_mode":"river","today_decay_days":2,"single_key_shortcuts":true,"theme":"system","measure":66,"font":"serif","feed_overrides":{},"rev":2}"#),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = ReaderSettingsModel(app: model)
        settings.draft = APIReaderSettings(theme: .dark, rev: 1)

        settings.save()
        try await waitUntil { !settings.busy }
        XCTAssertNotNil(settings.error)
        let firstOp = try XCTUnwrap(SettingsHTTP.body(at: 0)).opID()

        settings.draft.theme = .eink
        settings.save()
        try await waitUntil { !settings.busy }
        XCTAssertTrue(settings.confirmed)
        let secondOp = try XCTUnwrap(SettingsHTTP.body(at: 1)).opID()
        XCTAssertNotEqual(firstOp, secondOp)
        SettingsHTTP.assertFinished()
    }

    func testEndSessionDropsFrozenSaveAndColdLaunchRestoresAccountSettings() async throws {
        let (api, session) = try makeAPI([
            .init(method: "PUT", path: "/v1/settings", status: 503,
                  body: #"{"type":"about:blank","title":"Unavailable","status":503,"error":"down"}"#),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = ReaderSettingsModel(app: model)
        settings.draft = APIReaderSettings(theme: .black, rev: 2)

        settings.save()
        try await waitUntil { !settings.busy }
        XCTAssertNotNil(settings.error)
        XCTAssertEqual(SettingsHTTP.pending, 0)

        settings.endSession()
        XCTAssertFalse(settings.isDirty)
        XCTAssertEqual(settings.draft.rev, model.readerSettings.rev)
        try await Task.sleep(for: .milliseconds(50))
        XCTAssertEqual(SettingsHTTP.pending, 0, "a frozen request must never replay")

        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g", rev: 1))
        try engine.mergeSettings(APIReaderSettings(theme: .eink, measure: 70, font: .dyslexic, rev: 4), generation: "g")
        let restored = makeModel(api: nil, engine: engine)
        try restored.restoreReaderPreferences()
        XCTAssertEqual(restored.readerSettings.theme, .eink)
        XCTAssertEqual(restored.readerSettings.measure, 70)
        XCTAssertEqual(restored.readerSettings.rev, 4)
    }

    private final class ObservingSettingsStore: SyncStore {
        enum Failure: Error { case diskFull }
        let memory = MemorySyncStore()
        var failSaves = false
        var onSave: ((SyncSnapshot) -> Void)?
        func load() throws -> SyncSnapshot { try memory.load() }
        func save(_ snapshot: SyncSnapshot) throws {
            if failSaves { throw Failure.diskFull }
            onSave?(snapshot)
            try memory.save(snapshot)
        }
    }

    private func settingsJSON(theme: String = "dark", rev: Int64) -> String {
        #"{"reading_mode":"river","today_decay_days":2,"single_key_shortcuts":true,"theme":"\#(theme)","measure":66,"font":"serif","feed_overrides":{},"rev":\#(rev)}"#
    }

    func testUnauthorizedSaveSignsOutTheOwningSession() async throws {
        let (api, session) = try makeAPI([
            .init(method: "PUT", path: "/v1/settings", status: 401,
                  body: #"{"error":"unauthorized"}"#),
        ])
        defer { session.invalidateAndCancel() }
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g1", rev: 0))
        try engine.mergeSettings(APIReaderSettings(), generation: "g1")
        let app = makeModel(api: api, engine: engine)
        app.loggedIn = true
        try app.restoreReaderPreferences()
        let window = WindowModel(app: app)
        window.settings.draft.theme = .dark
        window.settings.save()
        try await waitUntil { !window.settings.busy }
        XCTAssertFalse(app.loggedIn)
        XCTAssertFalse(window.settings.confirmed)
        XCTAssertFalse(window.settings.isDirty)
        SettingsHTTP.assertFinished()
    }

    func testRestoreAdoptsLowerRevisionAndClearsDraft() throws {
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g1", rev: 0))
        try engine.mergeSettings(APIReaderSettings(theme: .dark, rev: 100), generation: "g1")
        let app = makeModel(engine: engine)
        try app.restoreReaderPreferences()
        let window = WindowModel(app: app)
        window.settings.draft.theme = .black
        try engine.bootstrap(Cursor(generation: "g2", rev: 0))
        try engine.mergeSettings(APIReaderSettings(theme: .light, rev: 2), generation: "g2")
        try app.restoreReaderPreferences()
        app.rebuildPresentation()
        XCTAssertEqual(app.readerSettings.rev, 2)
        XCTAssertEqual(app.readerSettings.theme, .light)
        XCTAssertEqual(window.settings.draft.theme, .light)
        XCTAssertFalse(window.settings.isDirty)
        XCTAssertFalse(window.settings.conflict)
    }

    func testPreRestoreSaveCannotPoisonTheNewSnapshot() async throws {
        let requested = expectation(description: "Old-generation save started")
        let resume = DispatchSemaphore(value: 0)
        let (api, session) = try makeAPI([
            .init(method: "PUT", path: "/v1/settings", body: settingsJSON(rev: 102), beforeResponse: {
                requested.fulfill()
                XCTAssertEqual(resume.wait(timeout: .now() + 10), .success)
            }),
        ])
        let store = ObservingSettingsStore()
        let engine = SyncEngine(store: store)
        try engine.bootstrap(Cursor(generation: "g1", rev: 0))
        try engine.mergeSettings(APIReaderSettings(rev: 3), generation: "g1")
        let app = makeModel(api: api, engine: engine)
        app.loggedIn = true
        try app.restoreReaderPreferences()
        let window = WindowModel(app: app)
        defer { resume.signal(); app.signOut(); session.invalidateAndCancel() }
        window.settings.draft.theme = .dark
        window.settings.save()
        await fulfillment(of: [requested], timeout: 5)
        try engine.bootstrap(Cursor(generation: "g2", rev: 0))
        try engine.mergeSettings(APIReaderSettings(theme: .light, rev: 2), generation: "g2")
        try app.restoreReaderPreferences()
        app.rebuildPresentation()
        let forbidden = expectation(description: "Old acknowledgement must not enter restored snapshot")
        forbidden.isInverted = true
        store.onSave = { snapshot in
            if snapshot.objects.contains(where: { $0.kind == .settings && $0.rev == 102 }) { forbidden.fulfill() }
        }
        resume.signal()
        await fulfillment(of: [forbidden], timeout: 0.2)
        XCTAssertEqual(try engine.readerSettings()?.rev, 2)
        XCTAssertEqual(app.readerSettings.rev, 2)
        XCTAssertFalse(window.settings.confirmed)
    }

    func testCacheWriteFailureDoesNotConfirmAndRetriesTheSameOperation() async throws {
        let (api, session) = try makeAPI([
            .init(method: "PUT", path: "/v1/settings", body: settingsJSON(rev: 9)),
            .init(method: "PUT", path: "/v1/settings", body: settingsJSON(rev: 9)),
        ])
        defer { session.invalidateAndCancel() }
        let store = ObservingSettingsStore()
        let engine = SyncEngine(store: store)
        try engine.bootstrap(Cursor(generation: "g1", rev: 0))
        try engine.mergeSettings(APIReaderSettings(), generation: "g1")
        let app = makeModel(api: api, engine: engine)
        app.loggedIn = true
        try app.restoreReaderPreferences()
        let settings = ReaderSettingsModel(app: app)
        settings.draft.theme = .dark
        store.failSaves = true
        settings.save()
        try await waitUntil { !settings.busy }
        XCTAssertFalse(settings.confirmed)
        XCTAssertNotNil(settings.error)
        XCTAssertEqual(app.readerSettings.rev, 0)
        XCTAssertEqual(try engine.readerSettings()?.rev, 0)
        let first = try XCTUnwrap(SettingsHTTP.body(at: 0))
        store.failSaves = false
        settings.save()
        try await waitUntil { !settings.busy }
        XCTAssertTrue(settings.confirmed)
        XCTAssertEqual(try XCTUnwrap(SettingsHTTP.body(at: 1)).opID(), try first.opID())
        XCTAssertEqual(try engine.readerSettings()?.rev, 9)
        SettingsHTTP.assertFinished()
    }

    func testEditsDuringAnInflightSaveRemainDirty() async throws {
        let requested = expectation(description: "Save started")
        let resume = DispatchSemaphore(value: 0)
        let (api, session) = try makeAPI([
            .init(method: "PUT", path: "/v1/settings", body: settingsJSON(rev: 9), beforeResponse: {
                requested.fulfill()
                XCTAssertEqual(resume.wait(timeout: .now() + 10), .success)
            }),
        ])
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g1", rev: 0))
        try engine.mergeSettings(APIReaderSettings(), generation: "g1")
        let app = makeModel(api: api, engine: engine)
        app.loggedIn = true
        try app.restoreReaderPreferences()
        let settings = ReaderSettingsModel(app: app)
        defer { resume.signal(); app.signOut(); session.invalidateAndCancel() }
        settings.draft.theme = .dark
        settings.save()
        await fulfillment(of: [requested], timeout: 5)
        settings.draft.theme = .eink
        resume.signal()
        try await waitUntil { !settings.busy }
        XCTAssertEqual(settings.draft.theme, .eink)
        XCTAssertTrue(settings.isDirty)
        XCTAssertFalse(settings.confirmed)
        XCTAssertEqual(app.readerSettings.theme, .dark)
        SettingsHTTP.assertFinished()
    }

    // MARK: - Overrides and inheritance

    func testFeedOverridesInheritAndRetainUntouchedEntries() {
        let model = makeModel()
        let settings = ReaderSettingsModel(app: model)
        settings.draft = APIReaderSettings(theme: .light, measure: 64, font: .serif, rev: 2)

        settings.setFeedOverride(feedID: 42, theme: .black)
        XCTAssertEqual(settings.draft.feedOverrides["42"]?.theme, .black)
        XCTAssertNil(settings.draft.feedOverrides["42"]?.measure)

        settings.setFeedOverride(feedID: 7, measure: 70)
        XCTAssertEqual(settings.draft.feedOverrides["42"]?.theme, .black,
                       "editing one feed must retain every other override")

        settings.resetFeedOverride(feedID: 42)
        XCTAssertNil(settings.draft.feedOverrides["42"])
        XCTAssertEqual(settings.draft.feedOverrides["7"]?.measure, 70)

        let inherited = ReaderStyle.resolve(settings.draft, feedID: 42)
        XCTAssertEqual(inherited.theme, .light)
        XCTAssertEqual(inherited.measure, 64)
        let overridden = ReaderStyle.resolve(settings.draft, feedID: 7)
        XCTAssertEqual(overridden.measure, 70)
        XCTAssertEqual(overridden.theme, .light)
    }

    func testDirtyDraftIsNotOverwrittenByNewerAccountSettings() throws {
        let engine = SyncEngine(store: MemorySyncStore())
        let model = makeModel(api: nil, engine: engine)
        let settings = ReaderSettingsModel(app: model)
        settings.draft = APIReaderSettings(theme: .dark, rev: 1)
        XCTAssertTrue(settings.isDirty)

        try engine.mergeSettings(APIReaderSettings(theme: .eink, rev: 2), generation: "g1")
        try model.restoreReaderPreferences()
        settings.update(from: APIReaderSettings(theme: .eink, rev: 2))
        XCTAssertEqual(settings.draft.theme, .dark, "local edits survive external changes")
        XCTAssertTrue(settings.conflict)

        // A clean draft follows the account.
        settings.load()
        XCTAssertEqual(settings.draft.theme, .eink)
        XCTAssertFalse(settings.conflict)
    }

    // MARK: - Safe style application

    func testReaderStyleUsesFixedEnumCssAndClampedMeasure() {
        XCTAssertTrue(ReaderStyle(theme: .dark, measure: 68, font: .serif).css
            .contains("background:#242424!important"))
        XCTAssertTrue(ReaderStyle(theme: .dark, measure: 68, font: .serif).css
            .contains("max-width:68ch"))
        XCTAssertTrue(ReaderStyle(theme: .black, measure: 99, font: .sans).css
            .contains("max-width:72ch"))
        XCTAssertTrue(ReaderStyle(theme: .light, measure: 10, font: .serif).css
            .contains("max-width:62ch"))
        XCTAssertTrue(ReaderStyle(theme: .eink, measure: 66, font: .dyslexic).css
            .contains("letter-spacing:.05em;word-spacing:.12em;"))
        XCTAssertTrue(ReaderStyle(theme: .system, measure: 66, font: .serif).css
            .contains("color-scheme:light dark"))
        XCTAssertFalse(ReaderStyle(theme: .light, measure: 66, font: .serif).css.contains("<"))
    }

    func testArticleSchemePageAppliesFixedStyleWithoutRelaxingImagePolicy() {
        let scheme = ArticleScheme()
        scheme.html = "<p>Article</p>"
        scheme.style = ReaderStyle(theme: .black, measure: 70, font: .dyslexic)
        let page = scheme.page
        XCTAssertTrue(page.contains("background:#000!important"))
        XCTAssertTrue(page.contains("max-width:70ch"))
        XCTAssertTrue(page.contains("color-scheme:dark"))
        XCTAssertTrue(page.contains("default-src 'none'"))
        XCTAssertTrue(page.contains("img-src 'none'"))
        XCTAssertTrue(page.contains("form-action 'none'"))
        XCTAssertFalse(page.contains("script-src"))
    }

    // MARK: - Shortcut toggle

    func testSingleKeyPreferenceUpdatesWindows() throws {
        let engine = SyncEngine(store: MemorySyncStore())
        let model = makeModel(api: nil, engine: engine)
        model.loggedIn = true
        let window = WindowModel(app: model)
        XCTAssertTrue(window.singleKeyShortcuts)

        try engine.mergeSettings(APIReaderSettings(singleKeyShortcuts: false, rev: 2), generation: "g1")
        try model.restoreReaderPreferences()
        window.rebuildPresentation()
        XCTAssertFalse(window.singleKeyShortcuts)

        try engine.mergeSettings(APIReaderSettings(singleKeyShortcuts: true, rev: 3), generation: "g1")
        try model.restoreReaderPreferences()
        window.rebuildPresentation()
        XCTAssertTrue(window.singleKeyShortcuts)
    }

    // MARK: - Counts gating and errors

    func testPrimaryRefreshDoesNotWaitForOptionalCountsFirst() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/subscriptions", body: "[]"),
            .init(path: "/v1/entries:counts", body: #"{"by_subscription":{},"later":1,"kept":0,"total":3}"#),
        ])
        defer { session.invalidateAndCancel() }
        let engine = SyncEngine(store: MemorySyncStore())
        let model = makeModel(api: api, engine: engine)
        try engine.mergeSettings(APIReaderSettings(readingMode: .unreadCounts, rev: 2), generation: "g1")
        try model.restoreReaderPreferences()
        model.loggedIn = true
        let window = WindowModel(app: model)
        window.section = .feeds

        await model.refresh()

        XCTAssertEqual(model.unreadCounts?.total, 3)
        SettingsHTTP.assertFinished()
    }

    func testCountsAreOnlyFetchedInUnreadModeAndPersisted() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/entries:counts",
                  body: #"{"by_subscription":{"1":2},"later":1,"kept":0,"total":3}"#),
        ])
        defer { session.invalidateAndCancel() }
        let engine = SyncEngine(store: MemorySyncStore())
        let model = makeModel(api: api, engine: engine)
        model.loggedIn = true

        await model.refreshUnreadCounts()
        XCTAssertNil(model.unreadCounts)
        XCTAssertEqual(SettingsHTTP.pending, 1, "river mode must not request counts")

        try engine.mergeSettings(APIReaderSettings(readingMode: .unreadCounts, rev: 2), generation: "g1")
        try model.restoreReaderPreferences()
        await model.refreshUnreadCounts()
        XCTAssertEqual(model.unreadCounts?.total, 3)
        XCTAssertEqual(try engine.snapshot().unreadCounts?.bySubscription["1"], 2)
        SettingsHTTP.assertFinished()
    }

    func testPreRestoreCountsCannotEnterTheNewSnapshot() async throws {
        let requested = expectation(description: "Old-generation counts requested")
        let resume = DispatchSemaphore(value: 0)
        let (api, session) = try makeAPI([
            .init(path: "/v1/entries:counts", body: #"{"by_subscription":{"1":99},"later":0,"kept":0,"total":99}"#, beforeResponse: {
                requested.fulfill()
                XCTAssertEqual(resume.wait(timeout: .now() + 10), .success)
            }),
        ])
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g1", rev: 0))
        try engine.mergeSettings(APIReaderSettings(readingMode: .unreadCounts, rev: 3), generation: "g1")
        let model = makeModel(api: api, engine: engine)
        model.loggedIn = true
        let task = Task { await model.refreshUnreadCounts() }
        defer { resume.signal(); task.cancel(); model.signOut(); session.invalidateAndCancel() }
        await fulfillment(of: [requested], timeout: 5)
        try engine.bootstrap(Cursor(generation: "g2", rev: 0))
        try engine.mergeSettings(APIReaderSettings(readingMode: .unreadCounts, rev: 1), generation: "g2")
        try model.restoreReaderPreferences()
        resume.signal()
        await task.value
        XCTAssertNil(model.unreadCounts)
        XCTAssertNil(try engine.snapshot().unreadCounts)
        SettingsHTTP.assertFinished()
    }

    func testUnauthorizedCountsSignsOutTheOwningSession() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/entries:counts", status: 401, body: #"{"error":"unauthorized"}"#),
        ])
        defer { session.invalidateAndCancel() }
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g1", rev: 0))
        try engine.mergeSettings(APIReaderSettings(readingMode: .unreadCounts, rev: 1), generation: "g1")
        let model = makeModel(api: api, engine: engine)
        model.loggedIn = true
        await model.refreshUnreadCounts()
        XCTAssertFalse(model.loggedIn)
        XCTAssertNil(model.unreadCounts)
        SettingsHTTP.assertFinished()
    }

    func testCountsFailureKeepsLastSyncedValues() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/entries:counts",
                  body: #"{"by_subscription":{"1":2},"later":1,"kept":0,"total":3}"#),
            .init(path: "/v1/entries:counts", status: 503,
                  body: #"{"type":"about:blank","title":"Unavailable","status":503,"error":"down"}"#),
        ])
        defer { session.invalidateAndCancel() }
        let engine = SyncEngine(store: MemorySyncStore())
        let model = makeModel(api: api, engine: engine)
        model.loggedIn = true
        try engine.mergeSettings(APIReaderSettings(readingMode: .unreadCounts, rev: 2), generation: "g1")
        try model.restoreReaderPreferences()

        await model.refreshUnreadCounts()
        XCTAssertEqual(model.unreadCounts?.total, 3)
        await model.refreshUnreadCounts()
        XCTAssertEqual(model.unreadCounts?.total, 3,
                       "a failed fetch must not masquerade as zero")
        XCTAssertNotNil(model.unreadCountsError)
        SettingsHTTP.assertFinished()
    }
}

private extension Data {
    func opID() throws -> String {
        let fields = try JSONDecoder().decode(APIValue.self, from: self)
        return try XCTUnwrap(fields["op_id"]?.stringValue)
    }
}

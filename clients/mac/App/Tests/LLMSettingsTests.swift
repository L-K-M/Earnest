import Foundation
import XCTest
import EarnestKit
@testable import Earnest

/// LLM services settings: explicit load/save, revision conflicts, frozen
/// retries, key reset, and the account-bound lifecycle. No GUI is launched.
@MainActor final class LLMSettingsTests: XCTestCase {
    // MARK: - Scripted HTTP

    final class LLMHTTP: URLProtocol {
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

    private final class LLMTokens: TokenStore, @unchecked Sendable {
        func load() throws -> String? { "test-token" }
        func save(_ token: String) throws {}
        func delete() throws {}
    }

    // MARK: - Fixtures

    private func makeModel(api: EarnestAPI? = nil) -> AppModel {
        AppModel(api: api, engine: SyncEngine(store: MemorySyncStore()), principalID: 7)
    }

    private func makeAPI(_ replies: [LLMHTTP.Reply]) throws -> (EarnestAPI, URLSession) {
        let session = LLMHTTP.install(replies)
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!,
                                 tokenStore: LLMTokens(), session: session)
        return (api, session)
    }

    private func waitUntil(file: StaticString = #filePath, line: UInt = #line,
                           _ condition: @escaping () -> Bool) async throws {
        let deadline = Date().addingTimeInterval(5)
        while !condition() && Date() < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertTrue(condition(), "Timed out waiting for LLM settings state", file: file, line: line)
    }

    private func settingsJSON(revision: Int64, hasKey: Bool = true) -> String {
        #"{"revision":\#(revision),"providers":[{"name":"openai","kind":"openai","base_url":"https://api.openai.com/v1","has_api_key":\#(hasKey),"monthly_limit_usd":"20.000000","timeout_seconds":60,"token_field":"max_completion_tokens"}],"tasks":[{"task":"embed","provider":"openai","model":"text-embedding-3-small"},{"task":"classify","provider":"","model":""},{"task":"blurb","provider":"","model":""},{"task":"summary","provider":"","model":""},{"task":"edition","provider":"","model":""},{"task":"compile_rule","provider":"","model":""},{"task":"ask","provider":"","model":""}],"models":[{"name":"text-embedding-3-small","input_per_1m":"0.000000","output_per_1m":"0.000000","embedding_per_1m":"0.020000","dimensions":1536,"truncatable":true}]}"#
    }

    private func fields(of body: Data) throws -> APIValue {
        try JSONDecoder().decode(APIValue.self, from: body)
    }

    // MARK: - Explicit load

    func testLoadPopulatesTheDraftAndNeverPrefillsAKey() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)

        XCTAssertFalse(settings.loaded)
        XCTAssertFalse(settings.busy)
        settings.load()
        XCTAssertTrue(settings.busy, "loading is visible")
        try await waitUntil { settings.loaded && !settings.busy }

        XCTAssertEqual(settings.revision, 2)
        XCTAssertEqual(settings.draft.providers.count, 1)
        XCTAssertEqual(settings.draft.providers[0].name, "openai")
        XCTAssertTrue(settings.draft.providers[0].hasAPIKey)
        XCTAssertEqual(settings.draft.providers[0].apiKey, "", "a key is never pre-filled")
        XCTAssertEqual(settings.draft.models.count, 1)
        XCTAssertEqual(settings.draft.models[0].name, "text-embedding-3-small")
        XCTAssertEqual(settings.draft.tasks.count, 7)
        XCTAssertEqual(settings.draft.tasks.first?.provider, "openai")
        XCTAssertFalse(settings.isDirty)
        LLMHTTP.assertFinished()
    }

    func testLoadFailureIsReportedAndLeavesThePaneUnloaded() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", status: 403,
                  body: #"{"type":"about:blank","title":"Forbidden","status":403,"detail":"admin scope required"}"#),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { !settings.busy }
        XCTAssertFalse(settings.loaded)
        XCTAssertNotNil(settings.error)
        XCTAssertFalse(settings.saved)
        LLMHTTP.assertFinished()
    }

    // MARK: - Explicit save

    func testSaveSendsTheWholeReplacementAndClearsTheKeyDraft() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
            .init(method: "PUT", path: "/v1/settings/llm", body: settingsJSON(revision: 3)),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }

        settings.draft.providers[0].apiKey = "sk-replacement"
        settings.draft.providers[0].monthlyLimitUSD = "30.5"
        settings.save()
        try await waitUntil { settings.saved && !settings.busy }

        XCTAssertTrue(settings.saved)
        XCTAssertNil(settings.error)
        XCTAssertEqual(settings.revision, 3)
        XCTAssertEqual(settings.draft.providers[0].apiKey, "", "the key draft resets after success")
        XCTAssertEqual(settings.draft.providers[0].hasAPIKey, true)
        XCTAssertFalse(settings.isDirty)

        let sent = try fields(of: XCTUnwrap(LLMHTTP.body(at: 0)))
        XCTAssertEqual(sent["revision"]?.int64Value, 2)
        XCTAssertEqual(try XCTUnwrap(sent["op_id"]?.stringValue).isEmpty, false)
        let providers = try XCTUnwrap(sent["providers"]?.arrayValue)
        XCTAssertEqual(providers[0]["api_key"]?.stringValue, "sk-replacement")
        XCTAssertEqual(providers[0]["monthly_limit_usd"]?.stringValue, "30.5")
        let tasks = try XCTUnwrap(sent["tasks"]?.arrayValue)
        XCTAssertEqual(tasks.count, 7)
        LLMHTTP.assertFinished()
    }

    func testSaveWithoutAKeyChangeKeepsTheStoredKey() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
            .init(method: "PUT", path: "/v1/settings/llm", body: settingsJSON(revision: 3, hasKey: true)),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }

        settings.draft.providers[0].timeoutSeconds = 90
        settings.save()
        try await waitUntil { settings.saved && !settings.busy }

        let sent = try fields(of: XCTUnwrap(LLMHTTP.body(at: 0)))
        let providers = try XCTUnwrap(sent["providers"]?.arrayValue)
        XCTAssertNil(providers[0]["api_key"], "keeping the key must not send it")
        XCTAssertEqual(providers[0]["clear_api_key"]?.boolValue, false)
        XCTAssertEqual(providers[0]["timeout_seconds"]?.int64Value, 90)
        LLMHTTP.assertFinished()
    }

    func testSaveWithoutLoadIsRejectedLocally() async throws {
        let (api, session) = try makeAPI([])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.save()
        XCTAssertNotNil(settings.error)
        XCTAssertFalse(settings.busy)
        XCTAssertEqual(LLMHTTP.pending, 0)
    }

    func testInvalidDraftFailsLocallyWithoutARequest() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }

        settings.draft.providers[0].name = "OpenAI"
        settings.save()
        try await waitUntil { settings.error != nil }
        XCTAssertFalse(settings.saved)
        XCTAssertFalse(settings.canRetry, "a validation failure must not freeze a retry")
        XCTAssertEqual(LLMHTTP.pending, 0, "client validation must keep a bad edit off the wire")
    }

    func testRenamingAKeyedProviderRequiresExplicitKeyIntent() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
            .init(method: "PUT", path: "/v1/settings/llm", body: settingsJSON(revision: 3, hasKey: false)),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }
        settings.draft.providers[0].name = "renamed"
        for index in settings.draft.tasks.indices {
            settings.draft.tasks[index].provider = ""
            settings.draft.tasks[index].model = ""
        }
        settings.save()
        try await waitUntil { !settings.busy }
        XCTAssertNotNil(settings.error)
        XCTAssertFalse(settings.saved)
        XCTAssertFalse(settings.canRetry)
        XCTAssertEqual(LLMHTTP.pending, 1, "renaming must not silently discard a saved key")

        settings.draft.providers[0].clearAPIKey = true
        settings.save()
        try await waitUntil { settings.saved && !settings.busy }
        let sent = try fields(of: XCTUnwrap(LLMHTTP.body(at: 0)))
        XCTAssertEqual(sent["providers"]?.arrayValue?.first?["name"]?.stringValue, "renamed")
        XCTAssertEqual(sent["providers"]?.arrayValue?.first?["clear_api_key"]?.boolValue, true)
        LLMHTTP.assertFinished()
    }

    func testRejectedSaveDoesNotOfferAnIdenticalRetry() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
            .init(method: "PUT", path: "/v1/settings/llm", status: 400,
                  body: #"{"type":"about:blank","title":"Invalid","status":400,"detail":"Invalid model configuration"}"#),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }
        settings.draft.providers[0].timeoutSeconds = 90
        settings.save()
        try await waitUntil { !settings.busy }
        XCTAssertNotNil(settings.error)
        XCTAssertFalse(settings.canRetry, "a rejected request needs an edit, not an identical retry")
        LLMHTTP.assertFinished()
    }

    func testChangingAKeyedProviderDestinationWithoutAKeyFailsLocally() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }

        settings.draft.providers[0].baseURL = "https://other.example/v1"
        settings.save()
        try await waitUntil { settings.error != nil }
        XCTAssertFalse(settings.saved)
        XCTAssertEqual(LLMHTTP.pending, 0, "a stored key must not follow a changed destination")
    }

    func testReplacementKeyAllowsADestinationChange() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
            .init(method: "PUT", path: "/v1/settings/llm", body: settingsJSON(revision: 3)),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }

        settings.draft.providers[0].baseURL = "https://other.example/v1"
        settings.draft.providers[0].apiKey = "sk-new"
        settings.save()
        try await waitUntil { settings.saved && !settings.busy }
        let sent = try fields(of: XCTUnwrap(LLMHTTP.body(at: 0)))
        let providers = try XCTUnwrap(sent["providers"]?.arrayValue)
        XCTAssertEqual(providers[0]["api_key"]?.stringValue, "sk-new")
        XCTAssertEqual(providers[0]["base_url"]?.stringValue, "https://other.example/v1")
        LLMHTTP.assertFinished()
    }

    // MARK: - Conflicts and retries

    func testConflictPreservesTheDraftAndRequiresReload() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
            .init(method: "PUT", path: "/v1/settings/llm", status: 409,
                  body: #"{"type":"about:blank","title":"Conflict","status":409,"detail":"LLM settings changed; reload before saving"}"#),
            .init(path: "/v1/settings/llm", body: "", failure: .notConnectedToInternet),
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 4)),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }
        settings.draft.providers[0].apiKey = "sk-new"
        settings.save()
        try await waitUntil { !settings.busy }

        XCTAssertTrue(settings.conflict)
        XCTAssertFalse(settings.saved)
        XCTAssertNotNil(settings.error)
        XCTAssertEqual(settings.draft.providers[0].apiKey, "sk-new", "the draft survives a conflict")
        XCTAssertFalse(settings.canRetry, "a definite 409 is not retried")
        XCTAssertEqual(settings.revision, 2)

        settings.save()
        try await Task.sleep(for: .milliseconds(50))
        XCTAssertEqual(LLMHTTP.pending, 2, "saving must wait for Reload")

        settings.load()
        try await waitUntil { !settings.busy }
        XCTAssertTrue(settings.conflict, "a failed Reload cannot resolve a revision conflict")
        XCTAssertEqual(settings.revision, 2)
        XCTAssertEqual(settings.draft.providers[0].apiKey, "sk-new")

        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }
        XCTAssertFalse(settings.conflict)
        XCTAssertEqual(settings.revision, 4)
        XCTAssertEqual(settings.draft.providers[0].apiKey, "", "reload discards the typed key")
        LLMHTTP.assertFinished()
    }

    func testRetryAfterAnAmbiguousFailureReusesTheBodyAndOpID() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
            .init(method: "PUT", path: "/v1/settings/llm", status: 500,
                  body: #"{"type":"about:blank","title":"Server","status":500,"error":"boom"}"#),
            .init(method: "PUT", path: "/v1/settings/llm", body: settingsJSON(revision: 3)),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }
        settings.draft.providers[0].monthlyLimitUSD = "12.25"
        settings.save()
        try await waitUntil { !settings.busy }
        XCTAssertNotNil(settings.error)
        XCTAssertTrue(settings.canRetry)
        let first = try XCTUnwrap(LLMHTTP.body(at: 0))

        settings.save()
        try await waitUntil { settings.saved && !settings.busy }
        let second = try XCTUnwrap(LLMHTTP.body(at: 1))
        XCTAssertEqual(first, second, "a retry must resend the exact body and op ID")
        LLMHTTP.assertFinished()
    }

    func testEditingAfterAFailedSaveMintsANewOpID() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
            .init(method: "PUT", path: "/v1/settings/llm", status: 500,
                  body: #"{"type":"about:blank","title":"Server","status":500,"error":"boom"}"#),
            .init(method: "PUT", path: "/v1/settings/llm", body: settingsJSON(revision: 3)),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }
        settings.draft.providers[0].monthlyLimitUSD = "12.25"
        settings.save()
        try await waitUntil { !settings.busy }
        let firstOp = try fields(of: XCTUnwrap(LLMHTTP.body(at: 0)))["op_id"]?.stringValue

        settings.draft.providers[0].monthlyLimitUSD = "15"
        settings.save()
        try await waitUntil { settings.saved && !settings.busy }
        let secondOp = try fields(of: XCTUnwrap(LLMHTTP.body(at: 1)))["op_id"]?.stringValue
        XCTAssertNotEqual(firstOp, secondOp)
        LLMHTTP.assertFinished()
    }

    func testEditsDuringAnInflightSaveAreKeptAndFlagged() async throws {
        let requested = expectation(description: "Save in flight")
        let resume = DispatchSemaphore(value: 0)
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
            .init(method: "PUT", path: "/v1/settings/llm", body: settingsJSON(revision: 3), beforeResponse: {
                requested.fulfill()
                XCTAssertEqual(resume.wait(timeout: .now() + 10), .success)
            }),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }
        settings.draft.providers[0].monthlyLimitUSD = "12.25"
        settings.save()
        await fulfillment(of: [requested], timeout: 5)
        settings.draft.providers[0].monthlyLimitUSD = "18"
        resume.signal()
        try await waitUntil { !settings.busy }
        XCTAssertEqual(settings.draft.providers[0].monthlyLimitUSD, "18", "in-flight edits are not discarded")
        XCTAssertTrue(settings.conflict)
        XCTAssertFalse(settings.saved)
        XCTAssertNotNil(settings.error)
        LLMHTTP.assertFinished()
    }

    // MARK: - Lifecycle

    func testEndSessionClearsTheDraftAndDropsTheFrozenRequest() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
            .init(method: "PUT", path: "/v1/settings/llm", status: 503,
                  body: #"{"type":"about:blank","title":"Unavailable","status":503,"error":"down"}"#),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }
        settings.draft.providers[0].apiKey = "sk-typed"
        settings.save()
        try await waitUntil { !settings.busy }
        XCTAssertNotNil(settings.error)
        XCTAssertEqual(LLMHTTP.pending, 0)

        settings.endSession()
        XCTAssertFalse(settings.loaded)
        XCTAssertNil(settings.revision)
        XCTAssertFalse(settings.ready)
        XCTAssertEqual(settings.draft.providers, [])
        XCTAssertFalse(settings.isDirty)
        try await Task.sleep(for: .milliseconds(50))
        XCTAssertEqual(LLMHTTP.pending, 0, "a frozen request must never replay")
    }

    func testSignOutResetsTheDraftThroughTheAppModel() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }
        settings.draft.providers[0].apiKey = "sk-typed"

        model.signOut()
        XCTAssertFalse(settings.ready)
        XCTAssertEqual(settings.draft.providers, [])
        XCTAssertFalse(settings.loaded)
    }

    func testUnauthorizedSaveSignsOutTheOwningSession() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
            .init(method: "PUT", path: "/v1/settings/llm", status: 401,
                  body: #"{"error":"unauthorized"}"#),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }
        settings.draft.providers[0].timeoutSeconds = 90
        settings.save()
        try await waitUntil { !settings.busy }
        XCTAssertFalse(model.loggedIn)
        XCTAssertFalse(settings.ready)
        XCTAssertEqual(settings.draft.providers, [])
        XCTAssertFalse(settings.saved)
        LLMHTTP.assertFinished()
    }

    func testRefreshScopeBindsThePaneAfterSignIn() {
        let model = makeModel()
        model.loggedIn = false
        let settings = LLMSettingsModel(app: model)
        XCTAssertFalse(settings.ready)
        model.loggedIn = true
        settings.refreshScope()
        XCTAssertTrue(settings.ready)
        XCTAssertFalse(settings.loaded)
        XCTAssertFalse(settings.isDirty)
    }

    func testRefreshScopeDiscardsTheDraftWhenTheAccountChanges() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }
        settings.draft.providers[0].apiKey = "sk-typed"

        model.signOut()
        model.loggedIn = true
        settings.refreshScope()
        XCTAssertTrue(settings.ready)
        XCTAssertEqual(settings.draft.providers, [])
        XCTAssertFalse(settings.loaded)
    }

    // MARK: - Key status badge

    /// The badge describes what the next Save would do: a typed-but-unsent
    /// replacement and a pending removal must show instead of the raw flag.
    func testKeyStatusReflectsTheDraftState() {
        var provider = LLMProviderDraft()
        XCTAssertEqual(provider.keyStatus.title, "No API key")

        provider.hasAPIKey = true
        XCTAssertEqual(provider.keyStatus.title, "API key saved")

        provider.apiKey = "sk-typed"
        XCTAssertEqual(provider.keyStatus.title, "API key entered · unsaved")

        provider.apiKey = "   "
        XCTAssertEqual(provider.keyStatus.title, "API key saved", "a whitespace draft is no key")

        provider.clearAPIKey = true
        provider.apiKey = "sk-typed"
        XCTAssertEqual(provider.keyStatus.title, "Key removal pending",
                       "the flag wins: Save removes the key and discards the draft text")

        provider.apiKey = ""
        provider.hasAPIKey = false
        XCTAssertEqual(provider.keyStatus.title, "Key removal pending")

        provider.clearAPIKey = false
        XCTAssertEqual(provider.keyStatus.title, "No API key")
    }

    // MARK: - Model discovery

    private func catalogueJSON() -> String {
        #"{"models":[{"id":"gpt-5","name":"GPT-5","chat":true,"embedding":false,"input_per_1m":"1.250000","output_per_1m":"10.000000"},{"id":"text-embedding-3-small","name":"Text Embedding 3 Small","chat":false,"embedding":true,"embedding_per_1m":"0.020000","dimensions":1536,"truncatable":true},{"id":"mystery","name":"Mystery"}]}"#
    }

    func testFetchModelsLoadsTheCatalogueForTheEditedProvider() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
            .init(method: "POST", path: "/v1/settings/llm/models", body: catalogueJSON()),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }

        settings.draft.providers[0].apiKey = "sk-draft"
        let providerID = settings.draft.providers[0].id
        settings.fetchModels(providerID: providerID)
        try await waitUntil {
            if case .loaded = settings.discoveries[providerID]?.phase { return true }
            return false
        }

        guard case .loaded(let models) = settings.discoveries[providerID]?.phase else {
            return XCTFail("catalogue must be loaded")
        }
        XCTAssertEqual(models.map(\.id), ["gpt-5", "text-embedding-3-small", "mystery"])
        XCTAssertEqual(models[0].chat, true)
        XCTAssertEqual(models[0].embedding, false)
        XCTAssertNil(models[2].chat, "unknown stays unknown, not false")

        let sent = try fields(of: XCTUnwrap(LLMHTTP.body(at: 0)))
        XCTAssertEqual(sent["revision"]?.int64Value, 2)
        let provider = try XCTUnwrap(sent["provider"])
        XCTAssertEqual(provider["name"]?.stringValue, "openai")
        XCTAssertEqual(provider["api_key"]?.stringValue, "sk-draft",
                       "the draft key previews, exactly like a save would send it")
        XCTAssertEqual(provider["clear_api_key"]?.boolValue, false)
        XCTAssertNil(sent["providers"], "discovery sends one provider, not a settings save")
        XCTAssertNil(sent["tasks"])
        LLMHTTP.assertFinished()
    }

    func testFetchWithoutLoadIsRejectedLocally() {
        let model = makeModel()
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.addProvider()
        let providerID = settings.draft.providers[0].id
        settings.fetchModels(providerID: providerID)
        XCTAssertNotNil(settings.error)
        XCTAssertNil(settings.discoveries[providerID])
    }

    func testCatalogueFilteringKeepsUnknownAndDropsKnownIncompatible() {
        let chatOnly = APIDiscoveredModel(id: "c", name: "C", chat: true, embedding: false)
        let embedOnly = APIDiscoveredModel(id: "e", name: "E", chat: false, embedding: true)
        let dual = APIDiscoveredModel(id: "d", name: "D", chat: true, embedding: true)
        let unknown = APIDiscoveredModel(id: "u", name: "U")

        XCTAssertTrue(llmModelSuits(chatOnly, task: .blurb))
        XCTAssertFalse(llmModelSuits(embedOnly, task: .blurb), "known embedding-only cannot chat")
        XCTAssertTrue(llmModelSuits(dual, task: .blurb))
        XCTAssertTrue(llmModelSuits(unknown, task: .blurb), "unknown stays a labelled candidate")

        XCTAssertTrue(llmModelSuits(embedOnly, task: .embed))
        XCTAssertFalse(llmModelSuits(chatOnly, task: .embed), "known chat-only cannot embed")
        XCTAssertTrue(llmModelSuits(dual, task: .embed))
        XCTAssertTrue(llmModelSuits(unknown, task: .embed))
    }

    func testPriceReviewDefaultsOnlyKnownInapplicableCapabilities() {
        // A known chat-only model may default embedding price and geometry.
        let chat = LLMModelReview(discovered: APIDiscoveredModel(id: "c", name: "C", chat: true, embedding: false))
        XCTAssertEqual(chat.embeddingPer1M, "0")
        XCTAssertEqual(chat.dimensions, "0")
        XCTAssertEqual(chat.inputPer1M, "", "a missing applicable price needs explicit input")
        XCTAssertEqual(chat.outputPer1M, "")
        XCTAssertFalse(chat.isReady)

        // A known embedding-only model may default chat prices; geometry is required.
        let embed = LLMModelReview(discovered: APIDiscoveredModel(id: "e", name: "E", chat: false, embedding: true))
        XCTAssertEqual(embed.inputPer1M, "0")
        XCTAssertEqual(embed.outputPer1M, "0")
        XCTAssertEqual(embed.embeddingPer1M, "")
        XCTAssertEqual(embed.dimensions, "")
        XCTAssertFalse(embed.isReady)

        // Unknown capability defaults nothing.
        let unknown = LLMModelReview(discovered: APIDiscoveredModel(id: "u", name: "U"))
        XCTAssertEqual(unknown.inputPer1M, "")
        XCTAssertEqual(unknown.embeddingPer1M, "")
        XCTAssertFalse(unknown.isReady)

        // Catalogue values prefill; user edits decide readiness.
        var priced = LLMModelReview(discovered: APIDiscoveredModel(
            id: "p", name: "P", chat: true, inputPer1M: "1.5", outputPer1M: "8",
            embeddingPer1M: "0.1", dimensions: 512, truncatable: true))
        XCTAssertTrue(priced.isReady)
        priced.inputPer1M = "abc"
        XCTAssertFalse(priced.isReady, "invalid money blocks the import")
        priced.inputPer1M = "1.5"
        let configured = priced.configured()
        XCTAssertEqual(configured.name, "p")
        XCTAssertEqual(configured.inputPer1M, "1.5")
        XCTAssertEqual(configured.dimensions, 512)
        XCTAssertTrue(configured.truncatable)
    }

    func testEditingTheProviderDiscardsItsCatalogue() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
            .init(method: "POST", path: "/v1/settings/llm/models", body: catalogueJSON()),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }
        let providerID = settings.draft.providers[0].id
        settings.fetchModels(providerID: providerID)
        try await waitUntil {
            if case .loaded = settings.discoveries[providerID]?.phase { return true }
            return false
        }

        settings.draft.providers[0].monthlyLimitUSD = "30"
        XCTAssertNil(settings.discoveries[providerID], "a provider edit makes its catalogue stale")
    }

    func testACatalogueReplyArrivingAfterAnEditIsDiscarded() async throws {
        let requested = expectation(description: "Fetch in flight")
        let resume = DispatchSemaphore(value: 0)
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
            .init(method: "POST", path: "/v1/settings/llm/models", body: catalogueJSON(), beforeResponse: {
                requested.fulfill()
                XCTAssertEqual(resume.wait(timeout: .now() + 10), .success)
            }),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }
        let providerID = settings.draft.providers[0].id
        settings.fetchModels(providerID: providerID)
        await fulfillment(of: [requested], timeout: 5)

        settings.draft.providers[0].timeoutSeconds = 90
        resume.signal()
        try await Task.sleep(for: .milliseconds(100))
        XCTAssertNil(settings.discoveries[providerID],
                     "a reply for an earlier provider draft must not be shown")
        LLMHTTP.assertFinished()
    }

    func testReloadDiscardsCatalogueSessions() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
            .init(method: "POST", path: "/v1/settings/llm/models", body: catalogueJSON()),
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 3)),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }
        let providerID = settings.draft.providers[0].id
        settings.fetchModels(providerID: providerID)
        try await waitUntil {
            if case .loaded = settings.discoveries[providerID]?.phase { return true }
            return false
        }

        settings.load()
        try await waitUntil { !settings.busy }
        XCTAssertTrue(settings.discoveries.isEmpty, "a reload rebuilds the draft and drops results")
        LLMHTTP.assertFinished()
    }

    func testSignOutDropsCatalogueSessionsAndKeys() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
            .init(method: "POST", path: "/v1/settings/llm/models", body: catalogueJSON()),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }
        let providerID = settings.draft.providers[0].id
        settings.fetchModels(providerID: providerID)
        try await waitUntil {
            if case .loaded = settings.discoveries[providerID]?.phase { return true }
            return false
        }

        model.signOut()
        XCTAssertTrue(settings.discoveries.isEmpty, "catalogues never survive into another account")
        LLMHTTP.assertFinished()
    }

    func testFailedFetchPreservesRoutesAndConfiguredModels() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
            .init(method: "POST", path: "/v1/settings/llm/models", status: 503,
                  body: #"{"type":"about:blank","title":"Unavailable","status":503,"detail":"provider unreachable"}"#),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }
        let providerID = settings.draft.providers[0].id
        settings.fetchModels(providerID: providerID)
        try await waitUntil {
            if case .failed = settings.discoveries[providerID]?.phase { return true }
            return false
        }

        guard case .failed(let message) = settings.discoveries[providerID]?.phase else {
            return XCTFail("a failed fetch reports instead of pretending success")
        }
        XCTAssertFalse(message.isEmpty)
        XCTAssertEqual(settings.draft.tasks[0].provider, "openai", "an error never touches a route")
        XCTAssertEqual(settings.draft.tasks[0].model, "text-embedding-3-small")
        XCTAssertEqual(settings.draft.models.map(\.name), ["text-embedding-3-small"],
                       "configured models remain the fallback")
        LLMHTTP.assertFinished()
    }

    func testImportAddsSelectedModelsAndKeepsConfiguredPricesOnCollision() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
            .init(method: "POST", path: "/v1/settings/llm/models", body: catalogueJSON()),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }
        let providerID = settings.draft.providers[0].id
        settings.fetchModels(providerID: providerID)
        try await waitUntil {
            if case .loaded = settings.discoveries[providerID]?.phase { return true }
            return false
        }

        // Fetching alone changes no route and imports nothing.
        XCTAssertEqual(settings.draft.models.map(\.name), ["text-embedding-3-small"])
        XCTAssertEqual(settings.draft.tasks[1].model, "", "classify stays unrouted")

        var fresh = LLMModelDraft()
        fresh.name = "gpt-5"
        fresh.inputPer1M = "1.25"
        fresh.outputPer1M = "10"
        var colliding = LLMModelDraft()
        colliding.name = "text-embedding-3-small"
        colliding.embeddingPer1M = "99"
        colliding.dimensions = 42

        let added = settings.importModels([fresh, colliding])
        XCTAssertEqual(added, 1, "an existing ID keeps its configured entry")
        XCTAssertEqual(settings.draft.models.count, 2)
        XCTAssertEqual(settings.draft.models[0].embeddingPer1M, "0.020000",
                       "prices are shared by model ID; the configured values win")
        XCTAssertEqual(settings.draft.models[0].dimensions, 1536)

        // Routing changes only through an explicit selection.
        settings.route(task: .blurb, providerName: "openai", modelName: "gpt-5")
        XCTAssertEqual(settings.draft.tasks[2].provider, "openai")
        XCTAssertEqual(settings.draft.tasks[2].model, "gpt-5")
        LLMHTTP.assertFinished()
    }

    func testImportRespectsTheConfiguredModelLimit() {
        let model = makeModel()
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.draft.models = (0..<llmModelLimit).map {
            var draft = LLMModelDraft()
            draft.name = "m\($0)"
            return draft
        }
        var extra = LLMModelDraft()
        extra.name = "overflow"
        XCTAssertEqual(settings.importModels([extra]), 0, "the 128-model cap is not exceeded")
        XCTAssertEqual(settings.draft.models.count, llmModelLimit)
    }

    func testEmbeddingReviewRequiresSupportedPositiveGeometry() {
        var review = LLMModelReview(discovered: APIDiscoveredModel(
            id: "embedding", name: "Embedding", chat: false, embedding: true,
            embeddingPer1M: "0.1", dimensions: 0))
        XCTAssertFalse(review.isReady, "zero is not embedding geometry")
        review.dimensions = "1536"
        XCTAssertFalse(review.isReady, "wide embeddings need explicit truncation")
        review.truncatable = true
        XCTAssertTrue(review.isReady)
    }

    func testCatalogueImportDoesNotSilentlyDropExcessSelections() {
        let model = makeModel()
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.draft.models = (0..<(llmModelLimit - 1)).map {
            var entry = LLMModelDraft()
            entry.name = "m\($0)"
            return entry
        }
        var first = LLMModelDraft(), second = LLMModelDraft()
        first.name = "first"
        second.name = "second"
        XCTAssertEqual(settings.importModels([first, second]), 0)
        XCTAssertEqual(settings.draft.models.count, llmModelLimit - 1)
        XCTAssertNotNil(settings.error)
    }

    func testApplyingCatalogueSelectionRechecksOwnership() async throws {
        let (api, session) = try makeAPI([
            .init(path: "/v1/settings/llm", body: settingsJSON(revision: 2)),
            .init(method: "POST", path: "/v1/settings/llm/models", body: catalogueJSON()),
        ])
        defer { session.invalidateAndCancel() }
        let model = makeModel(api: api)
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.load()
        try await waitUntil { settings.loaded && !settings.busy }
        let providerID = settings.draft.providers[0].id
        settings.fetchModels(providerID: providerID)
        try await waitUntil {
            if case .loaded = settings.discoveries[providerID]?.phase { return true }
            return false
        }
        let discovery = try XCTUnwrap(settings.discoveries[providerID])
        guard case .loaded(let models) = discovery.phase else { return XCTFail("missing catalogue") }
        let review = LLMModelReview(discovered: try XCTUnwrap(models.first { $0.id == "gpt-5" }))
        XCTAssertTrue(settings.applyCatalogueSelection([review], providerID: providerID,
                                                      fetchID: discovery.fetchID, task: .blurb))
        XCTAssertTrue(settings.draft.models.contains { $0.name == "gpt-5" })
        XCTAssertEqual(settings.draft.tasks.first { $0.task == .blurb }?.model, "gpt-5")
        settings.draft.providers[0].apiKey = "another-key"
        XCTAssertFalse(settings.applyCatalogueSelection([review], providerID: providerID,
                                                       fetchID: discovery.fetchID, task: .ask))
        XCTAssertEqual(settings.draft.tasks.first { $0.task == .ask }?.model, "")
        LLMHTTP.assertFinished()
    }

    // MARK: - Draft editing

    func testRemovingAProviderClearsItsRoutes() {
        let model = makeModel()
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.draft.providers = [
            LLMProviderDraft(settings: APILLMProviderSettings(name: "openai", kind: .openai,
                                                              baseURL: "https://api.openai.com/v1", hasAPIKey: true,
                                                              monthlyLimitUSD: "0", timeoutSeconds: 60,
                                                              tokenField: .maxCompletionTokens)),
        ]
        settings.draft.models = [
            LLMModelDraft(settings: APILLMModelSettings(name: "m", inputPer1M: "0", outputPer1M: "0",
                                                        embeddingPer1M: "0", dimensions: 0, truncatable: false)),
        ]
        settings.draft.tasks[0].provider = "openai"
        settings.draft.tasks[0].model = "m"
        let id = settings.draft.providers[0].id
        settings.removeProvider(id: id)
        XCTAssertEqual(settings.draft.tasks[0].provider, "")
        XCTAssertEqual(settings.draft.tasks[0].model, "")
    }

    func testNewProviderUsesTheKindDefaultURL() {
        let model = makeModel()
        model.loggedIn = true
        let settings = LLMSettingsModel(app: model)
        settings.addProvider()
        XCTAssertEqual(settings.draft.providers[0].baseURL, "https://api.openai.com/v1")
        let id = settings.draft.providers[0].id
        settings.setProviderKind(.anthropic, id: id)
        XCTAssertEqual(settings.draft.providers[0].kind, .anthropic)
        XCTAssertEqual(settings.draft.providers[0].baseURL, "https://api.anthropic.com")

        // A custom destination survives a kind change.
        settings.draft.providers[0].baseURL = "https://proxy.example/v1"
        settings.setProviderKind(.gemini, id: id)
        XCTAssertEqual(settings.draft.providers[0].baseURL, "https://proxy.example/v1")
    }
}

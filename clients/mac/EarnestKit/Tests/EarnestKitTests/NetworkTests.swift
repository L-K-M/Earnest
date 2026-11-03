import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import Testing
@testable import EarnestKit

final class TestTokens: TokenStore, @unchecked Sendable {
    private let lock = NSLock()
    private var token: String?
    init(_ token: String? = "test-token") { self.token = token }
    func load() throws -> String? { lock.withLock { token } }
    func save(_ token: String) throws { lock.withLock { self.token = token } }
    func delete() throws { lock.withLock { token = nil } }
}

/// Each test owns one queue; serialization prevents URLProtocol's global registry crossing tests.
final class ScriptedHTTP: URLProtocol {
    struct Reply: Sendable {
        var method = "GET"
        var path: String
        var status = 200
        var body: String
        var bearer: String? = "Bearer test-token"
        var checkBody: (@Sendable (Data?) throws -> Void)?
        var query: String?
    }
    private static let lock = NSLock()
    nonisolated(unsafe) private static var replies: [Reply] = []

    static func install(_ replies: [Reply]) -> URLSession {
        lock.withLock { Self.replies = replies }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [ScriptedHTTP.self]
        return URLSession(configuration: configuration)
    }
    static func assertFinished() { #expect(lock.withLock { replies.isEmpty }) }
    static var pendingCount: Int { lock.withLock { replies.count } }
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let reply = Self.lock.withLock { Self.replies.isEmpty ? nil : Self.replies.removeFirst() }
        guard let reply else {
            Issue.record("Unexpected HTTP request: \(request.url?.path ?? "")")
            client?.urlProtocol(self, didFailWithError: APIError.invalidResponse)
            return
        }
        #expect(request.url?.path == reply.path)
        #expect(request.httpMethod == reply.method)
        if let query = reply.query { #expect(request.url?.query == query) }
        #expect(request.value(forHTTPHeaderField: "Authorization") == reply.bearer)
        do { try reply.checkBody?(request.httpBody) }
        catch { Issue.record("Invalid HTTP body: \(error)") }
        let response = HTTPURLResponse(url: request.url!, statusCode: reply.status, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(reply.body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

@Suite(.serialized) struct NetworkTests {
    @Test func generatedRepositoryAuthAndProblem() async throws {
        let session = ScriptedHTTP.install([
            .init(method: "POST", path: "/v1/devices/start", body: #"{"device_code":"secret","user_code":"ABCD","verification_uri":"https://example.test/device","interval":5,"expires_in":600}"#, bearer: nil),
            .init(path: "/v1/changes", status: 410, body: #"{"type":"about:blank","title":"Gone","status":410,"detail":"cursor expired"}"#),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        #expect(try await api.startDevice(clientName: "Mac").userCode == "ABCD")
        do {
            _ = try await api.changes(cursor: "g1:9")
            Issue.record("Expected 410 problem")
        } catch let error as APIProblem { #expect(error.status == 410) }
        ScriptedHTTP.assertFinished()
    }

    @Test func contentSignedURLNeverReceivesBearer() async throws {
        let session = ScriptedHTTP.install([
            .init(path: "/v1/entries/1/content-url", body: #"{"url":"https://content.test/article/1?signature=abc","expires_at":"2099-01-01T00:00:00Z"}"#),
            .init(path: "/article/1", body: "<p>Article</p>", bearer: nil),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        #expect(try await api.signedContent(entryID: 1) == "<p>Article</p>")
        ScriptedHTTP.assertFinished()
    }

    @Test func expiredSignedURLDoesNotFetchContent() async throws {
        let session = ScriptedHTTP.install([
            .init(path: "/v1/entries/1/content-url", body: #"{"url":"https://content.test/article/1","expires_at":"2000-01-01T00:00:00Z"}"#),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        await #expect(throws: APIError.self) { try await api.signedContent(entryID: 1) }
        ScriptedHTTP.assertFinished()
    }
}

extension NetworkTests {
    @Test @MainActor func syncRejectsNonAdvancingChangesPage() async throws {
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g1", rev: 1))
        try engine.finishBootstrap()
        let session = ScriptedHTTP.install([
            .init(path: "/v1/changes", body: #"{"changes":[{"rev":1,"type":"entry.created","payload":{"entry":{"id":1,"rev":1}}}],"next":{"generation":"g1","rev":1},"next_cursor":"g1:1"}"#),
            // Bound the old retry loop so the regression fails without hanging.
            .init(path: "/v1/changes", status: 500, body: "{}"),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        await #expect(throws: SyncError.self) { try await SyncCoordinator(engine: engine, api: api).synchronize() }
        #expect(ScriptedHTTP.pendingCount == 1)
        #expect(try engine.snapshot().cursor == Cursor(generation: "g1", rev: 1))
    }

    @Test @MainActor func generationRecoveryReplaysOutboxOnce() async throws {
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "old", rev: 10))
        try engine.finishBootstrap()
        try engine.queue(Write(opID: "offline", principal: 1, entryID: 1, register: .kept, value: .flag(true), base: Cursor(generation: "old", rev: 0), occurredAt: Date()))
        let state = #"{"principal":1,"entry_id":1,"state_rev":2,"seen":{"value":"unseen","rev":0,"occurred_at":"2026-11-02T12:00:00Z","actor":1,"source":"deliberate"},"shelf":{"value":"none","rev":0,"occurred_at":"2026-11-02T12:00:00Z","actor":1,"source":"deliberate"},"kept":{"value":true,"rev":2,"occurred_at":"2026-11-02T12:00:00Z","actor":1,"source":"deliberate"}}"#
        let emptyChanges = #"{"changes":[],"next":{"generation":"new","rev":1},"next_cursor":"new:1"}"#
        let finalChanges = #"{"changes":[],"next":{"generation":"new","rev":2},"next_cursor":"new:2"}"#
        var replies: [ScriptedHTTP.Reply] = [
            .init(path: "/v1/changes", status: 410, body: #"{"type":"about:blank","title":"Gone","status":410}"#),
            .init(path: "/v1/bootstrap", body: #"{"generation":"new","rev":1}"#),
        ]
        for kind in ["subscriptions", "folders", "labels", "label_memberships", "entries", "state", "positions"] {
            let items = kind == "entries" ? #"[{"id":1,"subscription_id":1,"seq":1,"rev":1,"deleted":false}]"# : "[]"
            replies.append(.init(path: "/v1/bootstrap/\(kind)", body: "{\"items\":\(items),\"next_id\":0,\"next_principal\":0,\"next_view\":\"\",\"has_more\":false}"))
        }
        replies += [
            .init(path: "/v1/annotations", body: "[]"),
            .init(path: "/v1/changes", body: emptyChanges),
            .init(method: "POST", path: "/v1/state:batch", body: "[{\"op_id\":\"offline\",\"status\":\"applied\",\"state\":\(state),\"replay\":false}]", checkBody: { data in
                let items = try JSONDecoder().decode([APIValue].self, from: #require(data))
                guard case .object(let fields) = items.first, case .object(let base) = fields["base_rev"] else { Issue.record("Missing base"); return }
                #expect(base["generation"] == .string("new"))
                #expect(base["rev"] == .integer(0))
                #expect(fields["op_id"] == .string("offline"))
            }),
            .init(path: "/v1/changes", body: "{\"changes\":[{\"rev\":2,\"type\":\"state.updated\",\"payload\":{\"results\":[{\"op_id\":\"offline\",\"status\":\"applied\",\"state\":\(state),\"replay\":false}]}}],\"next\":{\"generation\":\"new\",\"rev\":2},\"next_cursor\":\"new:2\"}"),
            .init(path: "/v1/changes", body: finalChanges),
        ]
        let session = ScriptedHTTP.install(replies)
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        let coordinator = SyncCoordinator(engine: engine, api: api)
        let report = try await coordinator.synchronize()
        #expect(report.recovered)
        #expect(report.droppedOperationIDs.isEmpty)
        #expect(try engine.snapshot().outbox.isEmpty)
        #expect(try engine.snapshot().cursor == Cursor(generation: "new", rev: 2))
        #expect(try engine.state(principal: 1, entryID: 1).kept.value == .flag(true))
        ScriptedHTTP.assertFinished()
    }
}

extension NetworkTests {
    @Test @MainActor func recoveryStopsAfterSecondGoneResponse() async throws {
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g", rev: 1))
        try engine.finishBootstrap()
        var replies: [ScriptedHTTP.Reply] = [
            .init(path: "/v1/changes", status: 410, body: #"{"type":"about:blank","title":"Gone","status":410}"#),
            .init(path: "/v1/bootstrap", body: #"{"generation":"g","rev":1}"#),
        ]
        for kind in ["subscriptions", "folders", "labels", "label_memberships", "entries", "state", "positions"] {
            replies.append(.init(path: "/v1/bootstrap/\(kind)", body: #"{"items":[],"next_id":0,"next_principal":0,"next_view":"","has_more":false}"#))
        }
        replies.append(.init(path: "/v1/annotations", body: "[]"))
        replies.append(.init(path: "/v1/changes", status: 410, body: #"{"type":"about:blank","title":"Gone","status":410}"#))
        let session = ScriptedHTTP.install(replies)
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        await #expect(throws: APIProblem.self) { try await SyncCoordinator(engine: engine, api: api).synchronize() }
        ScriptedHTTP.assertFinished()
    }
}

extension NetworkTests {
    @Test(arguments: [0, 1]) func activityFollowsAfterIDUntilFinalPage(_ finalCount: Int) async throws {
        // IDs are not contiguous, and the cursor must retain full Int64 precision.
        let firstIDs = (1...1000).map { Int64($0) + 9_007_199_254_740_992 }
        let lastID = firstIDs.last! + (finalCount == 0 ? 0 : 7)
        let session = ScriptedHTTP.install([
            .init(path: "/v1/activity", body: activityPage(firstIDs), query: "after_id=0&limit=1000"),
            .init(path: "/v1/activity", body: activityPage(finalCount == 0 ? [] : [lastID]), query: "after_id=\(firstIDs.last!)&limit=1000"),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        let items = try await api.activity()
        #expect(items.count == 1000 + finalCount)
        #expect(items.last?["id"]?.int64Value == lastID)
        #expect(items.last?["next_action"]?.stringValue == "revert")
        ScriptedHTTP.assertFinished()
    }
}

private func activityPage(_ ids: [Int64]) -> String {
    let events = ids.map { id in
        "{\"id\":\(id),\"principal_id\":1,\"principal_name\":\"Scout\",\"action\":\"state.updated\",\"target\":{},\"before\":{},\"after\":{},\"generation\":\"g1\",\"created_at\":\"2026-11-03T00:00:00Z\",\"next_action\":\"revert\"}"
    }.joined(separator: ",")
    return "{\"activities\":[\(events)]}"
}

extension NetworkTests {
    @Test func activityRejectsNonAdvancingPage() async throws {
        let page = activityPage((1...1000).map(Int64.init))
        let session = ScriptedHTTP.install([
            .init(path: "/v1/activity", body: page, query: "after_id=0&limit=1000"),
            .init(path: "/v1/activity", body: page, query: "after_id=1000&limit=1000"),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        await #expect(throws: APIError.self) { try await api.activity() }
        ScriptedHTTP.assertFinished()
    }
}

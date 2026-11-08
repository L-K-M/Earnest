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
        /// Content type the scripted response advertises.
        var contentType = "application/json"
        /// When set, the request must advertise exactly this content type.
        var requestContentType: String?
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
        if let requestContentType = reply.requestContentType {
            #expect(request.value(forHTTPHeaderField: "Content-Type") == requestContentType)
        }
        #expect(request.value(forHTTPHeaderField: "Authorization") == reply.bearer)
        do { try reply.checkBody?(Self.body(of: request)) }
        catch { Issue.record("Invalid HTTP body: \(error)") }
        let response = HTTPURLResponse(url: request.url!, statusCode: reply.status, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": reply.contentType])!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(reply.body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}

    /// Darwin's URLSession hands protocols the body as a stream and leaves
    /// httpBody nil; FoundationNetworking on Linux keeps httpBody.
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
            .init(path: "/v1/entries/1/content-url", body: #"{"url":"https://content.test/article/1?signature=abc","expires_at":"2099-01-01T00:00:00Z"}"#, query: "mode=proxied"),
            .init(path: "/article/1", body: "<p>Article</p>", bearer: nil),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        let article = try await api.signedContent(entryID: 1)
        #expect(article.html == "<p>Article</p>")
        #expect(article.imageOrigin?.absoluteString == "https://content.test")
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
    @Test @MainActor func missingRiverInputsLeaveBootstrapIncomplete() async throws {
        let engine = SyncEngine(store: MemorySyncStore())
        var replies: [ScriptedHTTP.Reply] = [.init(path: "/v1/bootstrap", body: #"{"generation":"g","rev":0}"#)]
        for kind in ["subscriptions", "folders", "labels", "label_memberships", "entries", "state", "positions"] {
            replies.append(.init(path: "/v1/bootstrap/\(kind)", body: #"{"items":[],"next_id":0,"next_principal":0,"next_view":"","has_more":false}"#))
        }
        replies += [.init(path: "/v1/annotations", body: "[]"), .init(path: "/v1/mutes", body: "[]"),
                    .init(path: "/v1/settings", status: 503, body: "{}")]
        let session = ScriptedHTTP.install(replies)
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        await #expect(throws: (any Error).self) { try await SyncCoordinator(engine: engine, api: api).synchronize() }
        #expect(try !engine.snapshot().bootstrapComplete)
        ScriptedHTTP.assertFinished()
    }

    @Test @MainActor func syncRejectsNonAdvancingChangesPage() async throws {
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g1", rev: 1))
        try engine.finishBootstrap()
        let session = ScriptedHTTP.install([
            .init(path: "/v1/mutes", body: "[]"),
            .init(path: "/v1/settings", body: readerSettingsJSON),
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
            .init(path: "/v1/mutes", body: "[]"),
            .init(path: "/v1/settings", body: readerSettingsJSON),
            .init(path: "/v1/changes", status: 410, body: #"{"type":"about:blank","title":"Gone","status":410}"#),
            .init(path: "/v1/bootstrap", body: #"{"generation":"new","rev":1}"#),
        ]
        for kind in ["subscriptions", "folders", "labels", "label_memberships", "entries", "state", "positions"] {
            let items = kind == "entries" ? #"[{"id":1,"subscription_id":1,"seq":1,"rev":1,"deleted":false}]"# : "[]"
            replies.append(.init(path: "/v1/bootstrap/\(kind)", body: "{\"items\":\(items),\"next_id\":0,\"next_principal\":0,\"next_view\":\"\",\"has_more\":false}"))
        }
        replies += [
            .init(path: "/v1/annotations", body: "[]"),
            .init(path: "/v1/mutes", body: "[]"),
            .init(path: "/v1/settings", body: readerSettingsJSON),
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
            .init(path: "/v1/mutes", body: "[]"),
            .init(path: "/v1/settings", body: readerSettingsJSON),
            .init(path: "/v1/changes", status: 410, body: #"{"type":"about:blank","title":"Gone","status":410}"#),
            .init(path: "/v1/bootstrap", body: #"{"generation":"g","rev":1}"#),
        ]
        for kind in ["subscriptions", "folders", "labels", "label_memberships", "entries", "state", "positions"] {
            replies.append(.init(path: "/v1/bootstrap/\(kind)", body: #"{"items":[],"next_id":0,"next_principal":0,"next_view":"","has_more":false}"#))
        }
        replies.append(.init(path: "/v1/annotations", body: "[]"))
        replies.append(.init(path: "/v1/mutes", body: "[]"))
        replies.append(.init(path: "/v1/settings", body: readerSettingsJSON))
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

extension NetworkTests {
    /// Go writes RFC 3339 with microseconds and the server's zone offset;
    /// whole-second UTC fixtures hid that the generated client rejected it.
    @Test func serverTimestampsWithFractionalSecondsDecode() async throws {
        let annotation = #"{"id":1,"principal_id":2,"author_name":"Morning Agent","created_at":"2026-11-03T11:48:38.509896+01:00","kind":"note","entry_ids":[3],"value":"text","rev":4,"untrusted":true,"deleted":false}"#
        let session = ScriptedHTTP.install([
            .init(path: "/v1/annotations", body: "[\(annotation)]"),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        let items = try await api.annotations()
        #expect(items.count == 1)
        ScriptedHTTP.assertFinished()
    }
}

extension NetworkTests {
    @Test func clickContentKeepsImagesBlocked() async throws {
        let session = ScriptedHTTP.install([
            .init(path: "/v1/entries/3/content-url", body: #"{"url":"https://content.test:8081/article/3?signature=abc","expires_at":"2099-01-01T00:00:00Z"}"#, query: "mode=click"),
            .init(path: "/article/3", body: #"<a href="https://content.test:8081/image/signed" target="_blank">Load image</a>"#, bearer: nil),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        let article = try await api.signedContent(entryID: 3, mode: .click)
        #expect(article.imageOrigin == nil)
        #expect(article.html.contains("Load image"))
        ScriptedHTTP.assertFinished()
    }

    @Test func cancelNotificationUsesGeneratedOperationAndOpID() async throws {
        let session = ScriptedHTTP.install([
            .init(method: "POST", path: "/v1/notifications/19:cancel", body: "{}", checkBody: { data in
                let body = try JSONDecoder().decode([String: String].self, from: #require(data))
                #expect(UUID(uuidString: try #require(body["op_id"])) != nil)
            }),
            .init(method: "POST", path: "/v1/notifications/20:cancel", status: 409,
                  body: #"{"type":"about:blank","title":"Conflict","status":409,"detail":"already delivered"}"#),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        try await api.cancelNotification(id: 19)
        do {
            try await api.cancelNotification(id: 20)
            Issue.record("Expected cancellation conflict")
        } catch let problem as APIProblem { #expect(problem.status == 409) }
        ScriptedHTTP.assertFinished()
    }
}

extension NetworkTests {
    @Test func searchEncodesQueryFilterCursorAndDecodesNoteMetadata() async throws {
        let session = ScriptedHTTP.install([
            .init(
                path: "/v1/search",
                body: #"{"matches":[{"kind":"entry","entry_id":9223372036854775807,"subscription_id":9007199254740993,"feed_id":9223372036854775806,"seq":7,"title":"Beacon","url":"https://example.test/a","snippet":"A beacon","rank":1.5,"untrusted":false},{"kind":"agent_note","entry_id":9223372036854775807,"subscription_id":9007199254740993,"feed_id":9223372036854775806,"seq":7,"title":"Note","url":"https://example.test/a#note","snippet":"observed","rank":0.5,"annotation_id":9007199254740999,"agent_name":"Scout","untrusted":true}],"next_cursor":"abc/123=","status":"disabled"}"#,
                query: "q=hello%20world%20%26%20more%3F&mode=hybrid&subscription_id=9223372036854775807&folder_id=42&limit=25&cursor=abc%2F123%3D"
            ),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        let page = try await api.search(query: "hello world & more?", filter: APISearchFilter(mode: .hybrid, subscriptionID: Int64.max, folderID: 42, limit: 25), cursor: "abc/123=")
        #expect(page.status == "disabled")
        #expect(page.nextCursor == "abc/123=")
        let matches = try #require(page.matches)
        #expect(matches.count == 2)
        let entry = matches[0]
        #expect(entry.kind == "entry")
        #expect(entry.entryID == Int64.max)
        #expect(entry.subscriptionID == 9007199254740993)
        #expect(entry.feedID == Int64.max - 1)
        #expect(entry.annotationID == nil)
        #expect(entry.agentName == nil)
        #expect(!entry.untrusted)
        #expect(entry.id == "entry:\(Int64.max):0")
        let note = matches[1]
        #expect(note.kind == "agent_note")
        #expect(note.annotationID == 9007199254740999)
        #expect(note.agentName == "Scout")
        #expect(note.untrusted)
        #expect(note.id == "agent_note:\(Int64.max):9007199254740999")
        #expect(note.id != entry.id)
        ScriptedHTTP.assertFinished()
    }
}

extension NetworkTests {
    @Test func savedSearchCRUDPreservesIDsAndFilterEncoding() async throws {
        let session = ScriptedHTTP.install([
            .init(path: "/v1/saved-searches", body: #"[{"id":9223372036854775807,"name":"Saved","query":"beacon","filters":{"mode":"hybrid","subscription_id":9007199254740993}},{"id":7,"name":"Legacy","query":"old","filters":{"folder_id":5}}]"#),
            .init(method: "POST", path: "/v1/saved-searches", body: #"{"id":42,"name":"New","query":"fresh","filters":{"mode":"fulltext","folder_id":7,"limit":25}}"#, checkBody: { data in
                let body = try JSONDecoder().decode([String: APIValue].self, from: #require(data))
                #expect(body["name"] == .string("New"))
                #expect(body["query"] == .string("fresh"))
                guard case .object(let filters) = body["filters"] else { Issue.record("Missing filters"); return }
                #expect(filters["mode"] == .string("fulltext"))
                #expect(filters["folder_id"] == .integer(7))
                #expect(filters["limit"] == .integer(25))
                #expect(filters["subscription_id"] == nil)
            }),
            .init(method: "PATCH", path: "/v1/saved-searches/9223372036854775807", body: "{}", checkBody: { data in
                let body = try JSONDecoder().decode([String: APIValue].self, from: #require(data))
                #expect(body["name"] == .string("Renamed"))
            }),
            .init(method: "DELETE", path: "/v1/saved-searches/9223372036854775807", body: "{}"),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        let saved = try await api.savedSearches()
        #expect(saved.count == 2)
        #expect(saved[0].id == Int64.max)
        #expect(saved[0].filters.mode == .hybrid)
        #expect(saved[0].filters.subscriptionID == 9007199254740993)
        #expect(saved[1].filters.mode == .fulltext)
        #expect(saved[1].filters.folderID == 5)
        let created = try await api.createSavedSearch(name: "New", query: "fresh", filter: APISearchFilter(folderID: 7, limit: 25))
        #expect(created.id == 42)
        #expect(created.name == "New")
        #expect(created.filters.mode == .fulltext)
        #expect(created.filters.folderID == 7)
        try await api.renameSavedSearch(id: Int64.max, name: "Renamed")
        try await api.deleteSavedSearch(id: Int64.max)
        ScriptedHTTP.assertFinished()
    }
}

extension NetworkTests {
    @Test func searchSurfacesInBandStatusAndNullMatches() async throws {
        let session = ScriptedHTTP.install([
            .init(path: "/v1/search", body: #"{"matches":null,"status":"paused"}"#, query: "q=fresh&mode=fulltext"),
            .init(path: "/v1/search", body: #"{"matches":[],"status":"disabled"}"#, query: "q=again&mode=fulltext"),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        let paused = try await api.search(query: "fresh")
        #expect(paused.matches == nil)
        #expect(paused.status == "paused")
        let disabled = try await api.search(query: "again")
        #expect(try #require(disabled.matches).isEmpty)
        #expect(disabled.status == "disabled")
        ScriptedHTTP.assertFinished()
    }
}

extension NetworkTests {
    @Test func searchAndSavedSearchProblemsSurfaceAsAPIProblem() async throws {
        let session = ScriptedHTTP.install([
            .init(path: "/v1/search", status: 400, body: #"{"type":"about:blank","title":"Bad Request","status":400,"detail":"query too long"}"#),
            .init(method: "POST", path: "/v1/saved-searches", status: 409, body: #"{"type":"about:blank","title":"Conflict","status":409,"detail":"duplicate name"}"#),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        do {
            _ = try await api.search(query: "too long")
            Issue.record("Expected search 400")
        } catch let problem as APIProblem { #expect(problem.status == 400) }
        do {
            _ = try await api.createSavedSearch(name: "Duplicate", query: "fresh", filter: .init())
            Issue.record("Expected create 409")
        } catch let problem as APIProblem { #expect(problem.status == 409) }
        ScriptedHTTP.assertFinished()
    }
}

extension NetworkTests {
    @Test func pipelineStatusDecodesEveryField() async throws {
        let session = ScriptedHTTP.install([
            .init(path: "/v1/pipeline", body: #"{"embed":"ok","classify":"ok","semantic":"disabled","watch":"ok","blurb":"ok","summary":"ok","edition":"ok","ask":"ok","compile_rule":"ok"}"#),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        guard case .object(let fields) = try await api.pipelineStatus() else { Issue.record("Expected object"); return }
        #expect(fields.count == 9)
        #expect(fields["semantic"] == .string("disabled"))
        #expect(fields["compile_rule"] == .string("ok"))
        ScriptedHTTP.assertFinished()
    }
}

private let readerSettingsJSON = #"{"reading_mode":"river","today_decay_days":2,"single_key_shortcuts":true,"theme":"system","measure":68,"font":"serif","feed_overrides":{},"rev":0}"#

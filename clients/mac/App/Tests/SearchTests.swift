import Foundation
import XCTest
import EarnestKit
@testable import Earnest

@MainActor final class SearchModelTests: XCTestCase {
    private func fixture(_ replies: [ContentHTTP.Reply]) throws -> (URLSession, AppModel, WindowModel) {
        let session = ContentHTTP.install(replies)
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: ContentTokens(), session: session)
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g", rev: 0))
        try engine.finishBootstrap()
        let app = AppModel(api: api, engine: engine, principalID: 7)
        app.loggedIn = true
        return (session, app, WindowModel(app: app))
    }

    private func idle(_ search: SearchModel) async throws {
        let deadline = Date().addingTimeInterval(5)
        while (search.loading || search.loadingMetadata || search.saving) && Date() < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertFalse(search.loading)
        XCTAssertFalse(search.loadingMetadata)
        XCTAssertFalse(search.saving)
    }

    private var pipeline: String {
        #"{"embed":"enabled","classify":"disabled","semantic":"enabled","watch":"disabled","blurb":"disabled","summary":"disabled","edition":"disabled","ask":"disabled","compile_rule":"disabled"}"#
    }

    private func match(_ id: Int64, kind: String = "entry", annotation: Int64 = 0) -> String {
        #"{"kind":"\#(kind)","entry_id":\#(id),"subscription_id":5,"feed_id":2,"seq":\#(id),"title":"Article \#(id)","url":"https://publisher.test/\#(id)","snippet":"<img src=x> literal snippet","rank":1,"untrusted":\#(kind == "agent_note"),"annotation_id":\#(annotation),"agent_name":"Morning Agent"}"#
    }

    func testEnabledHybridPagesKeepNotesDistinctAndUseSubmittedFilters() async throws {
        let entry = match(42)
        let note = match(42, kind: "agent_note", annotation: 9)
        let (session, app, window) = try fixture([
            .init(path: "/v1/saved-searches", body: "[]"),
            .init(path: "/v1/pipeline", body: pipeline),
            .init(path: "/v1/search", body: "{\"matches\":[\(entry),\(note)],\"status\":\"enabled\",\"next_cursor\":\"c1\"}", query: "q=funding&mode=hybrid&subscription_id=5&folder_id=7&limit=10"),
            .init(path: "/v1/search", body: "{\"matches\":[\(entry),\(match(43))],\"status\":\"enabled\",\"next_cursor\":\"c2\"}", query: "q=funding&mode=hybrid&subscription_id=5&folder_id=7&limit=10&cursor=c1"),
            .init(path: "/v1/search", body: #"{"matches":[],"status":"enabled","next_cursor":"unused"}"#, query: "q=funding&mode=hybrid&subscription_id=5&folder_id=7&limit=10&cursor=c2"),
        ])
        defer { app.signOut(); session.invalidateAndCancel() }
        let search = window.search
        search.loadMetadata()
        try await idle(search)
        XCTAssertEqual(search.embeddingStatus, "enabled")
        search.query = "funding"
        search.filter = APISearchFilter(mode: .hybrid, subscriptionID: 5, folderID: 7, limit: 10)
        search.submit()
        try await idle(search)
        XCTAssertNil(search.error)
        XCTAssertNil(search.status, "Enabled is a successful page, not an unavailable state")
        XCTAssertEqual(search.matches.map(\.id), ["entry:42:0", "agent_note:42:9"])
        XCTAssertEqual(search.nextCursor, "c1")

        search.query = "unsent draft"
        search.filter = APISearchFilter(folderID: 99)
        search.loadMore()
        try await idle(search)
        XCTAssertEqual(search.matches.map(\.entryID), [42, 42, 43])
        XCTAssertEqual(search.nextCursor, "c2")
        search.loadMore()
        try await idle(search)
        XCTAssertNil(search.nextCursor)
        ContentHTTP.assertFinished()
    }

    func testMetadataFailureStillAllowsFullTextAndReportsRepeatedCursor() async throws {
        let (session, app, window) = try fixture([
            .init(path: "/v1/saved-searches", status: 503, body: "{}"),
            .init(path: "/v1/pipeline", status: 503, body: "{}"),
            .init(path: "/v1/search", body: "{\"matches\":[\(match(42))],\"next_cursor\":\"c1\"}", query: "q=funding&mode=fulltext"),
            .init(path: "/v1/search", body: "{\"matches\":[\(match(43))],\"next_cursor\":\"c1\"}", query: "q=funding&mode=fulltext&cursor=c1"),
        ])
        defer { app.signOut(); session.invalidateAndCancel() }
        let search = window.search
        search.loadMetadata()
        try await idle(search)
        XCTAssertNotNil(search.metadataError)
        search.query = "funding"
        search.filter.mode = .hybrid
        search.submit()
        try await idle(search)
        XCTAssertNil(search.error)
        search.loadMore()
        try await idle(search)
        XCTAssertTrue(search.error?.contains("repeated search cursor") == true)
        XCTAssertNil(search.nextCursor)
        XCTAssertEqual(search.matches.map(\.entryID), [42])
        ContentHTTP.assertFinished()
    }

    func testSavedSearchesRequireExplicitMutationsAndPreserveSearchContext() async throws {
        let stored = #"{"id":3,"name":"Stored","query":"funding","filters":{"folder_id":7}}"#
        let (session, app, window) = try fixture([
            .init(path: "/v1/saved-searches", body: "[\(stored)]"),
            .init(path: "/v1/pipeline", body: pipeline),
            .init(path: "/v1/search", body: #"{"matches":[]}"#, query: "q=funding&mode=fulltext&folder_id=7"),
            .init(path: "/v1/saved-searches/3", body: "{}", method: "PATCH"),
            .init(path: "/v1/saved-searches/3", body: "{}", method: "DELETE"),
        ])
        defer { app.signOut(); session.invalidateAndCancel() }
        let search = window.search
        search.loadMetadata()
        try await idle(search)
        XCTAssertEqual(search.savedSearches.count, 1)
        XCTAssertNil(search.notice)
        search.selectSaved(try XCTUnwrap(search.savedSearches.first))
        try await idle(search)
        search.savedName = "  "
        search.rename()
        XCTAssertNotNil(search.error)
        XCTAssertFalse(search.saving)
        search.savedName = "Renamed"
        search.rename()
        try await idle(search)
        XCTAssertEqual(search.savedSearches.first?.name, "Renamed")
        XCTAssertEqual(search.query, "funding")
        XCTAssertEqual(search.filter.folderID, 7)
        XCTAssertEqual(search.notice, "Saved search renamed.")
        search.delete()
        try await idle(search)
        XCTAssertTrue(search.savedSearches.isEmpty)
        XCTAssertNil(search.selectedSavedID)
        XCTAssertEqual(search.query, "funding")
        ContentHTTP.assertFinished()
    }

    func testSignOutAndRestoreClearSearchState() async throws {
        let (session, app, window) = try fixture([
            .init(path: "/v1/search", body: "{\"matches\":[\(match(42))]}", query: "q=funding&mode=fulltext"),
        ])
        defer { app.signOut(); session.invalidateAndCancel() }
        let search = window.search
        search.query = "funding"
        search.submit()
        try await idle(search)
        XCTAssertFalse(search.matches.isEmpty)
        try app.engine?.bootstrap(Cursor(generation: "restored", rev: 0))
        app.rebuildPresentation()
        XCTAssertTrue(search.matches.isEmpty)
        XCTAssertTrue(search.query.isEmpty)
        search.query = "private draft"
        search.savedName = "Private view"
        app.signOut()
        XCTAssertTrue(search.query.isEmpty)
        XCTAssertTrue(search.savedName.isEmpty)
        XCTAssertTrue(search.savedSearches.isEmpty)
        ContentHTTP.assertFinished()
    }

    func testLateSearchCannotPopulateAReplacementSession() async throws {
        let requested = expectation(description: "Old search started")
        let resume = DispatchSemaphore(value: 0)
        let (session, app, window) = try fixture([
            .init(path: "/v1/search", body: "{\"matches\":[\(match(42))]}", beforeResponse: {
                requested.fulfill()
                XCTAssertEqual(resume.wait(timeout: .now() + 10), .success)
            }, query: "q=old&mode=fulltext"),
            .init(path: "/v1/search", body: "{\"matches\":[\(match(43))]}", query: "q=new&mode=fulltext"),
        ])
        defer { resume.signal(); app.signOut(); session.invalidateAndCancel() }
        let search = window.search
        search.query = "old"
        search.submit()
        await fulfillment(of: [requested], timeout: 5)
        app.signOut()
        XCTAssertTrue(search.query.isEmpty)
        app.loggedIn = true
        search.query = "new"
        search.submit()
        resume.signal()
        try await idle(search)
        XCTAssertNil(search.error)
        XCTAssertEqual(search.matches.map(\.entryID), [43])
        XCTAssertEqual(search.submittedQuery, "new")
        ContentHTTP.assertFinished()
    }

    func testUnauthorizedSearchSignsOutAndClearsPrivateResults() async throws {
        let (session, app, window) = try fixture([
            .init(path: "/v1/search", status: 401, body: #"{"type":"about:blank","title":"Unauthorized","status":401}"#),
        ])
        defer { app.signOut(); session.invalidateAndCancel() }
        window.search.query = "private query"
        window.search.submit()
        try await idle(window.search)
        XCTAssertFalse(app.loggedIn)
        XCTAssertTrue(window.search.query.isEmpty)
        XCTAssertTrue(window.search.matches.isEmpty)
        ContentHTTP.assertFinished()
    }

    func testSearchCommandsRespectTextFocusAndAgentImagePolicy() throws {
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g", rev: 0))
        let app = AppModel(engine: engine, principalID: 7)
        app.loggedIn = true
        let window = WindowModel(app: app)
        defer { app.signOut() }
        window.section = .search
        XCTAssertTrue(window.canBrowse)
        window.editingSearch = true
        XCTAssertFalse(window.canBrowse, "Typing a query must not execute single-key reading commands")
        window.editingSearch = false
        let note = try JSONDecoder().decode(APISearchMatch.self, from: Data(match(42, kind: "agent_note", annotation: 9).utf8))
        window.selectSearchMatch(note)
        XCTAssertEqual(window.selected, 42)
        XCTAssertEqual(window.imageMode, .click)
        let entry = try JSONDecoder().decode(APISearchMatch.self, from: Data(match(42).utf8))
        window.selectSearchMatch(entry)
        XCTAssertEqual(window.imageMode, .proxied)
        XCTAssertTrue(window.paletteCommands().contains { $0.title == "Go to Search" })
    }
}

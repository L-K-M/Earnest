import Foundation
import XCTest
import EarnestKit
@testable import Earnest

/// Two windows over one AppModel must hold independent view state while
/// sharing the sync engine: a write in one window reaches the other only
/// through the shared rebuild.
@MainActor final class WindowModelTests: XCTestCase {
    private func makeModel(api: EarnestAPI? = nil) throws -> AppModel {
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g", rev: 1))
        try engine.mergeEntries([try entry(42), try entry(43)], generation: "g")
        try engine.finishBootstrap()
        let model = AppModel(api: api, engine: engine, principalID: 7)
        model.loggedIn = true
        return model
    }

    private func entry(_ id: Int64) throws -> APIEntry {
        let json = """
        {"id":\(id),"subscription_id":1,"feed_id":2,"seq":\(id),"title":"Article \(id)","url":"https://publisher.test/\(id)","author":"Ada","first_seen_at":"2026-11-04T08:00:00Z","rev":1,"deleted":false,"state":{"principal":7,"entry_id":\(id),"state_rev":0,"seen":{"value":"unseen","rev":0,"occurred_at":"","actor":0,"source":""},"shelf":{"value":"none","rev":0,"occurred_at":"","actor":0,"source":""},"kept":{"value":false,"rev":0,"occurred_at":"","actor":0,"source":""}}}
        """
        return try JSONDecoder().decode(APIEntry.self, from: Data(json.utf8))
    }

    private func waitForArticle(_ window: WindowModel, _ model: AppModel) async throws {
        let deadline = Date().addingTimeInterval(5)
        while window.html.isEmpty && model.error == nil && Date() < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertNil(model.error)
        XCTAssertFalse(window.html.isEmpty, "Article did not finish loading")
    }

    func testWindowsHoldDifferentSectionsSelectionsAndReaders() async throws {
        let session = ContentHTTP.install([
            .init(path: "/v1/entries/42/content-url", body: #"{"url":"https://content.test:8081/article/42?signature=abc","expires_at":"2099-01-01T00:00:00Z"}"#, mode: "proxied"),
            .init(path: "/article/42", body: "<p>First</p>"),
            .init(path: "/v1/entries/43/content-url", body: #"{"url":"https://content.test:8081/article/43?signature=abc","expires_at":"2099-01-01T00:00:00Z"}"#, mode: "proxied"),
            .init(path: "/article/43", body: "<p>Second</p>"),
        ])
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: ContentTokens(), session: session)
        let model = try makeModel(api: api)
        defer { model.signOut(); session.invalidateAndCancel() }
        let first = WindowModel(app: model)
        first.entries = [try entry(42), try entry(43)]
        let second = WindowModel(app: model)
        // A new window seeds its list from a sibling on the same section.
        XCTAssertEqual(second.entries.map(\.id), [42, 43])

        first.section = .later
        second.section = .done
        first.selectFromList(42)
        try await waitForArticle(first, model)
        second.selectFromList(43)
        try await waitForArticle(second, model)

        XCTAssertEqual(first.section, .later)
        XCTAssertEqual(second.section, .done)
        XCTAssertEqual(first.selected, 42)
        XCTAssertEqual(second.selected, 43)
        XCTAssertEqual(first.readingEntry?.id, 42)
        XCTAssertEqual(second.readingEntry?.id, 43)
        XCTAssertEqual(first.html, "<p>First</p>")
        XCTAssertEqual(second.html, "<p>Second</p>")
        first.palette = true
        XCTAssertFalse(second.palette)
        ContentHTTP.assertFinished()
    }

    func testReadingActionInOneWindowAppearsInTheOtherAfterRebuild() throws {
        let model = try makeModel()
        defer { model.signOut() }
        let first = WindowModel(app: model)
        let second = WindowModel(app: model)
        first.section = .later
        second.section = .done
        first.entries = [try entry(42), try entry(43)]
        second.entries = first.entries
        model.rebuildPresentation()
        XCTAssertTrue(first.rows.isEmpty)
        XCTAssertTrue(second.rows.isEmpty)

        first.selected = 42
        first.command(.done)

        XCTAssertTrue(first.rows.isEmpty, "The entry leaves the acting window's Later list")
        XCTAssertEqual(second.rows.map(\.id), [42], "The other window's Done list gains it from the shared engine")
        XCTAssertEqual(model.pendingOps, 1)
    }

    func testWindowCommandsAffectOnlyTheirWindow() throws {
        let model = try makeModel()
        defer { model.signOut() }
        let first = WindowModel(app: model)
        let second = WindowModel(app: model)
        first.entries = [try entry(42), try entry(43)]
        second.entries = first.entries
        model.rebuildPresentation()

        second.move(1)
        XCTAssertEqual(second.selected, 42)
        XCTAssertNil(first.selected)

        first.paletteCommands().first { $0.title == "Go to Done" }?.action()
        XCTAssertEqual(first.section, .done)
        XCTAssertEqual(second.section, .today)

        first.openDeepLink(URL(string: "earnest://entry/43")!)
        XCTAssertEqual(first.selected, 43)
        XCTAssertEqual(first.section, .today)
        XCTAssertEqual(first.imageMode, .click)
        XCTAssertEqual(second.selected, 42)
        XCTAssertEqual(second.section, .today)
        XCTAssertEqual(second.imageMode, .proxied)
    }
}

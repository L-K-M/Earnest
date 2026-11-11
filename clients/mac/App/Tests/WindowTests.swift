import Foundation
import XCTest
import EarnestKit
@testable import Earnest

/// Two windows over one AppModel must hold independent view state while
/// sharing the sync engine: a write in one window reaches the other only
/// through the shared rebuild.
@MainActor final class WindowModelTests: XCTestCase {
    private func makeModel(api: EarnestAPI? = nil, unreadOnly: Bool = false) throws -> AppModel {
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g", rev: 1))
        try engine.mergeEntries([try entry(42), try entry(43)], generation: "g")
        try engine.mergeBootstrap(kind: "subscriptions", items: [.object([
            "id": .integer(1), "title": .string("Feed"), "cadence": .string("now"),
            "initial_since": .null, "initial_through_seq": .integer(0),
        ])])
        try engine.finishBootstrap()
        if unreadOnly {
            try engine.replaceRiverResources(mutes: [], settings: .object([
                "today_decay_days": .integer(2), "show_only_unread": .bool(true),
            ]))
        }
        let model = AppModel(api: api, engine: engine, principalID: 7)
        try model.restoreReaderPreferences()
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
        // A real Done window has no copy of the entry before the action.
        second.entries = []
        var later = State(principal: 7, entryID: 42)
        later.shelf = Stamp(value: .text("later"), rev: 2)
        try model.engine?.mergeBootstrap([later])
        model.rebuildPresentation()
        XCTAssertEqual(first.rows.map(\.id), [42])
        XCTAssertTrue(second.rows.isEmpty)

        first.selected = 42
        first.command(.done)

        XCTAssertTrue(first.rows.isEmpty, "The entry leaves the acting window's Later list")
        XCTAssertEqual(second.rows.map(\.id), [42], "The other window's Done list gains it from the shared engine")
        XCTAssertEqual(model.pendingOps, 1)
    }

    func testNewWindowUsesCurrentSharedSnapshotWithoutASibling() throws {
        let model = try makeModel()
        defer { model.signOut() }
        let existing = WindowModel(app: model)
        existing.section = .done
        existing.entries = []

        let opened = WindowModel(app: model)
        XCTAssertEqual(opened.rows.map(\.id), [42, 43])
        XCTAssertEqual(existing.section, .done)
        XCTAssertTrue(existing.entries.isEmpty)
    }

    func testUnreadInAnotherWindowUpdatesAnOnlineRiverWithoutAddingMutedEntries() async throws {
        for muted in [false, true] {
            let session = ContentHTTP.install([.init(path: "/v1/entries", body: #"{"entries":[]}"#)])
            let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: ContentTokens(), session: session)
            let model = try makeModel(api: api)
            defer { model.signOut(); session.invalidateAndCancel() }
            let engine = try XCTUnwrap(model.engine)
            var done = State(principal: 7, entryID: 42)
            done.seen = Stamp(value: .text("seen"), rev: 2)
            done.shelf = Stamp(value: .text("done"), rev: 2)
            try engine.mergeBootstrap([done])
            try engine.replaceRiverResources(mutes: muted ? [.object([
                "id": .integer(1), "kind": .string("keyword"), "value": .string("Article"),
            ])] : [], settings: .object(["today_decay_days": .integer(2)]))
            let today = WindowModel(app: model)
            await today.refreshEntries()
            let acting = WindowModel(app: model)
            acting.section = .done
            model.rebuildPresentation()
            XCTAssertTrue(today.rows.isEmpty)

            acting.selected = 42
            acting.command(.unread)

            XCTAssertEqual(today.rows.map(\.id), muted ? [] : [42])
            ContentHTTP.assertFinished()
        }
    }

    func testOpenDoesNotDuplicateEntryAddedBySharedRebuild() async throws {
        let requested = expectation(description: "Entry request suspended")
        let resume = DispatchSemaphore(value: 0)
        // The generated HTTP client validates date-time fields, unlike the
        // tolerant APIEntry decoder used by the in-memory fixtures.
        let body = ContentLoadingTests.entryJSON.replacingOccurrences(of: "\"occurred_at\":\"\"", with: "\"occurred_at\":\"2026-11-04T08:00:00Z\"")
        let session = ContentHTTP.install([
            .init(path: "/v1/entries/42", body: body, beforeResponse: {
                requested.fulfill()
                XCTAssertEqual(resume.wait(timeout: .now() + 10), .success)
            }),
            .signed(mode: "proxied"), .article,
        ])
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: ContentTokens(), session: session)
        let model = try makeModel(api: api)
        defer { resume.signal(); model.signOut(); session.invalidateAndCancel() }
        let window = WindowModel(app: model)
        window.entries = []
        window.selectFromList(42)
        await fulfillment(of: [requested], timeout: 5)

        // A shared reading write and rebuild can supply the entry while the
        // individual entry request is still suspended.
        let engine = try XCTUnwrap(model.engine)
        try engine.queue(Write(opID: "other-window", principal: 7, entryID: 42,
                               register: .kept, value: .flag(true),
                               base: Cursor(generation: "g", rev: 0), occurredAt: Date()))
        model.rebuildPresentation()
        XCTAssertEqual(window.entries.filter { $0.id == 42 }.count, 1)
        resume.signal()
        try await waitForArticle(window, model)

        XCTAssertEqual(window.entries.filter { $0.id == 42 }.count, 1)
        ContentHTTP.assertFinished()
    }

    func testUnreadVisitKeepsReadRowsAfterSelectionAndBackgroundRefresh() async throws {
        let session = ContentHTTP.install([
            .init(path: "/v1/entries", body: #"{"entries":[]}"#),
            .init(path: "/v1/entries", body: #"{"entries":[]}"#),
        ])
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: ContentTokens(), session: session)
        let model = try makeModel(api: api, unreadOnly: true)
        defer { model.signOut(); session.invalidateAndCancel() }
        let window = WindowModel(app: model)
        XCTAssertEqual(window.rows.map(\.id), [42, 43])
        var seen = State(principal: 7, entryID: 42)
        seen.seen = Stamp(value: .text("seen"), rev: 2)
        try model.engine?.mergeBootstrap([seen])
        model.rebuildPresentation()
        XCTAssertEqual(window.rows.map(\.id), [42, 43])
        XCTAssertFalse(try XCTUnwrap(window.rows.first).unread)
        await window.refreshEntries()
        model.rebuildPresentation()
        XCTAssertEqual(window.rows.map(\.id), [42, 43])
        window.requestListReload()
        await window.refreshEntries()
        model.rebuildPresentation()
        XCTAssertTrue(window.rows.isEmpty)
        ContentHTTP.assertFinished()
    }

    func testUnreadVisitIsReappliedWhenParentSectionIsReopened() throws {
        let model = try makeModel(unreadOnly: true)
        defer { model.signOut() }
        let window = WindowModel(app: model)
        window.selected = 42
        var seen = State(principal: 7, entryID: 42)
        seen.seen = Stamp(value: .text("seen"), rev: 2)
        try model.engine?.mergeBootstrap([seen])
        window.selected = 43
        window.selected = nil
        model.rebuildPresentation()
        XCTAssertEqual(window.rows.map(\.id), [42, 43])
        window.section = .edition
        window.section = .today
        model.rebuildPresentation()
        XCTAssertEqual(window.rows.map(\.id), [43])
    }

    func testUnreadVisitCannotRetainMetadataAcrossRestore() throws {
        let model = try makeModel(unreadOnly: true)
        defer { model.signOut() }
        let window = WindowModel(app: model)
        XCTAssertEqual(window.rows.map(\.id), [42, 43])
        try model.engine?.bootstrap(Cursor(generation: "restored", rev: 0))
        window.resetListAfterRestore()
        model.rebuildPresentation()
        XCTAssertTrue(window.entries.isEmpty)
        XCTAssertTrue(window.rows.isEmpty)
    }

    func testGoneEntryDisappearsFromBothWindows() throws {
        let model = try makeModel()
        defer { model.signOut() }
        let first = WindowModel(app: model)
        let second = WindowModel(app: model)
        XCTAssertEqual(first.rows.map(\.id), [42, 43])
        XCTAssertEqual(second.rows.map(\.id), [42, 43])

        try model.engine?.markGone(entryID: 42)
        model.rebuildPresentation()

        XCTAssertEqual(first.rows.map(\.id), [43])
        XCTAssertEqual(second.rows.map(\.id), [43])
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

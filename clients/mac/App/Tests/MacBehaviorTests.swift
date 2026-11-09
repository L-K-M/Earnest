import AppKit
import Foundation
import XCTest
import EarnestKit
@testable import Earnest

/// Mac conventions layered over the shared models: the open article staying
/// put, undo of reading actions, navigation, and input parsing.
@MainActor final class MacBehaviorTests: XCTestCase {
    private func entry(_ id: Int64) throws -> APIEntry {
        let json = """
        {"id":\(id),"subscription_id":1,"feed_id":2,"seq":\(id),"title":"Article \(id)","url":"https://publisher.test/\(id)","author":"Ada","first_seen_at":"2026-11-04T08:00:00Z","rev":1,"deleted":false,"state":{"principal":7,"entry_id":\(id),"state_rev":0,"seen":{"value":"unseen","rev":0,"occurred_at":"","actor":0,"source":""},"shelf":{"value":"none","rev":0,"occurred_at":"","actor":0,"source":""},"kept":{"value":false,"rev":0,"occurred_at":"","actor":0,"source":""}}}
        """
        return try JSONDecoder().decode(APIEntry.self, from: Data(json.utf8))
    }

    private func makeModel(api: EarnestAPI? = nil) throws -> AppModel {
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g", rev: 1))
        try engine.mergeEntries([try entry(41), try entry(42), try entry(43)], generation: "g")
        try engine.mergeBootstrap(kind: "subscriptions", items: [.object([
            "id": .integer(1), "title": .string("Feed"), "cadence": .string("now"),
            "initial_since": .null, "initial_through_seq": .integer(0),
        ])])
        try engine.finishBootstrap()
        let model = AppModel(api: api, engine: engine, principalID: 7)
        model.loggedIn = true
        return model
    }

    /// Opening an article marks it seen without pulling it out of the list.
    private func markSeen(_ id: Int64, in model: AppModel) throws {
        try model.engine?.queue(Write(opID: "seen-\(id)", principal: 7, entryID: id, register: .seen,
                                      value: .text("seen"), base: Cursor(generation: "g", rev: 0),
                                      occurredAt: Date(), kind: .passive))
        model.rebuildPresentation()
    }

    func testOpenArticleStaysInTheRiverUntilSelectionMoves() throws {
        let model = try makeModel()
        defer { model.signOut() }
        let window = WindowModel(app: model)
        window.selected = 42
        try markSeen(42, in: model)
        XCTAssertEqual(window.rows.map(\.id), [41, 42, 43], "The open article keeps its place")
        XCTAssertEqual(window.rows.first { $0.id == 42 }?.unread, false)

        window.move(1)
        XCTAssertEqual(window.selected, 43)
        XCTAssertEqual(window.rows.map(\.id), [41, 43], "A seen article leaves once you move on")
    }

    func testRefreshKeepsTheOpenArticleSoItsCommandsStayAvailable() throws {
        let model = try makeModel()
        defer { model.signOut() }
        let window = WindowModel(app: model)
        window.selected = 42
        try markSeen(42, in: model)
        XCTAssertTrue(window.canActOnEntry)
        // The server's river no longer lists a seen article.
        window.restoreCachedEntries([try entry(41), try entry(43)])
        model.rebuildPresentation()
        XCTAssertEqual(window.entries.map(\.id), [41, 42, 43])
        XCTAssertEqual(window.rows.map(\.id), [41, 42, 43])
        XCTAssertTrue(window.canActOnEntry, "Done, Later and Keep still act on the article being read")
        window.command(.done)
        XCTAssertEqual(window.selected, 43)
        XCTAssertEqual(window.rows.map(\.id), [41, 43])
    }

    func testAFailedArticleSaysSoInsteadOfLoadingForever() async throws {
        let session = ContentHTTP.install([
            .init(path: "/v1/entries/42/content-url", status: 500,
                  body: #"{"type":"about:blank","title":"Server","status":500}"#, mode: "proxied"),
            .init(path: "/v1/entries/42/content", status: 500,
                  body: #"{"type":"about:blank","title":"Server","status":500}"#),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: ContentTokens(), session: session)
        let model = try makeModel(api: api)
        defer { model.signOut() }
        let window = WindowModel(app: model)
        window.selectFromList(42)
        let deadline = Date().addingTimeInterval(5)
        while window.readerFailure == nil && Date() < deadline { try await Task.sleep(for: .milliseconds(10)) }
        XCTAssertNotNil(window.readerFailure, "The reader explains the failure")
        XCTAssertTrue(window.html.isEmpty)
        model.error = nil
        ContentHTTP.assertFinished()
    }

    func testADeferredLinkOpensWithImagesBlocked() throws {
        let model = try makeModel()
        defer { model.signOut() }
        let window = WindowModel(app: model)
        window.go(to: .kept)
        // A link that waited for sign-in, as a reconnect hands it back.
        window.deferDeepLink(43)
        model.rebuildPresentation()
        XCTAssertEqual(window.section, .today)
        XCTAssertEqual(window.selected, 43)
        XCTAssertEqual(window.imageMode, .click, "External links never load images automatically")
        XCTAssertNil(window.pendingDeepLink)
    }

    func testUndoLeavesRegistersTheActionDidNotChange() throws {
        let model = try makeModel()
        defer { model.signOut() }
        let engine = try XCTUnwrap(model.engine)
        let window = WindowModel(app: model)
        let undo = UndoManager()
        undo.groupsByEvent = false
        window.undoManager = undo
        window.selected = 42
        undo.beginUndoGrouping()
        window.command(.done)
        undo.endUndoGrouping()
        // Kept on another device after the Mac's Done.
        try engine.queue(Write(opID: "web-keep", principal: 7, entryID: 42, register: .kept, value: .flag(true),
                               base: Cursor(generation: "g", rev: 0), occurredAt: Date()))
        undo.undo()
        let state = try engine.state(principal: 7, entryID: 42)
        XCTAssertEqual(state.shelf.value, .text("none"), "Undo reverses the Done")
        XCTAssertEqual(state.kept.value, .flag(true), "Undo leaves the other device's Keep alone")
    }

    func testActivityArticlesKeepImagesBlockedWhenMovedTo() throws {
        let model = try makeModel()
        defer { model.signOut() }
        let window = WindowModel(app: model)
        window.go(to: .activity)
        window.selectFromList(42)
        XCTAssertEqual(window.imageMode, .click, "Agent-surfaced articles never load images automatically")
    }

    func testSignOutClearsAPendingConfirmation() throws {
        let model = try makeModel()
        model.confirmingSignOut = true
        model.signOut()
        XCTAssertFalse(model.confirmingSignOut)
    }

    func testSignOutForgetsTheSubscriptionDraft() throws {
        let model = try makeModel()
        let window = WindowModel(app: model)
        window.subscribeURL = "https://private.example/feed"
        window.subscribeCadence = "week"
        model.signOut()
        XCTAssertEqual(window.subscribeURL, "")
        XCTAssertEqual(window.subscribeCadence, "today")
    }

    func testDoneOnTheOpenArticleSelectsTheNextAndUndoBringsItBack() throws {
        let model = try makeModel()
        defer { model.signOut() }
        let window = WindowModel(app: model)
        let undo = UndoManager()
        undo.groupsByEvent = false
        window.undoManager = undo
        window.selected = 42
        try markSeen(42, in: model)

        undo.beginUndoGrouping()
        window.command(.done)
        undo.endUndoGrouping()
        XCTAssertEqual(window.rows.map(\.id), [41, 43])
        XCTAssertEqual(window.selected, 43, "Done moves on to the next article")
        XCTAssertEqual(undo.undoActionName, "Move to Done")
        XCTAssertEqual(model.sessionActions.done, 1)

        undo.undo()
        XCTAssertEqual(try model.engine?.state(principal: 7, entryID: 42).shelf.value, .text("none"))
        XCTAssertTrue(window.rows.map(\.id).contains(42))
        XCTAssertEqual(window.selected, 42, "Undo reselects the article it brought back")
        XCTAssertEqual(model.sessionActions.done, 0)
        XCTAssertEqual(undo.redoActionName, "Move to Done")

        undo.redo()
        XCTAssertEqual(try model.engine?.state(principal: 7, entryID: 42).shelf.value, .text("done"))
        XCTAssertFalse(window.rows.map(\.id).contains(42))
        XCTAssertEqual(model.sessionActions.done, 1)
    }

    func testUndoAfterARefreshDroppedTheArticleStillBringsItBack() throws {
        let model = try makeModel()
        defer { model.signOut() }
        let window = WindowModel(app: model)
        let undo = UndoManager()
        undo.groupsByEvent = false
        window.undoManager = undo
        window.selected = 42
        try markSeen(42, in: model)
        undo.beginUndoGrouping()
        window.command(.done)
        undo.endUndoGrouping()
        XCTAssertEqual(window.selected, 43)
        // A sync refreshed the river without the done article.
        window.restoreCachedEntries([try entry(41), try entry(43)])
        model.rebuildPresentation()

        undo.undo()
        XCTAssertEqual(window.selected, 42, "Undo reselects the article even after a refresh dropped it")
        XCTAssertEqual(window.rows.map(\.id), [41, 42, 43])
        XCTAssertTrue(window.canActOnEntry)
    }

    func testUndoUnreadRestoresDoneThatTheServerRuleCleared() throws {
        let model = try makeModel()
        defer { model.signOut() }
        let engine = try XCTUnwrap(model.engine)
        var done = State(principal: 7, entryID: 42)
        done.seen = Stamp(value: .text("seen"), rev: 2)
        done.shelf = Stamp(value: .text("done"), rev: 2)
        try engine.mergeBootstrap([done])
        let window = WindowModel(app: model)
        window.section = .done
        let undo = UndoManager()
        undo.groupsByEvent = false
        window.undoManager = undo
        model.rebuildPresentation()
        window.selected = 42

        undo.beginUndoGrouping()
        window.command(.unread)
        undo.endUndoGrouping()
        // The server clears Done when an article is marked unread; emulate it.
        try engine.queue(Write(opID: "rule", principal: 7, entryID: 42, register: .shelf, value: .text("none"),
                               base: Cursor(generation: "g", rev: 2), occurredAt: Date()))
        undo.undo()
        let state = try engine.state(principal: 7, entryID: 42)
        XCTAssertEqual(state.seen.value, .text("seen"))
        XCTAssertEqual(state.shelf.value, .text("done"))
    }

    func testLaterTogglesLikeTheWebKey() throws {
        let model = try makeModel()
        defer { model.signOut() }
        let window = WindowModel(app: model)
        window.section = .later
        window.entries = [try entry(42)]
        window.selected = 42
        XCTAssertFalse(window.selectedIsLater)
        window.command(.later)
        XCTAssertTrue(window.selectedIsLater)
        XCTAssertEqual(window.rows.map(\.id), [42])
        window.command(.later)
        XCTAssertEqual(try model.engine?.state(principal: 7, entryID: 42).shelf.value, .text("none"))
        XCTAssertTrue(window.rows.isEmpty)
        XCTAssertEqual(model.sessionActions.later, 1, "Removing from Later is not counted as a new Later")
    }

    func testContextMenuActsOnItsRowWithoutMovingTheSelection() throws {
        let model = try makeModel()
        defer { model.signOut() }
        let window = WindowModel(app: model)
        window.selected = 41
        window.command(.keep, entryID: 43)
        XCTAssertEqual(try model.engine?.state(principal: 7, entryID: 43).kept.value, .flag(true))
        XCTAssertEqual(window.selected, 41)
        XCTAssertEqual(window.rows.first { $0.id == 43 }?.kept, true)
    }

    func testGoClosesTheReaderAndSearchReturnsWhereItStarted() throws {
        let model = try makeModel()
        defer { model.signOut() }
        let window = WindowModel(app: model)
        window.go(to: .later)
        window.selected = 42
        window.search.query = "garden"
        window.submitSearch()
        XCTAssertEqual(window.section, .search)
        XCTAssertNil(window.selected, "Search results start without an open article")
        window.endSearch()
        XCTAssertEqual(window.section, .later)
        XCTAssertEqual(window.search.query, "")
        window.selected = 41
        window.go(to: .kept)
        XCTAssertNil(window.selected)
        XCTAssertEqual(window.section, .kept)
    }

    func testPaletteListsEveryPlaceAndAccountCommand() throws {
        let model = try makeModel()
        defer { model.signOut() }
        let titles = Set(WindowModel(app: model).paletteCommands().map(\.title))
        for place in Section.places { XCTAssertTrue(titles.contains("Go to \(place.title)"), place.title) }
        for title in ["Search Articles", "Settings…", "Sign Out…", "New Subscription…", "Open in Browser", "Move to Done"] {
            XCTAssertTrue(titles.contains(title), title)
        }
    }

    func testServerAddressesGainTheExpectedScheme() {
        XCTAssertEqual(ServerAddress.normalize("earnest.example.com"), "https://earnest.example.com")
        XCTAssertEqual(ServerAddress.normalize(" earnest.example.com/ "), "https://earnest.example.com")
        XCTAssertEqual(ServerAddress.normalize("localhost:8080"), "http://localhost:8080")
        XCTAssertEqual(ServerAddress.normalize("nas:8080"), "http://nas:8080")
        XCTAssertEqual(ServerAddress.normalize("192.168.1.20:8080"), "http://192.168.1.20:8080")
        XCTAssertEqual(ServerAddress.normalize("reader.local"), "http://reader.local")
        XCTAssertEqual(ServerAddress.normalize("http://earnest.example.com"), "http://earnest.example.com")
        XCTAssertNil(ServerAddress.normalize(""))
        XCTAssertNil(ServerAddress.normalize("ftp://earnest.example.com"))
        XCTAssertNil(ServerAddress.normalize("user:secret@nas.local"), "Credentials never go in the address")
        XCTAssertNil(ServerAddress.normalize("https://user:secret@earnest.example.com"))
    }

    func testFeedLinksUnwrapToHTTP() throws {
        XCTAssertEqual(FeedLink.subscriptionURL(try XCTUnwrap(URL(string: "feed://example.com/rss.xml"))), "http://example.com/rss.xml")
        XCTAssertEqual(FeedLink.subscriptionURL(try XCTUnwrap(URL(string: "feed:https://example.com/atom"))), "https://example.com/atom")
        XCTAssertNil(FeedLink.subscriptionURL(try XCTUnwrap(URL(string: "feed:javascript:alert(1)"))))
        XCTAssertNil(FeedLink.subscriptionURL(try XCTUnwrap(URL(string: "earnest://entry/3"))))
        XCTAssertNil(FeedLink.subscriptionURL(try XCTUnwrap(URL(string: "feed://user:secret@example.com/rss"))))
    }

    func testOnlyHTTPAddressesWithAHostAreWebLinks() {
        XCTAssertEqual(WebLink.url("https://example.com/a"), URL(string: "https://example.com/a"))
        XCTAssertEqual(WebLink.url("HTTP://example.com"), URL(string: "HTTP://example.com"))
        XCTAssertNil(WebLink.url("http://"), "A scheme without a host is not a link")
        XCTAssertNil(WebLink.url("https:///path"))
        XCTAssertNil(WebLink.url("javascript:alert(1)"))
        XCTAssertNil(WebLink.url("file:///etc/hosts"))
        XCTAssertNil(WebLink.url(""))
    }

    func testImportReportStatesFailuresPlainly() {
        XCTAssertEqual(AppModel.opmlLimit, 8 << 20, "The server and transport accept 8 MiB")
        let clean = AppModel.report(for: OPMLImportSummary(subscribed: 3, folders: 1, deferred: 0, failures: [],
                                                           subscriptionIDs: [4, 5, 1]),
                                    name: "subs.opml", followed: [1, 2])
        XCTAssertEqual(clean.title, "Imported “subs.opml”")
        XCTAssertEqual(clean.detail, "2 new subscriptions. 1 feed you already follow. 1 folder.")
        let failed = AppModel.report(for: OPMLImportSummary(subscribed: 0, folders: 0, deferred: 1, failures: ["bad URL"]), name: "x.opml")
        XCTAssertEqual(failed.title, "Imported “x.opml” with problems")
        XCTAssertTrue(failed.detail.contains("No new subscriptions."))
        XCTAssertTrue(failed.detail.contains("1 feed will be checked shortly."))
        XCTAssertTrue(failed.detail.contains("1 entry could not be imported:\nbad URL"))
    }

    func testIntegerFieldsCommitClampedValuesAndKeepIncompleteText() {
        XCTAssertEqual(IntegerField.parse("400", in: 1...365), 365)
        XCTAssertEqual(IntegerField.parse("0", in: 1...365), 1)
        XCTAssertEqual(IntegerField.parse(" 36 ", in: 1...365), 36)
        XCTAssertEqual(IntegerField.parse("6", in: 62...72), 62, "Only a committed 6 clamps; typing keeps the text")
        XCTAssertNil(IntegerField.parse("", in: 1...365), "Empty text keeps the current value")
        XCTAssertNil(IntegerField.parse("abc", in: 1...365))
        XCTAssertEqual(IntegerField.clamp(80, to: 62...72), 72)
    }

    func testIntegerFieldKeepsAStepperChangeMadeWhileFocused() {
        var editing = IntegerEditing(value: 68)
        // The stepper moves the value while the field has focus.
        editing.valueChanged(to: 69, focused: true)
        var value: Int64 = 69
        editing.commit(&value, in: 62...72)
        XCTAssertEqual(value, 69, "An untouched field does not write its old text back")
        XCTAssertEqual(editing.text, "69")
        editing.typed("7")
        editing.valueChanged(to: 70, focused: true)
        XCTAssertEqual(editing.text, "7", "Typing in progress is not replaced")
        editing.commit(&value, in: 62...72)
        XCTAssertEqual(value, 62, "A committed 7 clamps to the range")
    }

    func testAStaleListFocusRequestDoesNotStealTheNextSearch() throws {
        let model = try makeModel()
        defer { model.signOut() }
        let window = WindowModel(app: model)
        window.go(to: .later)
        window.search.query = "garden"
        window.submitSearch()
        window.endSearch()
        // Later has no list to take the request; then a new search starts.
        window.search.query = "frost"
        window.submitSearch()
        XCTAssertFalse(window.takeListFocusRequest(), "The search field keeps the keyboard")
    }

    func testArticleRowsCarryMarkersAndLinks() throws {
        let model = try makeModel()
        defer { model.signOut() }
        let window = WindowModel(app: model)
        window.command(.keep, entryID: 41)
        let row = try XCTUnwrap(window.rows.first { $0.id == 41 })
        XCTAssertTrue(row.kept)
        XCTAssertFalse(row.later)
        XCTAssertEqual(row.url, URL(string: "https://publisher.test/41"))
        XCTAssertGreaterThan(EntryCell.height(for: row, width: 300), 30)
        let long = EntryRow(id: 1, title: String(repeating: "A long title ", count: 20), subtitle: "Feed", unread: true)
        XCTAssertGreaterThan(EntryCell.height(for: long, width: 300), EntryCell.height(for: row, width: 300),
                            "Long titles wrap to a second line")
        XCTAssertEqual(EntryCell.height(for: long, width: 300), EntryCell.height(for: long, width: 200),
                       "Titles stop at two lines")
    }
}

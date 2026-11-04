import Foundation
import XCTest
import EarnestKit
@testable import Earnest

final class PresentationTests: XCTestCase {
    /// APIState has no public initializer; the wire encoding is the public surface.
    private func state(seenRev: Int64 = 1, shelfRev: Int64 = 1, keptRev: Int64 = 1,
                       seen: String = "unseen", shelf: String = "none", kept: Bool = false) throws -> APIState {
        let json = """
        {"principal":7,"entry_id":42,"state_rev":\(max(seenRev, max(shelfRev, keptRev))),
         "seen":{"value":"\(seen)","rev":\(seenRev),"occurred_at":"","actor":0,"source":""},
         "shelf":{"value":"\(shelf)","rev":\(shelfRev),"occurred_at":"","actor":0,"source":""},
         "kept":{"value":\(kept),"rev":\(keptRev),"occurred_at":"","actor":0,"source":""}}
        """
        return try JSONDecoder().decode(APIState.self, from: Data(json.utf8))
    }

    private func synced(seenRev: Int64 = 0, shelfRev: Int64 = 0, keptRev: Int64 = 0,
                        seen: RegisterValue = .text(""), shelf: RegisterValue = .text(""),
                        kept: RegisterValue = .flag(false)) -> State {
        var state = State(principal: 7, entryID: 42)
        state.seen = Stamp(value: seen, rev: seenRev)
        state.shelf = Stamp(value: shelf, rev: shelfRev)
        state.kept = Stamp(value: kept, rev: keptRev)
        return state
    }

    private func engine(_ state: State) throws -> SyncEngine {
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g", rev: 10))
        try engine.mergeEntry(SyncEntry(id: 42))
        try engine.mergeState(state)
        return engine
    }

    func testNewerFetchedRegisterMergesBeforeOverlay() throws {
        let engine = try engine(synced(shelfRev: 2, shelf: .text("none")))
        try engine.queue(Write(opID: "later", principal: 7, entryID: 42, register: .shelf,
                               value: .text("later"), base: Cursor(generation: "g", rev: 2), occurredAt: Date()))
        let merged = try presentedState(fetched: state(shelfRev: 3, shelf: "done"), engine: engine)
        XCTAssertEqual(merged.shelf.value, .text("later"))
        XCTAssertEqual(try engine.snapshot().states.first?.shelf.value, .text("done"))
    }

    func testFetchWithoutPendingIntentShowsNewerRegister() throws {
        let engine = try engine(synced(shelfRev: 2, shelf: .text("later")))
        let merged = try presentedState(fetched: state(shelfRev: 3, shelf: "done"), engine: engine)
        XCTAssertEqual(merged.shelf.value, RegisterValue.text("done"))
    }

    func testFetchMergesRegistersIndependently() throws {
        let engine = try engine(synced(seenRev: 3, shelfRev: 1, seen: .text("unseen"), shelf: .text("later")))
        let merged = try presentedState(fetched: state(seenRev: 2, shelfRev: 4, seen: "seen", shelf: "done"), engine: engine)
        XCTAssertEqual(merged.seen.value, RegisterValue.text("unseen"))
        XCTAssertEqual(merged.shelf.value, RegisterValue.text("done"))
    }

    func testPrincipalIDFlat() {
        XCTAssertEqual(parsePrincipalID(.object([
            "user_id": .integer(3), "principal_id": .integer(9),
            "kind": .string("human"), "scopes": .array([])])), 9)
    }

    func testPrincipalIDNestedFallback() {
        XCTAssertEqual(parsePrincipalID(.object(["user": .object(["principal_id": .integer(5)])])), 5)
        XCTAssertNil(parsePrincipalID(.object(["kind": .string("human")])))
    }

    func testFeedInfoParses() {
        let feed = FeedInfo(.object([
            "id": .integer(11), "feed_id": .integer(4),
            "title": .string("Example"), "url": .string("https://ex.com/feed"),
            "cadence": .string("week"), "private": .bool(false),
            "health": .object(["consecutive_failures": .integer(2), "error_message": .string("gone")])]))
        XCTAssertEqual(feed?.cadence, "week")
        XCTAssertEqual(feed?.issue, "gone")
        XCTAssertEqual(feed?.id, 11)
    }

    func testActivityItemParsesStateUpdate() {
        let item = ActivityItem(.object([
            "id": .integer(8), "principal_id": .integer(2),
            "principal_name": .string("Morning Agent"),
            "action": .string("state.updated"),
            "target": .object(["principal": .integer(1), "entry_id": .integer(42),
                               "register": .string("shelf")]),
            "before": .object(["shelf": .object(["value": .string("none")])]),
            "after": .object(["shelf": .object(["value": .string("done")])]),
            "next_action": .string("revert"), "reason": .string(""),
            "created_at": .string("2026-01-02T03:04:05Z")]))
        XCTAssertEqual(item.detail, "moved to Done")
        XCTAssertEqual(item.entryID, 42)
        XCTAssertEqual(item.nextAction, "revert")
        XCTAssertEqual(item.principalName, "Morning Agent")
        XCTAssertNotNil(item.createdAt)
    }

    func testBriefItemParses() {
        let brief = BriefItem(.object([
            "id": .integer(3), "principal_id": .integer(2),
            "value": .string("Two items need eyes"),
            "entry_ids": .array([.integer(1), .integer(2)])]))
        XCTAssertEqual(brief?.text, "Two items need eyes")
        XCTAssertEqual(brief?.entryIDs, [1, 2])
    }

    func testDateParsing() {
        XCTAssertNotNil(DateFormatting.parse("2026-01-02T03:04:05Z"))
        XCTAssertNotNil(DateFormatting.parse("2026-01-02T03:04:05.123456789Z"))
        XCTAssertNil(DateFormatting.parse("nope"))
    }

    func testSessionCountsSummary() {
        var counts = SessionCounts()
        counts.record(.done)
        counts.record(.done)
        counts.record(.keep)
        XCTAssertEqual(counts.summary, "1 kept · 2 done this session.")
        counts.record(.keep, unkeep: true)
        XCTAssertEqual(counts.kept, 1)
    }
}

extension PresentationTests {
    func testHeldNotificationUsesTargetID() {
        let item = ActivityItem(.object([
            "id": .integer(8), "action": .string("notification.created"),
            "target": .object(["id": .integer(19)]), "next_action": .string("cancel")
        ]))
        XCTAssertEqual(item.notificationID, 19)
    }

    func testReaderHeaderFallsBackToFirstSeen() throws {
        let json = #"{"id":42,"subscription_id":1,"feed_id":2,"seq":3,"title":"A title","url":"https://example.test/article","author":"Ada","first_seen_at":"2026-11-04T08:00:00Z","rev":1,"deleted":false,"state":{"principal":7,"entry_id":42,"state_rev":0,"seen":{"value":"unseen","rev":0,"occurred_at":"","actor":0,"source":""},"shelf":{"value":"none","rev":0,"occurred_at":"","actor":0,"source":""},"kept":{"value":false,"rev":0,"occurred_at":"","actor":0,"source":""}}}"#
        let entry = try JSONDecoder().decode(APIEntry.self, from: Data(json.utf8))
        let header = ReaderHeader(entry: entry, feedTitle: "Example", openOriginal: {})
        XCTAssertEqual(header.date, DateFormatting.parse(entry.firstSeenAt))
    }
}

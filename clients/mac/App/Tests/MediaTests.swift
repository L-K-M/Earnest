import AppKit
import Foundation
import XCTest
import EarnestKit
import GRDB
@testable import Earnest

@MainActor final class MediaTests: XCTestCase {
    private func entry(_ id: Int64, thumbnail: String? = nil) throws -> APIEntry {
        let thumbnailField = thumbnail.map { ",\"thumbnail_url\":\"\($0)\"" } ?? ""
        let json = """
        {"id":\(id),"subscription_id":1,"feed_id":2,"seq":\(id),"title":"Article \(id)","url":"https://publisher.test/\(id)","author":"Ada","first_seen_at":"2026-11-04T08:00:00Z"\(thumbnailField),"rev":1,"deleted":false,"state":{"principal":7,"entry_id":\(id),"state_rev":0,"seen":{"value":"unseen","rev":0,"occurred_at":"","actor":0,"source":""},"shelf":{"value":"none","rev":0,"occurred_at":"","actor":0,"source":""},"kept":{"value":false,"rev":0,"occurred_at":"","actor":0,"source":""}}}
        """
        return try JSONDecoder().decode(APIEntry.self, from: Data(json.utf8))
    }

    private func makeModel(api: EarnestAPI, entries: [APIEntry]) throws -> AppModel {
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g", rev: 1))
        try engine.mergeEntries(entries, generation: "g")
        try engine.mergeBootstrap(kind: "subscriptions", items: [.object([
            "id": .integer(1), "title": .string("Feed"), "cadence": .string("now"),
            "initial_since": .null, "initial_through_seq": .integer(0),
        ])])
        try engine.finishBootstrap()
        let model = AppModel(api: api, engine: engine, principalID: 7)
        model.loggedIn = true
        return model
    }

    func testRowsCarryThumbnailAndIconPaths() async throws {
        let session = ContentHTTP.install([
            .init(path: "/v1/subscriptions", body: #"[{"id":1,"feed_id":2,"title":"Feed","url":"https://feed.test/rss","site_url":"https://feed.test","folder_id":0,"cadence":"now","extract":"","rev":1,"deleted":false,"private":false,"muted":false,"initial_fetch_pending":false,"initial_through_seq":0,"icon_url":"/v1/subscriptions/1/icon?v=ffeeddcc","health":{"next_fetch_at":"2026-11-05T00:00:00Z","consecutive_failures":0,"http_status":200,"posts_per_day":0,"firehose":false}}]"#),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: ContentTokens(), session: session)
        let model = try makeModel(api: api, entries: [
            try entry(41, thumbnail: "/v1/entries/41/thumbnail?v=a1b2c3"), try entry(42),
        ])
        defer { model.signOut() }
        await model.refreshFeeds()
        XCTAssertEqual(model.feedMap[1]?.iconURL, "/v1/subscriptions/1/icon?v=ffeeddcc")
        let window = WindowModel(app: model)
        window.entries = [try entry(41, thumbnail: "/v1/entries/41/thumbnail?v=a1b2c3"), try entry(42)]
        model.rebuildPresentation()

        let thumbnailRow = try XCTUnwrap(window.rows.first { $0.id == 41 })
        XCTAssertEqual(thumbnailRow.thumbnail, "/v1/entries/41/thumbnail?v=a1b2c3")
        XCTAssertEqual(thumbnailRow.icon, "/v1/subscriptions/1/icon?v=ffeeddcc")
        let plainRow = try XCTUnwrap(window.rows.first { $0.id == 42 })
        XCTAssertNil(plainRow.thumbnail)
        XCTAssertEqual(plainRow.icon, "/v1/subscriptions/1/icon?v=ffeeddcc")
        ContentHTTP.assertFinished()
    }

    // Regression: a load that fails (here a 500 and a dropped connection)
    // waits before it is retried, so a broken image does not cost a request
    // each time its row scrolls past; a 404 is remembered for the session.
    func testFailedLoadsWaitBeforeRetryAndMissesAreRemembered() async throws {
        let session = ContentHTTP.install([
            .init(path: "/v1/entries/9/thumbnail", status: 500, body: "{}"),
            .init(path: "/v1/entries/10/thumbnail", status: 404, body: "{}"),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: ContentTokens(), session: session)
        let store = MediaStore(identity: "account", api: api)
        let broken = await store.image(for: "/v1/entries/9/thumbnail")
        XCTAssertNil(broken)
        let again = await store.image(for: "/v1/entries/9/thumbnail")
        XCTAssertNil(again)
        let absent = await store.image(for: "/v1/entries/10/thumbnail")
        XCTAssertNil(absent)
        let absentAgain = await store.image(for: "/v1/entries/10/thumbnail")
        XCTAssertNil(absentAgain)
        XCTAssertNil(store.cachedImage(for: "/v1/entries/9/thumbnail"))
        // Each path was requested once; a second request would fail the stub.
        ContentHTTP.assertFinished()
    }

    func testEntryCellHeightGrowsOnlyForAThumbnailRow() throws {
        let plain = EntryRow(id: 1, title: "Title", subtitle: "Feed", unread: true)
        let plainHeight = EntryCell.height(for: plain, width: 300)
        var iconOnly = plain
        iconOnly.icon = "/v1/subscriptions/5/icon?v=abc"
        XCTAssertEqual(EntryCell.height(for: iconOnly, width: 300), plainHeight,
                       "A feed icon must not change the row height")
        var withThumb = plain
        withThumb.thumbnail = "/v1/entries/1/thumbnail?v=abc"
        let thumbHeight = EntryCell.height(for: withThumb, width: 300)
        XCTAssertGreaterThanOrEqual(thumbHeight, 7 + 48 + 7, "The thumbnail needs its full slot")
        XCTAssertGreaterThan(thumbHeight, plainHeight)
        // A two-line title already fills the slot; the floor holds either way.
        let longTitle = String(repeating: "A long title ", count: 20)
        let longThumb = EntryRow(id: 1, title: longTitle, subtitle: "Feed", unread: true,
                               thumbnail: "/v1/entries/1/thumbnail?v=abc")
        let longPlain = EntryRow(id: 1, title: longTitle, subtitle: "Feed", unread: true)
        XCTAssertGreaterThanOrEqual(EntryCell.height(for: longThumb, width: 300), 7 + 48 + 7)
        XCTAssertGreaterThanOrEqual(EntryCell.height(for: longThumb, width: 300),
                                    EntryCell.height(for: longPlain, width: 300))
    }

    func testFeedInfoParsesIconURL() throws {
        let feed = FeedInfo(.object([
            "id": .integer(11), "title": .string("Example"),
            "icon_url": .string("/v1/subscriptions/11/icon?v=ff00")]))
        XCTAssertEqual(feed?.iconURL, "/v1/subscriptions/11/icon?v=ff00")
        XCTAssertNil(FeedInfo(.object(["id": .integer(12)]))?.iconURL)
        XCTAssertNil(FeedInfo(.object(["id": .integer(13), "icon_url": .string("")]))?.iconURL)
    }

    func testMediaCopiesPersistAreIsolatedAndAreCleared() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        let identity = try AccountIdentity(server: URL(string: "https://nas.test")!, userID: 1, principalID: 7)
        let cache = try Cache(identity: identity, directory: directory)
        try cache.saveMedia(path: "/v1/entries/1/thumbnail?v=abc", data: Data([1, 2, 3]))
        let reopened = try Cache(identity: identity, directory: directory)
        XCTAssertEqual(try reopened.media(path: "/v1/entries/1/thumbnail?v=abc"), Data([1, 2, 3]))
        XCTAssertNil(try reopened.media(path: "/v1/entries/2/thumbnail?v=abc"))
        for other in [
            try AccountIdentity(server: identity.server, userID: 2, principalID: 7),
            try AccountIdentity(server: identity.server, userID: 1, principalID: 8),
            try AccountIdentity(server: URL(string: "http://nas.test")!, userID: 1, principalID: 7),
        ] {
            let isolated = try Cache(identity: other, directory: directory)
            XCTAssertNil(try isolated.media(path: "/v1/entries/1/thumbnail?v=abc"))
        }
        try reopened.clear()
        XCTAssertNil(try reopened.media(path: "/v1/entries/1/thumbnail?v=abc"))
    }

    func testMediaLimitUsesRecentReadsAndDoesNotEvictAnotherAccount() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        let first = try AccountIdentity(server: URL(string: "https://nas.test")!, userID: 1, principalID: 7)
        let second = try AccountIdentity(server: first.server, userID: 2, principalID: 8)
        let cache = try Cache(identity: first, directory: directory)
        let other = try Cache(identity: second, directory: directory)
        try other.saveMedia(path: "/v1/subscriptions/1/icon?v=abc", data: Data([7]))
        try cache.saveMedia(path: "/v1/entries/1/thumbnail?v=keep", data: Data([9]))
        for index in 0..<(Cache.mediaLimit - 1) {
            try cache.saveMedia(path: "/v1/entries/\(index + 1000)/thumbnail?v=x", data: Data([1]))
        }
        // A read counts as use: the kept path survives the next eviction.
        XCTAssertNotNil(try cache.media(path: "/v1/entries/1/thumbnail?v=keep"))
        try cache.saveMedia(path: "/v1/entries/99999/thumbnail?v=x", data: Data([2]))
        XCTAssertNil(try cache.media(path: "/v1/entries/1000/thumbnail?v=x"), "The oldest copy is evicted")
        XCTAssertNotNil(try cache.media(path: "/v1/entries/1/thumbnail?v=keep"))
        let db = try DatabaseQueue(path: directory.appendingPathComponent("cache.sqlite").path)
        let count = try db.read { db in
            try Int.fetchOne(db, sql: "SELECT COUNT(*) FROM media_copies WHERE identity = ?", arguments: [first.cacheKey])
        }
        XCTAssertEqual(count, Cache.mediaLimit)
        XCTAssertEqual(try other.media(path: "/v1/subscriptions/1/icon?v=abc"), Data([7]), "Another account's copies are untouched")
    }
}

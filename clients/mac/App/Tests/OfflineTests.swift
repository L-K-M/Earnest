import Foundation
import XCTest
import EarnestKit
@testable import Earnest

final class IdentityTests: XCTestCase {
    func testStoreSurvivesRecreationAndKeepsServersSeparate() throws {
        let suite = "Earnest.IdentityTests.\(UUID())"
        let defaults = UserDefaults(suiteName: suite)!
        defer { defaults.removePersistentDomain(forName: suite) }
        let store = IdentityStore(defaults: defaults)
        let first = try identity("https://NAS.test:443/", user: 1, principal: 7)
        let other = try identity("http://nas.test:8080", user: 2, principal: 8)
        try store.save(first)
        try store.save(other)
        XCTAssertEqual(first.server.absoluteString, "https://nas.test")
        XCTAssertEqual(try IdentityStore(defaults: defaults).load(server: URL(string: "https://nas.test/")!), first)
        XCTAssertEqual(try store.load(server: other.server), other)
        XCTAssertNil(try store.load(server: URL(string: "https://elsewhere.test")!))
        let record = try XCTUnwrap(defaults.data(forKey: "verifiedIdentity.https://nas.test"))
        let fields = try XCTUnwrap(JSONSerialization.jsonObject(with: record) as? [String: Any])
        XCTAssertEqual(Set(fields.keys), ["server", "userID", "principalID"])
        store.remove(server: first.server)
        XCTAssertNil(try store.load(server: first.server))
        XCTAssertEqual(try store.load(server: other.server), other)
    }

    func testRejectsInvalidIdentityAndTamperedStoredServer() throws {
        for server in ["https://user:secret@nas.test", "https://nas.test/path", "https://nas.test?q=1", "https://nas.test/#fragment", "file:///tmp/cache"] {
            XCTAssertThrowsError(try identity(server, user: 1, principal: 7))
        }
        XCTAssertThrowsError(try identity("https://nas.test", user: 0, principal: 7))
        XCTAssertThrowsError(try identity("https://nas.test", user: 1, principal: -1))
        let suite = "Earnest.IdentityTests.\(UUID())"
        let defaults = UserDefaults(suiteName: suite)!
        defer { defaults.removePersistentDomain(forName: suite) }
        defaults.set(Data(#"{"server":"https://other.test","userID":1,"principalID":7}"#.utf8), forKey: "verifiedIdentity.https://nas.test")
        XCTAssertThrowsError(try IdentityStore(defaults: defaults).load(server: URL(string: "https://nas.test")!))
    }

    func testOnlyConnectionErrorsReuseRememberedIdentity() throws {
        let remembered = try identity("http://127.0.0.1:18080", user: 1, principal: 7)
        for code in [URLError.Code.cannotConnectToHost, .notConnectedToInternet, .networkConnectionLost, .timedOut, .cannotFindHost, .dnsLookupFailed] {
            let result = Result<APIValue, Error>.failure(URLError(code))
            XCTAssertEqual(try SessionResolution.resolve(server: remembered.server, result: result, remembered: remembered), .offline(remembered))
            XCTAssertThrowsError(try SessionResolution.resolve(server: remembered.server, result: result, remembered: nil))
            XCTAssertThrowsError(try SessionResolution.resolve(server: URL(string: "https://different.test")!, result: result, remembered: remembered))
        }
        let problem = try JSONDecoder().decode(APIProblem.self, from: Data(#"{"type":"about:blank","title":"Unauthorized","status":401}"#.utf8))
        for error: Error in [problem, APIError.httpStatus(401), APIError.httpStatus(403), APIError.httpStatus(500), URLError(.secureConnectionFailed), URLError(.cancelled), APIError.invalidResponse] {
            XCTAssertThrowsError(try SessionResolution.resolve(server: remembered.server, result: .failure(error), remembered: remembered))
        }
    }

    func testSuccessfulMeAlwaysSelectsVerifiedAccount() throws {
        let remembered = try identity("https://nas.test", user: 1, principal: 7)
        for (user, principal) in [(1, 7), (1, 9), (2, 7)] {
            let me = APIValue.object(["user_id": .integer(Int64(user)), "principal_id": .integer(Int64(principal))])
            let verified = try identity("https://nas.test", user: Int64(user), principal: Int64(principal))
            XCTAssertEqual(try SessionResolution.resolve(server: remembered.server, result: .success(me), remembered: remembered), .verified(verified))
        }
        XCTAssertThrowsError(try SessionResolution.resolve(server: remembered.server, result: .success(.object(["principal_id": .integer(7)])), remembered: remembered))
    }

    private func identity(_ server: String, user: Int64, principal: Int64) throws -> AccountIdentity {
        try AccountIdentity(server: URL(string: server)!, userID: user, principalID: principal)
    }
}

final class OfflineCacheTests: XCTestCase {
    func testSnapshotsBriefsAndArticlesAreIsolatedAndDurable() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        let identity = try AccountIdentity(server: URL(string: "https://nas.test")!, userID: 1, principalID: 7)
        let cache = try Cache(identity: identity, directory: directory)
        var snapshot = SyncSnapshot()
        snapshot.cursor = Cursor(generation: "g", rev: 12)
        snapshot.outbox = [.write(Write(opID: "offline", principal: 7, entryID: 42, register: .kept, value: .flag(true), base: snapshot.cursor!, occurredAt: Date(timeIntervalSince1970: 1_700_000_000)))]
        try cache.save(snapshot)
        try cache.saveResource("briefs", values: [.string("Private brief")], generation: "g")
        try cache.saveArticle(entryID: 42, html: "<p>Private article</p>", generation: "g")
        let restored = try Cache(identity: identity, directory: directory)
        XCTAssertEqual(try restored.load(), snapshot)
        XCTAssertEqual(try restored.resource("briefs", generation: "g"), [.string("Private brief")])
        XCTAssertEqual(try restored.article(entryID: 42, generation: "g"), "<p>Private article</p>")
        XCTAssertNil(try restored.article(entryID: 42, generation: "restored"))
        XCTAssertTrue(try restored.resource("briefs", generation: "restored").isEmpty)
        for other in [
            try AccountIdentity(server: identity.server, userID: 2, principalID: 7),
            try AccountIdentity(server: identity.server, userID: 1, principalID: 8),
            try AccountIdentity(server: URL(string: "http://nas.test")!, userID: 1, principalID: 7),
            try AccountIdentity(server: URL(string: "https://nas.test:8443")!, userID: 1, principalID: 7),
        ] {
            let isolated = try Cache(identity: other, directory: directory)
            XCTAssertEqual(try isolated.load(), SyncSnapshot())
            XCTAssertTrue(try isolated.resource("briefs", generation: "g").isEmpty)
            XCTAssertNil(try isolated.article(entryID: 42, generation: "g"))
        }
    }

    func testArticleLimitUsesRecentReadsAndDoesNotEvictAnotherAccount() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        let first = try AccountIdentity(server: URL(string: "https://nas.test")!, userID: 1, principalID: 7)
        let second = try AccountIdentity(server: first.server, userID: 2, principalID: 8)
        let cache = try Cache(identity: first, directory: directory)
        let other = try Cache(identity: second, directory: directory)
        try other.saveArticle(entryID: 1, html: "Other account", generation: "g")
        for id in 1...Cache.articleLimit {
            try cache.saveArticle(entryID: Int64(id), html: "Article \(id)", generation: "g")
        }
        XCTAssertNotNil(try cache.article(entryID: 1, generation: "g"))
        try cache.saveArticle(entryID: 201, html: "Newest", generation: "g")
        XCTAssertNil(try cache.article(entryID: 2, generation: "g"))
        XCTAssertNotNil(try cache.article(entryID: 1, generation: "g"))
        try cache.saveArticle(entryID: 1, html: "Updated", generation: "g")
        let copies = try (1...201).compactMap { try cache.article(entryID: Int64($0), generation: "g") }
        XCTAssertEqual(copies.count, 200)
        XCTAssertEqual(try cache.article(entryID: 1, generation: "g"), "Updated")
        XCTAssertEqual(try other.article(entryID: 1, generation: "g"), "Other account")
    }
}

final class ErrorPresentationTests: XCTestCase {
    func testErrorsUseHumanMessages() throws {
        let connection = NSError(domain: NSURLErrorDomain, code: URLError.cannotConnectToHost.rawValue,
                                 userInfo: [NSURLErrorFailingURLErrorKey: URL(string: "http://127.0.0.1:18080/v1/me")!])
        XCTAssertTrue(ErrorPresentation.isConnection(connection))
        XCTAssertEqual(ErrorPresentation.message(connection), "Offline. Showing what was last synced.")
        XCTAssertEqual(ErrorPresentation.message(APIError.httpStatus(401)), "Your sign-in expired or was revoked. Sign in again.")
        XCTAssertEqual(ErrorPresentation.message(APIError.httpStatus(404)), "This item is no longer available.")
        XCTAssertEqual(ErrorPresentation.message(APIError.invalidResponse), "The server sent an unexpected response. Try again.")
        let local = NSError(domain: "test", code: 1, userInfo: [NSLocalizedDescriptionKey: "Could not save your changes."])
        XCTAssertEqual(ErrorPresentation.message(local), "Could not save your changes.")
        let problem = try JSONDecoder().decode(APIProblem.self, from: Data(#"{"type":"about:blank","title":"Bad request","detail":"Enter a feed URL.","status":400}"#.utf8))
        XCTAssertEqual(ErrorPresentation.message(problem), "Enter a feed URL.")
        XCTAssertFalse(ErrorPresentation.isConnection(APIError.httpStatus(503)))
        XCTAssertFalse(ErrorPresentation.isConnection(URLError(.serverCertificateUntrusted)))
    }
}

final class RiverCacheTests: XCTestCase {
    func testRiverInputsAndNamesPersistOnlyForTheirIdentityAndGeneration() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        addTeardownBlock { try FileManager.default.removeItem(at: directory) }
        let identity = try AccountIdentity(server: URL(string: "https://nas.test")!, userID: 1, principalID: 7)
        let cache = try Cache(identity: identity, directory: directory)
        let engine = SyncEngine(store: cache)
        try engine.bootstrap(Cursor(generation: "g", rev: 0))
        try engine.replaceRiverResources(
            mutes: [.object(["id": .integer(2), "kind": .string("author"), "value": .string("Alice"), "rev": .integer(1)])],
            settings: .object(["today_decay_days": .integer(4), "rev": .integer(2)]))
        let names: [APIValue] = [.object(["id": .integer(2), "name": .string("Morning Agent")])]
        try cache.saveResource("principal_names", values: names, generation: "g")
        let reopened = try Cache(identity: identity, directory: directory)
        let restored = SyncEngine(store: reopened)
        XCTAssertEqual(try restored.object(kind: .mute, id: 2)?.fields["mute_kind"], .string("author"))
        XCTAssertEqual(try restored.object(kind: .settings, id: 1)?.fields["today_decay_days"], .integer(4))
        XCTAssertEqual(try reopened.resource("principal_names", generation: "g"), names)
        XCTAssertTrue(try reopened.resource("principal_names", generation: "new").isEmpty)
        let other = try Cache(identity: AccountIdentity(server: identity.server, userID: 2, principalID: 7), directory: directory)
        XCTAssertTrue(try other.load().objects.isEmpty)
        XCTAssertTrue(try other.resource("principal_names", generation: "g").isEmpty)
    }
}

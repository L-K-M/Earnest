import Foundation
import XCTest
import EarnestKit
@testable import Earnest

@MainActor final class ContentLoadingTests: XCTestCase {
    func testListAndKeyboardSelectionsUseProxiedImages() async throws {
        let (model, session) = try makeModel([
            .signed(mode: "proxied"), .article,
            .signed(mode: "proxied"), .article,
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        model.selectFromList(42)
        try await waitForArticle(model)
        XCTAssertEqual(model.imageOrigin?.absoluteString, "https://content.test:8081")
        XCTAssertEqual(model.readingEntry?.title, "Article title")
        model.rows = [EntryRow(id: 42, title: "Article title", subtitle: "", unread: true)]
        model.move(1)
        try await waitForArticle(model)
        XCTAssertEqual(model.imageMode, .proxied)
        ContentHTTP.assertFinished()
    }

    func testDeepLinkBlocksImagesUntilExplicitLoadIncludingSameEntry() async throws {
        let (model, session) = try makeModel([
            .signed(mode: "proxied"), .article,
            .signed(mode: "click"), .article,
            .signed(mode: "proxied"), .article,
            .signed(mode: "click"), .article,
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        model.selectFromList(42)
        try await waitForArticle(model)
        model.openDeepLink(URL(string: "earnest://entry/42")!)
        XCTAssertNil(model.imageOrigin)
        try await waitForArticle(model)
        XCTAssertEqual(model.imageMode, .click)
        XCTAssertNil(model.imageOrigin)
        model.loadImages()
        try await waitForArticle(model)
        XCTAssertEqual(model.imageOrigin?.absoluteString, "https://content.test:8081")
        model.openDeepLink(URL(string: "earnest://entry/42")!)
        try await waitForArticle(model)
        XCTAssertNil(model.imageOrigin)
        ContentHTTP.assertFinished()
    }

    func testFallbackBlocksOriginalImageURLs() async throws {
        let (model, session) = try makeModel([
            .init(path: "/v1/entries/42/content-url", status: 503, body: "{}", mode: "proxied"),
            .init(path: "/v1/entries/42/content", body: #"{"entry_id":42,"html":"<img src='https://publisher.test/tracker'>","text":"","sanitizer_version":1,"source":"feed"}"#),
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        model.selectFromList(42)
        try await waitForArticle(model)
        XCTAssertTrue(model.html.contains("publisher.test"))
        XCTAssertNil(model.imageOrigin)
        let scheme = ArticleScheme()
        scheme.html = model.html
        scheme.imageOrigin = model.imageOrigin
        XCTAssertTrue(scheme.page.contains("img-src 'none';"))
        ContentHTTP.assertFinished()
    }

    func testCSPAllowsOnlySignedContentOrigin() {
        let scheme = ArticleScheme()
        scheme.html = "<img src='https://publisher.test/tracker'>"
        scheme.imageOrigin = URL(string: "https://content.test:8081")
        XCTAssertTrue(scheme.page.contains("default-src 'none'; img-src https://content.test:8081;"))
        XCTAssertTrue(scheme.page.contains("base-uri 'none'; form-action 'none'"))
    }

    func testNotificationCancellationUsesNotificationTargetAndRefreshesActivity() async throws {
        let (model, session) = try makeModel([
            .init(path: "/v1/notifications/19:cancel", body: "{}", method: "POST"),
            .init(path: "/v1/activity", body: #"{"activities":[]}"#),
        ])
        defer { model.signOut(); session.invalidateAndCancel() }
        let item = ActivityItem(.object([
            "id": .integer(8), "action": .string("notification.created"),
            "target": .object(["id": .integer(19)]), "next_action": .string("cancel")
        ]))
        model.section = .activity
        model.activityGroups = [ActivityGroup(principalID: 2, name: "Scout", items: [item])]
        model.cancelNotification(item)
        XCTAssertTrue(model.activityActionPending)
        let deadline = Date().addingTimeInterval(5)
        while model.activityActionPending && Date() < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertFalse(model.activityActionPending)
        XCTAssertNil(model.error)
        XCTAssertTrue(model.activityGroups.isEmpty)
        ContentHTTP.assertFinished()
    }

    private func waitForArticle(_ model: AppModel) async throws {
        let deadline = Date().addingTimeInterval(5)
        while model.html.isEmpty && model.error == nil && Date() < deadline {
            try await Task.sleep(for: .milliseconds(10))
        }
        XCTAssertNil(model.error)
        XCTAssertFalse(model.html.isEmpty, "Article did not finish loading")
    }

    private func makeModel(_ replies: [ContentHTTP.Reply]) throws -> (AppModel, URLSession) {
        let entry = try JSONDecoder().decode(APIEntry.self, from: Data(Self.entryJSON.utf8))
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "g", rev: 1))
        try engine.mergeEntries([entry], generation: "g")
        try engine.finishBootstrap()
        let session = ContentHTTP.install(replies)
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: ContentTokens(), session: session)
        let model = AppModel(api: api, engine: engine, principalID: 7)
        model.loggedIn = true
        model.entries = [entry]
        return (model, session)
    }

    static let entryJSON = #"{"id":42,"subscription_id":1,"feed_id":2,"seq":3,"title":"Article title","url":"https://publisher.test/article","author":"Ada","first_seen_at":"2026-11-04T08:00:00Z","rev":1,"deleted":false,"state":{"principal":7,"entry_id":42,"state_rev":0,"seen":{"value":"unseen","rev":0,"occurred_at":"","actor":0,"source":""},"shelf":{"value":"none","rev":0,"occurred_at":"","actor":0,"source":""},"kept":{"value":false,"rev":0,"occurred_at":"","actor":0,"source":""}}}"#
}

private struct ContentTokens: TokenStore {
    func load() throws -> String? { "test-token" }
    func save(_ token: String) throws {}
    func delete() throws {}
}

private final class ContentHTTP: URLProtocol {
    struct Reply {
        let path: String
        var status = 200
        let body: String
        var mode: String?
        var method = "GET"

        static func signed(mode: String) -> Self {
            .init(path: "/v1/entries/42/content-url", body: #"{"url":"https://content.test:8081/article/42?signature=abc","expires_at":"2099-01-01T00:00:00Z"}"#, mode: mode)
        }
        static let article = Self(path: "/article/42", body: "<p>Article</p>")
    }
    private static let lock = NSLock()
    private static var replies: [Reply] = []

    static func install(_ replies: [Reply]) -> URLSession {
        lock.withLock { Self.replies = replies }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [Self.self]
        return URLSession(configuration: configuration)
    }

    static func assertFinished() { XCTAssertTrue(lock.withLock { replies.isEmpty }) }
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        guard let reply = Self.lock.withLock({ Self.replies.isEmpty ? nil : Self.replies.removeFirst() }) else {
            XCTFail("Unexpected request: \(request.url?.path ?? "")")
            client?.urlProtocol(self, didFailWithError: URLError(.badServerResponse))
            return
        }
        XCTAssertEqual(request.url?.path, reply.path)
        XCTAssertEqual(request.httpMethod, reply.method)
        if let mode = reply.mode { XCTAssertEqual(request.url?.query, "mode=\(mode)") }
        let isContent = request.url?.host == "content.test"
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), isContent ? nil : "Bearer test-token")
        let response = HTTPURLResponse(url: request.url!, statusCode: reply.status, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(reply.body.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

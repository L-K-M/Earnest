import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import Testing
@testable import EarnestKit

private let opmlDocument = """
<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>Subscriptions</title></head>
  <body>
    <outline text="Example" type="rss" xmlUrl="https://example.test/feed.xml"/>
  </body>
</opml>
"""

/// One subscription, one folder, one deferred outline, and one failure.
private let opmlImportResults = ##"""
[
  {"path":[1],"deferred":false,"subscription":{"id":11,"feed_id":12,"title":"Example","url":"https://example.test/feed.xml","site_url":"https://example.test","folder_id":0,"cadence":"daily","extract":"default","rev":1,"deleted":false,"private":false,"muted":false,"initial_through_seq":0,"initial_fetch_pending":false,"health":{"next_fetch_at":"2099-01-01T00:00:00Z","consecutive_failures":0,"http_status":200,"posts_per_day":0.5,"firehose":false}}},
  {"path":[2],"deferred":false,"folder":{"id":3,"name":"Reading","rev":1,"deleted":false}},
  {"path":[],"deferred":true},
  {"path":[],"deferred":false,"error":"duplicate subscription"}
]
"""##

/// OPML import/export tests. They extend NetworkTests because ScriptedHTTP
/// keeps process-global reply queues; a separate suite would race it.
extension NetworkTests {
    @Test func exportOPMLReturnsExactBytesWithBearerToken() async throws {
        let expected = Data(opmlDocument.utf8)
        let session = ScriptedHTTP.install([
            .init(path: "/v1/opml", body: opmlDocument, contentType: "text/x-opml"),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        let exported = try await api.exportOPML()
        #expect(exported == expected)
        ScriptedHTTP.assertFinished()
    }

    @Test func exportOPMLRejectsBodiesOver16MiB() async throws {
        let oversized = String(repeating: "a", count: 16 * 1024 * 1024 + 1)
        let session = ScriptedHTTP.install([
            .init(path: "/v1/opml", body: oversized, contentType: "text/x-opml"),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        await #expect(throws: APIError.self) { try await api.exportOPML() }
        ScriptedHTTP.assertFinished()
    }

    @Test func importOPMLSendsDocumentAndSummarizesResults() async throws {
        let document = Data(opmlDocument.utf8)
        let session = ScriptedHTTP.install([
            .init(method: "POST", path: "/v1/opml:import", body: opmlImportResults,
                  requestContentType: "text/x-opml",
                  checkBody: { data in
                      let body = try #require(data)
                      #expect(body == document)
                  }),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        let summary = try await api.importOPML(document)
        #expect(summary.subscribed == 1)
        #expect(summary.folders == 1)
        #expect(summary.deferred == 1)
        #expect(summary.failures == ["duplicate subscription"])
        #expect(summary.subscriptionIDs == [11])
        ScriptedHTTP.assertFinished()
    }

    @Test func importOPMLDecodesTheServersResultShape() async throws {
        // As the server answers: fractional-second dates with offsets, and an
        // already-followed feed returned as its existing subscription.
        let reply = ##"[{"path":[0],"folder":{"id":1,"name":"Art","rev":59,"deleted":false},"deferred":false},{"path":[0,0],"subscription":{"initial_since":"2026-11-07T22:40:01.427885+01:00","initial_through_seq":0,"initial_fetch_pending":true,"id":4,"feed_id":4,"title":"Studio Notes","url":"http://127.0.0.1:58090/notes.xml","site_url":"","folder_id":1,"cadence":"today","extract":"","rev":60,"deleted":false,"private":false,"muted":false,"health":{"next_fetch_at":"2026-11-08T22:40:01.425279+01:00","consecutive_failures":0,"http_status":0,"posts_per_day":0,"firehose":false}},"deferred":false},{"path":[1],"subscription":{"initial_since":"2026-11-07T21:19:27.714744+01:00","initial_through_seq":12,"initial_fetch_pending":false,"id":1,"feed_id":1,"title":"Garden Notebook","url":"http://127.0.0.1:58090/garden.xml","site_url":"http://127.0.0.1:58090/garden/","folder_id":0,"cadence":"today","extract":"","rev":15,"deleted":false,"private":false,"muted":false,"health":{"last_success_at":"2026-11-08T21:50:03.825393+01:00","last_attempt_at":"2026-11-08T21:49:59.820415+01:00","next_fetch_at":"2026-11-09T00:51:20.39056+01:00","consecutive_failures":0,"http_status":200,"posts_per_day":8,"firehose":false}},"deferred":false}]"##
        let session = ScriptedHTTP.install([
            .init(method: "POST", path: "/v1/opml:import", body: reply, requestContentType: "text/x-opml"),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        let summary = try await api.importOPML(Data(opmlDocument.utf8))
        #expect(summary.folders == 1)
        #expect(summary.subscriptionIDs == [4, 1])
        #expect(summary.failures.isEmpty)
        ScriptedHTTP.assertFinished()
    }

    @Test func importOPMLNullBodySummarizesZeros() async throws {
        let session = ScriptedHTTP.install([
            .init(method: "POST", path: "/v1/opml:import", body: "null", requestContentType: "text/x-opml"),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        let summary = try await api.importOPML(Data(opmlDocument.utf8))
        #expect(summary == OPMLImportSummary(subscribed: 0, folders: 0, deferred: 0, failures: []))
        ScriptedHTTP.assertFinished()
    }

    @Test func refreshSubscriptionPostsFreshOpID() async throws {
        let session = ScriptedHTTP.install([
            .init(method: "POST", path: "/v1/subscriptions/7/refresh", body: "{}", checkBody: { data in
                let body = try JSONDecoder().decode([String: String].self, from: #require(data))
                #expect(UUID(uuidString: try #require(body["op_id"])) != nil)
            }),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        try await api.refreshSubscription(id: 7)
        ScriptedHTTP.assertFinished()
    }
}

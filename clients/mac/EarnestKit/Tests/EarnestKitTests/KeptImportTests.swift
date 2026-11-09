import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import Testing
@testable import EarnestKit

private func describe(_ skipped: [(feedURL: String, count: Int)]) -> [String] {
    skipped.map { "\($0.feedURL) (\($0.count))" }
}

/// A minimal subscription object, for the report's `subscribed` list.
private func makeSubscription(_ id: Int64) throws -> Components.Schemas.Subscription {
    let json = """
    {"id":\(id),"feed_id":\(id),"title":"Feed","url":"https://example.test/feed","site_url":"https://example.test","folder_id":0,"cadence":"daily","extract":"default","rev":1,"deleted":false,"private":false,"initial_through_seq":0,"initial_fetch_pending":false,"health":{"next_fetch_at":"2099-01-01T00:00:00Z","consecutive_failures":0,"http_status":200,"posts_per_day":0.5,"firehose":false}}
    """
    let value = try JSONDecoder().decode(APIValue.self, from: Data(json.utf8))
    return try adapt(value)
}

@Test func summarizeKeptResultsCountsEveryStatus() throws {
    let report = Components.Schemas.KeptImportReport(results: [
        .init(entryId: 1, feedUrl: "https://a.example/feed", index: 0, status: .created, subscriptionId: 2, title: "An article"),
        .init(entryId: 3, index: 1, status: .matched),
        .init(entryId: 3, index: 2, status: .unchanged),
        .init(index: 3, status: .unkept),
        .init(index: 4, status: .gone),
        .init(feedUrl: "https://b.example/rss", index: 5, status: .notSubscribed, title: "Another"),
        .init(error: "the article has no ID, link or title", index: 6, status: .failed, title: "Broken"),
    ], subscribed: [try makeSubscription(9)])
    let summary = summarizeKeptResults(report)
    #expect(summary.added == 1)
    #expect(summary.existing == 2)
    #expect(summary.unkept == 1)
    #expect(summary.gone == 1)
    #expect(describe(summary.skipped) == ["https://b.example/rss (1)"])
    #expect(summary.failures == ["Broken: the article has no ID, link or title"])
    #expect(summary.subscribed == 1)
}

@Test func summarizeKeptResultsTreatsNullListsAsEmpty() {
    let summary = summarizeKeptResults(Components.Schemas.KeptImportReport(results: nil, subscribed: nil))
    #expect(summary == KeptImportSummary(added: 0, existing: 0, unkept: 0, gone: 0))
    #expect(summarizeKeptResults(nil) == KeptImportSummary(added: 0, existing: 0, unkept: 0, gone: 0))
}

@Test func summarizeKeptResultsSortsSkippedFeedsByCountThenURL() {
    let report = Components.Schemas.KeptImportReport(results: [
        .init(feedUrl: "https://b.example/rss", index: 0, status: .notSubscribed),
        .init(feedUrl: "https://a.example/feed", index: 1, status: .notSubscribed),
        .init(feedUrl: "https://b.example/rss", index: 2, status: .notSubscribed),
        .init(index: 3, status: .notSubscribed),
    ])
    let summary = summarizeKeptResults(report)
    #expect(describe(summary.skipped) == [
        "https://b.example/rss (2)",
        " (1)",
        "https://a.example/feed (1)",
    ])
}

@Test func summarizeKeptResultsFallsBackForMissingTitlesAndErrors() {
    let report = Components.Schemas.KeptImportReport(results: [
        .init(index: 0, status: .failed),
        .init(index: 2, status: .failed, title: ""),
        .init(index: 3, status: .failed, title: "Named"),
    ])
    let summary = summarizeKeptResults(report)
    #expect(summary.failures == [
        "Article 1: could not be imported",
        "Article 3: could not be imported",
        "Named: could not be imported",
    ])
}

/// Kept import tests extend NetworkTests because ScriptedHTTP keeps
/// process-global reply queues; a separate suite would race it.
extension NetworkTests {
    @Test func importKeptSendsTheDocumentAndSubscribeQuery() async throws {
        let document = Data(#"{"items":[{"id":"a","title":"An article"}]}"#.utf8)
        let session = ScriptedHTTP.install([
            .init(method: "POST", path: "/v1/kept:import", body: #"{"results":[],"subscribed":[]}"#,
                  requestContentType: "application/json", checkBody: { data in
                      let received = try JSONDecoder().decode(APIValue.self, from: #require(data))
                      let expected = try JSONDecoder().decode(APIValue.self, from: document)
                      #expect(received == expected)
                  }, query: "missing_feeds=subscribe"),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        let summary = try await api.importKept(document, subscribeMissing: true)
        #expect(summary.added == 0)
        ScriptedHTTP.assertFinished()
    }

    @Test func importKeptSendsSkipQueryByDefault() async throws {
        let document = Data(#"{"items":[]}"#.utf8)
        let session = ScriptedHTTP.install([
            .init(method: "POST", path: "/v1/kept:import", body: #"{"results":[],"subscribed":[]}"#,
                  requestContentType: "application/json", checkBody: { data in
                      let received = try JSONDecoder().decode(APIValue.self, from: #require(data))
                      let expected = try JSONDecoder().decode(APIValue.self, from: document)
                      #expect(received == expected)
                  }, query: "missing_feeds=skip"),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        _ = try await api.importKept(document, subscribeMissing: false)
        ScriptedHTTP.assertFinished()
    }

    @Test func importKeptIgnoresUnknownResultFields() async throws {
        // Labels are counted; a field a later server adds is dropped before
        // the generated decoder, which rejects undeclared keys.
        let session = ScriptedHTTP.install([
            .init(method: "POST", path: "/v1/kept:import",
                  body: #"{"results":[{"index":0,"status":"created","labels":["one"],"future_field":true}],"subscribed":null,"future":1}"#),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        let summary = try await api.importKept(Data(#"{"items":[]}"#.utf8), subscribeMissing: false)
        #expect(summary.added == 1)
        #expect(summary.labelled == 1)
        #expect(summary.subscribed == 0)
        ScriptedHTTP.assertFinished()
    }

    @Test func importKeptRejectsANonJSONObject() async throws {
        let session = ScriptedHTTP.install([])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        await #expect(throws: APIError.self) { try await api.importKept(Data("[]".utf8), subscribeMissing: false) }
        ScriptedHTTP.assertFinished()
    }
}

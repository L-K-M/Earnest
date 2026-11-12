import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import Testing
@testable import EarnestKit

/// The front-page wire contract: every array is nonnull, the representative
/// is the first coverage member, and summary citations survive the round trip.
@Suite struct FrontPageDTOTests {
    private func entryJSON(_ id: Int64, _ title: String, seen: String = "unseen") -> String {
        """
        {"id":\(id),"subscription_id":1,"feed_id":2,"seq":\(id),"title":"\(title)","url":"https://publisher.test/\(id)","author":"Ada","first_seen_at":"2026-11-04T08:00:00Z","rev":1,"deleted":false,"state":{"principal":7,"entry_id":\(id),"state_rev":0,"seen":{"value":"\(seen)","rev":0,"occurred_at":"2026-11-04T08:00:00Z","actor":0,"source":""},"shelf":{"value":"none","rev":0,"occurred_at":"2026-11-04T08:00:00Z","actor":0,"source":""},"kept":{"value":false,"rev":0,"occurred_at":"2026-11-04T08:00:00Z","actor":0,"source":""}}}
        """
    }

    var pageJSON: String {
        """
        {"period":"daily","window_start":"2026-11-03T09:00:00Z","window_end":"2026-11-04T09:00:00Z","selected_at":"2026-11-04T09:00:00Z","revision":"abc123","sections":[{"name":"Across your feeds","stories":[{"id":"cluster:9","entry":\(entryJSON(9, "Lead story")),"source_title":"First Source","published_at":"2026-11-04T08:30:00Z","feed_count":2,"excerpt":"A publisher excerpt.","image_url":"/v1/entries/9/image?v=ff00","coverage":[{"entry":\(entryJSON(9, "Lead story")),"source_title":"First Source"},{"entry":\(entryJSON(10, "Second original")),"source_title":"Second Source"}],"summary":{"claims":[{"text":"The council approved the plan.","citations":[{"entry_id":9,"content_hash":"a" ,"passages":["The council approved the plan."],"verified":true}],"verified":true}],"model":"test-model","prompt_version":"front-page-v1","generated_at":"2026-11-04T09:05:00Z","excerpt_notice":"Some sources gave excerpts only."}},{"id":"entry:11","entry":\(entryJSON(11, "Single source")),"source_title":"Solo Source","published_at":"2026-11-04T07:00:00Z","feed_count":1,"excerpt":"","image_url":null,"coverage":[{"entry":\(entryJSON(11, "Single source")),"source_title":"Solo Source"}]}]}]}
        """
    }

    @Test func frontPageDecodesEveryFieldAndPreservesOrder() throws {
        let page = try JSONDecoder().decode(FrontPage.self, from: Data(pageJSON.utf8))
        #expect(page.period == .daily)
        #expect(page.revision == "abc123")
        #expect(page.windowStart == "2026-11-03T09:00:00Z")
        #expect(page.sections.count == 1)
        let stories = try #require(page.sections.first?.stories)
        #expect(stories.count == 2)
        let lead = try #require(stories.first)
        #expect(lead.id == "cluster:9")
        #expect(lead.entry.id == 9)
        #expect(lead.sourceTitle == "First Source")
        #expect(lead.feedCount == 2)
        #expect(lead.imageURL == "/v1/entries/9/image?v=ff00")
        #expect(lead.coverage.map(\.entry.id) == [9, 10], "Coverage keeps the server's newest-first order")
        let summary = try #require(lead.summary)
        #expect(summary.model == "test-model")
        #expect(summary.promptVersion == "front-page-v1")
        #expect(summary.excerptNotice == "Some sources gave excerpts only.")
        let claims = try #require(summary.claims)
        #expect(claims.count == 1)
        #expect(claims.first?.text == "The council approved the plan.")
        #expect(claims.first?.citations.first?.entryID == 9)
        #expect(claims.first?.citations.first?.passages == ["The council approved the plan."])
        #expect(claims.first?.citations.first?.verified == true)
        let single = try #require(stories.last)
        #expect(single.imageURL == nil)
        #expect(single.summary == nil)
        #expect(single.coverage.count == 1)
    }

    @Test func frontPageRoundTripsThroughTheCacheEncoding() throws {
        let page = try JSONDecoder().decode(FrontPage.self, from: Data(pageJSON.utf8))
        let data = try JSONEncoder().encode(page)
        let restored = try JSONDecoder().decode(FrontPage.self, from: data)
        #expect(restored.revision == page.revision)
        #expect(restored.sections.flatMap(\.stories).map(\.entry.id) == [9, 11])
        #expect(restored.sections.flatMap(\.stories).compactMap(\.summary?.claims.count) == [1])
    }

    @Test func unknownPeriodReadsAsDaily() throws {
        let raw = Data(#"{"period":"monthly","window_start":"2026-11-03T09:00:00Z","window_end":"2026-11-04T09:00:00Z","selected_at":"2026-11-04T09:00:00Z","revision":"r","sections":[]}"#.utf8)
        #expect(try JSONDecoder().decode(FrontPage.self, from: raw).period == .daily)
    }
}

/// HTTP-facing front-page tests share NetworkTests' serialized suite so the
/// process-wide URLProtocol replies never cross.
extension NetworkTests {
    @Test func repositoryFetchesTheRequestedFrontPagePeriod() async throws {
        let entry = FrontPageDTOTests().pageJSON
        let session = ScriptedHTTP.install([
            .init(path: "/v1/front-page", body: entry, query: "period=weekly"),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        let page = try await api.frontPage(period: .weekly)
        #expect(page.sections.flatMap(\.stories).map(\.entry.id) == [9, 11])
        ScriptedHTTP.assertFinished()
    }

    @Test func mediaFetchesTheLargerFeatureImage() async throws {
        let session = ScriptedHTTP.install([
            .init(path: "/v1/entries/9/image", body: "feature-bytes", contentType: "image/jpeg",
                  query: "v=ff00", accept: "image/png, image/jpeg"),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        #expect(try await api.media(path: "/v1/entries/9/image?v=ff00") == Data("feature-bytes".utf8))
        ScriptedHTTP.assertFinished()
    }

    @Test func mediaStillRejectsForeignImagePaths() async throws {
        let session = ScriptedHTTP.install([])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        for path in [
            "/v1/entries/1/image?x=1",
            "/v1/entries/1/image?v=",
            "/v1/entries/1/image?v=a b",
            "/v1/subscriptions/1/image",
            "/v1/subscriptions/1/icon?x=1",
        ] {
            await #expect(throws: APIError.self, "Path \(path.debugDescription) must not be fetched") {
                try await api.media(path: path)
            }
        }
        ScriptedHTTP.assertFinished()
    }
}

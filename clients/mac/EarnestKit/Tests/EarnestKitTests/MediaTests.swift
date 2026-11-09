import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif
import Testing
@testable import EarnestKit

/// Media fetch tests extend NetworkTests because ScriptedHTTP keeps
/// process-wide state that must run serialized.
extension NetworkTests {
    @Test func mediaFetchesVersionedThumbnailWithBearerAndAccept() async throws {
        let session = ScriptedHTTP.install([
            .init(path: "/v1/entries/123/thumbnail", body: "png-bytes", contentType: "image/png",
                  query: "v=0a1b2c3d4e5f6071", accept: "image/png, image/jpeg"),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        #expect(try await api.media(path: "/v1/entries/123/thumbnail?v=0a1b2c3d4e5f6071") == Data("png-bytes".utf8))
        ScriptedHTTP.assertFinished()
    }

    @Test func mediaFetchesUnversionedIcon() async throws {
        let session = ScriptedHTTP.install([
            .init(path: "/v1/subscriptions/5/icon", body: "jpeg-bytes", contentType: "image/jpeg",
                  accept: "image/png, image/jpeg"),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        #expect(try await api.media(path: "/v1/subscriptions/5/icon") == Data("jpeg-bytes".utf8))
        ScriptedHTTP.assertFinished()
    }

    /// Anything beyond the shapes the server generates throws before any
    /// request can leave, so the bearer token never reaches another URL.
    @Test func mediaRejectsForeignPathsWithoutARequest() async throws {
        let session = ScriptedHTTP.install([])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        for path in [
            "/v1/me",
            "/v1/entries",
            "/v1/entries/0/thumbnail",
            "/v1/entries/-1/thumbnail",
            "/v1/entries/abc/thumbnail",
            "/v1/entries/12345678901234567890/thumbnail",
            "/v1/entries/1/other",
            "/v1/entries/1/ICON",
            "/v1/entries/1/../icon",
            "/v1/entries/1/thumbnail?v=",
            "/v1/entries/1/thumbnail?x=1",
            "/v1/entries/1/thumbnail?v=a b",
            "/v1/subscriptions/5/icon?v=with+plus",
            "/v1/entries/1/thumbnail?v=abc&x=1",
            "https://other.test/v1/entries/1/thumbnail",
            "v1/entries/1/thumbnail",
            "",
        ] {
            await #expect(throws: APIError.self, "Path \(path.debugDescription) must not be fetched") {
                try await api.media(path: path)
            }
        }
        ScriptedHTTP.assertFinished()
    }

    @Test func mediaReturnsNilFor404() async throws {
        let session = ScriptedHTTP.install([
            .init(path: "/v1/entries/7/thumbnail", status: 404, body: "{}", query: "v=abc"),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        #expect(try await api.media(path: "/v1/entries/7/thumbnail?v=abc") == nil)
        ScriptedHTTP.assertFinished()
    }

    @Test func mediaRejectsWrongContentTypeEmptyAndOversizedBodies() async throws {
        let session = ScriptedHTTP.install([
            .init(path: "/v1/entries/1/thumbnail", body: "<html></html>", contentType: "text/html", query: "v=a"),
            .init(path: "/v1/entries/2/thumbnail", body: "", contentType: "image/png", query: "v=b"),
            .init(path: "/v1/entries/3/thumbnail", body: String(repeating: "x", count: 2 * 1024 * 1024 + 1),
                  contentType: "image/png", query: "v=c"),
            .init(path: "/v1/entries/4/thumbnail", status: 500, body: "{}", query: "v=d"),
        ])
        defer { session.invalidateAndCancel() }
        let api = try EarnestAPI(baseURL: URL(string: "https://api.test")!, tokenStore: TestTokens(), session: session)
        for path in [
            "/v1/entries/1/thumbnail?v=a",
            "/v1/entries/2/thumbnail?v=b",
            "/v1/entries/3/thumbnail?v=c",
            "/v1/entries/4/thumbnail?v=d",
        ] {
            await #expect(throws: (any Error).self) { try await api.media(path: path) }
        }
        ScriptedHTTP.assertFinished()
    }
}

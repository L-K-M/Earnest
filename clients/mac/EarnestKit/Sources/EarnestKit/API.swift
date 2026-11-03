import Foundation
#if canImport(FoundationNetworking)
import FoundationNetworking
#endif

/// JSON payloads whose shape depends on the change or bootstrap kind.
public indirect enum APIValue: Codable, Sendable, Equatable {
    case null, bool(Bool), number(Double), string(String), array([APIValue]), object([String: APIValue])

    public init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() { self = .null }
        else if let v = try? c.decode(Bool.self) { self = .bool(v) }
        else if let v = try? c.decode(Int64.self) { self = .integer(v) }
        else if let v = try? c.decode(Double.self) { self = .number(v) }
        else if let v = try? c.decode(String.self) { self = .string(v) }
        else if let v = try? c.decode([APIValue].self) { self = .array(v) }
        else { self = .object(try c.decode([String: APIValue].self)) }
    }
    case integer(Int64)
    public func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        switch self {
        case .null: try c.encodeNil()
        case .bool(let v): try c.encode(v)
        case .integer(let v): try c.encode(v)
        case .number(let v): try c.encode(v)
        case .string(let v): try c.encode(v)
        case .array(let v): try c.encode(v)
        case .object(let v): try c.encode(v)
        }
    }
}

public struct APIRegister: Codable, Sendable {
    public let value: APIValue
    public let rev: Int64
    public let occurredAt: String
    public let actor: Int64
    public let source: String
    enum CodingKeys: String, CodingKey { case value, rev, occurredAt = "occurred_at", actor, source }
}
public struct APIState: Codable, Sendable {
    public let principal: Int64
    public let entryID: Int64
    public let seen, shelf, kept: APIRegister
    public let stateRev: Int64
    enum CodingKeys: String, CodingKey { case principal, entryID = "entry_id", seen, shelf, kept, stateRev = "state_rev" }
}
public struct APIEntry: Codable, Sendable, Identifiable {
    public let id, subscriptionID, feedID, seq: Int64
    public let firstSeenAt, title, url, author: String
    public let publishedAt, updatedAt: String?
    public let rev: Int64
    public let deleted: Bool
    public let state: APIState
    enum CodingKeys: String, CodingKey {
        case id, subscriptionID = "subscription_id", feedID = "feed_id", seq, firstSeenAt = "first_seen_at"
        case title, url, author, publishedAt = "published_at", updatedAt = "updated_at", rev, deleted, state
    }
}
public struct EntriesPage: Codable, Sendable {
    public let entries: [APIEntry]?
    public let nextCursor: String?
    enum CodingKeys: String, CodingKey { case entries, nextCursor = "next_cursor" }
}
public struct ArticleContent: Codable, Sendable {
    public let entryID: Int64
    public let html, text: String
    public let sanitizerVersion: Int64
    public let source: String
    enum CodingKeys: String, CodingKey { case entryID = "entry_id", html, text, sanitizerVersion = "sanitizer_version", source }
}
private struct SignedContentURL: Decodable {
    let url: URL
    let contentOrigin: URL
    enum CodingKeys: String, CodingKey { case url, contentOrigin = "content_origin" }
}
public struct APIRevision: Codable, Sendable {
    public let generation: String
    public let rev: Int64
}
public struct APIBootstrapPage: Codable, Sendable {
    public let items: [APIValue]?
    public let nextID, nextPrincipal: Int64
    public let nextView: String
    public let hasMore: Bool
    enum CodingKeys: String, CodingKey { case items, nextID = "next_id", nextPrincipal = "next_principal", nextView = "next_view", hasMore = "has_more" }
}
public struct APIChange: Codable, Sendable {
    public let rev: Int64
    public let type: String
    public let payload: APIValue
}
public struct APIChangesPage: Codable, Sendable {
    public let changes: [APIChange]?
    public let next: APIRevision
    public let nextCursor: String
    enum CodingKeys: String, CodingKey { case changes, next, nextCursor = "next_cursor" }
}
public struct APIProblem: Codable, Error, Sendable {
    public let type, title: String
    public let status: Int
    public let detail, error: String?
    public let budget: APIValue?
}
public enum APIError: Error, Sendable {
    case invalidBaseURL, invalidResponse, httpStatus(Int), invalidEntryID
}

private final class RejectRedirects: NSObject, URLSessionTaskDelegate, @unchecked Sendable {
    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse, newRequest request: URLRequest, completionHandler: @escaping @Sendable (URLRequest?) -> Void) {
        completionHandler(nil)
    }
}

/// Repository for the REST endpoints used by the native client.
public final class EarnestAPI: Sendable {
    private let baseURL: URL
    private let tokenStore: any TokenStore
    private let session: URLSession

    public init(baseURL: URL, tokenStore: any TokenStore) throws {
        guard let scheme = baseURL.scheme?.lowercased(), ["https", "http"].contains(scheme),
              baseURL.host != nil, baseURL.user == nil, baseURL.password == nil,
              baseURL.query == nil, baseURL.fragment == nil else { throw APIError.invalidBaseURL }
        self.baseURL = baseURL
        self.tokenStore = tokenStore
        self.session = URLSession(configuration: .ephemeral, delegate: RejectRedirects(), delegateQueue: nil)
    }

    private func request<T: Decodable>(_ path: String, method: String = "GET", query: [URLQueryItem] = [], body: Data? = nil, authenticated: Bool = true) async throws -> T {
        var components = URLComponents(url: baseURL.appendingPathComponent(path), resolvingAgainstBaseURL: false)!
        if !query.isEmpty { components.queryItems = query }
        guard let url = components.url else { throw APIError.invalidBaseURL }
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.httpBody = body
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        if body != nil { request.setValue("application/json", forHTTPHeaderField: "Content-Type") }
        if authenticated, let token = try tokenStore.load() { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        let (data, response) = try await session.data(for: request)
        guard let response = response as? HTTPURLResponse else { throw APIError.invalidResponse }
        guard (200..<300).contains(response.statusCode) else {
            if let problem = try? JSONDecoder().decode(APIProblem.self, from: data) { throw problem }
            throw APIError.httpStatus(response.statusCode)
        }
        return try JSONDecoder().decode(T.self, from: data)
    }

    public func startDevice(clientName: String) async throws -> DeviceAuthorization {
        try await request("v1/devices/start", method: "POST", body: JSONEncoder().encode(["client_name": clientName]), authenticated: false)
    }
    /// Poll once at the server-provided interval; persist only a successful bearer token.
    public func pollDevice(deviceCode: String) async throws -> DeviceToken {
        let token: DeviceToken = try await request("v1/devices/poll", method: "POST", body: JSONEncoder().encode(["device_code": deviceCode]), authenticated: false)
        guard token.tokenType.lowercased() == "bearer", !token.accessToken.isEmpty else { throw APIError.invalidResponse }
        try tokenStore.save(token.accessToken)
        return token
    }
    public func entries(view: String? = nil, cursor: String? = nil) async throws -> EntriesPage {
        var query: [URLQueryItem] = []
        if let view { query.append(URLQueryItem(name: "view", value: view)) }
        if let cursor { query.append(URLQueryItem(name: "cursor", value: cursor)) }
        return try await request("v1/entries", query: query)
    }
    public func content(entryID: Int64) async throws -> ArticleContent {
        guard entryID > 0 else { throw APIError.invalidEntryID }
        return try await request("v1/entries/\(entryID)/content")
    }
    /// Fetch a signed article without forwarding the API bearer token.
    public func signedContent(entryID: Int64) async throws -> String {
        guard entryID > 0 else { throw APIError.invalidEntryID }
        let signed: SignedContentURL = try await request("v1/entries/\(entryID)/content-url")
        let url = signed.url
        let origin = signed.contentOrigin
        guard let scheme = url.scheme?.lowercased(), ["http", "https"].contains(scheme),
              let host = url.host?.lowercased(), !host.isEmpty,
              url.user == nil, url.password == nil, url.fragment == nil,
              origin.scheme?.lowercased() == scheme, origin.host?.lowercased() == host,
              (origin.port ?? (scheme == "https" ? 443 : 80)) == (url.port ?? (scheme == "https" ? 443 : 80)),
              origin.user == nil, origin.password == nil, origin.query == nil, origin.fragment == nil,
              origin.path.isEmpty || origin.path == "/" else { throw APIError.invalidResponse }
        let (data, response) = try await session.data(from: url)
        guard let response = response as? HTTPURLResponse, (200..<300).contains(response.statusCode),
              let html = String(data: data, encoding: .utf8) else { throw APIError.invalidResponse }
        return html
    }
    public func bootstrapCursor() async throws -> APIRevision { try await request("v1/bootstrap") }
    public func bootstrapPage(kind: String, query: [URLQueryItem] = []) async throws -> APIBootstrapPage {
        guard ["entries", "state", "subscriptions", "folders", "labels", "label_memberships", "positions"].contains(kind) else { throw APIError.invalidResponse }
        return try await request("v1/bootstrap/\(kind)", query: query)
    }
    public func writeBatch(_ writes: [APIValue]) async throws -> [APIValue]? {
        try await request("v1/state:batch", method: "POST", body: JSONEncoder().encode(writes))
    }
    public func changes(cursor: String) async throws -> APIChangesPage {
        try await request("v1/changes", query: [URLQueryItem(name: "cursor", value: cursor)])
    }
}

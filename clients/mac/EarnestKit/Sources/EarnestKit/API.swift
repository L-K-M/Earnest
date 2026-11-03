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
    public let status: String?
    public let groups: [ClusterGroup]?
    enum CodingKeys: String, CodingKey { case entries, nextCursor = "next_cursor", status, groups }
}
/// One story in a clustered page: its representative entry and how many
/// other entries tell the same story ("also in 3 others").
public struct ClusterGroup: Codable, Sendable {
    public let entry: APIEntry
    public let clusterID: Int64
    public let alsoIn: Int64
    enum CodingKeys: String, CodingKey { case entry, clusterID = "cluster_id", alsoIn = "also_in" }
}
public struct ArticleContent: Codable, Sendable {
    public let entryID: Int64
    public let html, text: String
    public let sanitizerVersion: Int64
    public let source: String
    enum CodingKeys: String, CodingKey { case entryID = "entry_id", html, text, sanitizerVersion = "sanitizer_version", source }
}
struct SignedContentURL: Codable, Sendable {
    let url: URL
    let expiresAt: String
    enum CodingKeys: String, CodingKey { case url, expiresAt = "expires_at" }
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
    private let client: Client

    public init(baseURL: URL, tokenStore: any TokenStore, session: URLSession? = nil) throws {
        guard let scheme = baseURL.scheme?.lowercased(), ["https", "http"].contains(scheme),
              baseURL.host != nil, baseURL.user == nil, baseURL.password == nil,
              baseURL.query == nil, baseURL.fragment == nil else { throw APIError.invalidBaseURL }
        self.baseURL = baseURL
        self.tokenStore = tokenStore
        let session = session ?? URLSession(configuration: .ephemeral, delegate: RejectRedirects(), delegateQueue: nil)
        self.session = session
        client = Client(serverURL: baseURL, transport: APITransport(session: session, tokenStore: tokenStore))
    }

    public func startDevice(clientName: String) async throws -> DeviceAuthorization {
        try await generated { try adapt(try await client.deviceStart(.init(body: .json(.init(clientName: clientName)))).ok.body.json) }
    }
    public func pollDevice(deviceCode: String) async throws -> DeviceToken {
        let token: DeviceToken = try await generated { try adapt(try await client.devicePoll(.init(body: .json(.init(deviceCode: deviceCode)))).ok.body.json) }
        guard token.tokenType.lowercased() == "bearer", !token.accessToken.isEmpty else { throw APIError.invalidResponse }
        try Task.checkCancellation()
        try tokenStore.save(token.accessToken)
        return token
    }
    public func entries(view: String? = nil, cursor: String? = nil, cadence: String? = nil) async throws -> EntriesPage {
        let query = Operations.ListEntries.Input.Query(view: view.flatMap { .init(rawValue: $0) }, cadence: cadence.flatMap { .init(rawValue: $0) }, cursor: cursor)
        if view != nil && query.view == nil || cadence != nil && query.cadence == nil { throw APIError.invalidResponse }
        return try await generated { try adapt(try await client.listEntries(.init(query: query)).ok.body.json) }
    }
    public func entry(id: Int64) async throws -> APIEntry {
        guard id > 0 else { throw APIError.invalidEntryID }
        return try await generated { try adapt(try await client.getEntry(.init(path: .init(id: id))).ok.body.json) }
    }
    public func content(entryID: Int64) async throws -> ArticleContent {
        guard entryID > 0 else { throw APIError.invalidEntryID }
        return try await generated { try adapt(try await client.entryContent(.init(path: .init(id: entryID))).ok.body.json) }
    }
    /// This endpoint is being added in parallel and is not yet in the committed OpenAPI document.
    public func signedContent(entryID: Int64) async throws -> String {
        guard entryID > 0 else { throw APIError.invalidEntryID }
        var request = URLRequest(url: baseURL.appendingPathComponent("v1/entries/\(entryID)/content-url"))
        if let token = try tokenStore.load() { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        let (data, response) = try await session.data(for: request)
        guard let response = response as? HTTPURLResponse else { throw APIError.invalidResponse }
        try validateHTTP(response, data: data)
        let signed = try JSONDecoder().decode(SignedContentURL.self, from: data)
        let url = signed.url
        guard let scheme = url.scheme?.lowercased(), ["http", "https"].contains(scheme),
              url.host != nil, url.user == nil, url.password == nil, url.fragment == nil,
              let expiry = parseAPIDate(signed.expiresAt), expiry > Date() else { throw APIError.invalidResponse }
        // The authenticated server selects its content origin. Never attach API credentials here.
        var unsigned = URLRequest(url: url)
        unsigned.httpShouldHandleCookies = false
        let (htmlData, htmlResponse) = try await session.data(for: unsigned)
        guard let http = htmlResponse as? HTTPURLResponse else { throw APIError.invalidResponse }
        try validateHTTP(http, data: htmlData)
        guard let html = String(data: htmlData, encoding: .utf8) else { throw APIError.invalidResponse }
        return html
    }
    public func bootstrapCursor() async throws -> APIRevision {
        try await generated { try adapt(try await client.bootstrapCursor(.init()).ok.body.json) }
    }
    public func bootstrapPage(kind: String, query: [URLQueryItem] = []) async throws -> APIBootstrapPage {
        let values = Dictionary(query.map { ($0.name, $0.value ?? "") }, uniquingKeysWith: { _, last in last })
        return try await generated {
            try adapt(try await client.bootstrapPage(.init(path: .init(kind: kind), query: .init(
                afterId: values["after_id"].flatMap(Int64.init), afterPrincipal: values["after_principal"].flatMap(Int64.init),
                afterView: values["after_view"], limit: 1000, since: values["since"].flatMap(parseAPIDate)
            ))).ok.body.json)
        }
    }
    public func writeBatch(_ writes: [APIValue]) async throws -> [APIValue]? {
        try await generated { try adapt(try await client.stateBatch(.init(body: .json(try adapt(writes)))).ok.body.json) }
    }
    public func writeRange(_ range: APIValue) async throws -> APIValue {
        try await generated { try adapt(try await client.stateThrough(.init(body: .json(try adapt(range)))).ok.body.json) }
    }
    public func changes(cursor: String) async throws -> APIChangesPage {
        try await generated { try adapt(try await client.getChanges(.init(query: .init(cursor: cursor, limit: 1000))).ok.body.json) }
    }
    public func me() async throws -> APIValue {
        try await generated { try adapt(try await client.getMe(.init()).ok.body.json) }
    }
    public func subscriptions() async throws -> [APIValue] {
        try await generated { try adapt(try await client.listSubscriptions(.init()).ok.body.json ?? []) }
    }
    public func subscribe(url: String, cadence: String) async throws -> APIValue {
        try await generated { try adapt(try await client.subscribe(.init(body: .json(.init(cadence: cadence, opId: UUID().uuidString, url: url)))).ok.body.json) }
    }
    public func setCadence(subscriptionID: Int64, cadence: String) async throws -> APIValue {
        try await generated { try adapt(try await client.patchSubscription(.init(path: .init(id: subscriptionID), body: .json(.init(cadence: cadence, opId: UUID().uuidString)))).ok.body.json) }
    }
    public func unsubscribe(subscriptionID: Int64) async throws {
        _ = try await generated { try await client.unsubscribe(.init(path: .init(id: subscriptionID), query: .init(opId: UUID().uuidString))).ok.body.json }
    }
    public func activity() async throws -> [APIValue] {
        try await generated { try adapt(try await client.listActivity(.init(query: .init(limit: 1000))).ok.body.json.activities ?? []) }
    }
    public func agentChanges(since: Date = Date().addingTimeInterval(-86400)) async throws -> [APIValue] {
        try await generated { try adapt(try await client.agentChanges(.init(query: .init(since: since))).ok.body.json ?? []) }
    }
    public func revertActivity(id: Int64) async throws -> APIValue {
        try await generated { try adapt(try await client.revertActivity(.init(path: .init(id: id), body: .json(.init(opId: UUID().uuidString)))).ok.body.json) }
    }
    /// Token metadata supplies agent names without exposing token secrets.
    public func agentNames() async throws -> [Int64: String] {
        let tokens = try await generated { try await client.listTokens(.init()).ok.body.json ?? [] }
        var names: [Int64: String] = [:]
        for token in tokens where token.kind == "agent" { names[token.principalId] = token.name }
        return names
    }
    public func todayBriefs() async throws -> [APIValue] {
        try await generated { try adapt(try await client.todayBriefs(.init()).ok.body.json ?? []) }
    }
    public func eventHints(cursor: String? = nil) throws -> AsyncThrowingStream<APIRevision, Error> {
        eventStream(baseURL: baseURL, token: try tokenStore.load(), cursor: cursor)
    }
    public func annotations(afterID: Int64 = 0) async throws -> [APIValue] {
        try await generated { try adapt(try await client.listAnnotations(.init(query: .init(includeDeleted: true, afterId: afterID, limit: 1000))).ok.body.json ?? []) }
    }
}

func parseAPIDate(_ text: String) -> Date? {
    let fractional = ISO8601DateFormatter()
    fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    return fractional.date(from: text) ?? ISO8601DateFormatter().date(from: text)
}

import Foundation
import HTTPTypes
import OpenAPIRuntime
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
    public let blurb: String?
    public let rev: Int64
    public let deleted: Bool
    public let state: APIState
    /// Server path of the thumbnail stored at ingest; the app does not show it yet.
    public let thumbnailURL: String?
    enum CodingKeys: String, CodingKey {
        case id, subscriptionID = "subscription_id", feedID = "feed_id", seq, firstSeenAt = "first_seen_at"
        case title, url, author, publishedAt = "published_at", updatedAt = "updated_at", blurb, rev, deleted, state
        case thumbnailURL = "thumbnail_url"
    }
}
public struct EntriesPage: Codable, Sendable {
    public let entries: [APIEntry]?
    public let nextCursor: String?
    public let status: String?
    public let groups: [ClusterGroup]?
    /// Full match count for a dry run; may exceed `entries?.count` because
    /// the sample is capped.
    public let total: Int64?
    enum CodingKeys: String, CodingKey { case entries, nextCursor = "next_cursor", status, groups, total }
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
public enum ArticleImageMode: String, Sendable {
    case proxied, click
}

/// Only proxied signed content may authorize images on its content origin.
public struct SignedArticle: Sendable {
    public let html: String
    public let imageOrigin: URL?
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
    case invalidBaseURL, invalidResponse, httpStatus(Int), invalidEntryID, invalidDocument
}

/// Outcome of an OPML import: how many outlines resolved to a subscription or
/// folder, new or already present, how many were deferred, and the
/// per-outline errors, in order.
public struct OPMLImportSummary: Equatable, Sendable {
    public let subscribed: Int
    public let folders: Int
    public let deferred: Int
    public let failures: [String]
    /// The subscriptions the document resolved to, new or already followed,
    /// in document order without repeats.
    public let subscriptionIDs: [Int64]

    public init(subscribed: Int, folders: Int, deferred: Int, failures: [String], subscriptionIDs: [Int64] = []) {
        self.subscribed = subscribed
        self.folders = folders
        self.deferred = deferred
        self.failures = failures
        self.subscriptionIDs = subscriptionIDs
    }
}

/// Outcome of a starred-articles import: how many articles it added, found
/// already present, left out or lost, the feeds it skipped, and the failures.
public struct KeptImportSummary: Equatable, Sendable {
    public let added: Int
    /// Articles Earnest already had, matched or unchanged.
    public let existing: Int
    public let unkept: Int
    public let gone: Int
    /// Feeds whose articles were skipped, with how many each, sorted by count
    /// descending then URL.
    public let skipped: [(feedURL: String, count: Int)]
    /// "<title or Article n>: <error>" for each rejected article.
    public let failures: [String]
    public let subscribed: Int
    /// Articles the import added at least one of the export's labels to.
    public let labelled: Int

    public init(added: Int, existing: Int, unkept: Int, gone: Int,
                skipped: [(feedURL: String, count: Int)] = [], failures: [String] = [], subscribed: Int = 0, labelled: Int = 0) {
        self.added = added
        self.existing = existing
        self.unkept = unkept
        self.gone = gone
        self.skipped = skipped
        self.failures = failures
        self.subscribed = subscribed
        self.labelled = labelled
    }

    public static func == (lhs: KeptImportSummary, rhs: KeptImportSummary) -> Bool {
        lhs.added == rhs.added && lhs.existing == rhs.existing && lhs.unkept == rhs.unkept && lhs.gone == rhs.gone
            && lhs.subscribed == rhs.subscribed && lhs.labelled == rhs.labelled && lhs.failures == rhs.failures
            && lhs.skipped.count == rhs.skipped.count
            && zip(lhs.skipped, rhs.skipped).allSatisfy { $0.feedURL == $1.feedURL && $0.count == $1.count }
    }
}

/// OPML exports are bounded so a runaway response cannot exhaust memory.
private let opmlDocumentLimit = 16 * 1024 * 1024

/// The largest starred-articles export the server accepts.
let keptImportLimit = 64 * 1024 * 1024

private final class RejectRedirects: NSObject, URLSessionTaskDelegate, @unchecked Sendable {
    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse, newRequest request: URLRequest, completionHandler: @escaping @Sendable (URLRequest?) -> Void) {
        completionHandler(nil)
    }
}

/// The generated client rejects unknown response fields. A future server can
/// add an optional field to a kept-import result (for example `labels`), so
/// drop keys the schema does not declare before the generated decoder runs.
private struct KeptImportTransport: ClientTransport {
    private static let reportKeys: Set<String> = ["results", "subscribed"]
    private static let resultKeys: Set<String> = ["index", "status", "entry_id", "subscription_id", "feed_url", "title", "error", "labels"]

    let base: APITransport

    func send(_ request: HTTPRequest, body: HTTPBody?, baseURL: URL, operationID: String) async throws -> (HTTPResponse, HTTPBody?) {
        var request = request
        if operationID == "import-kept" {
            // The runtime writes `application/json; charset=utf-8`; the server
            // and the export contract name the bare type.
            request.headerFields[.contentType] = "application/json"
        }
        let (response, body) = try await base.send(request, body: body, baseURL: baseURL, operationID: operationID)
        guard operationID == "import-kept", response.status.code == 200, let body else { return (response, body) }
        let data = try await Data(collecting: body, upTo: 64 << 20)
        return (response, HTTPBody(Self.sanitize(data)))
    }

    private static func sanitize(_ data: Data) -> Data {
        guard let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return data }
        var sanitized: [String: Any] = [:]
        for (key, value) in object where reportKeys.contains(key) {
            if key == "results", let results = value as? [[String: Any]] {
                sanitized[key] = results.map { $0.filter { resultKeys.contains($0.key) } }
            } else {
                sanitized[key] = value
            }
        }
        guard let encoded = try? JSONSerialization.data(withJSONObject: sanitized) else { return data }
        return encoded
    }
}

/// Repository for the REST endpoints used by the native client.
public final class EarnestAPI: Sendable {
    private let baseURL: URL
    private let tokenStore: any TokenStore
    private let session: URLSession
    private let client: Client
    private let keptImportClient: Client

    public init(baseURL: URL, tokenStore: any TokenStore, session: URLSession? = nil) throws {
        guard let scheme = baseURL.scheme?.lowercased(), ["https", "http"].contains(scheme),
              baseURL.host != nil, baseURL.user == nil, baseURL.password == nil,
              baseURL.query == nil, baseURL.fragment == nil else { throw APIError.invalidBaseURL }
        self.baseURL = baseURL
        self.tokenStore = tokenStore
        let session = session ?? URLSession(configuration: .ephemeral, delegate: RejectRedirects(), delegateQueue: nil)
        self.session = session
        let transport = APITransport(session: session, tokenStore: tokenStore)
        let configuration = OpenAPIRuntime.Configuration(dateTranscoder: RFC3339DateTranscoder())
        client = Client(serverURL: baseURL, configuration: configuration, transport: transport)
        keptImportClient = Client(serverURL: baseURL, configuration: configuration,
                                  transport: KeptImportTransport(base: transport))
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
    /// Fetch signed content without API credentials, preserving its image policy.
    public func signedContent(entryID: Int64, mode: ArticleImageMode = .proxied) async throws -> SignedArticle {
        guard entryID > 0 else { throw APIError.invalidEntryID }
        let signed = try await generated {
            try await client.entryContentUrl(.init(path: .init(id: entryID), query: .init(mode: .init(rawValue: mode.rawValue)))).ok.body.json
        }
        guard let url = URL(string: signed.url),
              let scheme = url.scheme?.lowercased(), ["http", "https"].contains(scheme),
              let host = url.host, url.user == nil, url.password == nil, url.fragment == nil,
              signed.expiresAt > Date() else { throw APIError.invalidResponse }

        // The authenticated server selects its content origin. Never attach API credentials here.
        var unsigned = URLRequest(url: url)
        unsigned.httpShouldHandleCookies = false
        let (htmlData, htmlResponse) = try await session.data(for: unsigned)
        guard let http = htmlResponse as? HTTPURLResponse else { throw APIError.invalidResponse }
        try validateHTTP(http, data: htmlData)
        guard let html = String(data: htmlData, encoding: .utf8) else { throw APIError.invalidResponse }

        var origin = URLComponents()
        origin.scheme = scheme
        origin.host = host
        origin.port = url.port
        return SignedArticle(html: html, imageOrigin: mode == .proxied ? origin.url : nil)
    }
    public func mutes() async throws -> [APIValue] {
        try await generated { try adapt(try await client.listMutes(.init()).ok.body.json ?? []) }
    }
    public func settings() async throws -> APIValue {
        try await generated { try adapt(try await client.getSettings(.init()).ok.body.json) }
    }
    /// The typed reader slice of `/v1/settings`. `settings()` stays raw for sync.
    public func readerSettings() async throws -> APIReaderSettings {
        try await generated { try adapt(try await client.getSettings(.init()).ok.body.json) }
    }
    /// Replaces the reader slice, deduplicated server-side by `opID`. `rev` is
    /// read-only and never sent; the returned settings carry the new revision.
    public func updateReaderSettings(_ settings: APIReaderSettings, opID: String) async throws -> APIReaderSettings {
        try validateReaderSettings(settings, opID: opID)
        let body = Components.Schemas.SettingsBody(
            feedOverrides: .init(additionalProperties: settings.feedOverrides.mapValues {
                Components.Schemas.ReaderOverride(font: $0.font?.rawValue, measure: $0.measure, theme: $0.theme?.rawValue)
            }),
            font: settings.font.rawValue,
            measure: settings.measure,
            opId: opID,
            readingMode: settings.readingMode.rawValue,
            singleKeyShortcuts: settings.singleKeyShortcuts,
            theme: settings.theme.rawValue,
            todayDecayDays: settings.todayDecayDays
        )
        return try await generated { try adapt(try await client.putSettings(.init(body: .json(body))).ok.body.json) }
    }
    /// Exact unread totals. Callers gate this on the unread-count reading
    /// mode; river mode must never request it.
    public func unreadCounts() async throws -> APIUnreadCounts {
        let counts: Components.Schemas.UnreadCounts = try await generated {
            try await client.entryCounts(.init()).ok.body.json
        }
        return APIUnreadCounts(
            bySubscription: counts.bySubscription.additionalProperties,
            later: counts.later,
            kept: counts.kept,
            total: counts.total
        )
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
    /// The account's OPML subscription document, capped at 16 MiB.
    public func exportOPML() async throws -> Data {
        let body = try await generated { try await client.exportOpml(.init()).ok.body }
        do { return try await Data(collecting: try body.textXOpml, upTo: opmlDocumentLimit) }
        catch is CancellationError { throw CancellationError() }
        catch { throw APIError.invalidResponse }
    }
    /// Imports an OPML document and summarizes each outline's outcome.
    public func importOPML(_ document: Data) async throws -> OPMLImportSummary {
        let results = try await generated {
            try await client.importOpml(.init(body: .textXOpml(HTTPBody(document)))).ok.body.json
        } ?? []
        return summarizeOPMLResults(results)
    }
    /// Imports a Google Reader JSON export of starred articles into Kept.
    /// `subscribeMissing` sends the server's `missing_feeds=subscribe`.
    public func importKept(_ document: Data, subscribeMissing: Bool) async throws -> KeptImportSummary {
        // Refuse before decoding: the server takes at most 64 MiB, and
        // decoding builds a tree several times the document's size.
        guard document.count <= keptImportLimit else { throw APIError.invalidDocument }
        let body: OpenAPIObjectContainer
        do { body = try JSONDecoder().decode(OpenAPIObjectContainer.self, from: document) }
        catch { throw APIError.invalidDocument }
        let report = try await generated {
            try await keptImportClient.importKept(.init(
                query: .init(missingFeeds: subscribeMissing ? .subscribe : .skip),
                body: .json(body)
            )).ok.body.json
        }
        return summarizeKeptResults(report)
    }
    /// Makes a feed due for the next fetcher pass.
    public func refreshSubscription(id: Int64) async throws {
        _ = try await generated {
            try await client.refreshSubscription(.init(path: .init(id: id), body: .json(.init(opId: UUID().uuidString)))).ok.body.json
        }
    }
    public func activity() async throws -> [APIValue] {
        let limit = 1000
        var afterID: Int64 = 0
        var all: [APIValue] = []
        while true {
            try Task.checkCancellation()
            let page: [APIValue] = try await generated {
                try adapt(try await client.listActivity(.init(query: .init(afterId: afterID, limit: Int64(limit)))).ok.body.json.activities ?? [])
            }
            if let last = page.last {
                guard let next = last["id"]?.int64Value, next > afterID else { throw APIError.invalidResponse }
                afterID = next
            }
            all.append(contentsOf: page)
            if page.count < limit { return all }
        }
    }
    public func agentChanges(since: Date = Date().addingTimeInterval(-86400)) async throws -> [APIValue] {
        try await generated { try adapt(try await client.agentChanges(.init(query: .init(since: since))).ok.body.json ?? []) }
    }
    public func revertActivity(id: Int64) async throws -> APIValue {
        try await generated { try adapt(try await client.revertActivity(.init(path: .init(id: id), body: .json(.init(opId: UUID().uuidString)))).ok.body.json) }
    }
    public func cancelNotification(id: Int64) async throws {
        guard id > 0 else { throw APIError.invalidEntryID }
        _ = try await generated {
            try await client.cancelNotification(.init(path: .init(id: id), body: .json(.init(opId: UUID().uuidString)))).ok.body.json
        }
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
    /// Search entries and agent notes, paging with an opaque cursor.
    public func search(query: String, filter: APISearchFilter = .init(), cursor: String? = nil) async throws -> APISearchPage {
        guard let mode = Operations.Search.Input.Query.ModePayload(rawValue: filter.mode.rawValue) else { throw APIError.invalidResponse }
        let input = Operations.Search.Input(query: .init(q: query, mode: mode, subscriptionId: filter.subscriptionID, folderId: filter.folderID, limit: filter.limit, cursor: cursor))
        return try await generated { try adapt(try await client.search(input).ok.body.json) }
    }
    public func savedSearches() async throws -> [APISavedSearch] {
        try await generated { try adapt(try await client.listSavedSearches(.init()).ok.body.json ?? []) }
    }
    public func createSavedSearch(name: String, query: String, filter: APISearchFilter) async throws -> APISavedSearch {
        try await generated {
            try adapt(try await client.createSavedSearch(.init(body: .json(.init(filters: adapt(filter), name: name, query: query)))).ok.body.json)
        }
    }
    public func renameSavedSearch(id: Int64, name: String) async throws {
        _ = try await generated { try await client.renameSavedSearch(.init(path: .init(id: id), body: .json(.init(name: name)))).ok.body.json }
    }
    public func deleteSavedSearch(id: Int64) async throws {
        _ = try await generated { try await client.deleteSavedSearch(.init(path: .init(id: id))).ok.body.json }
    }
    public func pipelineStatus() async throws -> APIValue {
        try await generated { try adapt(try await client.pipelineStatus(.init()).ok.body.json, as: APIValue.self) }
    }
}

/// The ranges mirror internal/service/settings.go so an out-of-range edit
/// fails locally instead of as a 400. Keys are canonical positive feed IDs.
func validateReaderSettings(_ settings: APIReaderSettings, opID: String) throws {
    guard (62...72).contains(settings.measure) else { throw APIReaderSettingsError(field: "measure") }
    guard (1...365).contains(settings.todayDecayDays) else { throw APIReaderSettingsError(field: "today_decay_days") }
    guard !opID.isEmpty, opID.utf8.count <= 1024 else { throw APIReaderSettingsError(field: "op_id") }
    guard settings.feedOverrides.count <= 512 else { throw APIReaderSettingsError(field: "feed_overrides") }
    for (key, override) in settings.feedOverrides {
        guard let id = Int64(key), id > 0, key.utf8.count <= 128, String(id) == key else {
            throw APIReaderSettingsError(field: "feed_overrides key")
        }
        if let measure = override.measure, !(62...72).contains(measure) {
            throw APIReaderSettingsError(field: "feed_overrides measure")
        }
    }
}

/// Folds the server's report into counts. Null lists count as empty; a
/// failure falls back to its position and a generic reason when the server
/// omits the title or error.
func summarizeKeptResults(_ report: Components.Schemas.KeptImportReport?) -> KeptImportSummary {
    var added = 0
    var existing = 0
    var unkept = 0
    var gone = 0
    var skipped: [String: Int] = [:]
    var failures: [String] = []
    var labelled = 0
    for result in report?.results ?? [] {
        if !(result.labels ?? []).isEmpty { labelled += 1 }
        switch result.status {
        case .created: added += 1
        case .matched, .unchanged: existing += 1
        case .unkept: unkept += 1
        case .gone: gone += 1
        case .notSubscribed: skipped[result.feedUrl ?? "", default: 0] += 1
        case .failed:
            let title = result.title.flatMap { $0.isEmpty ? nil : $0 } ?? "Article \(result.index + 1)"
            failures.append("\(title): \(result.error ?? "could not be imported")")
        }
    }
    let skippedFeeds: [(feedURL: String, count: Int)] = skipped
        .sorted { $0.value == $1.value ? $0.key < $1.key : $0.value > $1.value }
        .map { (feedURL: $0.key, count: $0.value) }
    return KeptImportSummary(added: added, existing: existing, unkept: unkept, gone: gone,
                             skipped: skippedFeeds, failures: failures, subscribed: report?.subscribed?.count ?? 0,
                             labelled: labelled)
}

/// A result counts as subscribed or as a folder only when it created that
/// object and reported no error; deferred and failure counts are independent.
func summarizeOPMLResults(_ results: [Components.Schemas.OPMLResult]) -> OPMLImportSummary {
    var subscribed = 0
    var folders = 0
    var deferred = 0
    var failures: [String] = []
    var ids: [Int64] = []
    for result in results {
        if let error = result.error, !error.isEmpty {
            failures.append(error)
        } else {
            if let subscription = result.subscription {
                subscribed += 1
                if !ids.contains(subscription.id) { ids.append(subscription.id) }
            }
            if result.folder != nil { folders += 1 }
        }
        if result.deferred { deferred += 1 }
    }
    return OPMLImportSummary(subscribed: subscribed, folders: folders, deferred: deferred, failures: failures,
                             subscriptionIDs: ids)
}

func parseAPIDate(_ text: String) -> Date? {
    let fractional = ISO8601DateFormatter()
    fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
    return fractional.date(from: text) ?? ISO8601DateFormatter().date(from: text)
}

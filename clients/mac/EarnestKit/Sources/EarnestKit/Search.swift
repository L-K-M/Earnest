import Foundation

/// Selects lexical search or reciprocal-rank hybrid retrieval.
public enum APISearchMode: String, Codable, CaseIterable, Sendable {
    case fulltext
    case hybrid
}

/// Narrows search to a subscription or folder; nil values select all.
public struct APISearchFilter: Codable, Equatable, Sendable {
    public var mode: APISearchMode
    public var subscriptionID: Int64?
    public var folderID: Int64?
    public var limit: Int64?

    public init(mode: APISearchMode = .fulltext, subscriptionID: Int64? = nil, folderID: Int64? = nil, limit: Int64? = nil) {
        self.mode = mode
        self.subscriptionID = subscriptionID
        self.folderID = folderID
        self.limit = limit
    }

    enum CodingKeys: String, CodingKey {
        case mode
        case subscriptionID = "subscription_id"
        case folderID = "folder_id"
        case limit
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        // A saved search stored before a mode was chosen omits the key.
        mode = try container.decodeIfPresent(APISearchMode.self, forKey: .mode) ?? .fulltext
        subscriptionID = try container.decodeIfPresent(Int64.self, forKey: .subscriptionID)
        folderID = try container.decodeIfPresent(Int64.self, forKey: .folderID)
        limit = try container.decodeIfPresent(Int64.self, forKey: .limit)
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(mode, forKey: .mode)
        try container.encodeIfPresent(subscriptionID, forKey: .subscriptionID)
        try container.encodeIfPresent(folderID, forKey: .folderID)
        try container.encodeIfPresent(limit, forKey: .limit)
    }
}

/// One entry or agent note in a search page.
public struct APISearchMatch: Codable, Sendable, Identifiable {
    public var kind: String
    public var entryID: Int64
    public var subscriptionID: Int64
    public var feedID: Int64
    public var seq: Int64
    public var title: String
    public var url: String
    public var snippet: String
    public var rank: Double
    public var annotationID: Int64?
    public var agentName: String?
    public var untrusted: Bool

    /// Stable identity for lists: the kind separates entries from notes, and
    /// the annotation distinguishes a note from its parent entry.
    public var id: String { "\(kind):\(entryID):\(annotationID ?? 0)" }

    enum CodingKeys: String, CodingKey {
        case kind
        case entryID = "entry_id"
        case subscriptionID = "subscription_id"
        case feedID = "feed_id"
        case seq
        case title
        case url
        case snippet
        case rank
        case annotationID = "annotation_id"
        case agentName = "agent_name"
        case untrusted
    }
}

/// A page of search results; `status` carries in-band disabled/paused state.
public struct APISearchPage: Codable, Sendable {
    public var matches: [APISearchMatch]?
    public var nextCursor: String?
    public var status: String?

    enum CodingKeys: String, CodingKey {
        case matches
        case nextCursor = "next_cursor"
        case status
    }
}

/// A named query and filter saved by the user.
public struct APISavedSearch: Codable, Sendable {
    public var id: Int64
    public var name: String
    public var query: String
    public var filters: APISearchFilter
}

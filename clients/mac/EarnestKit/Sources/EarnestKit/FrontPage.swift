import Foundation

/// The two windows the front page offers. The server selects and ranks the
/// stories for each; the client never reorders them.
public enum FrontPagePeriod: String, Codable, Sendable, CaseIterable, Identifiable {
    case daily, weekly

    public var id: String { rawValue }

    /// The tab's short name.
    public var title: String { self == .daily ? "Today" : "This week" }

    /// The window the tab covers, shown under its name.
    public var windowLabel: String { self == .daily ? "Past 24 hours" : "Past 7 days" }

    /// An unknown server value reads as the default window rather than
    /// discarding the whole page.
    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        self = Self(rawValue: try container.decode(String.self)) ?? .daily
    }
}

/// A verbatim publisher passage a summary claim rests on. It points at one
/// stored article version, never at an annotation.
public struct PassageCitation: Codable, Sendable {
    public let entryID: Int64
    public let contentHash: String
    public let passages: [String]
    public let verified: Bool
    private enum CodingKeys: String, CodingKey {
        case entryID = "entry_id", contentHash = "content_hash", passages, verified
    }
}

/// One grounded summary sentence. It stays visible even when a citation fails
/// verification; the citation carries its own `verified` flag.
public struct GroundedClaim: Codable, Sendable {
    public let text: String
    public let citations: [PassageCitation]
    public let verified: Bool
    private enum CodingKeys: String, CodingKey { case text, citations, verified }
}

/// The server's cached summary of a story. The front-page GET never awaits a
/// model: a summary is present only when the server already stored one.
public struct FrontPageSummary: Codable, Sendable {
    public let claims: [GroundedClaim]
    public let model: String
    public let promptVersion: String
    public let generatedAt: String
    public let excerptNotice: String?
    private enum CodingKeys: String, CodingKey {
        case claims, model, promptVersion = "prompt_version", generatedAt = "generated_at"
        case excerptNotice = "excerpt_notice"
    }
}

/// One original article in a story's coverage. The representative is the
/// first member; the server sorts coverage newest first.
public struct FrontPageCoverage: Codable, Sendable, Identifiable {
    public let entry: APIEntry
    public let sourceTitle: String
    public var id: Int64 { entry.id }
    private enum CodingKeys: String, CodingKey { case entry, sourceTitle = "source_title" }
}

/// A selected story. `entry` is the representative article; `coverage` holds
/// every original, including the representative.
public struct FrontPageStory: Codable, Sendable, Identifiable {
    public let id: String
    public let entry: APIEntry
    public let sourceTitle: String
    public let publishedAt: String
    public let feedCount: Int64
    public let excerpt: String
    /// Authenticated server path of the stored feature image, when one exists.
    public let imageURL: String?
    public let coverage: [FrontPageCoverage]
    public let summary: FrontPageSummary?
    private enum CodingKeys: String, CodingKey {
        case id, entry, sourceTitle = "source_title", publishedAt = "published_at"
        case feedCount = "feed_count", excerpt, imageURL = "image_url", coverage, summary
    }
}

public struct FrontPageSection: Codable, Sendable {
    public let name: String
    public let stories: [FrontPageStory]
    private enum CodingKeys: String, CodingKey { case name, stories }
}

/// The front page for one window. `revision` changes only when membership,
/// content, a summary or media changes; the client compares it to decide
/// whether new coverage is available.
public struct FrontPage: Codable, Sendable {
    public let period: FrontPagePeriod
    public let windowStart: String
    public let windowEnd: String
    public let selectedAt: String
    public let revision: String
    public let sections: [FrontPageSection]
    private enum CodingKeys: String, CodingKey {
        case period, windowStart = "window_start", windowEnd = "window_end"
        case selectedAt = "selected_at", revision, sections
    }
}

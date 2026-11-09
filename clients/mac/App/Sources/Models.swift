import Foundation
import EarnestKit

enum Section: String, CaseIterable, Identifiable {
    case today = "Today", edition = "Edition", later = "Later", kept = "Kept", done = "Done", feeds = "Feeds", activity = "Activity", search = "Search"

    var id: String { rawValue }

    /// The places a window can navigate to from the sidebar, the Go menu and
    /// the palette. Search is entered from the toolbar search field instead.
    static let places: [Section] = [.today, .edition, .later, .kept, .done, .feeds, .activity]

    var title: String { rawValue }

    var keyEquivalent: String? {
        switch self {
        case .today: return "1"
        case .edition: return "2"
        case .later: return "3"
        case .kept: return "4"
        case .done: return "5"
        case .feeds: return "6"
        case .activity: return "7"
        case .search: return nil
        }
    }

    var icon: String {
        switch self {
        case .today: return "sun.max"
        case .edition: return "newspaper"
        case .later: return "clock"
        case .kept: return "bookmark"
        case .done: return "checkmark.circle"
        case .feeds: return "dot.radiowaves.left.and.right"
        case .activity: return "clock.arrow.circlepath"
        case .search: return "magnifyingglass"
        }
    }

    /// The server-side entries view backing entry sections. Today and the
    /// Edition share the river and split it by subscription cadence.
    var entryView: String? {
        switch self {
        case .today, .edition: return "river"
        case .later: return "later"
        case .kept: return "kept"
        case .done: return "done"
        case .feeds, .activity, .search: return nil
        }
    }

    /// Sections whose detail column is the article reader.
    var showsReader: Bool { self != .feeds }
}

enum EntryCommand {
    case done, later, keep, unread
}

/// The cadences a subscription can declare, in menu order.
enum Cadence: String, CaseIterable, Identifiable {
    case now, today, week

    var id: String { rawValue }

    var title: String {
        switch self {
        case .now: return "Now"
        case .today: return "Today"
        case .week: return "This Week"
        }
    }

    /// Unknown server values display as Today, the server default.
    init(server value: String) { self = Cadence(rawValue: value) ?? .today }
}

struct EntryRow: Identifiable, Equatable {
    let id: Int64
    let title: String
    let subtitle: String
    let unread: Bool
    var kept = false
    var later = false
    /// The article's web address, when it is HTTP(S): for dragging, copying
    /// and opening in the browser.
    var url: URL? = nil
}

struct EditionGroup: Identifiable, Equatable {
    var id: Int64 { subscriptionID }
    let subscriptionID: Int64
    let title: String
    var rows: [EntryRow]
    var rowIDs: [Int64] { rows.map(\.id) }
}

struct FeedInfo: Identifiable, Equatable {
    let id: Int64
    let feedID: Int64
    let title: String
    let url: String
    let siteURL: String
    var cadence: String
    let isPrivate: Bool
    let deleted: Bool
    let issue: String?

    init?(_ value: APIValue) {
        guard let id = value["id"]?.int64Value, id > 0 else { return nil }
        self.id = id
        feedID = value["feed_id"]?.int64Value ?? 0
        title = value["title"]?.stringValue ?? ""
        url = value["url"]?.stringValue ?? ""
        siteURL = value["site_url"]?.stringValue ?? ""
        cadence = value["cadence"]?.stringValue ?? "today"
        isPrivate = value["private"]?.boolValue ?? false
        deleted = value["deleted"]?.boolValue ?? false
        let health = value["health"]
        let failures = health?["consecutive_failures"]?.int64Value ?? 0
        issue = failures > 0 ? (health?["error_message"]?.stringValue ?? health?["error_class"]?.stringValue) : nil
    }
}

struct ActivityItem: Identifiable, Equatable {
    let id: Int64
    let principalID: Int64
    let principalName: String
    let action: String
    let detail: String
    let entryID: Int64?
    let notificationID: Int64?
    let nextAction: String
    let reason: String
    let createdAt: Date?

    init(_ value: APIValue) {
        id = value["id"]?.int64Value ?? 0
        principalID = value["principal_id"]?.int64Value ?? 0
        principalName = value["principal_name"]?.stringValue ?? ""
        action = value["action"]?.stringValue ?? ""
        nextAction = value["next_action"]?.stringValue ?? "nothing"
        reason = value["reason"]?.stringValue ?? ""
        createdAt = value["created_at"]?.stringValue.flatMap(DateFormatting.parse)
        let target = value["target"]
        notificationID = action.hasPrefix("notification.") ? target?["id"]?.int64Value.flatMap { $0 > 0 ? $0 : nil } : nil
        let before = value["before"]
        let after = value["after"]
        var entry = target?["entry_id"]?.int64Value
        if entry == nil, let ids = after?["entry_ids"]?.arrayValue, let first = ids.first {
            entry = first.int64Value
        }
        entryID = entry
        detail = ActivityItem.describe(action: action, target: target, before: before, after: after)
    }

    private static func describe(action: String, target: APIValue?, before: APIValue?, after: APIValue?) -> String {
        switch action {
        case "state.updated":
            let register = target?["register"]?.stringValue ?? "state"
            let value = after?[register]?["value"]
            let text = value?.stringValue ?? (value?.boolValue == true ? "kept" : nil)
            switch (register, text) {
            case ("seen", "seen"): return "marked seen"
            case ("seen", "unseen"): return "marked unread"
            case ("shelf", "later"): return "moved to Later"
            case ("shelf", "done"): return "moved to Done"
            case ("shelf", _): return "cleared the shelf"
            case ("kept", "kept"): return "kept"
            case ("kept", _): return "unkept"
            default: return "changed \(register)"
            }
        case "annotation.updated":
            let kind = after?["kind"]?.stringValue ?? before?["kind"]?.stringValue ?? "annotation"
            return "wrote a \(kind)"
        case "annotation.deleted":
            let kind = before?["kind"]?.stringValue ?? "annotation"
            return "deleted a \(kind)"
        case "annotation.reverted":
            return "reverted an annotation"
        case "position.updated":
            return "moved its read position"
        case "notification.created":
            return "sent a notification"
        case "notification.cancelled":
            return "cancelled a notification"
        default:
            return action.replacingOccurrences(of: ".", with: " ")
        }
    }
}

struct ActivityGroup: Identifiable, Equatable {
    var id: Int64 { principalID }
    let principalID: Int64
    let name: String
    var items: [ActivityItem]
}

struct BriefItem: Identifiable, Equatable {
    let id: Int64
    let principalID: Int64
    let text: String
    let entryIDs: [Int64]

    init?(_ value: APIValue) {
        guard let id = value["id"]?.int64Value, value["deleted"]?.boolValue != true else { return nil }
        self.id = id
        principalID = value["principal_id"]?.int64Value ?? 0
        text = value["value"]?.stringValue ?? ""
        entryIDs = value["entry_ids"]?.arrayValue?.compactMap(\.int64Value) ?? []
    }
}

struct SessionCounts: Equatable {
    var done = 0
    var later = 0
    var kept = 0
    var unread = 0

    var total: Int { done + later + kept + unread }

    var summary: String {
        var parts: [String] = []
        if kept > 0 { parts.append("\(kept) kept") }
        if later > 0 { parts.append("\(later) for later") }
        if done > 0 { parts.append("\(done) done") }
        if unread > 0 { parts.append("\(unread) back in the river") }
        return parts.isEmpty ? "" : parts.joined(separator: " · ") + " this session."
    }

    mutating func record(_ command: EntryCommand, unkeep: Bool = false) {
        switch command {
        case .done: done += 1
        case .later: later += 1
        case .keep: if !unkeep { kept += 1 }
        case .unread: unread += 1
        }
    }

    /// Undo takes an action back out of the session summary.
    mutating func unrecord(_ command: EntryCommand, unkeep: Bool = false) {
        switch command {
        case .done: done = max(0, done - 1)
        case .later: later = max(0, later - 1)
        case .keep: if !unkeep { kept = max(0, kept - 1) }
        case .unread: unread = max(0, unread - 1)
        }
    }
}

struct PaletteCommand: Identifiable {
    var id: String { title }
    let title: String
    let key: String
    let enabled: Bool
    let action: () -> Void
}

/// Server addresses as people type them. A bare host name gets a scheme:
/// HTTPS for a fully qualified name, HTTP for localhost, an IP literal or an
/// unqualified name, which the local-network ATS exception permits.
enum ServerAddress {
    static func normalize(_ text: String) -> String? {
        var trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }
        if !trimmed.contains("://") {
            let host = trimmed.split(separator: "/", maxSplits: 1).first.map(String.init) ?? trimmed
            let name = host.split(separator: ":").first.map(String.init)?.lowercased() ?? host
            let local = name == "localhost" || !name.contains(".") || name.hasSuffix(".local")
                || name.allSatisfy { $0.isNumber || $0 == "." } || host.hasPrefix("[")
            trimmed = (local ? "http://" : "https://") + trimmed
        }
        while trimmed.hasSuffix("/") { trimmed.removeLast() }
        guard let url = URL(string: trimmed), let scheme = url.scheme?.lowercased(),
              ["http", "https"].contains(scheme), let host = url.host, !host.isEmpty,
              url.user == nil, url.password == nil else { return nil }
        return trimmed
    }
}

/// `feed:` links from browsers: `feed://host/path` means HTTP, and
/// `feed:https://host/path` wraps an explicit URL. Only HTTP(S) results are
/// accepted; the subscribe sheet still asks before subscribing.
enum FeedLink {
    static func subscriptionURL(_ url: URL) -> String? {
        guard url.scheme?.lowercased() == "feed" else { return nil }
        let text = url.absoluteString
        let rest = String(text.dropFirst("feed:".count))
        let candidate: String
        if rest.hasPrefix("//") {
            candidate = "http:" + rest
        } else {
            candidate = rest
        }
        guard let parsed = URL(string: candidate), let scheme = parsed.scheme?.lowercased(),
              ["http", "https"].contains(scheme), parsed.host?.isEmpty == false,
              parsed.user == nil, parsed.password == nil else { return nil }
        return parsed.absoluteString
    }
}

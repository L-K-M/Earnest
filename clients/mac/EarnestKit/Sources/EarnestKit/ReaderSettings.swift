import Foundation

/// How the river presents unread work: an ending river or per-feed unread
/// counts. Wire values mirror the server's `ReadingMode`.
public enum APIReadingMode: String, Codable, Sendable, Equatable, CaseIterable {
    case river = "river"
    case unreadCounts = "unread_counts"
}

/// The reading-view palette. `black` is true black for OLED and `eink` is the
/// high-contrast e-ink variant.
public enum APIReaderTheme: String, Codable, Sendable, Equatable, CaseIterable {
    case system = "system"
    case light = "light"
    case dark = "dark"
    case black = "black"
    case eink = "eink"
}

/// The reading-view face; `dyslexic` is the dyslexia-friendly cut.
public enum APIReaderFont: String, Codable, Sendable, Equatable, CaseIterable {
    case serif = "serif"
    case sans = "sans"
    case dyslexic = "dyslexic"
}

/// Per-feed reading-view override. Every member is optional; a nil member
/// inherits the account-wide setting.
public struct APIReaderOverride: Codable, Sendable, Equatable {
    public var theme: APIReaderTheme?
    public var measure: Int64?
    public var font: APIReaderFont?

    public init(theme: APIReaderTheme? = nil, measure: Int64? = nil, font: APIReaderFont? = nil) {
        self.theme = theme
        self.measure = measure
        self.font = font
    }
}

/// The typed reader slice of `/v1/settings`. The fields mirror
/// `internal/service/settings.go`; `rev` is the server's change-log revision
/// and is read-only for clients.
public struct APIReaderSettings: Codable, Sendable, Equatable {
    public var readingMode: APIReadingMode
    public var todayDecayDays: Int64
    public var singleKeyShortcuts: Bool
    public var theme: APIReaderTheme
    public var measure: Int64
    public var font: APIReaderFont
    public var feedOverrides: [String: APIReaderOverride]
    public private(set) var rev: Int64

    enum CodingKeys: String, CodingKey {
        case readingMode = "reading_mode"
        case todayDecayDays = "today_decay_days"
        case singleKeyShortcuts = "single_key_shortcuts"
        case theme, measure, font
        case feedOverrides = "feed_overrides"
        case rev
    }

    /// Server defaults for a user who never saved settings; `rev` starts at 0
    /// until the first write.
    public init(
        readingMode: APIReadingMode = .river,
        todayDecayDays: Int64 = 2,
        singleKeyShortcuts: Bool = true,
        theme: APIReaderTheme = .system,
        measure: Int64 = 66,
        font: APIReaderFont = .serif,
        feedOverrides: [String: APIReaderOverride] = [:],
        rev: Int64 = 0
    ) {
        self.readingMode = readingMode
        self.todayDecayDays = todayDecayDays
        self.singleKeyShortcuts = singleKeyShortcuts
        self.theme = theme
        self.measure = measure
        self.font = font
        self.feedOverrides = feedOverrides
        self.rev = rev
    }
}

/// Exact unread totals from `/v1/entries:counts`. `bySubscription` is keyed by
/// decimal subscription ID. The server owns the values; absence is not zero.
public struct APIUnreadCounts: Codable, Sendable, Equatable {
    public var bySubscription: [String: Int64]
    public var later: Int64
    public var kept: Int64
    public var total: Int64

    enum CodingKeys: String, CodingKey {
        case bySubscription = "by_subscription", later, kept, total
    }

    public init(bySubscription: [String: Int64] = [:], later: Int64 = 0, kept: Int64 = 0, total: Int64 = 0) {
        self.bySubscription = bySubscription
        self.later = later
        self.kept = kept
        self.total = total
    }
}

extension APIReaderSettings {
    /// Decode the durable settings object written by sync and acknowledged
    /// saves. Missing or legacy fields fall back to the server defaults;
    /// `rev` is the object's change revision.
    public init?(_ object: SyncObject) {
        guard object.kind == .settings else { return nil }
        var settings = APIReaderSettings()
        settings.rev = object.rev
        if let value = object.fields["reading_mode"]?.stringValue.flatMap(APIReadingMode.init(rawValue:)) {
            settings.readingMode = value
        }
        if let value = object.fields["today_decay_days"]?.int64Value, (1...365).contains(value) {
            settings.todayDecayDays = value
        }
        if let value = object.fields["single_key_shortcuts"]?.boolValue {
            settings.singleKeyShortcuts = value
        }
        if let value = object.fields["theme"]?.stringValue.flatMap(APIReaderTheme.init(rawValue:)) {
            settings.theme = value
        }
        if let value = object.fields["measure"]?.int64Value, (62...72).contains(value) {
            settings.measure = value
        }
        if let value = object.fields["font"]?.stringValue.flatMap(APIReaderFont.init(rawValue:)) {
            settings.font = value
        }
        guard let overrides = object.fields["feed_overrides"]?.objectValue else {
            self = settings
            return
        }
        var parsed: [String: APIReaderOverride] = [:]
        for (key, value) in overrides {
            guard let id = Int64(key), id > 0, String(id) == key, let fields = value.objectValue else { continue }
            var override = APIReaderOverride()
            if let theme = fields["theme"]?.stringValue.flatMap(APIReaderTheme.init(rawValue:)) {
                override.theme = theme
            }
            if let measure = fields["measure"]?.int64Value, (62...72).contains(measure) {
                override.measure = measure
            }
            if let font = fields["font"]?.stringValue.flatMap(APIReaderFont.init(rawValue:)) {
                override.font = font
            }
            parsed[key] = override
        }
        settings.feedOverrides = parsed
        self = settings
    }
}

/// Thrown when reader settings fail client-side validation, before any request
/// is sent. The server applies the same ranges; failing locally keeps a bad
/// edit off the wire.
public struct APIReaderSettingsError: LocalizedError, Sendable, Equatable {
    public let field: String
    public init(field: String) { self.field = field }
    public var errorDescription: String? { "Invalid reader setting: \(field)" }
}

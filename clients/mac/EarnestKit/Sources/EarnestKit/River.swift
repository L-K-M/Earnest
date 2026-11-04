import Foundation

/// Cached river membership uses the same clocks and import boundary as the service.
public enum River {
    private static let defaultDecayDays: Int64 = 2
    private static let weekDecayDays: Int64 = 7
    private static let maxDecayDays: Int64 = 365
    private static let secondsPerDay: TimeInterval = 86_400

    public static func contains(_ entry: SyncEntry, state: State, subscription: SyncObject?,
                                resources: [SyncObject], now: Date) -> Bool {
        guard !entry.deleted, let subscription, !subscription.deleted,
              state.seen.value == .text("unseen"), state.shelf.value == .text("none"),
              subscription.fields["muted"]?.boolValue != true,
              let firstSeen = entry.firstSeenAt,
              let through = subscription.fields["initial_through_seq"]?.int64Value else { return false }

        // Bootstrap represents the server's unbounded cutoff as null.
        if let since = subscription.fields["initial_since"]?.dateValue {
            if firstSeen < since { return false }
            if entry.seq <= through, let published = entry.publishedAt, published < since { return false }
        }

        if resources.contains(where: { matchesMute($0, entry: entry) }) { return false }
        let cadence = subscription.fields["cadence"]?.stringValue ?? "today"
        if cadence == "now" { return true }

        let configured = resources.first { $0.kind == .settings && !$0.deleted }?.fields["today_decay_days"]?.int64Value
        let todayDays = configured.flatMap { (1...maxDecayDays).contains($0) ? $0 : nil } ?? defaultDecayDays
        let days = cadence == "week" ? weekDecayDays : todayDays
        return firstSeen >= now.addingTimeInterval(-TimeInterval(days) * secondsPerDay)
    }

    private static func matchesMute(_ resource: SyncObject, entry: SyncEntry) -> Bool {
        guard resource.kind == .mute, !resource.deleted,
              let value = resource.fields["value"]?.stringValue?.lowercased() else { return false }
        switch resource.fields["mute_kind"]?.stringValue {
        case "author": return entry.author.lowercased() == value
        case "keyword": return entry.title.lowercased().contains(value)
        default: return false
        }
    }
}

extension SyncObject {
    /// Mutes have a native `kind` distinct from their sync resource kind.
    public init?(mute value: APIValue) {
        guard let id = value["id"]?.int64Value, id > 0,
              let kind = value["kind"]?.stringValue, ["author", "keyword"].contains(kind),
              let text = value["value"]?.stringValue else { return nil }
        self.init(kind: .mute, id: id, rev: value["rev"]?.int64Value ?? 0,
                  deleted: value["deleted"]?.boolValue ?? false)
        fields = ["mute_kind": .string(kind), "value": .string(text)]
    }

    public init?(settings value: APIValue) {
        guard let days = value["today_decay_days"]?.int64Value else { return nil }
        self.init(kind: .settings, id: 1, rev: value["rev"]?.int64Value ?? 0)
        fields = ["today_decay_days": .integer(days)]
    }
}

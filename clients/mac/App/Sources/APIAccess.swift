import Foundation
import EarnestKit

// Local property access over the raw APIValue payloads returned by the
// resource endpoints. Typed entry/state transport stays in EarnestKit.
// Names mirror the package's internal SyncWire accessors.
extension APIValue {
    var objectValue: [String: APIValue]? {
        guard case .object(let value) = self else { return nil }
        return value
    }

    var arrayValue: [APIValue]? {
        guard case .array(let value) = self else { return nil }
        return value
    }

    var stringValue: String? {
        guard case .string(let value) = self else { return nil }
        return value
    }

    var int64Value: Int64? {
        switch self {
        case .integer(let value): return value
        case .number(let value) where value.isFinite && value.rounded() == value && abs(value) < 9_007_199_254_740_992: return Int64(value)
        case .string(let value): return Int64(value)
        default: return nil
        }
    }

    var boolValue: Bool? {
        switch self {
        case .bool(let value): return value
        case .integer(let value): return value != 0
        case .number(let value): return value != 0
        default: return nil
        }
    }

    subscript(key: String) -> APIValue? {
        objectValue?[key]
    }
}

enum DateFormatting {
    private static let fractional: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter
    }()

    private static let plain = ISO8601DateFormatter()

    static func parse(_ text: String) -> Date? {
        fractional.date(from: text) ?? plain.date(from: text)
    }

    static let relative: RelativeDateTimeFormatter = {
        let formatter = RelativeDateTimeFormatter()
        formatter.unitsStyle = .abbreviated
        return formatter
    }()

    private static let weekday: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateFormat = "EEEE"
        return formatter
    }()

    private static let medium: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        return formatter
    }()

    /// End-of-river copy: "since Tuesday" inside a week, a date beyond it.
    static func endDay(_ date: Date, now: Date = Date()) -> String {
        if now.timeIntervalSince(date) < 7 * 24 * 60 * 60 {
            return weekday.string(from: date)
        }
        return medium.string(from: date)
    }
}

/// The identity document is flat: {"user_id", "principal_id", "kind", "scopes"}.
/// Nested shapes are tolerated in case the envelope wraps the record.
func parsePrincipalID(_ me: APIValue) -> Int64? {
    if let id = me["principal_id"]?.int64Value, id > 0 { return id }
    if let id = me["principal"]?["id"]?.int64Value, id > 0 { return id }
    if let id = me["user"]?["principal_id"]?.int64Value, id > 0 { return id }
    return nil
}

extension APIRegister {
    var stamp: Stamp {
        let value: RegisterValue
        switch self.value {
        case .string(let text): value = .text(text)
        case .bool(let flag): value = .flag(flag)
        case .integer(let number): value = .flag(number != 0)
        case .number(let number): value = .flag(number != 0)
        default: value = .text("")
        }
        return Stamp(value: value, rev: rev)
    }
}

extension APIState {
    var syncState: State {
        var state = State(principal: principal, entryID: entryID)
        state.seen = seen.stamp
        state.shelf = shelf.stamp
        state.kept = kept.stamp
        state.stateRev = stateRev
        return state
    }
}

/// Merge the server row before applying pending intent, so a newer fetch cannot hide it.
func presentedState(fetched: APIState, engine: SyncEngine) throws -> State {
    try engine.mergeState(fetched.syncState)
    return try engine.state(principal: fetched.principal, entryID: fetched.entryID)
}

/// Reconstruct transport rows from the durable snapshot for offline lists.
func cachedEntry(_ entry: SyncEntry, principal: Int64, engine: SyncEngine) throws -> APIEntry {
    let encoder = JSONEncoder()
    let decoder = JSONDecoder()
    var fields = try decoder.decode(APIValue.self, from: encoder.encode(entry)).objectValue ?? [:]
    let state = try engine.state(principal: principal, entryID: entry.id)
    fields["state"] = try decoder.decode(APIValue.self, from: encoder.encode(state))
    fields["first_seen_at"] = .string(entry.firstSeenAt.map { ISO8601DateFormatter().string(from: $0) } ?? "")
    return try decoder.decode(APIEntry.self, from: encoder.encode(APIValue.object(fields)))
}

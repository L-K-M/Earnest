import Foundation
import Synchronization

/// RFC 3339 wire clocks. The sync model is UTC-only; fractional seconds are
/// accepted on decode and emitted on encode.
enum SyncTime {
    // Snapshots process many timestamps at once. Reuse the formatters and
    // serialize access to Foundation's mutable formatter instances.
    private static let formatters = {
        let precise = ISO8601DateFormatter()
        precise.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        let plain = ISO8601DateFormatter()
        plain.formatOptions = [.withInternetDateTime]
        return Mutex((precise: precise, plain: plain))
    }()

    /// The Go zero time `0001-01-01T00:00:00Z`, used for unset wire dates.
    static let zero = Date(timeIntervalSince1970: -62135596800)

    static func parse(_ text: String) -> Date? {
        formatters.withLock { $0.precise.date(from: text) ?? $0.plain.date(from: text) }
    }

    static func string(_ date: Date) -> String {
        formatters.withLock { $0.precise.string(from: date) }
    }
}

extension APIValue {
    var objectValue: [String: APIValue]? {
        if case .object(let object) = self { return object }
        return nil
    }
    var arrayValue: [APIValue]? {
        if case .array(let array) = self { return array }
        return nil
    }
    var stringValue: String? {
        if case .string(let string) = self { return string }
        return nil
    }
    var boolValue: Bool? {
        if case .bool(let bool) = self { return bool }
        return nil
    }
    /// Lossless integer access: JSON numbers decode as `.integer`, but
    /// tolerates integral doubles inside the exact-integer range.
    var int64Value: Int64? {
        switch self {
        case .integer(let value): return value
        case .number(let value) where value.isFinite && value.rounded() == value && abs(value) < 9_007_199_254_740_992:
            return Int64(value)
        default: return nil
        }
    }
    var dateValue: Date? {
        stringValue.flatMap(SyncTime.parse)
    }
    subscript(key: String) -> APIValue? { objectValue?[key] }

    static func object(pairs: [(String, APIValue?)]) -> APIValue {
        var object: [String: APIValue] = [:]
        for (key, value) in pairs where value != nil { object[key] = value }
        return .object(object)
    }
    static func intArray(_ values: [Int64]) -> APIValue { .array(values.map { .integer($0) }) }
}

extension Cursor {
    public init?(_ value: APIValue) {
        guard let generation = value["generation"]?.stringValue, let rev = value["rev"]?.int64Value else { return nil }
        self.init(generation: generation, rev: rev)
    }
    public var apiValue: APIValue {
        .object(pairs: [("generation", .string(generation)), ("rev", .integer(rev))])
    }
}

extension RegisterValue {
    public init?(_ value: APIValue) {
        switch value {
        case .bool(let flag): self = .flag(flag)
        case .string(let text): self = .text(text)
        default: return nil
        }
    }
    public var apiValue: APIValue {
        switch self {
        case .flag(let flag): return .bool(flag)
        case .text(let text): return .string(text)
        }
    }
}

extension Stamp {
    /// A `occurred_at` at or before the Go zero time decodes as unset; the
    /// wire form always emits the zero-time string for an unset stamp.
    public init?(_ value: APIValue) {
        guard let registerValue = value["value"].flatMap(RegisterValue.init) else { return nil }
        let at = value["occurred_at"]?.dateValue
        self.init(value: registerValue, rev: value["rev"]?.int64Value ?? 0,
                  occurredAt: at.flatMap { $0 > SyncTime.zero ? $0 : nil },
                  actor: value["actor"]?.int64Value ?? 0,
                  source: value["source"]?.stringValue ?? "")
    }
    public var apiValue: APIValue {
        .object(pairs: [
            ("value", value.apiValue),
            ("rev", .integer(rev)),
            ("occurred_at", .string(SyncTime.string(occurredAt ?? SyncTime.zero))),
            ("actor", .integer(actor)),
            ("source", .string(source)),
        ])
    }
}

extension State {
    /// Decodes the wire shape `{"principal","entry_id","seen","shelf","kept","state_rev"}`.
    /// Missing registers fall back to their initial values so state fragments
    /// merge per register.
    public init?(_ value: APIValue) {
        guard let principal = value["principal"]?.int64Value, principal > 0, let entryID = value["entry_id"]?.int64Value, entryID > 0 else { return nil }
        self.init(principal: principal, entryID: entryID)
        stateRev = value["state_rev"]?.int64Value ?? 0
        for register in [Register.seen, .shelf, .kept] {
            guard let raw = value[register.rawValue] else { continue }
            guard let stamp = Stamp(raw), stamp.rev >= 0 else { return nil }
            switch (register, stamp.value) {
            case (.seen, .text("seen")), (.seen, .text("unseen")),
                 (.shelf, .text("none")), (.shelf, .text("later")), (.shelf, .text("done")), (.kept, .flag):
                self[register] = stamp
            default: return nil
            }
        }
    }
    public var apiValue: APIValue {
        .object(pairs: [
            ("principal", .integer(principal)),
            ("entry_id", .integer(entryID)),
            ("seen", seen.apiValue),
            ("shelf", shelf.apiValue),
            ("kept", kept.apiValue),
            ("state_rev", .integer(stateRev)),
        ])
    }
    public var unread: Bool { seen.value == .text("unseen") && shelf.value != .text("done") }
    public var googleRead: Bool { seen.value == .text("seen") || shelf.value == .text("done") }
}

extension Write {
    /// Decodes a fixture/wire write. `register` and `value` are required here;
    /// `occurred_at` defaults to the zero clock which `queue` rejects.
    /// Range ops decode through `RangeWrite(_:)`, which applies the seen defaults.
    public init?(_ value: APIValue) {
        guard let opID = value["op_id"]?.stringValue,
              let register = value["register"]?.stringValue.flatMap(Register.init(rawValue:)),
              let registerValue = value["value"].flatMap(RegisterValue.init),
              let base = value["base_rev"].flatMap(Cursor.init) else { return nil }
        self.init(opID: opID,
                  principal: value["principal"]?.int64Value ?? 0,
                  entryID: value["entry_id"]?.int64Value ?? 0,
                  register: register,
                  value: registerValue,
                  base: base,
                  occurredAt: value["occurred_at"]?.dateValue ?? SyncTime.zero,
                  kind: value["kind"]?.stringValue.flatMap(OperationKind.init(rawValue:)) ?? .explicit,
                  inFlight: value["in_flight"]?.boolValue ?? false)
    }
    /// The wire write body: `op_id principal entry_id register value base_rev
    /// occurred_at kind`. `in_flight` is local bookkeeping, never sent.
    public var apiValue: APIValue {
        .object(pairs: [
            ("op_id", .string(opID)),
            ("principal", .integer(principal)),
            ("entry_id", .integer(entryID)),
            ("register", .string(register.rawValue)),
            ("value", value.apiValue),
            ("base_rev", base.apiValue),
            ("occurred_at", .string(SyncTime.string(occurredAt))),
            ("kind", .string(kind.rawValue)),
        ])
    }
}

extension Scope {
    public init?(_ value: APIValue) {
        guard let kind = value["kind"]?.stringValue.flatMap(ScopeKind.init(rawValue:)) else { return nil }
        self.init(kind: kind, ids: value["ids"]?.arrayValue?.compactMap { $0.int64Value } ?? [])
    }
    public var apiValue: APIValue {
        .object(pairs: [("kind", .string(kind.rawValue)), ("ids", ids.isEmpty ? nil : .intArray(ids))])
    }
}

extension RangeWrite {
    /// Decodes `{"write","scope","through_seq"}`. The range op defaults to
    /// register `seen` / value `seen` when its write omits them.
    public init?(_ value: APIValue) {
        var writeValue = value["write"]
        if case .object(var object) = writeValue ?? .null {
            if object["register"] == nil { object["register"] = .string(Register.seen.rawValue) }
            if object["value"] == nil { object["value"] = .string("seen") }
            writeValue = .object(object)
        }
        guard let write = writeValue.flatMap(Write.init),
              let scope = value["scope"].flatMap(Scope.init),
              let throughSeq = value["through_seq"]?.int64Value else { return nil }
        self.init(write: write, scope: scope, throughSeq: throughSeq)
    }
    public var apiValue: APIValue {
        .object(pairs: [
            ("write", write.apiValue),
            ("scope", scope.apiValue),
            ("through_seq", .integer(throughSeq)),
        ])
    }
}

extension OutboxItem {
    public init?(_ value: APIValue) {
        if let write = value["write"].flatMap(Write.init) {
            self = .write(write)
        } else if let range = value["range"].flatMap(RangeWrite.init) {
            self = .range(range)
        } else {
            return nil
        }
    }
    public var apiValue: APIValue {
        switch self {
        case .write(let write): return .object(pairs: [("write", write.apiValue)])
        case .range(let range): return .object(pairs: [("range", range.apiValue)])
        }
    }
}

extension SyncEntry {
    /// Decodes both bootstrap entry rows and `entry.*` change payloads; only
    /// `id` is required. Dates at or before the Go zero time decode as nil.
    public init?(_ value: APIValue) {
        guard let id = value["id"]?.int64Value, id > 0 else { return nil }
        func date(_ key: String) -> Date? {
            value[key]?.dateValue.flatMap { $0 > SyncTime.zero ? $0 : nil }
        }
        self.init(id: id,
                  subscriptionID: value["subscription_id"]?.int64Value ?? 0,
                  feedID: value["feed_id"]?.int64Value ?? 0,
                  seq: value["seq"]?.int64Value ?? 0,
                  guid: value["guid"]?.stringValue,
                  title: value["title"]?.stringValue ?? "",
                  url: value["url"]?.stringValue ?? "",
                  author: value["author"]?.stringValue ?? "",
                  publishedAt: date("published_at"),
                  updatedAt: date("updated_at"),
                  firstSeenAt: date("first_seen_at"),
                  language: value["language"]?.stringValue,
                  rev: value["rev"]?.int64Value ?? 0,
                  deleted: value["deleted"]?.boolValue ?? false,
                  hasBody: value["_has_body"]?.boolValue ?? (value["title"] != nil && value["url"] != nil))
    }
    /// `first_seen_at` and `published_at` are non-omitted `time.Time` fields in
    /// the reference model, so they emit the zero-time string when unset.
    public var apiValue: APIValue {
        .object(pairs: [
            ("id", .integer(id)),
            ("subscription_id", .integer(subscriptionID)),
            ("feed_id", .integer(feedID)),
            ("seq", .integer(seq)),
            ("guid", guid.map { .string($0) }),
            ("title", .string(title)),
            ("url", .string(url)),
            ("author", .string(author)),
            ("published_at", .string(SyncTime.string(publishedAt ?? SyncTime.zero))),
            ("updated_at", updatedAt.map { .string(SyncTime.string($0)) } ?? .null),
            ("first_seen_at", .string(SyncTime.string(firstSeenAt ?? SyncTime.zero))),
            ("language", language.map { .string($0) }),
            ("rev", .integer(rev)),
            ("deleted", .bool(deleted)),
            ("_has_body", .bool(hasBody)),
        ])
    }
    /// Entry age at `now` is measured from `first_seen_at`, never the
    /// untrusted publisher clock; absent clock means the zero time, and a
    /// first-seen in the future clamps to zero like the reference model.
    public func age(asOf now: Date) -> TimeInterval {
        max(0, now.timeIntervalSince(firstSeenAt ?? SyncTime.zero))
    }
}

extension SyncObject {
    /// Decodes a `syncmodel.Object`: `kind id principal entry_id folder_id
    /// label_id name rev deleted`. `kind` and `id` are required.
    public init?(_ value: APIValue) {
        guard let kind = value["kind"]?.stringValue.flatMap(ObjectKind.init(rawValue:)),
              let id = value["id"]?.int64Value, id > 0 else { return nil }
        self.init(kind: kind, id: id,
                  principal: value["principal"]?.int64Value ?? 0,
                  entryID: value["entry_id"]?.int64Value ?? 0,
                  folderID: value["folder_id"]?.int64Value ?? 0,
                  labelID: value["label_id"]?.int64Value ?? 0,
                  name: value["name"]?.stringValue ?? "",
                  rev: value["rev"]?.int64Value ?? 0,
                  deleted: value["deleted"]?.boolValue ?? false)
        fields = Self.extraFields(value)
    }
    /// A `subscriptions` bootstrap row or `subscription.*` payload is not a
    /// syncmodel.Object; its folder binding and title map onto one here.
    public init?(subscription value: APIValue) {
        guard let id = value["id"]?.int64Value, id > 0 else { return nil }
        self.init(kind: .subscription, id: id,
                  folderID: value["folder_id"]?.int64Value ?? 0,
                  name: value["title"]?.stringValue ?? value["name"]?.stringValue ?? "",
                  rev: value["rev"]?.int64Value ?? 0,
                  deleted: value["deleted"]?.boolValue ?? false)
        fields = Self.extraFields(value)
    }
    /// A `folders` bootstrap row or `folder.*` payload.
    public init?(folder value: APIValue) {
        guard let id = value["id"]?.int64Value, id > 0 else { return nil }
        self.init(kind: .folder, id: id,
                  name: value["name"]?.stringValue ?? "",
                  rev: value["rev"]?.int64Value ?? 0,
                  deleted: value["deleted"]?.boolValue ?? false)
        fields = Self.extraFields(value)
    }
    /// A `label_memberships` bootstrap row or membership body: `id label_id
    /// entry_id name principal rev deleted`. `entry_id` distinguishes it from
    /// a label definition, which has no entry binding.
    public init?(labelMembership value: APIValue) {
        guard let id = value["id"]?.int64Value, id > 0,
              let entryID = value["entry_id"]?.int64Value, entryID > 0 else { return nil }
        self.init(kind: .label, id: id,
                  principal: value["principal"]?.int64Value ?? 0,
                  entryID: entryID,
                  labelID: value["label_id"]?.int64Value ?? 0,
                  name: value["name"]?.stringValue ?? "",
                  rev: value["rev"]?.int64Value ?? 0,
                  deleted: value["deleted"]?.boolValue ?? false)
        fields = Self.extraFields(value)
    }
    /// `principal` is emitted unconditionally like the reference `Object`;
    /// the rest are `omitempty` fields.
    private static func extraFields(_ value: APIValue) -> [String: APIValue] {
        let canonical: Set<String> = ["kind", "id", "principal", "entry_id", "folder_id", "label_id", "name", "rev", "deleted"]
        return (value.objectValue ?? [:]).filter { !canonical.contains($0.key) }
    }
    public var apiValue: APIValue {
        let canonical = APIValue.object(pairs: [
            ("kind", .string(kind.rawValue)),
            ("id", .integer(id)),
            ("principal", .integer(principal)),
            ("entry_id", entryID == 0 ? nil : .integer(entryID)),
            ("folder_id", folderID == 0 ? nil : .integer(folderID)),
            ("label_id", labelID == 0 ? nil : .integer(labelID)),
            ("name", name.isEmpty ? nil : .string(name)),
            ("rev", .integer(rev)),
            ("deleted", .bool(deleted)),
        ])
        return .object(fields.merging(canonical.objectValue ?? [:]) { _, value in value })
    }
}

extension LabelDefinition {
    /// Label definitions use `id name rev deleted` on REST and sync surfaces.
    public init?(_ value: APIValue) {
        let id = value["id"]?.int64Value
        guard let id, id > 0 else { return nil }
        self.init(id: id,
                  name: value["name"]?.stringValue ?? "",
                  rev: value["rev"]?.int64Value ?? 0,
                  deleted: value["deleted"]?.boolValue ?? false)
    }
    public var apiValue: APIValue {
        .object(pairs: [
            ("id", .integer(id)),
            ("name", .string(name)),
            ("rev", .integer(rev)),
            ("deleted", .bool(deleted)),
        ])
    }
}

extension SyncAnnotation {
    /// Decodes `{"id","principal_id","kind","entry_ids","value","rev",
    /// "untrusted","deleted"}` from `annotation.*` payloads.
    public init?(_ value: APIValue) {
        guard let id = value["id"]?.int64Value, id > 0 else { return nil }
        self.init(id: id,
                  principalID: value["principal_id"]?.int64Value ?? 0,
                  kind: value["kind"]?.stringValue ?? "",
                  entryIDs: value["entry_ids"]?.arrayValue?.compactMap { $0.int64Value } ?? [],
                  value: value["value"]?.stringValue ?? "",
                  rev: value["rev"]?.int64Value ?? 0,
                  untrusted: value["untrusted"]?.boolValue ?? false,
                  deleted: value["deleted"]?.boolValue ?? false)
    }
    public var apiValue: APIValue {
        .object(pairs: [
            ("id", .integer(id)),
            ("principal_id", .integer(principalID)),
            ("kind", .string(kind)),
            ("entry_ids", .intArray(entryIDs)),
            ("value", .string(value)),
            ("rev", .integer(rev)),
            ("untrusted", .bool(untrusted)),
            ("deleted", .bool(deleted)),
        ])
    }
}

extension SyncPosition {
    public init?(_ value: APIValue) {
        guard let principal = value["principal"]?.int64Value, let view = value["view"]?.stringValue else { return nil }
        self.init(principal: principal, view: view, seq: value["seq"]?.int64Value ?? 0, rev: value["rev"]?.int64Value ?? 0)
    }
    public var apiValue: APIValue {
        .object(pairs: [
            ("principal", .integer(principal)),
            ("view", .string(view)),
            ("seq", .integer(seq)),
            ("rev", .integer(rev)),
        ])
    }
}

extension WriteAcknowledgement {
    public init?(_ value: APIValue) {
        guard let opID = value["op_id"]?.stringValue,
              let status = value["status"]?.stringValue.flatMap(WriteStatus.init(rawValue:)) else { return nil }
        let state = value["state"].flatMap(State.init)
        guard state != nil || status == .gone || status == .expired else { return nil }
        self.init(opID: opID, status: status, state: state,
                  replay: value["replay"]?.boolValue ?? false)
    }
    public var apiValue: APIValue {
        .object(pairs: [
            ("op_id", .string(opID)),
            ("status", .string(status.rawValue)),
            ("state", state.map { $0.apiValue } ?? .null),
            ("replay", .bool(replay)),
        ])
    }
}

extension RangeResult {
    public init?(_ value: APIValue) {
        guard let status = value["status"]?.stringValue.flatMap(WriteStatus.init(rawValue:)) else { return nil }
        let rawResults = value["results"]?.arrayValue ?? []
        let results = rawResults.compactMap(WriteAcknowledgement.init)
        let rawSubscriptions = value["subscriptions"]?.arrayValue ?? []
        let subscriptions = rawSubscriptions.compactMap { $0.int64Value }
        let rawExceptions = value["except"]?.arrayValue ?? []
        let exceptions = rawExceptions.compactMap { $0.int64Value }
        guard results.count == rawResults.count, subscriptions.count == rawSubscriptions.count,
              exceptions.count == rawExceptions.count else { return nil }
        self.init(status: status,
                  throughSeq: value["through_seq"]?.int64Value ?? 0,
                  subscriptions: subscriptions, except: exceptions, results: results,
                  replay: value["replay"]?.boolValue ?? false)
    }
    public var apiValue: APIValue {
        .object(pairs: [
            ("status", .string(status.rawValue)),
            ("through_seq", .integer(throughSeq)),
            ("subscriptions", .intArray(subscriptions)),
            ("except", .intArray(except)),
            ("results", .array(results.map { $0.apiValue })),
            ("replay", .bool(replay)),
        ])
    }
}

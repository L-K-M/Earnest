import Foundation

public struct Cursor: Codable, Equatable, Sendable {
    public var generation: String
    public var rev: Int64
    public init(generation: String, rev: Int64) { self.generation = generation; self.rev = rev }
}
public enum Register: String, Codable, Sendable { case seen, shelf, kept }
public enum RegisterValue: Codable, Equatable, Sendable {
    case text(String), flag(Bool)
    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if let flag = try? container.decode(Bool.self) { self = .flag(flag) }
        else { self = .text(try container.decode(String.self)) }
    }
    public func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self { case .text(let text): try container.encode(text); case .flag(let flag): try container.encode(flag) }
    }
}
/// One register's resolved stamp, mirroring the wire `{value, rev,
/// occurred_at, actor, source}` record. Provenance fields are optional so
/// partial state fragments still decode.
public struct Stamp: Codable, Equatable, Sendable {
    public var value: RegisterValue
    public var rev: Int64
    public var occurredAt: Date?
    public var actor: Int64
    public var source: String
    public init(value: RegisterValue, rev: Int64, occurredAt: Date? = nil, actor: Int64 = 0, source: String = "") {
        self.value = value
        self.rev = rev
        self.occurredAt = occurredAt
        self.actor = actor
        self.source = source
    }
    private enum CodingKeys: String, CodingKey { case value, rev, occurredAt = "occurred_at", actor, source }
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        value = try container.decode(RegisterValue.self, forKey: .value)
        rev = try container.decodeIfPresent(Int64.self, forKey: .rev) ?? 0
        var at: Date?
        if let text = try container.decodeIfPresent(String.self, forKey: .occurredAt) {
            at = SyncTime.parse(text)
        } else if let seconds = try container.decodeIfPresent(Double.self, forKey: .occurredAt) {
            at = Date(timeIntervalSinceReferenceDate: seconds)
        }
        occurredAt = at.flatMap { $0 > SyncTime.zero ? $0 : nil }
        actor = try container.decodeIfPresent(Int64.self, forKey: .actor) ?? 0
        source = try container.decodeIfPresent(String.self, forKey: .source) ?? ""
    }
    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(value, forKey: .value)
        try container.encode(rev, forKey: .rev)
        try container.encode(SyncTime.string(occurredAt ?? SyncTime.zero), forKey: .occurredAt)
        try container.encode(actor, forKey: .actor)
        try container.encode(source, forKey: .source)
    }
}
public struct State: Codable, Equatable, Sendable {
    public var principal: Int64
    public var entryID: Int64
    public var stateRev: Int64
    public var seen: Stamp
    public var shelf: Stamp
    public var kept: Stamp
    public init(principal: Int64, entryID: Int64) {
        self.principal = principal; self.entryID = entryID; stateRev = 0
        seen = Stamp(value: .text("unseen"), rev: 0); shelf = Stamp(value: .text("none"), rev: 0); kept = Stamp(value: .flag(false), rev: 0)
    }
    private enum CodingKeys: String, CodingKey { case principal, entryID = "entry_id", entryIDLegacy = "entryID", stateRev = "state_rev", stateRevLegacy = "stateRev", seen, shelf, kept }
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        principal = try container.decode(Int64.self, forKey: .principal)
        entryID = try container.decodeIfPresent(Int64.self, forKey: .entryID) ?? container.decode(Int64.self, forKey: .entryIDLegacy)
        stateRev = try container.decodeIfPresent(Int64.self, forKey: .stateRev) ?? container.decodeIfPresent(Int64.self, forKey: .stateRevLegacy) ?? 0
        let initial = State(principal: principal, entryID: entryID)
        seen = try container.decodeIfPresent(Stamp.self, forKey: .seen) ?? initial.seen
        shelf = try container.decodeIfPresent(Stamp.self, forKey: .shelf) ?? initial.shelf
        kept = try container.decodeIfPresent(Stamp.self, forKey: .kept) ?? initial.kept
    }
    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(principal, forKey: .principal)
        try container.encode(entryID, forKey: .entryID)
        try container.encode(stateRev, forKey: .stateRev)
        try container.encode(seen, forKey: .seen)
        try container.encode(shelf, forKey: .shelf)
        try container.encode(kept, forKey: .kept)
    }
    public subscript(_ register: Register) -> Stamp {
        get { switch register { case .seen: seen; case .shelf: shelf; case .kept: kept } }
        set { switch register { case .seen: seen = newValue; case .shelf: shelf = newValue; case .kept: kept = newValue } }
    }
    /// Per-register merge: an older fragment fills registers it wins and
    /// leaves newer registers alone; `state_rev` takes the maximum.
    public mutating func merge(_ incoming: State) {
        for register in [Register.seen, .shelf, .kept] where incoming[register].rev > self[register].rev { self[register] = incoming[register] }
        stateRev = max(stateRev, incoming.stateRev)
    }
}
public struct Write: Codable, Equatable, Sendable {
    public var opID: String
    public var principal: Int64
    public var entryID: Int64
    public var register: Register
    public var value: RegisterValue
    public var base: Cursor
    public var occurredAt: Date
    public var kind: OperationKind
    public var inFlight: Bool
    public init(opID: String, principal: Int64, entryID: Int64, register: Register, value: RegisterValue, base: Cursor, occurredAt: Date, kind: OperationKind = .explicit, inFlight: Bool = false) {
        self.opID = opID; self.principal = principal; self.entryID = entryID; self.register = register; self.value = value; self.base = base; self.occurredAt = occurredAt; self.kind = kind; self.inFlight = inFlight
    }
    private enum CodingKeys: String, CodingKey {
        case opID = "op_id", opIDLegacy = "opID", principal, entryID = "entry_id", entryIDLegacy = "entryID"
        case register, value, base = "base_rev", baseLegacy = "base", occurredAt = "occurred_at", occurredAtLegacy = "occurredAt"
        case kind, inFlight = "in_flight", inFlightLegacy = "inFlight"
    }
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        opID = try container.decodeIfPresent(String.self, forKey: .opID) ?? container.decode(String.self, forKey: .opIDLegacy)
        principal = try container.decode(Int64.self, forKey: .principal)
        entryID = try container.decodeIfPresent(Int64.self, forKey: .entryID) ?? container.decodeIfPresent(Int64.self, forKey: .entryIDLegacy) ?? 0
        register = try container.decode(Register.self, forKey: .register)
        value = try container.decode(RegisterValue.self, forKey: .value)
        base = try container.decodeIfPresent(Cursor.self, forKey: .base) ?? container.decode(Cursor.self, forKey: .baseLegacy)
        let clockKey: CodingKeys = container.contains(.occurredAt) ? .occurredAt : .occurredAtLegacy
        let clock = try container.decode(APIValue.self, forKey: clockKey)
        switch clock {
        case .string(let text):
            guard let parsed = SyncTime.parse(text) else {
                throw DecodingError.dataCorruptedError(forKey: clockKey, in: container, debugDescription: "unparseable occurred_at")
            }
            occurredAt = parsed
        case .number(let seconds): occurredAt = Date(timeIntervalSinceReferenceDate: seconds)
        case .integer(let seconds): occurredAt = Date(timeIntervalSinceReferenceDate: Double(seconds))
        default:
            throw DecodingError.dataCorruptedError(forKey: clockKey, in: container, debugDescription: "invalid occurred_at")
        }
        kind = try container.decodeIfPresent(OperationKind.self, forKey: .kind) ?? .explicit
        inFlight = try container.decodeIfPresent(Bool.self, forKey: .inFlight) ?? container.decodeIfPresent(Bool.self, forKey: .inFlightLegacy) ?? false
    }
    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(opID, forKey: .opID)
        try container.encode(principal, forKey: .principal)
        try container.encode(entryID, forKey: .entryID)
        try container.encode(register, forKey: .register)
        try container.encode(value, forKey: .value)
        try container.encode(base, forKey: .base)
        try container.encode(SyncTime.string(occurredAt), forKey: .occurredAt)
        try container.encode(kind, forKey: .kind)
        try container.encode(inFlight, forKey: .inFlight)
    }
}
/// The durable client cache: everything under `cursor` plus the ordered
/// outbox. Saving must atomically replace the whole snapshot.
public struct SyncSnapshot: Codable, Equatable, Sendable {
    public var cursor: Cursor?
    public var bootstrapComplete = false
    public var entries: [SyncEntry] = []
    public var states: [State] = []
    public var objects: [SyncObject] = []
    public var labelDefinitions: [LabelDefinition] = []
    public var annotations: [SyncAnnotation] = []
    public var positions: [SyncPosition] = []
    public var outbox: [OutboxItem] = []
    public init() {}
    private enum CodingKeys: String, CodingKey { case cursor, bootstrapComplete, entries, states, objects, labelDefinitions, annotations, positions, outbox }
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        cursor = try container.decodeIfPresent(Cursor.self, forKey: .cursor)
        bootstrapComplete = try container.decodeIfPresent(Bool.self, forKey: .bootstrapComplete) ?? false
        entries = try container.decodeIfPresent([SyncEntry].self, forKey: .entries) ?? []
        states = try container.decodeIfPresent([State].self, forKey: .states) ?? []
        objects = try container.decodeIfPresent([SyncObject].self, forKey: .objects) ?? []
        labelDefinitions = try container.decodeIfPresent([LabelDefinition].self, forKey: .labelDefinitions) ?? []
        annotations = try container.decodeIfPresent([SyncAnnotation].self, forKey: .annotations) ?? []
        positions = try container.decodeIfPresent([SyncPosition].self, forKey: .positions) ?? []
        let values = try container.decodeIfPresent([APIValue].self, forKey: .outbox) ?? []
        outbox = try values.map { value in
            let encoded = try JSONEncoder().encode(value)
            if value["write"] != nil || value["range"] != nil {
                return try JSONDecoder().decode(OutboxItem.self, from: encoded)
            }
            // The first client persisted untagged writes and numeric Foundation dates.
            return .write(try JSONDecoder().decode(Write.self, from: encoded))
        }
    }
}
public protocol SyncStore {
    func load() throws -> SyncSnapshot
    func save(_ snapshot: SyncSnapshot) throws
}
public final class MemorySyncStore: SyncStore {
    private var snapshot = SyncSnapshot()
    public init() {}
    public func load() throws -> SyncSnapshot { snapshot }
    public func save(_ snapshot: SyncSnapshot) throws { self.snapshot = snapshot }
}
public enum SyncError: Error { case invalidPage, invalidWrite, generationMismatch }
public enum WriteStatus: String, Codable, Sendable { case applied, superseded, gone, expired }
/// One operation's resolved outcome as returned by the server.
public struct WriteAcknowledgement: Codable, Equatable, Sendable {
    public var opID: String
    public var status: WriteStatus
    public var state: State?
    public var replay: Bool
    public init(opID: String, status: WriteStatus, state: State?, replay: Bool = false) {
        self.opID = opID; self.status = status; self.state = state; self.replay = replay
    }
    public init(from decoder: Decoder) throws {
        guard let decoded = WriteAcknowledgement(try APIValue(from: decoder)) else {
            throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "invalid result wire value"))
        }
        self = decoded
    }
    public func encode(to encoder: Encoder) throws { try apiValue.encode(to: encoder) }
}
public struct StateChange: Sendable {
    public var rev: Int64
    public var state: State
    public init(rev: Int64, state: State) { self.rev = rev; self.state = state }
}
/// The portable sync engine. Synchronous and owner-serialized: callers
/// serialize access (an actor, a queue) and every call loads and saves the
/// snapshot atomically through the `SyncStore`.
public final class SyncEngine {
    private let store: any SyncStore
    public init(store: any SyncStore) { self.store = store }
    public func snapshot() throws -> SyncSnapshot { try store.load() }

    /// Called with the cursor read BEFORE paging resources. Replaces the
    /// server cache but retains local intent; a new generation rebases pending
    /// intent onto `{generation, 0}` so it competes stale rather than forcing a
    /// match, preserving the original action clocks.
    public func bootstrap(_ cursor: Cursor) throws {
        var data = try store.load()
        if let previous = data.cursor, previous.generation != cursor.generation {
            data.outbox = data.outbox.map { $0.rebased(generation: cursor.generation) }
        }
        data.cursor = cursor
        data.bootstrapComplete = false
        data.entries = []; data.states = []; data.objects = []; data.labelDefinitions = []; data.annotations = []; data.positions = []
        try store.save(data)
    }
    /// A crash during paging must restart bootstrap rather than trust a partial cache.
    public func finishBootstrap() throws {
        var data = try store.load()
        guard data.cursor != nil else { throw SyncError.invalidPage }
        data.bootstrapComplete = true
        try store.save(data)
    }
    public func mergeBootstrap(_ states: [State]) throws {
        var data = try store.load()
        for state in states { merge(state, into: &data.states) }
        try store.save(data)
    }
    /// Read these resources before pulling changes because bootstrap omits them.
    /// Replacing the active mute set also removes deletions older than our cursor.
    public func replaceRiverResources(mutes: [APIValue], settings: APIValue) throws {
        var data = try store.load()
        let objects = try mutes.map { try require(SyncObject(mute: $0), "mutes") }
        let settingsObject = try require(SyncObject(settings: settings), "settings")
        data.objects.removeAll { $0.kind == .mute }
        for object in objects { merge(object: object, into: &data) }
        merge(object: settingsObject, into: &data)
        try store.save(data)
    }

    /// Merge a fetched list or individual entry response only into its request's generation.
    public func mergeEntries(_ entries: [APIEntry], generation: String) throws {
        var data = try store.load()
        guard data.cursor?.generation == generation else { throw SyncError.generationMismatch }
        for entry in entries {
            let item = try JSONDecoder().decode(APIValue.self, from: JSONEncoder().encode(entry))
            merge(entry: try require(SyncEntry(item), "entries"), into: &data)
            merge(state: try require(State(item["state"] ?? .null), "state"), into: &data)
        }
        try store.save(data)
    }
    /// Merge one `/v1/bootstrap/{kind}` page. Kinds are the endpoint's names:
    /// "entries", "state", "subscriptions", "folders", "labels",
    /// "label_memberships", "positions". Pages merge idempotently by revision.
    public func mergeBootstrap(kind: String, items: [APIValue]) throws {
        var data = try store.load()
        switch kind {
        case "entries":
            for item in items { merge(entry: try require(SyncEntry(item), kind), into: &data) }
        case "state":
            for item in items { merge(state: try require(State(item), kind), into: &data) }
        case "subscriptions":
            for item in items { merge(object: try require(SyncObject(subscription: item), kind), into: &data) }
        case "folders":
            for item in items { merge(object: try require(SyncObject(folder: item), kind), into: &data) }
        case "labels":
            for item in items { merge(label: try require(LabelDefinition(item), kind), into: &data) }
        case "objects":
            for item in items { merge(object: try require(SyncObject(item), kind), into: &data) }
        case "label_memberships":
            for item in items { merge(object: try require(SyncObject(labelMembership: item), kind), into: &data) }
        case "annotations":
            for item in items { merge(annotation: try require(SyncAnnotation(item), kind), into: &data) }
        case "positions":
            for item in items { merge(position: try require(SyncPosition(item), kind), into: &data) }
        default:
            throw SyncError.invalidPage
        }
        try store.save(data)
    }

    /// Apply one captured change page atomically, including its cursor. The
    /// page must be contiguous above the current cursor and land exactly on
    /// `next`; a truncated page or a gap is rejected with nothing applied, and
    /// replaying an already-applied page is a no-op.
    public func apply(_ changes: [APIChange], next: Cursor) throws {
        var data = try store.load()
        guard let cursor = data.cursor, next.generation == cursor.generation else { throw SyncError.generationMismatch }
        if next.rev <= cursor.rev { return }
        var expected = cursor.rev
        var previous: Int64?
        for change in changes {
            if let previous {
                guard previous < Int64.max, change.rev == previous + 1 else { throw SyncError.invalidPage }
            }
            previous = change.rev
            if change.rev > cursor.rev {
                guard expected < Int64.max, change.rev == expected + 1 else { throw SyncError.invalidPage }
                expected = change.rev
            }
        }
        guard next.rev == expected else { throw SyncError.invalidPage }
        for change in changes where change.rev > cursor.rev {
            try applyChange(change, into: &data)
        }
        data.cursor = next
        try store.save(data)
    }
    /// A state.updated batch resolves registers; a state.range resolves the
    /// membership the server committed. Both merge per register and never
    /// create state for unknown or deleted entries.
    private func applyChange(_ change: APIChange, into data: inout SyncSnapshot) throws {
        let payload = change.payload
        func decoded<T>(_ value: APIValue, _ decode: (APIValue) -> T?) throws -> T {
            guard let result = decode(value) else { throw SyncError.invalidPage }
            return result
        }
        func pick<T>(_ key: String, _ decode: (APIValue) -> T?) throws -> T {
            try decoded(payload[key] ?? payload, decode)
        }
        func results() throws -> [WriteAcknowledgement] {
            guard let items = (payload["results"] ?? payload).arrayValue else { throw SyncError.invalidPage }
            return try items.map { try decoded($0, WriteAcknowledgement.init) }
        }
        func mergeResultStates(_ results: [WriteAcknowledgement]) {
            for result in results {
                guard let state = result.state,
                      let entry = data.entries.first(where: { $0.id == state.entryID }),
                      !entry.deleted else { continue }
                merge(state: state, into: &data)
            }
        }
        switch change.type {
        case "entry.created", "entry.updated", "entry.deleted":
            let raw = payload["entry"] ?? payload
            var entry = try decoded(raw, SyncEntry.init)
            if raw["rev"] == nil { entry.rev = change.rev }
            if change.type == "entry.deleted" { entry.deleted = true }
            merge(entry: entry, into: &data)
        case "state.updated":
            mergeResultStates(try results())
        case "state.range":
            mergeResultStates(try pick("range", RangeResult.init).results)
        case "mute.updated", "mute.deleted":
            merge(object: try pick("mute", SyncObject.init(mute:)), into: &data)
        case "settings.updated":
            merge(object: try pick("settings", SyncObject.init(settings:)), into: &data)
        case "position.updated":
            merge(position: try pick("position", SyncPosition.init), into: &data)
        case "label.added", "label.removed",
             "subscription.created", "subscription.updated", "subscription.deleted",
             "folder.updated", "folder.deleted",
             "annotation.updated", "annotation.deleted", "token.revoked":
            try applyResourceChange(change, into: &data)
        default:
            break
        }
    }
    /// Resource changes carry `{"object": <syncmodel.Object>}` where `kind`
    /// selects the resource. Named keys are tolerated for typed bodies:
    /// `{"subscription": row}`, `{"folder": row}`, `{"label": membership or
    /// definition}`, `{"annotation": body}`, plus a bare token ID payload.
    private func applyResourceChange(_ change: APIChange, into data: inout SyncSnapshot) throws {
        let payload = change.payload
        var merged = false
        if let raw = payload["object"] {
            guard let object = SyncObject(raw) else { throw SyncError.invalidPage }
            merge(object: object, into: &data)
            merged = true
        }
        if let raw = payload["subscription"], let object = SyncObject(subscription: raw) {
            merge(object: object, into: &data)
            merged = true
        }
        if let raw = payload["folder"], let object = SyncObject(folder: raw) {
            merge(object: object, into: &data)
            merged = true
        }
        if let raw = payload["annotation"], let annotation = SyncAnnotation(raw) {
            merge(annotation: annotation, into: &data)
            merged = true
        }
        if let raw = payload["label"] {
            if let object = SyncObject(labelMembership: raw) {
                merge(object: object, into: &data)
            } else if let label = LabelDefinition(raw) {
                merge(label: label, into: &data)
            } else {
                throw SyncError.invalidPage
            }
            merged = true
        }
        if !merged, change.type == "token.revoked",
           let id = payload["id"]?.int64Value ?? payload.int64Value, id > 0 {
            merge(object: SyncObject(kind: .token, id: id, rev: change.rev, deleted: true), into: &data)
            merged = true
        }
        if !merged {
            if let object = SyncObject(payload) {
                merge(object: object, into: &data)
            } else {
                throw SyncError.invalidPage
            }
        }
    }
    /// Legacy states-only page application, kept for earlier callers.
    public func apply(_ changes: [StateChange], next: Cursor) throws {
        var data = try store.load()
        guard let cursor = data.cursor, cursor.generation == next.generation else { throw SyncError.generationMismatch }
        if next.rev <= cursor.rev { return }
        var expected = cursor.rev
        var previous: Int64?
        for change in changes {
            if let previous {
                guard previous < Int64.max, change.rev == previous + 1 else { throw SyncError.invalidPage }
            }
            previous = change.rev
            if change.rev > cursor.rev {
                guard expected < Int64.max, change.rev == expected + 1 else { throw SyncError.invalidPage }
                expected = change.rev
            }
        }
        guard next.rev == expected else { throw SyncError.invalidPage }
        for change in changes where change.rev > cursor.rev { merge(change.state, into: &data.states) }
        data.cursor = next
        try store.save(data)
    }

    /// Queue one register intent. Pending intent coalesces per principal,
    /// entry and register, keeping the oldest observed base and the newest
    /// action time and operation ID; an in-flight operation is never replaced.
    /// Unread on a Done item also queues a shelf-none intent under
    /// `opID + "/clear-done"`, which survives later coalescing.
    public func queue(_ write: Write) throws {
        guard write.principal > 0, write.entryID > 0, !write.opID.isEmpty,
              !write.opID.hasSuffix("/clear-done"), valid(write.register, write.value),
              write.occurredAt > SyncTime.zero else { throw SyncError.invalidWrite }
        var data = try store.load()
        // Queue rejects a stale base generation as invalid like the reference
        // model: only acknowledgements and pages report a generation error.
        guard data.cursor?.generation == write.base.generation else { throw SyncError.invalidWrite }
        for item in data.outbox where item.opID == write.opID { throw SyncError.invalidWrite }
        let clearDone = write.register == .seen && write.value == .text("unseen")
            && overlay(principal: write.principal, entryID: write.entryID, data: data).shelf.value == .text("done")
        if clearDone {
            for item in data.outbox where item.opID == write.opID + "/clear-done" { throw SyncError.invalidWrite }
            var clear = write
            clear.opID += "/clear-done"; clear.register = .shelf; clear.value = .text("none")
            clear.base.rev = serverState(principal: write.principal, entryID: write.entryID, data: data).shelf.rev
            coalesce(clear, into: &data.outbox)
        }
        coalesce(write, into: &data.outbox)
        try store.save(data)
    }
    /// Queue a range as one first-class outbox item, retaining its scope,
    /// fixed `throughSeq` boundary and original action clock. It stays in
    /// submit order with register writes and is never expanded locally.
    public func queueRange(_ range: RangeWrite) throws {
        let write = range.write
        guard !write.opID.isEmpty, write.principal > 0, !write.opID.hasSuffix("/clear-done"),
              write.register == .seen, write.value == .text("seen"),
              write.occurredAt > SyncTime.zero else { throw SyncError.invalidWrite }
        switch range.scope.kind {
        case .all: guard range.scope.ids.isEmpty else { throw SyncError.invalidWrite }
        case .subscriptions: guard !range.scope.ids.isEmpty else { throw SyncError.invalidWrite }
        case .folder: guard range.scope.ids.count == 1 else { throw SyncError.invalidWrite }
        }
        guard range.scope.ids.allSatisfy({ $0 > 0 }) else { throw SyncError.invalidWrite }
        var data = try store.load()
        guard data.cursor?.generation == write.base.generation else { throw SyncError.invalidWrite }
        for item in data.outbox where item.opID == write.opID { throw SyncError.invalidWrite }
        data.outbox.append(.range(range))
        try store.save(data)
    }

    /// Client state is server state plus the pending overlay, applied in
    /// outbox order. A locally deleted entry takes no overlay. Queued ranges
    /// overlay only locally cached members, never uncached ones.
    public func state(principal: Int64, entryID: Int64) throws -> State {
        overlay(principal: principal, entryID: entryID, data: try store.load())
    }
    public func entry(_ id: Int64) throws -> SyncEntry? {
        try store.load().entries.first { $0.id == id }
    }
    public func object(kind: ObjectKind, id: Int64) throws -> SyncObject? {
        try store.load().objects.first { $0.kind == kind && $0.id == id }
    }
    public func labelDefinition(_ id: Int64) throws -> LabelDefinition? {
        try store.load().labelDefinitions.first { $0.id == id }
    }
    public func position(principal: Int64, view: String) throws -> SyncPosition? {
        try store.load().positions.first { $0.principal == principal && $0.view == view }
    }
    /// Direct merges for bootstrap pages and supplied wire fragments. Each
    /// merges independently by revision and, for state, per register.
    public func mergeEntry(_ entry: SyncEntry) throws {
        var data = try store.load(); merge(entry: entry, into: &data); try store.save(data)
    }
    /// A body fetch returning 404 is authoritative even before its deletion event arrives.
    public func markGone(entryID: Int64) throws {
        guard entryID > 0 else { throw SyncError.invalidPage }
        var data = try store.load()
        if let index = data.entries.firstIndex(where: { $0.id == entryID }) {
            data.entries[index].deleted = true
        } else {
            data.entries.append(SyncEntry(id: entryID, deleted: true))
        }
        try store.save(data)
    }
    public func mergeState(_ state: State) throws {
        var data = try store.load(); merge(state: state, into: &data); try store.save(data)
    }
    public func mergeObject(_ object: SyncObject) throws {
        var data = try store.load(); merge(object: object, into: &data); try store.save(data)
    }
    public func mergePosition(_ position: SyncPosition) throws {
        var data = try store.load(); merge(position: position, into: &data); try store.save(data)
    }
    public func mergeLabelDefinition(_ label: LabelDefinition) throws {
        var data = try store.load(); merge(label: label, into: &data); try store.save(data)
    }
    public func mergeAnnotation(_ annotation: SyncAnnotation) throws {
        var data = try store.load(); merge(annotation: annotation, into: &data); try store.save(data)
    }

    /// Flag operations sent but not yet acknowledged. Retries must reuse their
    /// exact payload; queueing never replaces an in-flight item, and a
    /// subsequent action takes a new operation ID.
    public func markInFlight(opIDs: Set<String>) throws {
        var data = try store.load()
        for index in data.outbox.indices where opIDs.contains(data.outbox[index].opID) {
            data.outbox[index] = data.outbox[index].markedInFlight
        }
        try store.save(data)
    }
    public func acknowledge(opID: String, generation: String) throws {
        var data = try store.load()
        guard data.cursor?.generation == generation else { throw SyncError.generationMismatch }
        data.outbox.removeAll { $0.opID == opID && $0.base.generation == generation }
        try store.save(data)
    }
    /// Persist effective states and remove only acknowledged operations
    /// together. `generation` is the request's generation: a response from a
    /// superseded generation is rejected whole, before merging or removal, so
    /// the preserved intent still replays into the new generation. Returns
    /// gone and expired operations so the caller can report them.
    @discardableResult public func acknowledge(_ results: [WriteAcknowledgement], generation: String) throws -> [OutboxItem] {
        var data = try store.load()
        guard data.cursor?.generation == generation else { throw SyncError.generationMismatch }
        guard Set(results.map(\.opID)).count == results.count else { throw SyncError.invalidPage }
        var dropped: [OutboxItem] = []
        for result in results {
            guard let pending = data.outbox.first(where: { $0.opID == result.opID && $0.base.generation == generation }) else { continue }
            if let state = result.state, state.principal == pending.principal, state.entryID == pending.entryID {
                merge(state, into: &data.states)
            } else if result.status == .applied || result.status == .superseded {
                throw SyncError.invalidPage
            }
            if result.status == .gone {
                let entryID = result.state?.entryID ?? pending.entryID
                if let index = data.entries.firstIndex(where: { $0.id == entryID }) {
                    data.entries[index].deleted = true
                }
            }
            if result.status == .gone || result.status == .expired {
                dropped.append(pending)
            }
            data.outbox.removeAll { $0.opID == result.opID && $0.base.generation == generation }
        }
        try store.save(data)
        return dropped
    }
    /// Adopt a queued range's resolved states and remove that exact item,
    /// under the same generation rule as `acknowledge(_:generation:)`.
    /// Returns the range when the result reports it gone or expired.
    @discardableResult public func acknowledgeRange(opID: String, generation: String, result: RangeResult) throws -> [OutboxItem] {
        var data = try store.load()
        guard data.cursor?.generation == generation else { throw SyncError.generationMismatch }
        for item in result.results {
            if let state = item.state { merge(state, into: &data.states) }
        }
        var dropped: [OutboxItem] = []
        if result.status == .gone || result.status == .expired,
           let pending = data.outbox.first(where: { $0.opID == opID && $0.base.generation == generation }) {
            dropped.append(pending)
        }
        data.outbox.removeAll { $0.opID == opID && $0.base.generation == generation }
        try store.save(data)
        return dropped
    }
    /// Drop queued intent older than the 30-day validity bound, strictly;
    /// exactly 30 days survives. Returns the dropped items for reporting.
    @discardableResult public func expire(asOf now: Date) throws -> [OutboxItem] {
        var data = try store.load()
        let expired = data.outbox.filter { now.timeIntervalSince($0.occurredAt) > 30 * 24 * 60 * 60 }
        data.outbox.removeAll { now.timeIntervalSince($0.occurredAt) > 30 * 24 * 60 * 60 }
        try store.save(data)
        return expired
    }

    private func require<T>(_ value: T?, _ context: String) throws -> T {
        guard let value else { throw SyncError.invalidPage }
        return value
    }
    private func valid(_ register: Register, _ value: RegisterValue) -> Bool {
        switch (register, value) {
        case (.seen, .text("seen")), (.seen, .text("unseen")), (.shelf, .text("none")), (.shelf, .text("later")), (.shelf, .text("done")), (.kept, .flag): true
        default: false
        }
    }
    private func serverState(principal: Int64, entryID: Int64, data: SyncSnapshot) -> State {
        data.states.first { $0.principal == principal && $0.entryID == entryID } ?? State(principal: principal, entryID: entryID)
    }
    private func merge(_ state: State, into states: inout [State]) {
        if let index = states.firstIndex(where: { $0.principal == state.principal && $0.entryID == state.entryID }) { states[index].merge(state) }
        else { states.append(state) }
    }
    private func merge(state: State, into data: inout SyncSnapshot) {
        merge(state, into: &data.states)
    }
    private func merge(entry: SyncEntry, into data: inout SyncSnapshot) {
        if let index = data.entries.firstIndex(where: { $0.id == entry.id }) {
            let previous = data.entries[index]
            guard !previous.deleted, entry.rev > previous.rev || (entry.rev == previous.rev && entry.hasBody && !previous.hasBody) else { return }
            var incoming = entry
            if !entry.hasBody {
                incoming.title = previous.title
                incoming.url = previous.url
                incoming.author = previous.author
                incoming.guid = previous.guid
                incoming.language = previous.language
                if incoming.subscriptionID == 0 { incoming.subscriptionID = previous.subscriptionID }
                if incoming.feedID == 0 { incoming.feedID = previous.feedID }
                if incoming.seq == 0 { incoming.seq = previous.seq }
                if incoming.firstSeenAt == nil { incoming.firstSeenAt = previous.firstSeenAt }
            }
            data.entries[index] = incoming
        } else {
            data.entries.append(entry)
        }
    }
    private func merge(object: SyncObject, into data: inout SyncSnapshot) {
        if let index = data.objects.firstIndex(where: { $0.kind == object.kind && $0.id == object.id }) {
            if object.rev > data.objects[index].rev {
                var incoming = object
                incoming.fields = data.objects[index].fields.merging(object.fields) { _, value in value }
                data.objects[index] = incoming
            }
        } else {
            data.objects.append(object)
        }
    }
    private func merge(label: LabelDefinition, into data: inout SyncSnapshot) {
        if let index = data.labelDefinitions.firstIndex(where: { $0.id == label.id }) {
            if label.rev > data.labelDefinitions[index].rev { data.labelDefinitions[index] = label }
        } else {
            data.labelDefinitions.append(label)
        }
    }
    private func merge(annotation: SyncAnnotation, into data: inout SyncSnapshot) {
        if let index = data.annotations.firstIndex(where: { $0.id == annotation.id }) {
            if annotation.rev > data.annotations[index].rev { data.annotations[index] = annotation }
        } else {
            data.annotations.append(annotation)
        }
    }
    private func merge(position: SyncPosition, into data: inout SyncSnapshot) {
        if let index = data.positions.firstIndex(where: { $0.principal == position.principal && $0.view == position.view }) {
            if position.rev > data.positions[index].rev { data.positions[index] = position }
        } else {
            data.positions.append(position)
        }
    }
    private func coalesce(_ write: Write, into outbox: inout [OutboxItem]) {
        var next = write
        if let index = outbox.lastIndex(where: {
            guard case .write(let old) = $0 else { return false }
            return !old.inFlight && old.principal == write.principal && old.entryID == write.entryID && old.register == write.register
        }) {
            next.base = outbox[index].base
            outbox.remove(at: index)
        }
        outbox.append(.write(next))
    }
    private func rangeContains(_ range: RangeWrite, entry: SyncEntry, data: SyncSnapshot) -> Bool {
        if entry.seq > range.throughSeq { return false }
        switch range.scope.kind {
        case .all:
            return true
        case .subscriptions:
            return range.scope.ids.contains(entry.subscriptionID)
        case .folder:
            guard let subscription = data.objects.first(where: { $0.kind == .subscription && $0.id == entry.subscriptionID }) else { return false }
            return !subscription.deleted && subscription.folderID == range.scope.ids[0]
        }
    }
    private func overlay(principal: Int64, entryID: Int64, data: SyncSnapshot) -> State {
        var result = serverState(principal: principal, entryID: entryID, data: data)
        // Pending intent only shows against a cached, live entry: the server
        // is authoritative for everything else.
        guard let cached = data.entries.first(where: { $0.id == entryID }), !cached.deleted else { return result }
        for item in data.outbox {
            switch item {
            case .range(let range):
                guard range.write.principal == principal, rangeContains(range, entry: cached, data: data) else { continue }
                result.seen.value = .text("seen")
            case .write(let write):
                guard write.principal == principal, write.entryID == entryID else { continue }
                switch write.register {
                case .seen:
                    result.seen.value = write.value
                    if write.value == .text("unseen") && result.shelf.value == .text("done") { result.shelf.value = .text("none") }
                case .shelf:
                    result.shelf.value = write.value
                case .kept:
                    result.kept.value = write.value
                }
            }
        }
        return result
    }
}

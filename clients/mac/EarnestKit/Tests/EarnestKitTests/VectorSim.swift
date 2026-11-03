import Foundation

/// In-memory reference simulator for the vector server, ported line-for-line
/// from internal/syncmodel (model.go, objects.go, compat.go, resources.go).
/// It performs no I/O and reads no clock: every step supplies its receipt
/// time, so the runner is deterministic. Values serialize to the Go wire
/// shapes so `expect` subsets compare against the documented protocol.

enum SimError: Error, Equatable {
    case invalid, generation, cursorGone, withheld, attempted
    var name: String {
        switch self {
        case .invalid: return "invalid"
        case .generation: return "generation"
        case .cursorGone: return "cursor_gone"
        case .withheld: return "withheld"
        case .attempted: return "attempted"
        }
    }
}

enum SimSource: String, Sendable { case passive, ruleOrAgent = "rule_or_agent", deliberate }
enum SimStatus: String, Sendable { case applied, superseded, gone, expired }

struct SimCursor: Equatable, Sendable {
    var generation: String
    var rev: Int64
    var json: VJSON { .object(["generation": .string(generation), "rev": .int(rev)]) }
}

struct SimRegister: Equatable, Sendable {
    var value: VJSON = .string("unseen")
    var rev: Int64 = 0
    var occurredAt: Date = VectorTime.zero
    var actor: Int64 = 0
    var source: SimSource = .passive
    var json: VJSON {
        .object(["value": value, "rev": .int(rev), "occurred_at": .string(VectorTime.string(occurredAt)),
                 "actor": .int(actor), "source": .string(source.rawValue)])
    }
}

struct SimState: Equatable, Sendable {
    var principal: Int64
    var entryID: Int64
    var stateRev: Int64 = 0
    var seen = SimRegister()
    var shelf = SimRegister(value: .string("none"))
    var kept = SimRegister(value: .bool(false))

    static func initial(_ principal: Int64, _ entryID: Int64) -> SimState { SimState(principal: principal, entryID: entryID) }
    func register(_ name: String) -> SimRegister {
        switch name {
        case "seen": return seen
        case "shelf": return shelf
        case "kept": return kept
        default: return SimRegister()
        }
    }
    mutating func setRegister(_ name: String, _ register: SimRegister) {
        switch name {
        case "seen": seen = register
        case "shelf": shelf = register
        case "kept": kept = register
        default: break
        }
    }
    var unread: Bool { seen.value == .string("unseen") && shelf.value != .string("done") }
    var googleRead: Bool { seen.value == .string("seen") || shelf.value == .string("done") }
    var json: VJSON {
        .object(["principal": .int(principal), "entry_id": .int(entryID), "state_rev": .int(stateRev),
                 "seen": seen.json, "shelf": shelf.json, "kept": kept.json])
    }
}

struct SimEntry: Equatable, Sendable {
    var id: Int64 = 0
    var subscriptionID: Int64 = 0
    var seq: Int64 = 0
    var firstSeenAt: Date = VectorTime.zero
    var publishedAt: Date = VectorTime.zero
    var rev: Int64 = 0
    var deleted = false
    var json: VJSON {
        .object(["id": .int(id), "subscription_id": .int(subscriptionID), "seq": .int(seq),
                 "first_seen_at": .string(VectorTime.string(firstSeenAt)),
                 "published_at": .string(VectorTime.string(publishedAt)),
                 "rev": .int(rev), "deleted": .bool(deleted)])
    }
    func age(_ now: Date) -> Double {
        now < firstSeenAt ? 0 : now.timeIntervalSince(firstSeenAt)
    }
}

struct SimWrite: Equatable, Sendable {
    var opID = ""
    var principal: Int64 = 0
    var entryID: Int64 = 0
    var register = ""
    var value: VJSON = .null
    var base = SimCursor(generation: "", rev: 0)
    var occurredAt: Date = VectorTime.zero
    var kind = ""
}

struct SimScope: Equatable, Sendable {
    var kind = ""
    var ids: [Int64] = []
}

struct SimRangeWrite: Equatable, Sendable {
    var write: SimWrite
    var scope: SimScope
    var throughSeq: Int64
}

struct SimResult: Equatable, Sendable {
    var opID = ""
    var status: SimStatus = .applied
    var state = SimState(principal: 0, entryID: 0)
    var replay = false
    var json: VJSON {
        .object(["op_id": .string(opID), "status": .string(status.rawValue), "state": state.json, "replay": .bool(replay)])
    }
}

struct SimRangeResult: Equatable, Sendable {
    var status: SimStatus = .applied
    var throughSeq: Int64 = 0
    var subscriptions: [Int64]? = nil
    var except: [Int64]? = nil
    var results: [SimResult]? = nil
    var replay = false
    var json: VJSON {
        .object(["status": .string(status.rawValue), "through_seq": .int(throughSeq),
                 "subscriptions": subscriptions.map { .array($0.map { .int($0) }) } ?? .null,
                 "except": except.map { .array($0.map { .int($0) }) } ?? .null,
                 "results": results.map { .array($0.map { $0.json }) } ?? .null,
                 "replay": .bool(replay)])
    }
}

struct SimObject: Equatable, Sendable {
    var kind = ""
    var id: Int64 = 0
    var principal: Int64 = 0
    var entryID: Int64 = 0
    var folderID: Int64 = 0
    var name = ""
    var rev: Int64 = 0
    var deleted = false
    var json: VJSON {
        // Go omitempty: entry_id, folder_id and name drop out at zero/empty.
        var object: [String: VJSON] = ["kind": .string(kind), "id": .int(id), "principal": .int(principal),
                                       "rev": .int(rev), "deleted": .bool(deleted)]
        if entryID != 0 { object["entry_id"] = .int(entryID) }
        if folderID != 0 { object["folder_id"] = .int(folderID) }
        if !name.isEmpty { object["name"] = .string(name) }
        return .object(object)
    }
}

struct SimPosition: Equatable, Sendable {
    var principal: Int64 = 0
    var view = ""
    var seq: Int64 = 0
    var rev: Int64 = 0
    var json: VJSON {
        .object(["principal": .int(principal), "view": .string(view), "seq": .int(seq), "rev": .int(rev)])
    }
}

struct SimChange: Equatable, Sendable {
    var rev: Int64 = 0
    var type = ""
    var entry: SimEntry? = nil
    var results: [SimResult]? = nil
    var range: SimRangeResult? = nil
    var object: SimObject? = nil
    var position: SimPosition? = nil
    var json: VJSON {
        var body: [String: VJSON] = ["rev": .int(rev), "type": .string(type)]
        if let entry { body["entry"] = entry.json }
        if let results { body["results"] = .array(results.map { $0.json }) }
        if let range { body["range"] = range.json }
        if let object { body["object"] = object.json }
        if let position { body["position"] = position.json }
        return .object(body)
    }
}

struct SimNotification: Equatable, Sendable {
    var actor: Int64 = 0
    var opID = ""
    var generation = ""
    var status = "pending" // pending | uncertain | delivered
    var replay = false
    var attemptedAt: Date = VectorTime.zero
    var json: VJSON {
        .object(["actor": .int(actor), "op_id": .string(opID), "generation": .string(generation),
                 "status": .string(status), "replay": .bool(replay),
                 "attempted_at": .string(VectorTime.string(attemptedAt))])
    }
}

struct SimContext: Sendable {
    var actor: Int64 = 1
    var token = "human"
    var receipt: Date
    /// Source from the trusted token and operation kind: explicit and passive
    /// stay distinct for human/compat; agent and rule always write
    /// rule_or_agent regardless of kind; anything else is rejected.
    func source(kind: String) -> SimSource? {
        guard kind == "explicit" || kind == "passive" else { return nil }
        switch token {
        case "agent", "rule": return .ruleOrAgent
        case "human", "compat": return kind == "passive" ? .passive : .deliberate
        default: return nil
        }
    }
}

struct SimResources {
    var entries: [SimEntry] = []
    var states: [SimState] = []
    var objects: [SimObject] = []
    var positions: [SimPosition] = []
}

/// The single-user reference machine. Committed revisions are gapless; every
/// accepted mutation appends exactly one typed change.
final class SimModel {
    private(set) var generation: String
    private(set) var rev: Int64 = 0
    private(set) var seq: Int64 = 0
    private(set) var floor: Int64 = 0
    private(set) var entries: [Int64: SimEntry] = [:]
    private var states: [String: SimState] = [:]
    private var operations: [String: SimRemembered] = [:]
    private(set) var changes: [SimChange] = []
    private var objects: [String: SimObject] = [:]
    private var positions: [String: SimPosition] = [:]
    private var notifications: [String: SimNotification] = [:]
    private var withheld = false

    struct SimRemembered {
        var principal: Int64 = 0
        var entry: Int64 = 0
        var status: SimStatus = .applied
        var rangeResult: SimRangeResult? = nil
    }

    init(generation: String) throws {
        guard !generation.isEmpty else { throw SimError.invalid }
        self.generation = generation
    }

    var cursor: SimCursor { SimCursor(generation: generation, rev: rev) }
    func entry(_ id: Int64) -> SimEntry? { entries[id] }
    func state(_ principal: Int64, _ entryID: Int64) -> SimState {
        states["\(principal)/\(entryID)"] ?? .initial(principal, entryID)
    }
    func object(_ kind: String, _ id: Int64) -> SimObject? { objects["\(kind)/\(id)"] }
    func position(_ principal: Int64, _ view: String) -> SimPosition? { positions["\(principal)/\(view)"] }
    func notification(_ actor: Int64, _ opID: String) -> SimNotification? { notifications["\(actor)/\(opID)"] }

    private func append(_ change: SimChange) {
        rev += 1
        var copy = change
        copy.rev = rev
        changes.append(copy)
    }

    // MARK: entries

    func ingest(_ e: SimEntry, receipt: Date) throws -> SimEntry {
        guard e.id > 0, e.subscriptionID > 0, receipt != VectorTime.zero, !e.deleted else { throw SimError.invalid }
        guard entries[e.id] == nil else { throw SimError.invalid }
        var committed = e
        committed.seq = e.seq == 0 ? seq + 1 : e.seq
        guard committed.seq > seq else { throw SimError.invalid }
        committed.firstSeenAt = receipt
        committed.rev = rev + 1
        seq = committed.seq
        entries[committed.id] = committed
        append(SimChange(type: "entry.created", entry: committed))
        return committed
    }
    func updateEntry(_ id: Int64, published: Date) throws {
        guard var e = entries[id], !e.deleted else { throw SimError.invalid }
        e.publishedAt = published
        e.rev = rev + 1
        entries[id] = e
        append(SimChange(type: "entry.updated", entry: e))
    }
    func deleteEntry(_ id: Int64) throws {
        guard var e = entries[id], !e.deleted else { throw SimError.invalid }
        e.deleted = true
        e.rev = rev + 1
        entries[id] = e
        append(SimChange(type: "entry.deleted", entry: e))
    }

    // MARK: writes

    private static func validValue(_ register: String, _ value: VJSON) -> Bool {
        switch register {
        case "seen": return value == .string("unseen") || value == .string("seen")
        case "shelf": return value == .string("none") || value == .string("later") || value == .string("done")
        case "kept": return value == .bool(true) || value == .bool(false)
        default: return false
        }
    }
    private func validateWrite(_ ctx: SimContext, _ w: SimWrite, current: SimCursor) throws {
        guard w.base.generation == current.generation else { throw SimError.generation }
        guard ctx.actor > 0, ctx.receipt != VectorTime.zero, !w.opID.isEmpty, w.principal > 0, w.entryID > 0,
              w.occurredAt != VectorTime.zero, SimModel.validValue(w.register, w.value), w.base.rev <= current.rev,
              ctx.source(kind: w.kind) != nil else { throw SimError.invalid }
    }
    private static func rank(_ source: SimSource) -> Int {
        switch source { case .deliberate: return 3; case .ruleOrAgent: return 2; case .passive: return 1 }
    }
    private static func wins(_ register: String, _ current: SimRegister, _ next: SimRegister, base: Int64) -> Bool {
        if base == current.rev { return true }
        if register == "seen", current.source == .passive, next.source == .passive {
            return next.value == .string("seen")
        }
        if rank(next.source) != rank(current.source) { return rank(next.source) > rank(current.source) }
        if next.occurredAt != current.occurredAt { return next.occurredAt > current.occurredAt }
        return next.rev > current.rev
    }
    private static func occurred(_ client: Date, _ receipt: Date) -> Date {
        min(client, receipt)
    }

    /// ResolveWrite: e is the zero entry for absent/deleted bodies.
    static func resolveWrite(ctx: SimContext, w: SimWrite, e: SimEntry, s: SimState, revision: SimCursor) throws -> SimResult {
        guard !revision.generation.isEmpty, revision.rev > 0 else { throw SimError.invalid }
        var state = s
        var result = SimResult(opID: w.opID, state: s)
        if e.id == 0 || e.deleted {
            result.status = .gone
            return result
        }
        let occurred = occurred(w.occurredAt, ctx.receipt)
        if ctx.receipt.timeIntervalSince(occurred) > 30 * 24 * 3600 {
            result.status = .expired
            return result
        }
        guard let source = ctx.source(kind: w.kind) else { throw SimError.invalid }
        var next = SimRegister(value: w.value, rev: revision.rev, occurredAt: occurred, actor: ctx.actor, source: source)
        let current = state.register(w.register)
        if w.register == "seen", w.base.rev != current.rev, current.source == .passive, next.source == .passive,
           current.value == .string("unseen") {
            next.value = .string("seen")
        }
        var base = w.base.rev
        if current.rev == revision.rev { base = current.rev } // causal batch side effect
        guard wins(w.register, current, next, base: base) else {
            result.status = .superseded
            return result
        }
        if w.register == "seen" {
            state.seen = next
            if next.value == .string("unseen"), state.shelf.value == .string("done") {
                state.shelf = next
                state.shelf.value = .string("none")
            }
        } else {
            state.setRegister(w.register, next)
        }
        state.stateRev = revision.rev
        result.status = .applied
        result.state = state
        return result
    }
    private func resolve(_ ctx: SimContext, _ w: SimWrite, _ revision: Int64) throws -> SimResult {
        let result = try SimModel.resolveWrite(ctx: ctx, w: w, e: entries[w.entryID] ?? SimEntry(),
                                               s: state(w.principal, w.entryID),
                                               revision: SimCursor(generation: generation, rev: revision))
        if result.status == .applied {
            states["\(w.principal)/\(w.entryID)"] = result.state
        }
        return result
    }
    private func replay(_ key: String) -> SimResult? {
        guard let old = operations[key] else { return nil }
        var result = SimResult(opID: opIDForKey(key),
                               status: old.status, state: state(old.principal, old.entry), replay: true)
        if entries[old.entry]?.deleted ?? true { result.status = .gone }
        return result
    }
    private func opKey(_ actor: Int64, _ opID: String) -> String { "\(actor)/\(opID)" }
    private func opIDForKey(_ key: String) -> String {
        guard let slash = key.firstIndex(of: "/") else { return key }
        return String(key[key.index(after: slash)...])
    }

    func batch(_ ctx: SimContext, _ writes: [SimWrite]) throws -> [SimResult] {
        var targets: Set<String> = []
        var ids: Set<String> = []
        for (index, w) in writes.enumerated() {
            do { try validateWrite(ctx, w, current: cursor) }
            catch { throw error is SimError ? error : SimError.invalid }
            let target = "\(w.principal)/\(w.entryID)/\(w.register)"
            if targets.contains(target) || ids.contains(w.opID) { throw SimError.invalid }
            if let old = operations[opKey(ctx.actor, w.opID)], old.rangeResult != nil { throw SimError.invalid }
            targets.insert(target)
            ids.insert(w.opID)
            _ = index
        }
        var results: [SimResult] = []
        var applied = false
        for w in writes {
            let key = opKey(ctx.actor, w.opID)
            if let replayed = replay(key) {
                results.append(replayed)
                continue
            }
            let result = try resolve(ctx, w, rev + 1)
            results.append(result)
            applied = applied || result.status == .applied
            operations[key] = SimRemembered(principal: w.principal, entry: w.entryID, status: result.status)
        }
        for index in results.indices {
            results[index].state = state(results[index].state.principal, results[index].state.entryID)
        }
        if applied { append(SimChange(type: "state.updated", results: results)) }
        return results
    }

    // MARK: ranges

    private func subscriptions(_ scope: SimScope) throws -> [Int64] {
        var ids: Set<Int64> = []
        switch scope.kind {
        case "all":
            for entry in entries.values { ids.insert(entry.subscriptionID) }
        case "subscriptions":
            guard !scope.ids.isEmpty else { throw SimError.invalid }
            for id in scope.ids {
                guard id > 0 else { throw SimError.invalid }
                ids.insert(id)
            }
        case "folder":
            guard scope.ids.count == 1, scope.ids[0] > 0 else { throw SimError.invalid }
            for object in objects.values where object.kind == "subscription" && !object.deleted && object.folderID == scope.ids[0] {
                ids.insert(object.id)
            }
        default:
            throw SimError.invalid
        }
        return ids.sorted()
    }

    func through(_ ctx: SimContext, _ request: SimRangeWrite) throws -> SimRangeResult {
        var w = request.write
        w.entryID = 1
        try validateWrite(ctx, w, current: cursor)
        guard w.register == "seen", w.value == .string("seen") else { throw SimError.invalid }
        let subs = try subscriptions(request.scope)
        let key = opKey(ctx.actor, w.opID)
        if let old = operations[key] {
            guard var result = old.rangeResult else { throw SimError.invalid }
            result.replay = true
            for index in result.results!.indices {
                var item = result.results![index]
                item.state = state(item.state.principal, item.state.entryID)
                item.replay = true
                if let entry = entries[item.state.entryID], entry.deleted { item.status = .gone }
                result.results![index] = item
            }
            return result
        }
        var result = SimRangeResult(status: .applied, throughSeq: request.throughSeq, subscriptions: subs, except: [], results: [])
        if ctx.receipt.timeIntervalSince(SimModel.occurred(w.occurredAt, ctx.receipt)) > 30 * 24 * 3600 {
            result.status = .expired
        } else {
            let members = entries.values
                .filter { !$0.deleted && $0.seq <= result.throughSeq && subs.contains($0.subscriptionID) }
                .map(\.id).sorted()
            for id in members {
                var item = w
                item.entryID = id
                let resolved = try resolve(ctx, item, rev + 1)
                result.results!.append(resolved)
                if resolved.status != .applied { result.except!.append(id) }
            }
            append(SimChange(type: "state.range", range: result))
        }
        operations[key] = SimRemembered(rangeResult: result)
        return result
    }
    func throughAt(_ timestamp: Date?) -> Int64 {
        guard let timestamp else { return seq }
        var through: Int64 = 0
        for entry in entries.values where entry.firstSeenAt <= timestamp && entry.seq > through {
            through = entry.seq
        }
        return through
    }

    // MARK: log

    func changesPage(_ cursor: SimCursor, limit: Int64 = 100_000) throws -> ([SimChange], SimCursor) {
        guard cursor.generation == generation else { throw SimError.generation }
        guard cursor.rev >= floor, cursor.rev <= rev else { throw SimError.cursorGone }
        guard limit > 0 else { throw SimError.invalid }
        var out: [SimChange] = []
        var next = cursor
        for change in changes where change.rev > cursor.rev {
            out.append(change)
            next.rev = change.rev
            if out.count == limit { break }
        }
        return (out, next)
    }
    func retainAfter(_ target: Int64) throws {
        guard target >= floor, target <= rev else { throw SimError.invalid }
        floor = target
        changes.removeAll { $0.rev <= target }
    }

    // MARK: objects and positions

    func putObject(_ input: SimObject) throws -> SimObject {
        guard input.id > 0 else { throw SimError.invalid }
        let key = "\(input.kind)/\(input.id)"
        let old = objects[key]
        var type = ""
        var object = input
        switch input.kind {
        case "subscription":
            type = old == nil ? "subscription.created" : "subscription.updated"
            if input.deleted { type = "subscription.deleted" }
        case "folder":
            type = input.deleted ? "folder.deleted" : "folder.updated"
        case "label":
            guard input.entryID > 0, !input.name.isEmpty, input.principal > 0 else { throw SimError.invalid }
            guard let entry = entries[input.entryID], !entry.deleted else { throw SimError.invalid }
            if let old, old.entryID != input.entryID || old.principal != input.principal || old.name != input.name {
                throw SimError.invalid
            }
            type = input.deleted ? "label.removed" : "label.added"
        case "annotation":
            type = input.deleted ? "annotation.deleted" : "annotation.updated"
        case "token":
            guard input.deleted else { throw SimError.invalid }
            type = "token.revoked"
        default:
            throw SimError.invalid
        }
        object.rev = rev + 1
        objects[key] = object
        append(SimChange(type: type, object: object))
        return object
    }
    func putPosition(_ input: SimPosition) throws -> SimPosition {
        guard input.principal > 0, !input.view.isEmpty, input.seq <= seq else { throw SimError.invalid }
        var position = input
        position.rev = rev + 1
        positions["\(position.principal)/\(position.view)"] = position
        append(SimChange(type: "position.updated", position: position))
        return position
    }

    // MARK: notifications (server-internal; vectors 10, 19, 33 only)

    func acceptNotification(_ actor: Int64, _ opID: String, _ generation: String) throws -> SimNotification {
        guard generation == self.generation else { throw SimError.generation }
        guard actor > 0, !opID.isEmpty else { throw SimError.invalid }
        let key = "\(actor)/\(opID)"
        if var existing = notifications[key] {
            existing.replay = true
            return existing
        }
        let notification = SimNotification(actor: actor, opID: opID, generation: generation, status: "pending")
        notifications[key] = notification
        return notification
    }
    /// Returns the stored notification alongside the error: a non-pending
    /// attempt replays the record with `attempted`, and a withheld or
    /// stale-generation attempt returns the still-pending record with
    /// `withheld` — the Go model returns both payload and error.
    func attemptNotification(_ actor: Int64, _ opID: String, _ receipt: Date) -> (SimNotification?, SimError?) {
        guard receipt != VectorTime.zero else { return (nil, .invalid) }
        let key = "\(actor)/\(opID)"
        guard var notification = notifications[key] else { return (nil, .invalid) }
        guard notification.status == "pending" else { return (notification, .attempted) }
        guard !withheld, notification.generation == generation else { return (notification, .withheld) }
        notification.status = "uncertain"
        notification.attemptedAt = receipt
        notifications[key] = notification
        return (notification, nil)
    }
    func completeNotification(_ actor: Int64, _ opID: String) throws {
        let key = "\(actor)/\(opID)"
        guard var notification = notifications[key], notification.status == "uncertain" else { throw SimError.invalid }
        notification.status = "delivered"
        notifications[key] = notification
    }
    func releaseNotifications() { withheld = false }

    // MARK: checkpoints, restore and bootstrap source

    func clone() -> SimModel {
        let copy = try! SimModel(generation: generation)
        copy.rev = rev; copy.seq = seq; copy.floor = floor; copy.withheld = withheld
        copy.entries = entries; copy.states = states; copy.objects = objects
        copy.positions = positions; copy.notifications = notifications
        copy.changes = changes
        copy.operations = operations
        return copy
    }
    static func restore(_ snapshot: SimModel?, generation: String) throws -> SimModel {
        guard let snapshot, !generation.isEmpty, generation != snapshot.generation else { throw SimError.invalid }
        let restored = snapshot.clone()
        restored.generation = generation
        restored.withheld = true
        return restored
    }
    func resources() -> SimResources {
        var out = SimResources()
        out.entries = entries.values.sorted { $0.id < $1.id }
        out.states = states.values.sorted { ($0.principal, $0.entryID) < ($1.principal, $1.entryID) }
        out.objects = objects.values.sorted { ($0.kind, $0.id) < ($1.kind, $1.id) }
        out.positions = positions.values.sorted { ($0.principal, $0.view) < ($1.principal, $1.view) }
        return out
    }
}

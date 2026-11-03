import Foundation
@testable import EarnestKit

// MARK: - Strict step/document decoding

/// One vector step. Fields mirror the Go `step` struct exactly; every present
/// field is type-checked at decode so an unknown field or wrong type fails the
/// file before execution, matching `json.Decoder.DisallowUnknownFields`.
struct VStep {
    var op = ""
    var server = "main"
    var device = ""
    var name = ""
    var at: Date? = nil
    var actor: Int64 = 0
    var token = ""
    var entry: SimEntry? = nil
    var state: VJSON? = nil
    var write: SimWrite? = nil
    var writes: [SimWrite] = []
    var scope: SimScope? = nil
    var throughSeq: Int64? = nil
    var ts: Date? = nil
    var action = ""
    var generation = ""
    var cursor: SimCursor? = nil
    var rev: Int64 = 0
    var object: SimObject? = nil
    var position: SimPosition? = nil
    var principal: Int64 = 0
    var entryID: Int64 = 0
    var opID = ""
    var count: Int64? = nil
    var repeats: Int64 = 0
    var expect: VJSON = .null
    var expectPresent = false
    var error = ""

    static let fields: Set<String> = [
        "op", "server", "device", "name", "at", "actor", "token", "entry", "state",
        "write", "writes", "scope", "through_seq", "ts", "action", "generation",
        "cursor", "rev", "object", "position", "principal", "entry_id", "op_id",
        "count", "repeat", "expect", "error",
    ]

    static func decode(_ value: VJSON) throws -> VStep {
        try value.checkKeys(fields)
        var step = VStep()
        step.op = try value.optString("op") ?? ""
        step.server = try value.optString("server") ?? "main"
        step.device = try value.optString("device") ?? ""
        step.name = try value.optString("name") ?? ""
        step.at = try value.optDate("at")
        step.actor = try value.optInt("actor") ?? 0
        step.token = try value.optString("token") ?? ""
        if let entry = value["entry"] { step.entry = try entry.decodeEntry() }
        if let state = value["state"] { try state.checkStateShape(); step.state = state }
        if let write = value["write"] { step.write = try write.decodeWrite() }
        if let writes = value["writes"] {
            guard let array = writes.array else { throw VJSON.Failure.wrongType("writes") }
            step.writes = try array.map { try $0.decodeWrite() }
        }
        if let scope = value["scope"] { step.scope = try scope.decodeScope() }
        step.throughSeq = try value.optInt("through_seq")
        step.ts = try value.optDate("ts")
        step.action = try value.optString("action") ?? ""
        step.generation = try value.optString("generation") ?? ""
        if let cursor = value["cursor"] { step.cursor = try cursor.decodeCursor() }
        step.rev = try value.optInt("rev") ?? 0
        if let object = value["object"] { step.object = try object.decodeObject() }
        if let position = value["position"] { step.position = try position.decodePosition() }
        step.principal = try value.optInt("principal") ?? 0
        step.entryID = try value.optInt("entry_id") ?? 0
        step.opID = try value.optString("op_id") ?? ""
        step.count = try value.optInt("count")
        step.repeats = try value.optInt("repeat") ?? 0
        guard let expect = value["expect"] else { throw VJSON.Failure.missingKey("expect") }
        step.expect = expect
        step.expectPresent = true
        step.error = try value.optString("error") ?? ""
        return step
    }
}

extension VJSON {
    func optInt(_ key: String) throws -> Int64? {
        guard let value = self[key], !value.isNull else { return nil }
        guard let int = value.int64 else { throw Failure.wrongType(key) }
        return int
    }
    func optString(_ key: String) throws -> String? {
        guard let value = self[key], !value.isNull else { return nil }
        guard let string = value.string else { throw Failure.wrongType(key) }
        return string
    }
    func optBool(_ key: String) throws -> Bool? {
        guard let value = self[key], !value.isNull else { return nil }
        guard let flag = value.bool else { throw Failure.wrongType(key) }
        return flag
    }
    func optDate(_ key: String) throws -> Date? {
        guard let string = try optString(key) else { return nil }
        return try VectorTime.parse(string)
    }
    /// syncmodel.Value: literal JSON true/false are the kept booleans; strings
    /// are register text except "true"/"false", which Go rejects on decode.
    func optRegisterValue(_ key: String) throws -> VJSON? {
        guard let value = self[key], !value.isNull else { return nil }
        switch value {
        case .bool(let flag): return .bool(flag)
        case .string(let text):
            if text == "true" || text == "false" { throw Failure.badValue("\(key): \"\(text)\" is not a register value") }
            return .string(text)
        default: throw Failure.badValue(key)
        }
    }
    func decodeCursor() throws -> SimCursor {
        try checkKeys(["generation", "rev"])
        return SimCursor(generation: try optString("generation") ?? "", rev: try optInt("rev") ?? 0)
    }
    func decodeEntry() throws -> SimEntry {
        try checkKeys(["id", "subscription_id", "seq", "first_seen_at", "published_at", "rev", "deleted"])
        var entry = SimEntry()
        entry.id = try optInt("id") ?? 0
        entry.subscriptionID = try optInt("subscription_id") ?? 0
        entry.seq = try optInt("seq") ?? 0
        entry.firstSeenAt = try optDate("first_seen_at") ?? VectorTime.zero
        entry.publishedAt = try optDate("published_at") ?? VectorTime.zero
        entry.rev = try optInt("rev") ?? 0
        entry.deleted = try optBool("deleted") ?? false
        return entry
    }
    func decodeWrite() throws -> SimWrite {
        try checkKeys(["op_id", "principal", "entry_id", "register", "value", "base_rev", "occurred_at", "kind"])
        var write = SimWrite()
        write.opID = try optString("op_id") ?? ""
        write.principal = try optInt("principal") ?? 0
        write.entryID = try optInt("entry_id") ?? 0
        write.register = try optString("register") ?? ""
        write.value = try optRegisterValue("value") ?? .null
        if let base = self["base_rev"] { write.base = try base.decodeCursor() }
        write.occurredAt = try optDate("occurred_at") ?? VectorTime.zero
        write.kind = try optString("kind") ?? ""
        return write
    }
    func decodeScope() throws -> SimScope {
        try checkKeys(["kind", "ids"])
        var scope = SimScope()
        scope.kind = try optString("kind") ?? ""
        if let raw = self["ids"], !raw.isNull {
            guard let array = raw.array else { throw Failure.wrongType("ids") }
            scope.ids = try array.map {
                guard let id = $0.int64 else { throw Failure.wrongType("ids") }
                return id
            }
        }
        return scope
    }
    func decodeObject() throws -> SimObject {
        try checkKeys(["kind", "id", "principal", "entry_id", "folder_id", "name", "rev", "deleted"])
        var object = SimObject()
        object.kind = try optString("kind") ?? ""
        object.id = try optInt("id") ?? 0
        object.principal = try optInt("principal") ?? 0
        object.entryID = try optInt("entry_id") ?? 0
        object.folderID = try optInt("folder_id") ?? 0
        object.name = try optString("name") ?? ""
        object.rev = try optInt("rev") ?? 0
        object.deleted = try optBool("deleted") ?? false
        return object
    }
    func decodePosition() throws -> SimPosition {
        try checkKeys(["principal", "view", "seq", "rev"])
        return SimPosition(principal: try optInt("principal") ?? 0,
                           view: try optString("view") ?? "",
                           seq: try optInt("seq") ?? 0,
                           rev: try optInt("rev") ?? 0)
    }
    /// The `state` input is only validated here; client_merge_state hands the
    /// value to `State.init?(_:)` for the real decode.
    func checkStateShape() throws {
        try checkKeys(["principal", "entry_id", "state_rev", "seen", "shelf", "kept"])
        _ = try optInt("principal"); _ = try optInt("entry_id"); _ = try optInt("state_rev")
        for name in ["seen", "shelf", "kept"] {
            guard let register = self[name] else { continue }
            try register.checkKeys(["value", "rev", "occurred_at", "actor", "source"])
            _ = try register.optRegisterValue("value")
            _ = try register.optInt("rev"); _ = try register.optInt("actor")
            _ = try register.optDate("occurred_at"); _ = try register.optString("source")
        }
    }
}

struct VectorDocument {
    var name = ""
    var decision = ""
    var setup: [VStep] = []
    var operations: [VStep] = []
    var final: [VStep] = []

    static let fields: Set<String> = ["version", "name", "decision", "setup", "operations", "final"]
    static func decode(_ value: VJSON) throws -> VectorDocument {
        try value.checkKeys(fields)
        guard try value.required("version") == .int(1) else { throw VJSON.Failure.badValue("version") }
        var document = VectorDocument()
        document.name = try value.requiredString("name")
        document.decision = try value.requiredString("decision")
        for section in ["setup", "operations", "final"] {
            guard let raw = value[section] else { continue }
            guard let array = raw.array else { throw VJSON.Failure.wrongType(section) }
            let steps = try array.map { try VStep.decode($0) }
            switch section {
            case "setup": document.setup = steps
            case "operations": document.operations = steps
            default: document.final = steps
            }
        }
        guard !document.name.isEmpty, !document.decision.isEmpty, !document.final.isEmpty else {
            throw VJSON.Failure.badValue("document")
        }
        return document
    }
}

// MARK: - Engine bridge

extension SimWrite {
    /// The wire write shape `Write.init?(_:)` decodes.
    var apiValue: APIValue {
        APIValue.object(pairs: [
            ("op_id", .string(opID)), ("principal", .integer(principal)), ("entry_id", .integer(entryID)),
            ("register", .string(register)), ("value", value.apiValue), ("base_rev", base.json.apiValue),
            ("occurred_at", .string(VectorTime.string(occurredAt))), ("kind", .string(kind.isEmpty ? "explicit" : kind)),
        ])
    }
    init(_ write: Write) {
        opID = write.opID; principal = write.principal; entryID = write.entryID
        register = write.register.rawValue; value = VJSON(write.value.apiValue)
        base = SimCursor(generation: write.base.generation, rev: write.base.rev)
        occurredAt = write.occurredAt; kind = write.kind.rawValue
    }
}
extension SimRangeWrite {
    init(_ range: RangeWrite) {
        write = SimWrite(range.write)
        scope = SimScope(kind: range.scope.kind.rawValue, ids: range.scope.ids)
        throughSeq = range.throughSeq
    }
}

func simError(_ error: Error) -> SimError {
    guard let sync = error as? SyncError else { return .invalid }
    switch sync {
    case .generationMismatch: return .generation
    case .invalidPage, .invalidWrite: return .invalid
    }
}

// MARK: - Runner

/// One saved submit response, keyed by the submitting step's `name` so a later
/// `client_ack` with the same name acknowledges it — as in the Go runner.
struct VectorResponse {
    var generation = ""
    var results: [SimResult]? = nil
    var rangeResult: SimRangeResult? = nil
    var opID = ""
}

/// The per-file fixture: one reference model per named server, one SyncEngine
/// per named device, plus prepared ingests, snapshots, captured pages and
/// submit responses. Nothing crosses vectors: every file starts empty.
final class VectorRunner {
    private(set) var models: [String: SimModel] = [:]
    private var devices: [String: SyncEngine] = [:]
    private var snapshots: [String: SimModel] = [:]
    private var prepared: [String: SimEntry] = [:]
    private var pages: [String: (changes: [SimChange], next: SimCursor)] = [:]
    private var responses: [String: VectorResponse] = [:]

    static let defaultReceipt = try! VectorTime.parse("2026-11-02T12:00:00Z")

    func model(_ server: String) throws -> SimModel {
        if let model = models[server] { return model }
        let model = try SimModel(generation: "g1")
        models[server] = model
        return model
    }

    /// Executes one step; returns `(actual, errorName)` where errorName is ""
    /// for success. Fixture-defect throws (missing engine value, bad decode)
    /// propagate as test failures, not expected errors.
    func execute(_ step: VStep) throws -> (VJSON, String) {
        let model = try model(step.server)
        var ctx = SimContext(actor: step.actor == 0 ? 1 : step.actor,
                             token: step.token.isEmpty ? "human" : step.token,
                             receipt: step.at ?? VectorRunner.defaultReceipt)
        let principal = step.principal == 0 ? 1 : step.principal
        func defaults(_ input: SimWrite?) -> SimWrite {
            var write = input ?? SimWrite()
            if write.principal == 0 { write.principal = principal }
            if write.entryID == 0 { write.entryID = step.entryID }
            if write.base.generation.isEmpty { write.base.generation = model.generation }
            if write.occurredAt == VectorTime.zero { write.occurredAt = ctx.receipt }
            if write.kind.isEmpty { write.kind = "explicit" }
            return write
        }
        func engine() throws -> SyncEngine {
            guard let engine = devices[step.device] else { throw SimError.invalid }
            return engine
        }
        /// The mirror: a throwaway engine holding exactly the server's current
        /// resources. Non-device assertions evaluate this client cache, which
        /// is what the shared vectors mean by observable state.
        func mirror(_ model: SimModel) throws -> SyncEngine {
            let engine = SyncEngine(store: MemorySyncStore())
            try engine.bootstrap(Cursor(generation: model.generation, rev: model.rev))
            try feed(engine, from: model)
            return engine
        }
        func through(_ write: SimWrite) -> SimRangeWrite {
            var w = write
            if w.register.isEmpty { w.register = "seen" }
            if w.value.isNull || w.value == .string("") { w.value = .string("seen") }
            let bound = step.throughSeq ?? model.throughAt(step.ts)
            return SimRangeWrite(write: w, scope: step.scope ?? SimScope(), throughSeq: bound)
        }
        let zeroRange: VJSON = .object(["status": .string(""), "through_seq": .int(0), "subscriptions": .null,
                                        "except": .null, "results": .null, "replay": .bool(false)])
        let zeroNotification: VJSON = .object(["actor": .int(0), "op_id": .string(""), "generation": .string(""),
                                               "status": .string(""), "replay": .bool(false),
                                               "attempted_at": .string("0001-01-01T00:00:00Z")])
        func attempt(_ zero: VJSON, _ body: () throws -> VJSON) -> (VJSON, String) {
            do { return (try body(), "") }
            catch let error as SimError { return (zero, error.name) }
            catch { return (zero, simError(error).name) }
        }
        let empty: VJSON = .object([:])

        switch step.op {
        case "ingest":
            return attempt(SimEntry().json) { try model.ingest(step.entry ?? SimEntry(), receipt: ctx.receipt).json }
        case "repeat_ingest":
            return attempt(.null) {
                for index in 0..<step.repeats {
                    var entry = step.entry ?? SimEntry()
                    entry.id += index
                    _ = try model.ingest(entry, receipt: ctx.receipt)
                }
                return .object(["rev": .int(model.rev), "seq": .int(model.seq)])
            }
        case "prepare_ingest":
            prepared[step.name] = step.entry ?? SimEntry()
            return (.object(["seq": .int(model.seq), "rev": .int(model.rev)]), "")
        case "commit_ingest":
            return attempt(SimEntry().json) {
                guard let entry = prepared[step.name] else { throw SimError.invalid }
                prepared.removeValue(forKey: step.name)
                return try model.ingest(entry, receipt: ctx.receipt).json
            }
        case "delete":
            return attempt(empty) { try model.deleteEntry(step.entryID); return empty }
        case "update_entry":
            return attempt(empty) { try model.updateEntry(step.entryID, published: step.entry?.publishedAt ?? VectorTime.zero); return empty }
        case "write":
            return attempt(.null) { try model.batch(ctx, [defaults(step.write)])[0].json }
        case "batch", "cluster":
            return attempt(.null) {
                let results = try model.batch(ctx, step.writes.map { defaults($0) })
                return .array(results.map { $0.json })
            }
        case "google":
            return attempt(.null) {
                ctx.token = "compat"
                let write = try Self.googleWrite(step, principal: principal, model: model, ctx: ctx)
                return try model.batch(ctx, [write])[0].json
            }
        case "google_batch":
            ctx.token = "compat"
            var writes: [SimWrite] = []
            var builds: SimError? = nil
            for index in 0..<step.repeats where builds == nil {
                var numbered = step
                numbered.opID = "\(step.opID)-\(index)"
                numbered.entryID = step.entryID + index
                do { writes.append(try Self.googleWrite(numbered, principal: principal, model: model, ctx: ctx)) }
                catch let error as SimError { builds = error }
            }
            func counts(_ results: [SimResult]) -> VJSON {
                var tally: [SimStatus: Int64] = [.applied: 0, .superseded: 0, .gone: 0, .expired: 0]
                for result in results { tally[result.status, default: 0] += 1 }
                return .object(["applied": .int(tally[.applied]!), "superseded": .int(tally[.superseded]!),
                                "gone": .int(tally[.gone]!), "expired": .int(tally[.expired]!)])
            }
            if let builds { return (counts([]), builds.name) }
            do {
                return (counts(try model.batch(ctx, writes)), "")
            } catch let error as SimError { return (counts([]), error.name) }
            catch { return (counts([]), simError(error).name) }
        case "range":
            return attempt(zeroRange) { try model.through(ctx, through(defaults(step.write))).json }
        case "retain":
            return attempt(empty) { try model.retainAfter(step.rev); return empty }
        case "changes", "capture_changes":
            var cursor = step.cursor ?? SimCursor(generation: "", rev: 0)
            if cursor.generation.isEmpty { cursor.generation = model.generation }
            let zero: VJSON = .object(["types": .array([]), "revs": .array([]),
                                       "next": SimCursor(generation: "", rev: 0).json, "changes": .null])
            return attempt(zero) {
                let (changes, next) = try model.changesPage(cursor)
                if step.op == "capture_changes" { pages[step.name] = (changes, next) }
                return .object(["types": .array(changes.map { .string($0.type) }),
                                "revs": .array(changes.map { .int($0.rev) }),
                                "next": next.json, "changes": .array(changes.map { $0.json })])
            }
        case "checkpoint":
            snapshots[step.name] = model.clone()
            return (.object(["rev": .int(model.rev)]), "")
        case "restore":
            return attempt(empty) {
                models[step.server] = try SimModel.restore(snapshots[step.name], generation: step.generation)
                return empty
            }
        case "bootstrap_begin":
            return attempt(empty) {
                if devices[step.device] == nil { devices[step.device] = SyncEngine(store: MemorySyncStore()) }
                let cursor = Cursor(generation: model.generation, rev: model.rev)
                try devices[step.device]!.bootstrap(cursor)
                return cursor.apiValueJSON
            }
        case "bootstrap_page":
            return attempt(.null) {
                try feed(try engine(), from: model)
                return empty
            }
        case "client_pull":
            guard let engine = devices[step.device], let cursor = try engine.snapshot().cursor else {
                return (.null, "invalid")
            }
            let (changes, next): ([SimChange], SimCursor)
            do {
                (changes, next) = try model.changesPage(SimCursor(generation: cursor.generation, rev: cursor.rev))
            } catch let error as SimError { return (.null, error.name) }
            do {
                try engine.apply(changes.map(Self.apiChange), next: Cursor(generation: next.generation, rev: next.rev))
            } catch let error as SyncError { return (VJSON(cursor.apiValue), simError(error).name) }
            return (try engine.snapshot().cursor.map { VJSON($0.apiValue) } ?? .null, "")
        case "client_apply":
            guard let engine = devices[step.device] else { return (.null, "invalid") }
            guard let page = pages[step.name] else { return (.null, "invalid") }
            var changes = page.changes
            if let count = step.count {
                guard count >= 0, count <= Int64(changes.count) else { return (.null, "invalid") }
                changes = Array(changes.prefix(Int(count)))
            }
            do {
                try engine.apply(changes.map(Self.apiChange), next: Cursor(generation: page.next.generation, rev: page.next.rev))
            } catch let error as SyncError {
                let current = try engine.snapshot().cursor.map { VJSON($0.apiValue) } ?? .null
                return (current, simError(error).name)
            }
            return (try engine.snapshot().cursor.map { VJSON($0.apiValue) } ?? .null, "")
        case "client_queue":
            return attempt(empty) {
                guard let write = Write(defaults(step.write).apiValue) else { throw SimError.invalid }
                try engine().queue(write)
                return empty
            }
        case "client_queue_range":
            return attempt(empty) {
                let request = through(defaults(step.write))
                guard let write = Write(request.write.apiValue), let kind = ScopeKind(rawValue: request.scope.kind) else {
                    throw SimError.invalid
                }
                try engine().queueRange(RangeWrite(write: write, scope: Scope(kind: kind, ids: request.scope.ids),
                                                 throughSeq: request.throughSeq))
                return empty
            }
        case "client_merge_state":
            return attempt(empty) {
                guard let raw = step.state, let state = State(raw.apiValue) else { throw SimError.invalid }
                try engine().mergeState(state)
                return empty
            }
        case "client_submit":
            guard let engine = devices[step.device],
                  let head = try engine.snapshot().outbox.first,
                  let generation = try engine.snapshot().cursor?.generation else {
                return (.null, "invalid")
            }
            switch head {
            case .range(let range):
                do {
                    let result = try model.through(ctx, SimRangeWrite(range))
                    responses[step.name] = VectorResponse(generation: generation, rangeResult: result, opID: range.write.opID)
                    try engine.markInFlight(opIDs: [range.write.opID])
                    return (result.json, "")
                } catch let error as SimError { return (.null, error.name) }
            case .write(let write):
                do {
                    let results = try model.batch(ctx, [SimWrite(write)])
                    responses[step.name] = VectorResponse(generation: generation, results: results, opID: write.opID)
                    try engine.markInFlight(opIDs: [write.opID])
                    return (results[0].json, "")
                } catch let error as SimError { return (.null, error.name) }
            }
        case "client_ack":
            return attempt(empty) {
                let engine = try engine()
                guard let saved = responses[step.name] else { throw SimError.invalid }
                if let rangeResult = saved.rangeResult {
                    guard let result = RangeResult(rangeResult.json.apiValue) else { throw SimError.invalid }
                    try engine.acknowledgeRange(opID: saved.opID, generation: saved.generation, result: result)
                } else if let results = saved.results {
                    let acknowledgements = try results.map {
                        guard let ack = WriteAcknowledgement($0.json.apiValue) else { throw SimError.invalid }
                        return ack
                    }
                    try engine.acknowledge(acknowledgements, generation: saved.generation)
                } else {
                    throw SimError.invalid
                }
                return empty
            }
        case "client_flush":
            guard let engine = devices[step.device] else { return (.null, "invalid") }
            do {
                var out: [VJSON] = []
                while let head = try engine.snapshot().outbox.first {
                    if case .range(let range) = head {
                        let result = try model.through(ctx, SimRangeWrite(range))
                        guard let converted = RangeResult(result.json.apiValue) else { throw SimError.invalid }
                        try engine.acknowledgeRange(opID: range.write.opID, generation: range.write.base.generation, result: converted)
                        out.append(result.json)
                        continue
                    }
                    var writes: [Write] = []
                    for item in try engine.snapshot().outbox {
                        guard case .write(let write) = item else { break }
                        writes.append(write)
                    }
                    let results = try model.batch(ctx, writes.map(SimWrite.init))
                    let acknowledgements = try results.map {
                        guard let ack = WriteAcknowledgement($0.json.apiValue) else { throw SimError.invalid }
                        return ack
                    }
                    try engine.acknowledge(acknowledgements, generation: writes[0].base.generation)
                    out.append(contentsOf: results.map { $0.json })
                }
                return (.array(out), "")
            } catch let error as SimError { return (.null, error.name) }
            catch { return (.null, simError(error).name) }
        case "object":
            return attempt(SimObject().json) { try model.putObject(step.object ?? SimObject()).json }
        case "position":
            return attempt(SimPosition().json) { try model.putPosition(step.position ?? SimPosition()).json }
        case "accept_notification":
            return attempt(zeroNotification) {
                let generation = step.generation.isEmpty ? model.generation : step.generation
                return try model.acceptNotification(ctx.actor, step.opID, generation).json
            }
        case "attempt_notification":
            let (notification, notificationError) = model.attemptNotification(ctx.actor, step.opID, ctx.receipt)
            return (notification?.json ?? zeroNotification, notificationError?.name ?? "")
        case "complete_notification":
            return attempt(empty) { try model.completeNotification(ctx.actor, step.opID); return empty }
        case "release_notifications":
            model.releaseNotifications()
            return (empty, "")
        case "assert_state":
            return attempt(.null) {
                let state = try (step.device.isEmpty ? mirror(model) : engine())
                    .state(principal: principal, entryID: step.entryID)
                return .object(["state": VJSON(state.apiValue), "unread": .bool(state.unread), "google_read": .bool(state.googleRead)])
            }
        case "assert_entry":
            return attempt(.null) {
                var entry: VJSON = SimEntry().json
                var exists = false
                var age = ctx.receipt.timeIntervalSince(VectorTime.zero)
                if let found = try (step.device.isEmpty ? mirror(model) : engine()).entry(step.entryID) {
                    entry = VJSON(found.apiValue)
                    exists = true
                    if let seen = found.firstSeenAt, ctx.receipt >= seen { age = ctx.receipt.timeIntervalSince(seen) } else { age = 0 }
                }
                return .object(["entry": entry, "exists": .bool(exists), "age_seconds": .double(age)])
            }
        case "assert_object":
            return attempt(.null) {
                let input = step.object ?? SimObject()
                var object: VJSON = SimObject().json
                var exists = false
                if let kind = ObjectKind(rawValue: input.kind),
                   let found = try (step.device.isEmpty ? mirror(model) : engine()).object(kind: kind, id: input.id) {
                    object = VJSON(found.apiValue)
                    exists = true
                }
                return .object(["object": object, "exists": .bool(exists)])
            }
        case "assert_position":
            return attempt(.null) {
                let view = step.position?.view ?? ""
                var position: VJSON = SimPosition().json
                var exists = false
                if let found = try (step.device.isEmpty ? mirror(model) : engine()).position(principal: principal, view: view) {
                    position = VJSON(found.apiValue)
                    exists = true
                }
                return .object(["position": position, "exists": .bool(exists)])
            }
        case "assert_client":
            return attempt(.null) {
                let snapshot = try engine().snapshot()
                return .object(["outbox_size": .int(Int64(snapshot.outbox.count)),
                                "cursor": snapshot.cursor.map { VJSON($0.apiValue) } ?? .null])
            }
        case "assert_log":
            // The log itself is server-internal, but the retained page is
            // client-observable: replay it through a fresh engine. An apply
            // failure or a cursor that lands off the server rev is drift.
            var replayError: SimError? = nil
            do {
                let replay = SyncEngine(store: MemorySyncStore())
                try replay.bootstrap(Cursor(generation: model.generation, rev: model.floor))
                let (changes, next) = try model.changesPage(SimCursor(generation: model.generation, rev: model.floor))
                try replay.apply(changes.map(Self.apiChange), next: Cursor(generation: next.generation, rev: next.rev))
                if try replay.snapshot().cursor?.rev != model.rev { replayError = .invalid }
            } catch let error as SimError { replayError = error }
            catch { replayError = simError(error) }
            var revs: [VJSON] = []
            var types: [VJSON] = []
            var gapless = true
            var last = model.floor
            for change in model.changes {
                revs.append(.int(change.rev))
                types.append(.string(change.type))
                if change.rev != last + 1 { gapless = false }
                last = change.rev
            }
            let log: VJSON = .object(["rev": .int(model.rev), "seq": .int(model.seq), "generation": .string(model.generation),
                                      "revs": .array(revs), "types": .array(types), "gapless": .bool(gapless),
                                      "count": .int(Int64(model.changes.count))])
            return (log, replayError?.name ?? "")
        default:
            return (.null, "invalid")
        }
    }

    /// Google compatibility writes use a zero base and the step receipt as the
    /// client clock; `read` is passive, the rest explicit.
    private static func googleWrite(_ step: VStep, principal: Int64, model: SimModel, ctx: SimContext) throws -> SimWrite {
        var write = SimWrite(opID: step.opID, principal: principal, entryID: step.entryID,
                             base: SimCursor(generation: model.generation, rev: 0),
                             occurredAt: ctx.receipt, kind: "explicit")
        switch step.action {
        case "read":
            write.register = "seen"; write.value = .string("seen"); write.kind = "passive"
        case "unread", "kept-unread":
            write.register = "seen"; write.value = .string("unseen")
        case "star":
            write.register = "kept"; write.value = .bool(true)
        case "unstar":
            write.register = "kept"; write.value = .bool(false)
        default:
            throw SimError.invalid
        }
        return write
    }

    /// One bootstrap page: entries, states, objects and positions merged by
    /// revision, objects routed through their endpoint kinds. Used by both
    /// `bootstrap_page` and the assert mirror.
    private func feed(_ engine: SyncEngine, from model: SimModel) throws {
        let resources = model.resources()
        try engine.mergeBootstrap(kind: "entries", items: resources.entries.map { $0.json.apiValue })
        try engine.mergeBootstrap(kind: "state", items: resources.states.map { $0.json.apiValue })
        for object in resources.objects {
            switch object.kind {
            case "subscription": try engine.mergeBootstrap(kind: "subscriptions", items: [object.json.apiValue])
            case "folder": try engine.mergeBootstrap(kind: "folders", items: [object.json.apiValue])
            case "label": try engine.mergeBootstrap(kind: "label_memberships", items: [object.json.apiValue])
            default:
                guard let syncObject = SyncObject(object.json.apiValue) else { throw SimError.invalid }
                try engine.mergeObject(syncObject)
            }
        }
        try engine.mergeBootstrap(kind: "positions", items: resources.positions.map { $0.json.apiValue })
    }

    /// Maps a simulated change record onto the wire `APIChange` payload the
    /// engine's `apply` decodes, matching the API server's per-type bodies.
    static func apiChange(_ change: SimChange) -> APIChange {
        let payload: APIValue
        switch change.type {
        case "entry.created", "entry.updated", "entry.deleted":
            payload = .object(["entry": change.entry!.json.apiValue])
        case "state.updated":
            payload = .object(["results": .array((change.results ?? []).map { $0.json.apiValue })])
        case "state.range":
            payload = .object(["range": change.range!.json.apiValue])
        case "position.updated":
            payload = .object(["position": change.position!.json.apiValue])
        case "token.revoked":
            payload = .object(["id": .integer(change.object!.id)])
        default:
            // label.*, subscription.*, folder.*, annotation.* all carry the
            // syncmodel.Object body; SyncObject decodes it directly.
            payload = change.object?.json.apiValue ?? .object([:])
        }
        return APIChange(rev: change.rev, type: change.type, payload: payload)
    }
}

extension Cursor {
    var apiValueJSON: VJSON { VJSON(apiValue) }
}

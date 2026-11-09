import Foundation
import Testing
@testable import EarnestKit

private let base = Date(timeIntervalSince1970: 1_793_620_800)

private func write(_ opID: String, entryID: Int64 = 1, register: Register = .seen, value: RegisterValue = .text("seen"), rev: Int64 = 0, occurredAt: Date = base) -> Write {
    Write(opID: opID, principal: 1, entryID: entryID, register: register, value: value, base: Cursor(generation: "g1", rev: rev), occurredAt: occurredAt)
}

private func change(_ rev: Int64, _ type: String, _ payload: APIValue) -> APIChange {
    APIChange(rev: rev, type: type, payload: payload)
}

@Test func doneClearSurvivesCoalescing() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    let cursor = Cursor(generation: "g1", rev: 1)
    try engine.bootstrap(cursor)
    try engine.mergeEntry(SyncEntry(id: 1, subscriptionID: 1, seq: 1, rev: 1))
    var state = State(principal: 1, entryID: 1)
    state.shelf = Stamp(value: .text("done"), rev: 1)
    try engine.mergeBootstrap([state])
    let now = Date(timeIntervalSince1970: 0)
    try engine.queue(Write(opID: "unread", principal: 1, entryID: 1, register: .seen, value: .text("unseen"), base: cursor, occurredAt: now))
    try engine.queue(Write(opID: "read", principal: 1, entryID: 1, register: .seen, value: .text("seen"), base: cursor, occurredAt: now))
    #expect(try engine.snapshot().outbox.map(\.opID) == ["unread/clear-done", "read"])
    #expect(try engine.state(principal: 1, entryID: 1).shelf.value == .text("none"))
}

@Test func atomicPageAndGeneration() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    let cursor = Cursor(generation: "g1", rev: 0)
    try engine.bootstrap(cursor)
    let state = State(principal: 1, entryID: 1)
    #expect(throws: SyncError.self) { try engine.apply([StateChange(rev: 1, state: state)], next: Cursor(generation: "g1", rev: 2)) }
    #expect(try engine.snapshot().cursor == cursor)
    let ancient = Date(timeIntervalSince1970: 1)
    try engine.queue(Write(opID: "one", principal: 1, entryID: 1, register: .kept, value: .flag(true), base: cursor, occurredAt: ancient))
    #expect(throws: SyncError.self) { try engine.queue(Write(opID: "one", principal: 1, entryID: 2, register: .kept, value: .flag(false), base: cursor, occurredAt: ancient)) }
    try engine.bootstrap(Cursor(generation: "g2", rev: 4))
    #expect(try engine.snapshot().outbox.first?.base == Cursor(generation: "g2", rev: 0))
    #expect(throws: SyncError.self) { try engine.acknowledge(opID: "one", generation: "g1") }
    #expect(try engine.expire(asOf: Date()).count == 1)
}

@Test func legacyStatePagesAllowContiguousOverlap() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g1", rev: 1))
    var first = State(principal: 1, entryID: 1)
    first.seen = Stamp(value: .text("seen"), rev: 1)
    var second = first
    second.kept = Stamp(value: .flag(true), rev: 2)
    let page = [StateChange(rev: 1, state: first), StateChange(rev: 2, state: second)]
    let next = Cursor(generation: "g1", rev: 2)
    try engine.apply(page, next: next)
    try engine.apply(page, next: next)
    #expect(try engine.snapshot().cursor == next)
    #expect(try engine.state(principal: 1, entryID: 1).kept.value == .flag(true))
}

@Test func legacyStatePagesRejectGapsAtomically() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    let cursor = Cursor(generation: "g1", rev: 1)
    try engine.bootstrap(cursor)
    let state = State(principal: 1, entryID: 1)
    for revisions: [Int64] in [[], [1], [3], [1, 3], [2, 2], [0, 2]] {
        #expect(throws: SyncError.self) {
            try engine.apply(revisions.map { StateChange(rev: $0, state: state) }, next: Cursor(generation: "g1", rev: 3))
        }
        #expect(try engine.snapshot().cursor == cursor)
        #expect(try engine.snapshot().states.isEmpty)
    }
}

@Test func legacyStatePagesReachMaximumRevision() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g1", rev: .max - 1))
    let next = Cursor(generation: "g1", rev: .max)
    try engine.apply([StateChange(rev: .max, state: State(principal: 1, entryID: 1))], next: next)
    try engine.apply([StateChange](), next: next)
    #expect(try engine.snapshot().cursor == next)
}

@Test func batchAcknowledgementMergesStateAndKeepsNewerIntent() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    let cursor = Cursor(generation: "g1", rev: 0)
    try engine.bootstrap(cursor)
    try engine.mergeEntry(SyncEntry(id: 1, subscriptionID: 1, seq: 1, rev: 1))
    let now = Date()
    let first = Write(opID: "first", principal: 1, entryID: 1, register: .kept, value: .flag(true), base: cursor, occurredAt: now)
    try engine.queue(first)
    try engine.markInFlight(opIDs: ["first"])
    try engine.queue(Write(opID: "second", principal: 1, entryID: 1, register: .kept, value: .flag(false), base: cursor, occurredAt: now))
    var state = State(principal: 1, entryID: 1)
    state.kept = Stamp(value: .flag(true), rev: 1)
    state.stateRev = 1
    let result = WriteAcknowledgement(opID: "first", status: .applied, state: state)
    #expect(throws: SyncError.self) { try engine.acknowledge([result], generation: "old") }
    #expect(try engine.snapshot().outbox.count == 2)
    #expect(try engine.acknowledge([result], generation: "g1").isEmpty)
    #expect(try engine.snapshot().outbox.map(\.opID) == ["second"])
    #expect(try engine.snapshot().states.first?.kept.value == .flag(true))
    #expect(try engine.state(principal: 1, entryID: 1).kept.value == .flag(false))
}

@Test func batchAcknowledgementReportsDroppedWrites() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    let cursor = Cursor(generation: "g", rev: 0)
    try engine.bootstrap(cursor)
    let write = Write(opID: "gone", principal: 1, entryID: 1, register: .seen, value: .text("seen"), base: cursor, occurredAt: Date())
    try engine.queue(write)
    let dropped = try engine.acknowledge([WriteAcknowledgement(opID: "gone", status: .gone, state: nil)], generation: "g")
    #expect(dropped == [.write(write)])
    #expect(try engine.snapshot().outbox.isEmpty)
}

@Test func expirationIsStrictlyAfterThirtyDays() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    let cursor = Cursor(generation: "g", rev: 0)
    try engine.bootstrap(cursor)
    let now = Date(timeIntervalSince1970: 2_000_000_000)
    let age: TimeInterval = 30 * 24 * 60 * 60
    for (opID, entryID, elapsed) in [("exact", Int64(1), age), ("old", Int64(2), age + 1)] {
        try engine.queue(Write(opID: opID, principal: 1, entryID: entryID, register: .seen, value: .text("seen"), base: cursor, occurredAt: now.addingTimeInterval(-elapsed)))
    }
    #expect(try engine.expire(asOf: now).map(\.opID) == ["old"])
    #expect(try engine.snapshot().outbox.map(\.opID) == ["exact"])
}

@Test func applyMergesEveryChangeTypeContiguously() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g1", rev: 0))
    let entryJSON: APIValue = .object(pairs: [
        ("entry", .object(pairs: [("id", .integer(1)), ("subscription_id", .integer(7)), ("seq", .integer(1)), ("rev", .integer(1))])),
    ])
    let stateJSON: APIValue = .object(pairs: [
        ("results", .array([.object(pairs: [
            ("op_id", .string("op")), ("status", .string("applied")),
            ("state", .object(pairs: [
                ("principal", .integer(1)), ("entry_id", .integer(1)),
                ("seen", .object(pairs: [("value", .string("seen")), ("rev", .integer(2))])),
            ])),
        ])])),
    ])
    try engine.apply([
        change(1, "entry.created", entryJSON),
        change(2, "state.updated", stateJSON),
        change(3, "subscription.created", .object(pairs: [("subscription", .object(pairs: [("id", .integer(7)), ("folder_id", .integer(3)), ("title", .string("Feed")), ("rev", .integer(3))]))])),
        change(4, "folder.updated", .object(pairs: [("folder", .object(pairs: [("id", .integer(3)), ("name", .string("F")), ("rev", .integer(4))]))])),
        change(5, "label.added", .object(pairs: [("object", .object(pairs: [("kind", .string("label")), ("id", .integer(9)), ("entry_id", .integer(1)), ("principal", .integer(1)), ("name", .string("work")), ("rev", .integer(5))]))])),
        change(6, "position.updated", .object(pairs: [("position", .object(pairs: [("principal", .integer(1)), ("view", .string("today")), ("seq", .integer(1)), ("rev", .integer(6))]))])),
        change(7, "annotation.updated", .object(pairs: [
            ("object", .object(pairs: [("kind", .string("annotation")), ("id", .integer(4)), ("entry_id", .integer(1)), ("principal", .integer(1)), ("rev", .integer(7))])),
            ("annotation", .object(pairs: [("id", .integer(4)), ("principal_id", .integer(1)), ("kind", .string("note")), ("entry_ids", .intArray([1])), ("value", .string("hi")), ("rev", .integer(7))])),
        ])),
        change(8, "token.revoked", .object(pairs: [("id", .integer(2))])),
    ], next: Cursor(generation: "g1", rev: 8))
    let snapshot = try engine.snapshot()
    #expect(snapshot.cursor == Cursor(generation: "g1", rev: 8))
    #expect(snapshot.entries.first?.seq == 1)
    #expect(snapshot.states.first?.seen.value == .text("seen"))
    #expect(snapshot.states.first?.seen.rev == 2)
    #expect(snapshot.objects.contains { $0.kind == .subscription && $0.id == 7 && $0.folderID == 3 && $0.name == "Feed" })
    #expect(snapshot.objects.contains { $0.kind == .folder && $0.id == 3 })
    #expect(snapshot.objects.contains { $0.kind == .label && $0.id == 9 && $0.entryID == 1 })
    #expect(snapshot.objects.contains { $0.kind == .token && $0.id == 2 && $0.rev == 8 && $0.deleted })
    #expect(snapshot.annotations.first?.value == "hi")
    #expect(snapshot.positions.first?.seq == 1)
    // Idempotent replay of the same page.
    try engine.apply([change(8, "token.revoked", .object(pairs: [("id", .integer(2))]))], next: Cursor(generation: "g1", rev: 8))
}

@Test func applyRejectsGapsTruncationAndGeneration() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g1", rev: 2))
    let entry = change(3, "entry.created", .object(pairs: [("entry", .object(pairs: [("id", .integer(1)), ("rev", .integer(3))]))]))
    // Wrong generation.
    #expect(throws: SyncError.self) { try engine.apply([entry], next: Cursor(generation: "g2", rev: 3)) }
    // Final cursor beyond the contiguous run.
    #expect(throws: SyncError.self) { try engine.apply([entry], next: Cursor(generation: "g1", rev: 4)) }
    // Truncated page carrying the final cursor.
    #expect(throws: SyncError.self) { try engine.apply([APIChange](), next: Cursor(generation: "g1", rev: 3)) }
    // A gap in the sequence.
    #expect(throws: SyncError.self) { try engine.apply([change(4, "entry.created", .object(pairs: [("entry", .object(pairs: [("id", .integer(2)), ("rev", .integer(4))]))]))], next: Cursor(generation: "g1", rev: 4)) }
    #expect(try engine.snapshot().cursor == Cursor(generation: "g1", rev: 2))
    #expect(try engine.snapshot().entries.isEmpty)
}

@Test func stateRangeResultsSkipDeletedEntries() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g1", rev: 0))
    try engine.apply([
        change(1, "entry.created", .object(pairs: [("entry", .object(pairs: [("id", .integer(1)), ("subscription_id", .integer(1)), ("seq", .integer(1)), ("rev", .integer(1))]))])),
        change(2, "entry.deleted", .object(pairs: [("entry", .object(pairs: [("id", .integer(2)), ("subscription_id", .integer(1)), ("seq", .integer(2)), ("rev", .integer(2)), ("deleted", .bool(true))]))])),
    ], next: Cursor(generation: "g1", rev: 2))
    let range: APIValue = .object(pairs: [
        ("range", .object(pairs: [
            ("status", .string("applied")),
            ("results", .array([1, 2].map { id in
                .object(pairs: [
                    ("op_id", .string("all")), ("status", .string("applied")),
                    ("state", .object(pairs: [
                        ("principal", .integer(1)), ("entry_id", .integer(id)),
                        ("seen", .object(pairs: [("value", .string("seen")), ("rev", .integer(3))])),
                        ("state_rev", .integer(3)),
                    ])),
                ])
            })),
        ])),
    ])
    try engine.apply([change(3, "state.range", range)], next: Cursor(generation: "g1", rev: 3))
    #expect(try engine.state(principal: 1, entryID: 1).seen.value == .text("seen"))
    #expect(try engine.snapshot().states.allSatisfy { $0.entryID != 2 })
}

@Test func bootstrapMergesAllResourceKinds() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g1", rev: 9))
    try engine.mergeBootstrap(kind: "entries", items: [
        .object(pairs: [("id", .integer(1)), ("subscription_id", .integer(5)), ("seq", .integer(4)), ("title", .string("T")), ("rev", .integer(2))]),
    ])
    try engine.mergeBootstrap(kind: "subscriptions", items: [
        .object(pairs: [("id", .integer(5)), ("folder_id", .integer(2)), ("title", .string("Feed")), ("rev", .integer(3))]),
    ])
    try engine.mergeBootstrap(kind: "folders", items: [.object(pairs: [("id", .integer(2)), ("name", .string("Tech")), ("rev", .integer(1))])])
    try engine.mergeBootstrap(kind: "labels", items: [.object(pairs: [("id", .integer(8)), ("name", .string("work")), ("rev", .integer(2)), ("deleted", .bool(false))])])
    try engine.mergeBootstrap(kind: "label_memberships", items: [
        .object(pairs: [("id", .integer(12)), ("kind", .string("label")), ("label_id", .integer(8)), ("entry_id", .integer(1)), ("name", .string("work")), ("principal", .integer(1)), ("rev", .integer(4))]),
    ])
    try engine.mergeBootstrap(kind: "positions", items: [.object(pairs: [("principal", .integer(1)), ("view", .string("today")), ("seq", .integer(4)), ("rev", .integer(5))])])
    try engine.mergeBootstrap(kind: "state", items: [
        .object(pairs: [
            ("principal", .integer(1)), ("entry_id", .integer(1)),
            ("seen", .object(pairs: [("value", .string("seen")), ("rev", .integer(6))])),
            ("state_rev", .integer(6)),
        ]),
    ])
    let snapshot = try engine.snapshot()
    #expect(snapshot.entries.first?.title == "T")
    #expect(snapshot.objects.contains { $0.kind == .subscription && $0.folderID == 2 })
    #expect(snapshot.objects.contains { $0.kind == .folder && $0.name == "Tech" })
    #expect(snapshot.objects.contains { $0.kind == .label && $0.labelID == 8 })
    #expect(snapshot.labelDefinitions.first?.name == "work")
    #expect(snapshot.positions.first?.seq == 4)
    #expect(snapshot.states.first?.seen.value == .text("seen"))
    // Older pages never roll back a merged revision; unknown kinds fail.
    try engine.mergeBootstrap(kind: "entries", items: [.object(pairs: [("id", .integer(1)), ("rev", .integer(1)), ("title", .string("Old"))])])
    #expect(try engine.snapshot().entries.first?.title == "T")
    #expect(throws: SyncError.self) { try engine.mergeBootstrap(kind: "bogus", items: []) }
}

@Test func tombstonesMergeByRevision() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g1", rev: 0))
    try engine.mergeEntry(SyncEntry(id: 1, subscriptionID: 1, seq: 1, rev: 3, deleted: true))
    // A deleted entry never merges again within the generation.
    try engine.mergeEntry(SyncEntry(id: 1, subscriptionID: 1, seq: 1, rev: 9))
    #expect(try engine.entry(1)?.deleted == true)
    // Objects take the higher revision either direction.
    try engine.mergeObject(SyncObject(kind: .folder, id: 2, name: "x", rev: 4))
    try engine.mergeObject(SyncObject(kind: .folder, id: 2, rev: 2, deleted: true))
    #expect(try engine.object(kind: .folder, id: 2)?.deleted == false)
    try engine.mergeObject(SyncObject(kind: .folder, id: 2, rev: 6, deleted: true))
    #expect(try engine.object(kind: .folder, id: 2)?.deleted == true)
    try engine.mergePosition(SyncPosition(principal: 1, view: "today", seq: 5, rev: 7))
    try engine.mergePosition(SyncPosition(principal: 1, view: "today", seq: 2, rev: 6))
    #expect(try engine.position(principal: 1, view: "today")?.seq == 5)
}

@Test func independentRegisterMerge() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g1", rev: 0))
    try engine.mergeEntry(SyncEntry(id: 1, subscriptionID: 1, seq: 1, rev: 1))
    try engine.mergeState(try #require(State(.object(pairs: [
        ("principal", .integer(1)), ("entry_id", .integer(1)), ("state_rev", .integer(9)),
        ("seen", .object(pairs: [("value", .string("unseen")), ("rev", .integer(8))])),
        ("shelf", .object(pairs: [("value", .string("none")), ("rev", .integer(0))])),
        ("kept", .object(pairs: [("value", .bool(true)), ("rev", .integer(9))])),
    ]))))
    try engine.mergeState(try #require(State(.object(pairs: [
        ("principal", .integer(1)), ("entry_id", .integer(1)), ("state_rev", .integer(7)),
        ("seen", .object(pairs: [("value", .string("seen")), ("rev", .integer(7))])),
        ("shelf", .object(pairs: [("value", .string("later")), ("rev", .integer(6))])),
        ("kept", .object(pairs: [("value", .bool(false)), ("rev", .integer(0))])),
    ]))))
    let state = try engine.state(principal: 1, entryID: 1)
    #expect(state.seen.value == .text("unseen") && state.seen.rev == 8)
    #expect(state.shelf.value == .text("later") && state.shelf.rev == 6)
    #expect(state.kept.value == .flag(true) && state.kept.rev == 9)
    #expect(state.stateRev == 9 && state.unread)
}

@Test func queuedRangeStaysFirstClassAndOrdered() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g1", rev: 2))
    try engine.mergeBootstrap(kind: "entries", items: [
        .object(pairs: [("id", .integer(1)), ("subscription_id", .integer(5)), ("seq", .integer(1)), ("rev", .integer(1))]),
        .object(pairs: [("id", .integer(2)), ("subscription_id", .integer(5)), ("seq", .integer(2)), ("rev", .integer(1))]),
        .object(pairs: [("id", .integer(3)), ("subscription_id", .integer(6)), ("seq", .integer(3)), ("rev", .integer(1))]),
    ])
    try engine.mergeState({
        var state = State(principal: 1, entryID: 1)
        state.seen = Stamp(value: .text("unseen"), rev: 2)
        return state
    }())
    let range = RangeWrite(write: Write(opID: "all-seen", principal: 1, entryID: 0, register: .seen, value: .text("seen"), base: Cursor(generation: "g1", rev: 0), occurredAt: base),
                           scope: Scope(kind: .subscriptions, ids: [5]), throughSeq: 2)
    try engine.queueRange(range)
    try engine.queue(write("kept", register: .kept, value: .flag(true)))
    // Outbox keeps submit order across item kinds.
    let outbox = try engine.snapshot().outbox
    #expect(outbox.count == 2)
    guard case .range(let queued) = outbox[0] else { Issue.record("expected a range item"); return }
    #expect(queued.scope == range.scope && queued.throughSeq == 2)
    // The overlay applies only to in-scope cached members.
    #expect(try engine.state(principal: 1, entryID: 1).seen.value == .text("seen"))
    #expect(try engine.state(principal: 1, entryID: 2).seen.value == .text("seen"))
    #expect(try engine.state(principal: 1, entryID: 3).seen.value == .text("unseen"))
    // Validation: duplicate op, wrong register, empty scope ids.
    #expect(throws: SyncError.self) { try engine.queueRange(range) }
    #expect(throws: SyncError.self) {
        try engine.queueRange(RangeWrite(write: Write(opID: "x", principal: 1, entryID: 0, register: .shelf, value: .text("none"), base: Cursor(generation: "g1", rev: 0), occurredAt: base), scope: Scope(kind: .all), throughSeq: 1))
    }
    #expect(throws: SyncError.self) {
        try engine.queueRange(RangeWrite(write: Write(opID: "y", principal: 1, entryID: 0, register: .seen, value: .text("seen"), base: Cursor(generation: "g1", rev: 0), occurredAt: base), scope: Scope(kind: .subscriptions, ids: []), throughSeq: 1))
    }
}

@Test func acknowledgeRangeMergesAndRemoves() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g1", rev: 1))
    try engine.mergeEntry(SyncEntry(id: 1, subscriptionID: 1, seq: 1, rev: 1))
    try engine.queueRange(RangeWrite(write: write("all-seen", entryID: 0), scope: Scope(kind: .all), throughSeq: 5))
    var seen = State(principal: 1, entryID: 1)
    seen.seen = Stamp(value: .text("seen"), rev: 4)
    seen.stateRev = 4
    let result = RangeResult(status: .applied, throughSeq: 5, subscriptions: [1], except: [],
                             results: [WriteAcknowledgement(opID: "all-seen", status: .applied, state: seen)])
    #expect(throws: SyncError.self) { try engine.acknowledgeRange(opID: "all-seen", generation: "g9", result: result) }
    #expect(try engine.snapshot().outbox.count == 1)
    #expect(try engine.acknowledgeRange(opID: "all-seen", generation: "g1", result: result).isEmpty)
    #expect(try engine.snapshot().outbox.isEmpty)
    #expect(try engine.state(principal: 1, entryID: 1).seen.value == .text("seen"))
    // An expired range reports its item when acknowledged.
    try engine.queueRange(RangeWrite(write: write("old-range", entryID: 0), scope: Scope(kind: .all), throughSeq: 5))
    let expired = try engine.acknowledgeRange(opID: "old-range", generation: "g1", result: RangeResult(status: .expired))
    #expect(expired.map(\.opID) == ["old-range"])
    #expect(try engine.snapshot().outbox.isEmpty)
}

@Test func generationRebasePreservesAllPendingKinds() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g1", rev: 1))
    try engine.queue(write("pending"))
    try engine.queueRange(RangeWrite(write: write("range", entryID: 0), scope: Scope(kind: .all), throughSeq: 3))
    try engine.markInFlight(opIDs: ["pending"])
    try engine.bootstrap(Cursor(generation: "g2", rev: 7))
    let outbox = try engine.snapshot().outbox
    #expect(outbox.map(\.opID) == ["pending", "range"])
    #expect(outbox.allSatisfy { $0.base == Cursor(generation: "g2", rev: 0) && !$0.inFlight })
}

@Test func staleGenerationAckPreservesOutboxAndSkipsMerge() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g1", rev: 1))
    try engine.queue(write("seen"))
    var state = State(principal: 1, entryID: 1)
    state.seen = Stamp(value: .text("seen"), rev: 2)
    state.stateRev = 2
    let result = WriteAcknowledgement(opID: "seen", status: .applied, state: state)
    try engine.bootstrap(Cursor(generation: "g2", rev: 1))
    // The stale-generation response is rejected whole: no merge, no removal.
    #expect(throws: SyncError.self) { try engine.acknowledge([result], generation: "g1") }
    #expect(try engine.snapshot().outbox.count == 1)
    #expect(try engine.state(principal: 1, entryID: 1).seen.rev == 0)
    #expect(try engine.acknowledge([result], generation: "g2").isEmpty)
}

@Test func inFlightOperationsAreNotCoalesced() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g1", rev: 0))
    try engine.queue(write("first", register: .kept, value: .flag(true)))
    try engine.markInFlight(opIDs: ["first"])
    try engine.queue(write("second", register: .kept, value: .flag(false)))
    #expect(try engine.snapshot().outbox.map(\.opID) == ["first", "second"])
    // The acknowledgement removes only its own operation.
    var state = State(principal: 1, entryID: 1)
    state.kept = Stamp(value: .flag(true), rev: 1)
    try engine.acknowledge([WriteAcknowledgement(opID: "first", status: .applied, state: state)], generation: "g1")
    #expect(try engine.snapshot().outbox.map(\.opID) == ["second"])
}

@Test func snapshotRoundTripsThroughCodable() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g1", rev: 4))
    try engine.mergeEntry(SyncEntry(id: 1, subscriptionID: 2, seq: 3, title: "T", rev: 2))
    try engine.mergeState({
        var state = State(principal: 1, entryID: 1)
        state.seen = Stamp(value: .text("seen"), rev: 3, occurredAt: base, actor: 1, source: "deliberate")
        state.stateRev = 3
        return state
    }())
    try engine.mergeObject(SyncObject(kind: .subscription, id: 2, folderID: 1, name: "F", rev: 4))
    try engine.queue(write("pending"))
    try engine.queueRange(RangeWrite(write: write("range", entryID: 0), scope: Scope(kind: .folder, ids: [1]), throughSeq: 8))
    let snapshot = try engine.snapshot()
    let decoded = try JSONDecoder().decode(SyncSnapshot.self, from: JSONEncoder().encode(snapshot))
    #expect(decoded == snapshot)
}

@Test func entryResponsesCannotCrossDatabaseGenerations() throws {
    for count in [1, 2] { // Individual entry and list responses use the same guarded merge.
        let engine = SyncEngine(store: MemorySyncStore())
        try engine.bootstrap(Cursor(generation: "before-restore", rev: 100))
        let requestGeneration = try #require(engine.snapshot().cursor?.generation)
        let oldEntries = (1...count).map { id -> APIEntry in
            let seen = APIRegister(value: .string("seen"), rev: 100, occurredAt: "2026-11-02T12:00:00Z", actor: 1, source: "deliberate")
            let shelf = APIRegister(value: .string("none"), rev: 0, occurredAt: "", actor: 1, source: "deliberate")
            let kept = APIRegister(value: .bool(false), rev: 0, occurredAt: "", actor: 1, source: "deliberate")
            return APIEntry(id: Int64(id), subscriptionID: 1, feedID: 1, seq: Int64(id), firstSeenAt: "2026-11-02T12:00:00Z", title: "Old", url: "", author: "", publishedAt: nil, updatedAt: nil, blurb: nil, rev: 100, deleted: false,
                            state: APIState(principal: 1, entryID: Int64(id), seen: seen, shelf: shelf, kept: kept, stateRev: 100), thumbnailURL: nil)
        }
        // The coordinator completes restore bootstrap while the HTTP request is suspended.
        try engine.bootstrap(Cursor(generation: "after-restore", rev: 5))
        try engine.mergeEntry(SyncEntry(id: 1, subscriptionID: 1, seq: 1, title: "Restored", rev: 5))
        var restored = State(principal: 1, entryID: 1)
        restored.seen = Stamp(value: .text("unseen"), rev: 5)
        restored.stateRev = 5
        try engine.mergeState(restored)
        try engine.finishBootstrap()
        let before = try engine.snapshot()
        #expect(throws: SyncError.self) { try engine.mergeEntries(oldEntries, generation: requestGeneration) }
        #expect(try engine.snapshot() == before)
        restored.seen = Stamp(value: .text("seen"), rev: 6)
        try engine.mergeState(restored)
        #expect(try engine.state(principal: 1, entryID: 1).seen.rev == 6)
        try engine.mergeEntries(oldEntries, generation: "after-restore")
        #expect(try engine.state(principal: 1, entryID: 1).seen.rev == 100)
    }
}

@Test func acknowledgeRangeTombstonesGone() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g1", rev: 1))
    for id: Int64 in [1, 2] {
        try engine.mergeEntry(SyncEntry(id: id, subscriptionID: 1, seq: id, rev: 1))
    }
    for principal: Int64 in [1, 9] {
        var state = State(principal: principal, entryID: 1)
        state.kept = Stamp(value: .flag(true), rev: 1)
        state.stateRev = 1
        try engine.mergeState(state)
    }
    try engine.queueRange(RangeWrite(write: write("range", entryID: 0), scope: Scope(kind: .all), throughSeq: 2))
    let result = RangeResult(status: .applied, results: [
        WriteAcknowledgement(opID: "range", status: .gone, state: State(principal: 1, entryID: 1)),
        WriteAcknowledgement(opID: "range", status: .applied, state: State(principal: 1, entryID: 2)),
        WriteAcknowledgement(opID: "range", status: .gone, state: State(principal: 1, entryID: 3))
    ])
    #expect(throws: SyncError.self) { try engine.acknowledgeRange(opID: "range", generation: "old", result: result) }
    #expect(try engine.entry(1)?.deleted == false)
    try engine.acknowledgeRange(opID: "range", generation: "g1", result: result)
    try engine.mergeEntry(SyncEntry(id: 1, subscriptionID: 1, seq: 1, rev: 10))
    #expect(try engine.entry(1)?.deleted == true)
    #expect(try engine.entry(2)?.deleted == false)
    #expect(try engine.entry(3) == nil)
    #expect(try engine.snapshot().states.map(\.entryID) == [2])
    #expect(try engine.snapshot().outbox.isEmpty)
}

@Test func bulkStatesMatchPerEntryStates() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    let cursor = Cursor(generation: "g1", rev: 1)
    try engine.bootstrap(cursor)
    try engine.mergeEntry(SyncEntry(id: 1, subscriptionID: 1, seq: 1, rev: 1))
    try engine.mergeEntry(SyncEntry(id: 2, subscriptionID: 1, seq: 2, rev: 1))
    var state = State(principal: 1, entryID: 1)
    state.shelf = Stamp(value: .text("done"), rev: 1)
    try engine.mergeBootstrap([state])
    try engine.queue(Write(opID: "keep", principal: 1, entryID: 2, register: .kept, value: .flag(true), base: cursor, occurredAt: base))
    // The bulk read must apply the same overlay as the per-entry path,
    // including a queued write and an uncached entry.
    let bulk = try engine.states(principal: 1, entryIDs: [1, 2, 3])
    #expect(bulk.count == 3)
    for id in [Int64(1), 2, 3] {
        let single = try engine.state(principal: 1, entryID: id)
        #expect(bulk[id]?.seen.value == single.seen.value)
        #expect(bulk[id]?.seen.rev == single.seen.rev)
        #expect(bulk[id]?.shelf.value == single.shelf.value)
        #expect(bulk[id]?.shelf.rev == single.shelf.rev)
        #expect(bulk[id]?.kept.value == single.kept.value)
        #expect(bulk[id]?.kept.rev == single.kept.rev)
    }
    #expect(bulk[2]?.kept.value == .flag(true))
}

// Regression: the server sends a subscription.updated with both a protocol
// object and the native projection at one revision. The projection's extra
// fields, such as a newly stored icon_url, must still reach the cache even
// though the object merged first.
@Test func subscriptionProjectionFieldsApplyAtTheObjectsRevision() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g1", rev: 0))
    try engine.apply([
        change(1, "subscription.created", .object(pairs: [("subscription", .object(pairs: [("id", .integer(7)), ("title", .string("Feed")), ("rev", .integer(1))]))])),
        change(2, "subscription.updated", .object(pairs: [
            ("object", .object(pairs: [("kind", .string("subscription")), ("id", .integer(7)), ("name", .string("Feed")), ("rev", .integer(2))])),
            ("subscription", .object(pairs: [("id", .integer(7)), ("title", .string("Feed")), ("rev", .integer(2)), ("icon_url", .string("/v1/subscriptions/7/icon?v=ab12"))])),
        ])),
    ], next: Cursor(generation: "g1", rev: 2))
    let object = try #require(try engine.snapshot().objects.first { $0.kind == .subscription && $0.id == 7 })
    #expect(object.rev == 2)
    #expect(object.fields["icon_url"] == .string("/v1/subscriptions/7/icon?v=ab12"))
}

// Regression: a bodyless entry.updated, such as the one sent when a
// thumbnail is stored, keeps the row listable from the cache while it waits
// for the REST refresh that restores its body.
@Test func bodylessUpdateKeepsCachedMetadataListable() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g1", rev: 0))
    try engine.mergeEntry(SyncEntry(id: 1, subscriptionID: 7, seq: 1, title: "Story", url: "https://example.com/story", rev: 1))
    try engine.apply([
        change(1, "entry.updated", .object(pairs: [("entry", .object(pairs: [("id", .integer(1)), ("subscription_id", .integer(7)), ("seq", .integer(1)), ("rev", .integer(2))]))])),
    ], next: Cursor(generation: "g1", rev: 1))
    let entry = try #require(try engine.snapshot().entries.first { $0.id == 1 })
    #expect(entry.rev == 2)
    #expect(!entry.hasBody)
    #expect(entry.hasMetadata)
    #expect(entry.title == "Story")
    #expect(!SyncEntry(id: 2, rev: 1, hasBody: false).hasMetadata)
}

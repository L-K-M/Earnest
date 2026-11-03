import Foundation
import Testing
@testable import EarnestKit

@Test func doneClearSurvivesCoalescing() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    let cursor = Cursor(generation: "g1", rev: 1)
    try engine.bootstrap(cursor)
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
    try engine.queue(Write(opID: "one", principal: 1, entryID: 1, register: .kept, value: .flag(true), base: cursor, occurredAt: .distantPast))
    try engine.bootstrap(Cursor(generation: "g2", rev: 4))
    #expect(try engine.snapshot().outbox.first?.base == Cursor(generation: "g2", rev: 0))
    #expect(throws: SyncError.self) { try engine.acknowledge(opID: "one", generation: "g1") }
    #expect(try engine.expire(asOf: Date()).count == 1)
}

@Test func batchAcknowledgementMergesStateAndKeepsNewerIntent() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    let cursor = Cursor(generation: "g1", rev: 0)
    try engine.bootstrap(cursor)
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
    #expect(dropped == [write])
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

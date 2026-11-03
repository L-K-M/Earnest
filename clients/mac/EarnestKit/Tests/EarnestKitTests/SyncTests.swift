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

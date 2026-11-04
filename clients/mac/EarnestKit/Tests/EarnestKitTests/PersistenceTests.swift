import Foundation
import Testing
@testable import EarnestKit

@Test func legacySnapshotKeepsOfflineIntent() throws {
    let legacy = Data(#"{"cursor":{"generation":"g1","rev":2},"states":[{"principal":1,"entryID":1,"stateRev":2,"seen":{"value":"seen","rev":2},"shelf":{"value":"none","rev":0},"kept":{"value":false,"rev":0}}],"outbox":[{"opID":"offline","principal":1,"entryID":1,"register":"kept","value":true,"base":{"generation":"g1","rev":0},"occurredAt":800000000,"inFlight":true}]}"#.utf8)
    let snapshot = try JSONDecoder().decode(SyncSnapshot.self, from: legacy)
    #expect(snapshot.outbox.count == 1)
    #expect(snapshot.outbox.first?.opID == "offline")
    #expect(snapshot.outbox.first?.occurredAt == Date(timeIntervalSinceReferenceDate: 800000000))
    #expect(snapshot.outbox.first?.inFlight == true)
    #expect(snapshot.states.first?.seen.value == .text("seen"))
    let roundTrip = try JSONDecoder().decode(SyncSnapshot.self, from: JSONEncoder().encode(snapshot))
    #expect(roundTrip == snapshot)
}

@Test func corruptOutboxDoesNotSilentlyDisappear() throws {
    let corrupt = Data(#"{"cursor":{"generation":"g1","rev":1},"outbox":[{"write":{"op_id":"valuable-intent","principal":1,"entry_id":1,"register":"kept","value":true,"base_rev":{"generation":"g1","rev":0},"occurred_at":"not-a-date"}}]}"#.utf8)
    #expect(throws: (any Error).self) { try JSONDecoder().decode(SyncSnapshot.self, from: corrupt) }
}

@Test func bootstrapCompletionPersistsAndResets() throws {
    let store = MemorySyncStore()
    let engine = SyncEngine(store: store)
    try engine.bootstrap(Cursor(generation: "g", rev: 0))
    #expect(try !engine.snapshot().bootstrapComplete)
    try engine.finishBootstrap()
    let restored = try JSONDecoder().decode(SyncSnapshot.self, from: JSONEncoder().encode(engine.snapshot()))
    #expect(restored.bootstrapComplete)
    try engine.bootstrap(Cursor(generation: "g", rev: 1))
    #expect(try !engine.snapshot().bootstrapComplete)
}

@Test func resourcesRetainSubscriptionFieldsAndMemberships() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g", rev: 0))
    let subscription: APIValue = .object(["id": .integer(3), "rev": .integer(5), "title": .string("Weekly"), "cadence": .string("week"), "url": .string("https://example.test/feed"), "feed_id": .integer(7)])
    try engine.mergeBootstrap(kind: "subscriptions", items: [subscription])
    let saved = try JSONDecoder().decode(SyncSnapshot.self, from: JSONEncoder().encode(engine.snapshot()))
    #expect(saved.objects.first?.apiValue["cadence"] == .string("week"))
    #expect(saved.objects.first?.apiValue["feed_id"] == .integer(7))
    try engine.mergeBootstrap(kind: "label_memberships", items: [.object(["id": .integer(4), "entry_id": .integer(2), "label_id": .integer(9), "rev": .integer(6), "deleted": .bool(true)])])
    #expect(try engine.object(kind: .label, id: 4)?.entryID == 2)
    #expect(try engine.object(kind: .label, id: 4)?.deleted == true)
}

@Test func missingBodyBecomesTombstoneWithoutInventingRevision() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g", rev: 2))
    try engine.mergeEntry(SyncEntry(id: 1, rev: 2))
    try engine.markGone(entryID: 1)
    #expect(try engine.entry(1)?.deleted == true)
    #expect(try engine.entry(1)?.rev == 2)
    try engine.mergeEntry(SyncEntry(id: 1, title: "stale body", rev: 1))
    #expect(try engine.entry(1)?.deleted == true)
}

@Test func duplicateRevisionsRejectPageAtomically() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g", rev: 0))
    let event = APIChange(rev: 1, type: "entry.created", payload: .object(["entry": .object(["id": .integer(1), "rev": .integer(1)])]))
    #expect(throws: SyncError.self) { try engine.apply([event, event], next: Cursor(generation: "g", rev: 1)) }
    #expect(try engine.snapshot().cursor?.rev == 0)
    #expect(try engine.snapshot().entries.isEmpty)
}

@Test func malformedStatePayloadRejectsPageBeforeCursorMoves() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g", rev: 0))
    try engine.mergeEntry(SyncEntry(id: 1))
    let state: APIValue = .object(["principal": .integer(1), "entry_id": .integer(1), "seen": .object(["value": .bool(true), "rev": .integer(1)])])
    let result: APIValue = .object(["op_id": .string("x"), "status": .string("applied"), "state": state, "replay": .bool(false)])
    let event = APIChange(rev: 1, type: "state.updated", payload: .object(["results": .array([result])]))
    #expect(throws: SyncError.self) { try engine.apply([event], next: Cursor(generation: "g", rev: 1)) }
    #expect(try engine.snapshot().cursor?.rev == 0)
}

@Test func metadataHintUsesEventRevisionAndAllowsBodyEnrichment() throws {
    let engine = SyncEngine(store: MemorySyncStore())
    try engine.bootstrap(Cursor(generation: "g", rev: 0))
    let hint = APIChange(rev: 1, type: "entry.created", payload: .object(["entry": .object(["id": .integer(1), "seq": .integer(2)])]))
    try engine.apply([hint], next: Cursor(generation: "g", rev: 1))
    #expect(try engine.entry(1)?.rev == 1)
    #expect(try engine.entry(1)?.hasBody == false)
    try engine.mergeEntry(SyncEntry(id: 1, seq: 2, title: "Fetched title", url: "https://example.test/", rev: 1))
    #expect(try engine.entry(1)?.title == "Fetched title")
    #expect(try engine.entry(1)?.hasBody == true)
}

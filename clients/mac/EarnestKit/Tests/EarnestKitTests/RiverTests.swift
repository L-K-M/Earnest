import Foundation
import Testing
@testable import EarnestKit

private let riverDirectory = URL(fileURLWithPath: #filePath)
    .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
    .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
    .appendingPathComponent("testdata/river")

@Test func sharedRiverFixtures() throws {
    let files = try FileManager.default.contentsOfDirectory(at: riverDirectory, includingPropertiesForKeys: nil)
        .filter { $0.pathExtension == "json" }.sorted { $0.path < $1.path }
    #expect(!files.isEmpty)
    for file in files {
        let fixture = try JSONDecoder().decode(APIValue.self, from: Data(contentsOf: file))
        let subscriptions = try #require(fixture["subscriptions"]?.arrayValue).compactMap(SyncObject.init(subscription:))
        let mutes = try #require(fixture["mutes"]?.arrayValue).compactMap(SyncObject.init(mute:))
        let settings = SyncObject(settings: .object(["today_decay_days": fixture["today_decay_days"] ?? .integer(2)]))!
        let now = try #require(fixture["now"]?.dateValue)
        let expected = try #require(fixture["expected_ids"]?.arrayValue).compactMap(\.int64Value)
        let actual = try #require(fixture["entries"]?.arrayValue).compactMap { value -> Int64? in
            guard let entry = SyncEntry(value) else { return nil }
            var state = State(principal: 1, entryID: entry.id)
            state.seen.value = .text(value["seen"]?.stringValue ?? "unseen")
            state.shelf.value = .text(value["shelf"]?.stringValue ?? "none")
            return River.contains(entry, state: state, subscription: subscriptions.first { $0.id == entry.subscriptionID },
                                  resources: mutes + [settings], now: now) ? entry.id : nil
        }
        #expect(actual == expected, "\(file.lastPathComponent)")
    }
}

@Test func riverResourcesMergeChangesAndSurviveReload() throws {
    let store = MemorySyncStore()
    let engine = SyncEngine(store: store)
    try engine.bootstrap(Cursor(generation: "g", rev: 0))
    let mute: APIValue = .object(["id": .integer(2), "kind": .string("author"), "value": .string("Alice"), "rev": .integer(0), "deleted": .bool(false)])
    try engine.replaceRiverResources(mutes: [mute], settings: .object(["today_decay_days": .integer(4), "rev": .integer(0)]))
    let change = try JSONDecoder().decode(APIChange.self, from: Data(#"{"rev":1,"type":"mute.deleted","payload":{"mute":{"id":2,"kind":"author","value":"Alice","rev":1,"deleted":true}}}"#.utf8))
    try engine.apply([change], next: Cursor(generation: "g", rev: 1))
    #expect(try engine.object(kind: .mute, id: 2)?.deleted == true)
    let update = try JSONDecoder().decode(APIChange.self, from: Data(#"{"rev":2,"type":"mute.updated","payload":{"mute":{"id":2,"kind":"keyword","value":"Spoiler","rev":2,"deleted":false}}}"#.utf8))
    let settings = try JSONDecoder().decode(APIChange.self, from: Data(#"{"rev":3,"type":"settings.updated","payload":{"settings":{"today_decay_days":7,"rev":3}}}"#.utf8))
    try engine.apply([update, settings], next: Cursor(generation: "g", rev: 3))
    let restored = SyncEngine(store: store)
    #expect(try restored.object(kind: .mute, id: 2)?.fields["mute_kind"] == .string("keyword"))
    #expect(try restored.object(kind: .settings, id: 1)?.fields["today_decay_days"] == .integer(7))
    #expect(try restored.snapshot().cursor == Cursor(generation: "g", rev: 3))
    try restored.bootstrap(Cursor(generation: "new", rev: 0))
    #expect(try restored.snapshot().objects.isEmpty)
}

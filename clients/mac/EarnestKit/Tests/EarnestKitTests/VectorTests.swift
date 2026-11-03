import Foundation
import Testing
@testable import EarnestKit

private let vectors = URL(fileURLWithPath: #filePath)
    .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
    .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
    .appendingPathComponent("testdata/vectors")

// Registered files without a runner are unsupported: they require server replay,
// ranges, object caches, or notification behavior the Swift engine does not implement.
// Registration is a file inventory, not proof that each vector was exercised.
private let registered: Set<String> = [
    "01-range-exception.json", "02-queue-age.json", "03-retention-deleted.json", "04-source-ranks.json",
    "05-equal-time.json", "06-backdated-ingest.json", "07-publisher-clock.json", "08-expired-cursor.json",
    "09-restore-generation.json", "10-notification-response-lost.json", "11-shared-ingest.json",
    "12-offline-google-batch.json", "13-bootstrap-range-race.json", "14-ingest-commit-order.json",
    "15-repeated-operation.json", "16-google-unread-done.json", "17-independent-kept.json",
    "18-late-cluster-member.json", "19-notification-before-snapshot.json", "20-google-tradeoff.json",
    "21-bootstrap-newer-range-crash.json", "22-native-unread-done.json", "23-batch-atomicity.json",
    "24-label-tombstones.json", "25-position-live.json", "26-missing-ts.json", "27-client-coalescing.json",
    "28-clock-boundaries.json", "29-resource-tombstones.json", "30-principal-isolation.json",
    "31-queued-range-client.json", "32-coalesced-done-clear.json", "33-restore-release-safety.json",
    "34-integer-identity.json", "35-batch-causal-shelf.json"
]

@Test func vectorRegistry() throws {
    let names = Set(try FileManager.default.contentsOfDirectory(atPath: vectors.path).filter { $0.hasSuffix(".json") })
    #expect(names == registered)
}

private func number(_ value: Any?) -> Int64 { (value as? NSNumber)?.int64Value ?? 1 }
private func value(_ value: Any?) -> RegisterValue {
    if let string = value as? String { return .text(string) }
    return .flag((value as? Bool) ?? false)
}

/// These vectors need only a single-entry server; other vectors require a complete server simulator.
@Test(arguments: ["27-client-coalescing.json", "32-coalesced-done-clear.json"])
func clientCoalescingVectors(name: String) throws {
    let document = try #require(JSONSerialization.jsonObject(with: Data(contentsOf: vectors.appendingPathComponent(name))) as? [String: Any])
    let engine = SyncEngine(store: MemorySyncStore())
    var server = State(principal: 1, entryID: 1)
    var rev: Int64 = 0
    let date = Date(timeIntervalSince1970: 1_793_620_800)
    for section in ["setup", "operations", "final"] {
        for step in try #require(document[section] as? [[String: Any]]) {
            let op = try #require(step["op"] as? String)
            let expected = step["expect"] as? [String: Any] ?? [:]
            switch op {
            case "ingest": rev += 1
            case "write":
                let write = try #require(step["write"] as? [String: Any])
                let registerName = try #require(write["register"] as? String)
                let register = try #require(Register(rawValue: registerName))
                rev += 1
                server[register] = Stamp(value: value(write["value"]), rev: rev)
                server.stateRev = rev
            case "bootstrap_begin": try engine.bootstrap(Cursor(generation: "g1", rev: rev))
            case "bootstrap_page": try engine.mergeBootstrap([server])
            case "client_pull": break // Bootstrap already captured the simulated server's current revision.
            case "client_queue":
                let input = try #require(step["write"] as? [String: Any])
                let registerName = try #require(input["register"] as? String)
                let register = try #require(Register(rawValue: registerName))
                let current = try engine.state(principal: 1, entryID: number(input["entry_id"]))
                try engine.queue(Write(opID: try #require(input["op_id"] as? String), principal: 1,
                                       entryID: number(input["entry_id"]), register: register, value: value(input["value"]),
                                       base: Cursor(generation: "g1", rev: current[register].rev), occurredAt: date))
            case "client_flush":
                let pending = try engine.snapshot().outbox
                let results = try #require(step["expect"] as? [[String: Any]])
                #expect(pending.count == results.count)
                for (write, result) in zip(pending, results) {
                    if let id = result["op_id"] as? String { #expect(write.opID == id) }
                    #expect(result["status"] as? String == "applied")
                    rev += 1
                    server[write.register] = Stamp(value: write.value, rev: rev)
                    server.stateRev = rev
                    try engine.acknowledge(opID: write.opID, generation: "g1")
                }
                try engine.mergeBootstrap([server])
            case "assert_state":
                let actual = step["device"] == nil ? server : try engine.state(principal: number(step["principal"]), entryID: number(step["entry_id"]))
                if let state = expected["state"] as? [String: [String: Any]] {
                    for (key, assertion) in state {
                        let register = try #require(Register(rawValue: key))
                        if let expectedValue = assertion["value"] { #expect(actual[register].value == value(expectedValue)) }
                    }
                }
                if let unread = expected["unread"] as? Bool {
                    #expect((actual.seen.value == .text("unseen") && actual.shelf.value != .text("done")) == unread)
                }
            case "assert_client":
                if let size = expected["outbox_size"] as? Int { #expect(try engine.snapshot().outbox.count == size) }
            case "assert_log": break // Server revision and batch atomicity are outside this client simulator.
            default: Issue.record("Unsupported step \(op) in \(name)")
            }
        }
    }
}

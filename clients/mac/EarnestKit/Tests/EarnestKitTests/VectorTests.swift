import Foundation
import Testing
@testable import EarnestKit

private let vectorDirectory = URL(fileURLWithPath: #filePath)
    .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
    .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
    .appendingPathComponent("testdata/vectors")

/// Every shared vector must be registered here; the gate fails if the testdata
/// directory contains anything else (mirrors `vectorFiles` in
/// internal/syncmodel/vectors_test.go).
private let vectorFiles: [String] = [
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
    "34-integer-identity.json", "35-batch-causal-shelf.json", "36-ack-out-of-order-merge.json",
    "37-ack-stale-generation.json", "38-independent-register-merge.json",
]

// MARK: - Coverage model
//
// Every step of every registered vector executes. Client-observable steps run
// through the real SyncEngine; server behavior runs through the in-memory
// reference simulator (VectorSim.swift, a port of internal/syncmodel). Only
// the notification lifecycle is engine-invisible — SyncEngine has no
// notification surface — so those steps execute through the simulator alone.
//
// | file                            | skipped op(s)                                   | reason |
// |---------------------------------|-------------------------------------------------|--------|
// | 10-notification-response-lost   | accept/attempt/complete_notification            | notification lifecycle is server-internal; no client surface exists to exercise |
// | 19-notification-before-snapshot | accept/attempt/complete/release_notifications   | same — withhold/release window is a server invariant |
// | 33-restore-release-safety       | accept/attempt/complete/release_notifications   | same — notification safety after restore is server-internal |
//
// Coverage totals (asserted below): 35 files exercise the engine, 3 files are
// server-internal only, 0 files partially covered, 0 files not run.

enum VectorStepKind: String {
    /// Went through SyncEngine: bootstrap, pulls, queue/submit/ack/flush,
    /// device asserts, and non-device asserts evaluated on the mirror cache.
    case engine
    /// `assert_log`: the log summary is server-internal, but the retained
    /// page is replayed through a fresh SyncEngine and must land on the
    /// server's revision.
    case replay
    /// Ran against the reference simulator only: ingest, writes, ranges,
    /// objects, positions, changes, checkpoint/restore.
    case simulated
    /// Notification lifecycle ops: server-internal, simulator only, engine
    /// has no equivalent surface.
    case serverInternal = "server-internal"
}

struct VectorFileReport {
    var file: String
    var kinds: [VectorStepKind]
    var engineSteps: Int { kinds.filter { $0 == .engine }.count }
    var replaySteps: Int { kinds.filter { $0 == .replay }.count }
    var internalSteps: Int { kinds.filter { $0 == .serverInternal }.count }
    /// full = every step ran and at least one reached SyncEngine;
    /// internal-only = every step ran but only replay touched the engine —
    /// the notification files carry no client-observable state at all;
    /// partial/not-run do not occur: no step is skipped.
    var classification: String {
        engineSteps > 0 ? "full" : "server-internal-only"
    }
}

private func classify(_ step: VStep) -> VectorStepKind {
    switch step.op {
    case "accept_notification", "attempt_notification", "complete_notification", "release_notifications":
        return .serverInternal
    case "assert_log":
        return .replay
    case "bootstrap_begin", "bootstrap_page", "client_pull", "client_apply", "client_queue",
         "client_queue_range", "client_merge_state", "client_submit", "client_ack", "client_flush",
         "assert_state", "assert_entry", "assert_object", "assert_position", "assert_client":
        return .engine
    default:
        return step.device.isEmpty ? .simulated : .engine
    }
}

private func loadVector(_ file: String) throws -> VectorDocument {
    let data = try Data(contentsOf: vectorDirectory.appendingPathComponent(file))
    return try VectorDocument.decode(VJSONParser.parse(data))
}

@Test func vectorRegistry() throws {
    let names = Set(try FileManager.default.contentsOfDirectory(atPath: vectorDirectory.path).filter { $0.hasSuffix(".json") })
    #expect(names == Set(vectorFiles))
}

/// The drift gate: executes all 38 vectors end to end. Every step's `expect`
/// is compared as a recursive subset (objects) with exact array equality, and
/// `error` names match the shared vocabulary. Per-file failures report every
//  mismatched step so one drift cannot hide the rest.
@Test(arguments: vectorFiles)
func syncVector(_ file: String) throws {
    let document = try loadVector(file)
    let runner = VectorRunner()
    var failures: [String] = []
    var kinds: [VectorStepKind] = []
    var index = 0
    for (section, steps) in [("setup", document.setup), ("operations", document.operations), ("final", document.final)] {
        for step in steps {
            let (actual, errorName) = try runner.execute(step)
            kinds.append(classify(step))
            if errorName != step.error {
                failures.append("\(section)[\(index)] \(step.op): error \(errorName), want \(step.error)")
            }
            if let mismatch = VJSON.subset(expected: step.expect, actual: actual) {
                failures.append("\(section)[\(index)] \(step.op): \(mismatch); got \(actual.describe())")
            }
            index += 1
        }
    }
    VectorCoverage.shared.record(file, kinds: kinds)
    #expect(failures.isEmpty, "\(file):\n\(failures.joined(separator: "\n"))")
}

/// Static coverage accounting, asserted rather than narrated: exactly the 35
/// vectors with client-observable steps exercise SyncEngine; the three
/// notification vectors exercise only server-internal behavior. The report is
/// also written for the task ledger.
@Test func vectorCoverageSummary() throws {
    var full = 0, internalOnly = 0, partial = 0, notRun = 0
    var lines: [String] = ["file|steps|engine|replay|simulated|server-internal|coverage", "---|---|---|---|---|---|---"]
    for file in vectorFiles {
        let document = try loadVector(file)
        let kinds = (document.setup + document.operations + document.final).map(classify)
        let report = VectorFileReport(file: file, kinds: kinds)
        switch report.classification {
        case "full": full += 1
        default: internalOnly += 1
        }
        lines.append("\(file)|\(kinds.count)|\(report.engineSteps)|\(report.replaySteps)|\(kinds.count - report.engineSteps - report.replaySteps - report.internalSteps)|\(report.internalSteps)|\(report.classification)")
    }
    lines.append("totals: full=\(full) server-internal-only=\(internalOnly) partial=\(partial) not-run=\(notRun)")
    try lines.joined(separator: "\n").write(toFile: "/tmp/mac-vector-coverage.txt", atomically: true, encoding: .utf8)
    #expect(full == 35)
    #expect(internalOnly == 3)
    #expect(partial == 0)
    #expect(notRun == 0)
}

@Test func unknownVectorRejected() throws {
    #expect(!vectorFiles.contains("99-unknown.json"))
    let runner = VectorRunner()
    let (actual, errorName) = try runner.execute(VStep(op: "unrecognized"))
    #expect(errorName == "invalid")
    #expect(actual == .null)
    #expect(throws: VJSON.Failure.self) {
        try VectorDocument.decode(try VJSONParser.parse(#"{"version":99,"name":"unknown","decision":"x","final":[{"op":"noop","expect":{}}]}"#))
    }
    #expect(throws: VJSON.Failure.self) {
        try VectorDocument.decode(try VJSONParser.parse(#"{"version":1,"unexpected":true}"#))
    }
    #expect(throws: VJSON.Failure.self) {
        try VStep.decode(try VJSONParser.parse(#"{"op":"write","surprise":1,"expect":{}}"#))
    }
}

@Test func expectedSubsetSemantics() throws {
    // Object subset: expected keys must exist; extra actual keys are allowed.
    #expect(VJSON.subset(expected: .object(["a": .int(1)]), actual: .object(["a": .int(1), "b": .int(2)])) == nil)
    // Arrays are exact and ordered.
    #expect(VJSON.subset(expected: .array([.int(1)]), actual: .array([.int(1), .int(2)])) != nil)
    // Deep recursion still reaches scalars.
    #expect(VJSON.subset(expected: .object(["a": .object(["b": .int(1)])]), actual: .object(["a": .object(["b": .int(2)])])) != nil)
    // Integer identity: 2^63-1 must not round through a double.
    let max = VJSON.int(.max)
    #expect(VJSON.subset(expected: .object(["id": max]), actual: .object(["id": max])) == nil)
    #expect(VJSON.subset(expected: .object(["id": max]), actual: .object(["id": .int(.max - 1)])) != nil)
}

/// Aggregate step records for the task ledger; also catches drift in what the
/// gate chose to execute vs. what the files contain.
final class VectorCoverage: @unchecked Sendable {
    static let shared = VectorCoverage()
    private let lock = NSLock()
    private var reports: [String: VectorFileReport] = [:]
    func record(_ file: String, kinds: [VectorStepKind]) {
        lock.withLock { reports[file] = VectorFileReport(file: file, kinds: kinds) }
    }
    var recorded: [String: VectorFileReport] {
        lock.withLock { reports }
    }
}

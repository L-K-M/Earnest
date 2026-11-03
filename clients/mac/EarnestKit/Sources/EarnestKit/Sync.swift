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
public struct Stamp: Codable, Equatable, Sendable {
    public var value: RegisterValue
    public var rev: Int64
    public init(value: RegisterValue, rev: Int64) { self.value = value; self.rev = rev }
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
    public subscript(_ register: Register) -> Stamp {
        get { switch register { case .seen: seen; case .shelf: shelf; case .kept: kept } }
        set { switch register { case .seen: seen = newValue; case .shelf: shelf = newValue; case .kept: kept = newValue } }
    }
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
    public var inFlight: Bool
    public init(opID: String, principal: Int64, entryID: Int64, register: Register, value: RegisterValue, base: Cursor, occurredAt: Date, inFlight: Bool = false) {
        self.opID = opID; self.principal = principal; self.entryID = entryID; self.register = register; self.value = value; self.base = base; self.occurredAt = occurredAt; self.inFlight = inFlight
    }
}
public struct SyncSnapshot: Codable, Equatable, Sendable {
    public var cursor: Cursor?
    public var states: [State] = []
    public var outbox: [Write] = []
    public init() {}
}
/// Saving must atomically replace the snapshot, including the cursor.
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
public enum WriteStatus: String, Sendable { case applied, superseded, gone, expired }
public struct WriteAcknowledgement: Sendable {
    public let opID: String
    public let status: WriteStatus
    public let state: State?
    public init(opID: String, status: WriteStatus, state: State?) {
        self.opID = opID; self.status = status; self.state = state
    }
}
public struct StateChange: Sendable {
    public var rev: Int64
    public var state: State
    public init(rev: Int64, state: State) { self.rev = rev; self.state = state }
}
public final class SyncEngine {
    private let store: any SyncStore
    public init(store: any SyncStore) { self.store = store }
    public func snapshot() throws -> SyncSnapshot { try store.load() }
    public func bootstrap(_ cursor: Cursor) throws {
        var data = try store.load()
        if let previous = data.cursor, previous.generation != cursor.generation {
            for index in data.outbox.indices {
                data.outbox[index].base = Cursor(generation: cursor.generation, rev: 0)
                data.outbox[index].inFlight = false
            }
        }
        data.cursor = cursor; data.states = []
        try store.save(data)
    }
    public func mergeBootstrap(_ states: [State]) throws {
        var data = try store.load()
        for state in states { merge(state, into: &data.states) }
        try store.save(data)
    }
    public func apply(_ changes: [StateChange], next: Cursor) throws {
        var data = try store.load()
        guard let cursor = data.cursor, cursor.generation == next.generation else { throw SyncError.generationMismatch }
        if next.rev <= cursor.rev { return }
        guard next.rev - cursor.rev == Int64(changes.count), changes.enumerated().allSatisfy({ $0.element.rev == cursor.rev + Int64($0.offset) + 1 }) else { throw SyncError.invalidPage }
        for change in changes { merge(change.state, into: &data.states) }
        data.cursor = next
        try store.save(data)
    }
    public func queue(_ write: Write) throws {
        guard write.principal > 0, write.entryID > 0, !write.opID.isEmpty, !write.opID.hasSuffix("/clear-done"), valid(write.register, write.value) else { throw SyncError.invalidWrite }
        var data = try store.load()
        guard data.cursor?.generation == write.base.generation else { throw SyncError.generationMismatch }
        let current = overlay(principal: write.principal, entryID: write.entryID, data: data)
        if write.register == .seen, write.value == .text("unseen"), current.shelf.value == .text("done") {
            var clear = write
            clear.opID += "/clear-done"; clear.register = .shelf; clear.value = .text("none"); clear.base.rev = current.shelf.rev
            coalesce(clear, into: &data.outbox)
        }
        coalesce(write, into: &data.outbox)
        try store.save(data)
    }
    public func state(principal: Int64, entryID: Int64) throws -> State { overlay(principal: principal, entryID: entryID, data: try store.load()) }
    public func markInFlight(opIDs: Set<String>) throws {
        var data = try store.load()
        for index in data.outbox.indices where opIDs.contains(data.outbox[index].opID) { data.outbox[index].inFlight = true }
        try store.save(data)
    }
    public func acknowledge(opID: String, generation: String) throws {
        var data = try store.load()
        guard data.cursor?.generation == generation else { throw SyncError.generationMismatch }
        data.outbox.removeAll { $0.opID == opID && $0.base.generation == generation }
        try store.save(data)
    }
    /// Persist effective states and remove only acknowledged operations together.
    /// Returns gone and expired writes so the caller can report them.
    @discardableResult public func acknowledge(_ results: [WriteAcknowledgement], generation: String) throws -> [Write] {
        var data = try store.load()
        guard data.cursor?.generation == generation else { throw SyncError.generationMismatch }
        guard Set(results.map(\.opID)).count == results.count else { throw SyncError.invalidPage }
        var dropped: [Write] = []
        for result in results {
            guard let write = data.outbox.first(where: { $0.opID == result.opID && $0.base.generation == generation }) else { continue }
            if let state = result.state, state.principal == write.principal, state.entryID == write.entryID {
                merge(state, into: &data.states)
            } else if result.status == .applied || result.status == .superseded {
                throw SyncError.invalidPage
            }
            if result.status == .gone || result.status == .expired { dropped.append(write) }
            data.outbox.removeAll { $0.opID == result.opID && $0.base.generation == generation }
        }
        try store.save(data)
        return dropped
    }
    @discardableResult public func expire(asOf now: Date) throws -> [Write] {
        var data = try store.load()
        let expired = data.outbox.filter { now.timeIntervalSince($0.occurredAt) > 30 * 24 * 60 * 60 }
        data.outbox.removeAll { now.timeIntervalSince($0.occurredAt) > 30 * 24 * 60 * 60 }
        try store.save(data)
        return expired
    }
    private func valid(_ register: Register, _ value: RegisterValue) -> Bool {
        switch (register, value) {
        case (.seen, .text("seen")), (.seen, .text("unseen")), (.shelf, .text("none")), (.shelf, .text("later")), (.shelf, .text("done")), (.kept, .flag): true
        default: false
        }
    }
    private func merge(_ state: State, into states: inout [State]) {
        if let index = states.firstIndex(where: { $0.principal == state.principal && $0.entryID == state.entryID }) { states[index].merge(state) }
        else { states.append(state) }
    }
    private func coalesce(_ write: Write, into outbox: inout [Write]) {
        var next = write
        if let index = outbox.lastIndex(where: { !$0.inFlight && $0.principal == write.principal && $0.entryID == write.entryID && $0.register == write.register }) {
            next.base = outbox[index].base
            outbox.remove(at: index)
        }
        outbox.append(next)
    }
    private func overlay(principal: Int64, entryID: Int64, data: SyncSnapshot) -> State {
        var result = data.states.first { $0.principal == principal && $0.entryID == entryID } ?? State(principal: principal, entryID: entryID)
        for write in data.outbox where write.principal == principal && write.entryID == entryID { result[write.register].value = write.value }
        return result
    }
}

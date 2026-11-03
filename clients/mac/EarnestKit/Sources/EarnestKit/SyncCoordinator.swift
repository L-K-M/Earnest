import Foundation

public struct SyncReport: Sendable {
    public var droppedOperationIDs: [String] = []
    public var recovered = false
    public init() {}
}

/// Main-actor ownership serializes the engine and coalesces concurrent refresh/event requests.
@MainActor public final class SyncCoordinator {
    private let engine: SyncEngine
    private let api: EarnestAPI
    private var active: Task<SyncReport, Error>?

    public init(engine: SyncEngine, api: EarnestAPI) { self.engine = engine; self.api = api }

    public func cancel() { active?.cancel() }

    public func synchronize() async throws -> SyncReport {
        if let active { return try await active.value }
        let task = Task { try await self.synchronizeOnce() }
        active = task
        defer { active = nil }
        return try await task.value
    }

    private func synchronizeOnce() async throws -> SyncReport {
        var report = SyncReport()
        do {
            let snapshot = try engine.snapshot()
            if snapshot.cursor == nil || !snapshot.bootstrapComplete { try await bootstrap() }
            try await pull()
            try await push(report: &report)
            try await pull()
        } catch {
            guard needsBootstrap(error) else { throw error }
            // Exactly one recovery per sync attempt; a second 410 is surfaced to the caller.
            report.recovered = true
            try await bootstrap()
            try await pull()
            try await push(report: &report)
            try await pull()
        }
        return report
    }

    private func bootstrap() async throws {
        let start = try await api.bootstrapCursor()
        try Task.checkCancellation()
        try engine.bootstrap(Cursor(generation: start.generation, rev: start.rev))
        for kind in ["subscriptions", "folders", "labels", "label_memberships", "entries", "state", "positions"] {
            var boundary = "0:0:"
            var query: [URLQueryItem] = []
            while true {
                let page = try await api.bootstrapPage(kind: kind, query: query)
                try Task.checkCancellation()
                try engine.mergeBootstrap(kind: kind, items: page.items ?? [])
                guard page.hasMore else { break }
                let next = "\(page.nextPrincipal):\(page.nextID):\(page.nextView)"
                guard next != boundary else { throw SyncError.invalidPage }
                boundary = next
                query = [.init(name: "after_id", value: String(page.nextID)),
                         .init(name: "after_principal", value: String(page.nextPrincipal)),
                         .init(name: "after_view", value: page.nextView)]
            }
        }
        // Annotations currently page through their ordinary endpoint, including tombstones.
        var afterID: Int64 = 0
        while true {
            let items = try await api.annotations(afterID: afterID)
            try Task.checkCancellation()
            try engine.mergeBootstrap(kind: "annotations", items: items)
            guard items.count == 1000 else { break }
            let ids = items.compactMap { item -> Int64? in
                guard case .object(let fields) = item, case .integer(let id) = fields["id"] else { return nil }
                return id
            }
            guard let next = ids.max(), next > afterID else { throw SyncError.invalidPage }
            afterID = next
        }
        try engine.finishBootstrap()
    }

    private func pull() async throws {
        while true {
            try Task.checkCancellation()
            guard let cursor = try engine.snapshot().cursor else { throw SyncError.invalidPage }
            let page = try await api.changes(cursor: "\(cursor.generation):\(cursor.rev)")
            try Task.checkCancellation()
            let next = Cursor(generation: page.next.generation, rev: page.next.rev)
            try engine.apply(page.changes ?? [], next: next)
            if (page.changes ?? []).isEmpty { return }
            guard next.rev > cursor.rev else { throw SyncError.invalidPage }
        }
    }

    private func push(report: inout SyncReport) async throws {
        report.droppedOperationIDs += try engine.expire(asOf: Date()).map(\.opID)
        while let first = try engine.snapshot().outbox.first {
            try Task.checkCancellation()
            let generation = first.base.generation
            switch first {
            case .range(let range):
                try engine.markInFlight(opIDs: [first.opID])
                let response = try await api.writeRange(range.apiValue)
                try Task.checkCancellation()
                guard let result = RangeResult(response) else { throw SyncError.invalidPage }
                report.droppedOperationIDs += try engine.acknowledgeRange(opID: first.opID, generation: generation, result: result).map(\.opID)
            case .write:
                var batch: [OutboxItem] = []
                var targets = Set<String>()
                for item in try engine.snapshot().outbox {
                    guard case .write = item, item.inFlight == first.inFlight, batch.count < 1000 else { break }
                    let target = "\(item.principal):\(item.entryID):\(item.register.rawValue)"
                    guard targets.insert(target).inserted else { break }
                    batch.append(item)
                }
                let ids = Set(batch.map(\.opID))
                try engine.markInFlight(opIDs: ids)
                let response = try await api.writeBatch(batch.map { $0.write.apiValue }) ?? []
                try Task.checkCancellation()
                let results = response.compactMap(WriteAcknowledgement.init)
                guard results.count == batch.count, Set(results.map(\.opID)) == ids else { throw SyncError.invalidPage }
                report.droppedOperationIDs += try engine.acknowledge(results, generation: generation).map(\.opID)
            }
        }
    }

    /// Reconnect with the persisted cursor. Every connection's initial hint closes the offline gap.
    public func runEvents(onSync: @escaping @MainActor (SyncReport) async -> Void = { _ in }) async throws {
        while !Task.isCancelled {
            do {
                let cursor = try engine.snapshot().cursor
                let text = cursor.map { "\($0.generation):\($0.rev)" }
                for try await _ in try api.eventHints(cursor: text) {
                    let report = try await synchronize()
                    await onSync(report)
                }
            } catch {
                try Task.checkCancellation()
                if needsBootstrap(error) {
                    let report = try await synchronize()
                    await onSync(report)
                } else if isAuthenticationError(error) { throw error }
            }
            try await Task.sleep(for: .seconds(3))
        }
    }

    private func needsBootstrap(_ error: any Error) -> Bool {
        if case APIError.httpStatus(410) = error { return true }
        if let problem = error as? APIProblem { return problem.status == 410 }
        if case SyncError.generationMismatch = error { return true }
        return false
    }

    private func isAuthenticationError(_ error: any Error) -> Bool {
        if let problem = error as? APIProblem { return [401, 403].contains(problem.status) }
        if case APIError.httpStatus(let status) = error { return [401, 403].contains(status) }
        return false
    }
}

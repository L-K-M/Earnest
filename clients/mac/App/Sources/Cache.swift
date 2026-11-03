import Foundation
import GRDB
import EarnestKit

// SQLite transactions make a cache snapshot and its cursor durable together.
final class Cache: SyncStore {
    private let db: DatabaseQueue
    private let key: String

    init(server: URL, principal: Int64) throws {
        guard principal > 0 else { throw CacheError.invalidPrincipal }
        key = "\(server.absoluteString)#\(principal)"
        let directory = try FileManager.default.url(for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true).appendingPathComponent("Earnest", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        db = try DatabaseQueue(path: directory.appendingPathComponent("cache.sqlite").path)
        try db.write { db in
            try db.execute(sql: "CREATE TABLE IF NOT EXISTS snapshots (key TEXT PRIMARY KEY, value BLOB NOT NULL)")
        }
    }

    func load() throws -> SyncSnapshot {
        guard let data = try read(key) else { return SyncSnapshot() }
        return try JSONDecoder().decode(SyncSnapshot.self, from: data)
    }

    func save(_ snapshot: SyncSnapshot) throws {
        try write(key, value: JSONEncoder().encode(snapshot))
    }

    private func read(_ key: String) throws -> Data? {
        try db.read { db in try Data.fetchOne(db, sql: "SELECT value FROM snapshots WHERE key = ?", arguments: [key]) }
    }

    private func write(_ key: String, value: Data) throws {
        try db.write { db in
            try db.execute(sql: "INSERT INTO snapshots (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", arguments: [key, value])
        }
    }
}

enum CacheError: Error { case invalidPrincipal }

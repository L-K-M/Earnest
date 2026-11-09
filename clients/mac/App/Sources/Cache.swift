import Foundation
import GRDB
import EarnestKit

// SQLite transactions make a cache snapshot and its cursor durable together.
// The database queue serializes every access and the identity key never
// changes, so the cache may be used off the main actor.
final class Cache: SyncStore, @unchecked Sendable {
    static let articleLimit = 200
    static let mediaLimit = 2000
    private let db: DatabaseQueue
    private let key: String

    /// Location of the SQLite file shared by every identity.
    static func databaseURL(in directory: URL?) throws -> URL {
        let directory = try directory ?? FileManager.default.url(for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true).appendingPathComponent("Earnest", isDirectory: true)
        return directory.appendingPathComponent("cache.sqlite")
    }

    /// Whether an error from opening the cache means the database file itself
    /// is corrupt or undecodable, as opposed to a transient environment
    /// failure (permissions, disk full, SQLITE_BUSY, directory creation).
    static func isUnrecoverableOpenFailure(_ error: Error) -> Bool {
        if let database = error as? DatabaseError {
            return database.resultCode == .SQLITE_CORRUPT || database.resultCode == .SQLITE_NOTADB
        }
        return error is DecodingError
    }

    /// Move an unusable database and its journal siblings aside, keeping only
    /// the newest quarantine copy, so the original bytes stay inspectable and
    /// repeated corruption cannot fill the disk.
    static func quarantineCorruptCache(in directory: URL?) throws {
        let database = try databaseURL(in: directory)
        let fileManager = FileManager.default
        let folder = database.deletingLastPathComponent()
        let stamp = ISO8601DateFormatter().string(from: Date())
        for suffix in ["", "-wal", "-shm"] {
            let source = URL(fileURLWithPath: database.path + suffix)
            guard fileManager.fileExists(atPath: source.path) else { continue }
            let destination = URL(fileURLWithPath: source.path + ".corrupt-" + stamp)
            if fileManager.fileExists(atPath: destination.path) { try fileManager.removeItem(at: destination) }
            try fileManager.moveItem(at: source, to: destination)
        }
        let base = database.lastPathComponent
        let names = try fileManager.contentsOfDirectory(atPath: folder.path)
            .filter { $0.hasPrefix(base) && $0.contains(".corrupt-") }
        let stamps = Set(names.compactMap { name -> String? in
            name.range(of: ".corrupt-").map { String(name[$0.upperBound...]) }
        })
        guard let newest = stamps.max() else { return }
        for name in names {
            guard let range = name.range(of: ".corrupt-") else { continue }
            if String(name[range.upperBound...]) != newest {
                try fileManager.removeItem(at: folder.appendingPathComponent(name))
            }
        }
    }

    init(identity: AccountIdentity, directory: URL? = nil) throws {
        key = identity.cacheKey
        let database = try Self.databaseURL(in: directory)
        try FileManager.default.createDirectory(at: database.deletingLastPathComponent(), withIntermediateDirectories: true)
        db = try DatabaseQueue(path: database.path)
        try db.write { db in
            try db.execute(sql: "CREATE TABLE IF NOT EXISTS snapshots (key TEXT PRIMARY KEY, value BLOB NOT NULL)")
            try db.execute(sql: """
                CREATE TABLE IF NOT EXISTS cached_resources (
                    identity TEXT NOT NULL, name TEXT NOT NULL, generation TEXT NOT NULL, value BLOB NOT NULL,
                    PRIMARY KEY (identity, name)
                )
                """)
            try db.execute(sql: """
                CREATE TABLE IF NOT EXISTS article_copies (
                    identity TEXT NOT NULL, entry_id INTEGER NOT NULL, generation TEXT NOT NULL,
                    html TEXT NOT NULL, recency INTEGER NOT NULL,
                    PRIMARY KEY (identity, entry_id)
                )
                """)
            try db.execute(sql: """
                CREATE TABLE IF NOT EXISTS media_copies (
                    identity TEXT NOT NULL, path TEXT NOT NULL, data BLOB NOT NULL, recency INTEGER NOT NULL,
                    PRIMARY KEY (identity, path)
                )
                """)
        }
    }

    /// Forget everything persisted for this identity, as if it never synced.
    func clear() throws {
        try db.write { db in
            try db.execute(sql: "DELETE FROM snapshots WHERE key = ?", arguments: [key])
            try db.execute(sql: "DELETE FROM cached_resources WHERE identity = ?", arguments: [key])
            try db.execute(sql: "DELETE FROM article_copies WHERE identity = ?", arguments: [key])
            try db.execute(sql: "DELETE FROM media_copies WHERE identity = ?", arguments: [key])
        }
    }

    func load() throws -> SyncSnapshot {
        let data = try db.read { db in
            try Data.fetchOne(db, sql: "SELECT value FROM snapshots WHERE key = ?", arguments: [key])
        }
        guard let data else { return SyncSnapshot() }
        return try JSONDecoder().decode(SyncSnapshot.self, from: data)
    }

    func save(_ snapshot: SyncSnapshot) throws {
        let data = try JSONEncoder().encode(snapshot)
        try db.write { db in
            try db.execute(sql: "INSERT INTO snapshots (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", arguments: [key, data])
        }
    }

    func resource(_ name: String, generation: String) throws -> [APIValue] {
        let data = try db.read { db in
            try Data.fetchOne(db, sql: "SELECT value FROM cached_resources WHERE identity = ? AND name = ? AND generation = ?", arguments: [key, name, generation])
        }
        guard let data else { return [] }
        return try JSONDecoder().decode([APIValue].self, from: data)
    }

    func saveResource(_ name: String, values: [APIValue], generation: String) throws {
        let data = try JSONEncoder().encode(values)
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO cached_resources (identity, name, generation, value) VALUES (?, ?, ?, ?)
                ON CONFLICT(identity, name) DO UPDATE SET generation = excluded.generation, value = excluded.value
                """, arguments: [key, name, generation, data])
        }
    }

    func article(entryID: Int64, generation: String) throws -> String? {
        try db.write { db in
            let html = try String.fetchOne(db, sql: "SELECT html FROM article_copies WHERE identity = ? AND entry_id = ? AND generation = ?", arguments: [key, entryID, generation])
            if html != nil { try touchArticle(entryID, db: db) }
            return html
        }
    }

    func removeArticle(entryID: Int64, generation: String) throws {
        try db.write { db in
            try db.execute(sql: "DELETE FROM article_copies WHERE identity = ? AND entry_id = ? AND generation = ?", arguments: [key, entryID, generation])
        }
    }

    func saveArticle(entryID: Int64, html: String, generation: String) throws {
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO article_copies (identity, entry_id, generation, html, recency) VALUES (?, ?, ?, ?, 0)
                ON CONFLICT(identity, entry_id) DO UPDATE SET generation = excluded.generation, html = excluded.html
                """, arguments: [key, entryID, generation, html])
            try touchArticle(entryID, db: db)
            try db.execute(sql: """
                DELETE FROM article_copies WHERE identity = ? AND entry_id NOT IN (
                    SELECT entry_id FROM article_copies WHERE identity = ? ORDER BY recency DESC LIMIT ?
                )
                """, arguments: [key, key, Self.articleLimit])
        }
    }

    private func touchArticle(_ entryID: Int64, db: Database) throws {
        try db.execute(sql: """
            UPDATE article_copies SET recency = (SELECT COALESCE(MAX(recency), 0) + 1 FROM article_copies WHERE identity = ?)
            WHERE identity = ? AND entry_id = ?
            """, arguments: [key, key, entryID])
    }

    /// A stored thumbnail or icon; reading counts as use so eviction keeps
    /// what rows still show.
    func media(path: String) throws -> Data? {
        try db.write { db in
            let data = try Data.fetchOne(db, sql: "SELECT data FROM media_copies WHERE identity = ? AND path = ?", arguments: [key, path])
            if data != nil { try touchMedia(path, db: db) }
            return data
        }
    }

    func saveMedia(path: String, data: Data) throws {
        try db.write { db in
            try db.execute(sql: """
                INSERT INTO media_copies (identity, path, data, recency) VALUES (?, ?, ?, 0)
                ON CONFLICT(identity, path) DO UPDATE SET data = excluded.data
                """, arguments: [key, path, data])
            try touchMedia(path, db: db)
            try db.execute(sql: """
                DELETE FROM media_copies WHERE identity = ? AND path NOT IN (
                    SELECT path FROM media_copies WHERE identity = ? ORDER BY recency DESC LIMIT ?
                )
                """, arguments: [key, key, Self.mediaLimit])
        }
    }

    private func touchMedia(_ path: String, db: Database) throws {
        try db.execute(sql: """
            UPDATE media_copies SET recency = (SELECT COALESCE(MAX(recency), 0) + 1 FROM media_copies WHERE identity = ?)
            WHERE identity = ? AND path = ?
            """, arguments: [key, key, path])
    }
}

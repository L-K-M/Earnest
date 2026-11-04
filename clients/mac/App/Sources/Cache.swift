import Foundation
import GRDB
import EarnestKit

// SQLite transactions make a cache snapshot and its cursor durable together.
final class Cache: SyncStore {
    static let articleLimit = 200
    private let db: DatabaseQueue
    private let key: String

    init(identity: AccountIdentity, directory: URL? = nil) throws {
        key = identity.cacheKey
        let directory = try directory ?? FileManager.default.url(for: .applicationSupportDirectory, in: .userDomainMask, appropriateFor: nil, create: true).appendingPathComponent("Earnest", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        db = try DatabaseQueue(path: directory.appendingPathComponent("cache.sqlite").path)
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
}

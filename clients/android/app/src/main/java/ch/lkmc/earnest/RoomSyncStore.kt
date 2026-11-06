package ch.lkmc.earnest

import ch.lkmc.earnest.core.Revision
import ch.lkmc.earnest.core.SyncSnapshot
import ch.lkmc.earnest.core.SyncStore
import ch.lkmc.earnest.db.AppDatabase
import ch.lkmc.earnest.db.SyncSnapshotEntity
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.Json

/**
 * Persists the whole snapshot as one JSON document. [save] only records the
 * latest snapshot; [flush] writes it in one Room transaction, so a change page
 * is durable and atomic.
 */
class RoomSyncStore(private val db: AppDatabase) : SyncStore {
    private val json = Json { ignoreUnknownKeys = true }
    private val mutex = Mutex()
    private var pending: SyncSnapshot? = null

    override fun load(): SyncSnapshot {
        val row = runBlocking { db.snapshot().load() } ?: return SyncSnapshot(Revision("", 0))
        return json.decodeFromString(SyncSnapshot.serializer(), row.json)
    }

    /** The snapshot row's write counter; every flush increments it. */
    override fun version(): Long = db.snapshot().version() ?: 0

    override fun save(snapshot: SyncSnapshot) {
        pending = snapshot
    }

    override fun flush() = runBlocking {
        mutex.withLock {
            pending?.let { snapshot ->
                val encoded = json.encodeToString(SyncSnapshot.serializer(), snapshot)
                // A single-statement bump keeps the counter monotonic even if two
                // stores flush concurrently; seed the row on the first write.
                if (db.snapshot().rewrite(encoded) == 0) {
                    db.snapshot().save(SyncSnapshotEntity(json = encoded, version = 1))
                }
            }
            pending = null
        }
    }
}

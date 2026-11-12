package ch.lkmc.earnest

import android.content.Context
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import ch.lkmc.earnest.core.*
import ch.lkmc.earnest.db.AppDatabase
import ch.lkmc.earnest.db.SyncSnapshotEntity
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.SQLiteMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@SQLiteMode(SQLiteMode.Mode.NATIVE)
class RoomSyncStoreTest {
    @Test fun oversizedSnapshotSurvivesDatabaseReopen() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val name = "oversized-snapshot.db"
        fun open() = Room.databaseBuilder(context, AppDatabase::class.java, name).allowMainThreadQueries().build()
        val snapshot = oversizedSnapshot()
        var db = open()
        try {
            RoomSyncStore(db).apply { save(snapshot); flush() }
            db.close()
            db = open()
            assertEquals(snapshot, RoomSyncStore(db).load())
            assertEquals(1L, RoomSyncStore(db).version())
        } finally {
            db.close()
            context.deleteDatabase(name)
        }
    }

    @Test fun existingOversizedRowRestoresPendingActionsWithoutResettingVersion() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val db = Room.inMemoryDatabaseBuilder(context, AppDatabase::class.java).allowMainThreadQueries().build()
        try {
            val snapshot = oversizedSnapshot()
            val encoded = Json.encodeToString(SyncSnapshot.serializer(), snapshot)
            assertTrue("fixture must exceed Android's usual 2 MiB CursorWindow", encoded.toByteArray().size > 4 * 1024 * 1024)
            // Seed the exact single-row format written by older app versions.
            db.snapshot().save(SyncSnapshotEntity(json = encoded, version = 41))
            val store = RoomSyncStore(db)
            assertEquals(snapshot, store.load())
            assertEquals(41L, store.version())
            val next = snapshot.copy(cursor = Revision("g1", 43))
            store.save(next)
            store.flush()
            assertEquals(next, RoomSyncStore(db).load())
            assertEquals(42L, store.version())
            // A smaller replacement must not retain bytes from the old document.
            val smaller = snapshot.copy(entries = emptyList())
            store.save(smaller)
            store.flush()
            assertEquals(smaller, RoomSyncStore(db).load())
            assertEquals(43L, store.version())
        } finally {
            db.close()
        }
    }

    @Test fun snapshotChunksAndVersionAreReadInOneTransaction() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val transactions = mutableListOf<Boolean>()
        lateinit var db: AppDatabase
        db = Room.inMemoryDatabaseBuilder(context, AppDatabase::class.java)
            .allowMainThreadQueries()
            .setQueryCallback({ sql, _ ->
                if (sql.startsWith("SELECT") && sql.contains("sync_snapshot")) transactions += db.inTransaction()
            }, { it.run() })
            .build()
        try {
            val snapshot = oversizedSnapshot()
            RoomSyncStore(db).apply { save(snapshot); flush() }
            assertEquals(snapshot, RoomSyncStore(db).load())
            assertTrue("expected multiple chunk queries", transactions.size > 3)
            assertTrue("a snapshot query escaped the read transaction", transactions.all { it })
        } finally {
            db.close()
        }
    }

    private fun oversizedSnapshot(): SyncSnapshot = SyncSnapshot(
        cursor = Revision("g1", 42),
        entries = List(5000) { index ->
            CachedEntry(id = index + 1L, seq = index + 1L, title = "日本語 🐟 e\u0301 ".repeat(50))
        },
        objects = listOf(SyncObject(kind = River.SETTINGS, id = 1, rev = 42, raw = """{"today_decay_days":7}""")),
        positions = listOf(CachedPosition(principal = 1, view = "today", seq = 123, rev = 40)),
        outbox = listOf(Pending(write = StateWrite("offline-later", 1, 2, "shelf", JsonPrimitive("later"), Revision("g1", 0), "2026-11-03T00:00:00Z", "explicit"))),
    )

    @Test fun versionAdvancesOnlyWhenTheSnapshotRowIsRewritten() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val db = Room.inMemoryDatabaseBuilder(context, AppDatabase::class.java).allowMainThreadQueries().build()
        try {
            val store = RoomSyncStore(db)
            assertEquals(0, store.version())
            val snapshot = SyncSnapshot(Revision("g1", 0), outbox = listOf(Pending(write = StateWrite("op", 1, 2, "shelf", JsonPrimitive("later"), Revision("g1", 0), "2026-11-03T00:00:00Z", "explicit"))))
            store.save(snapshot)
            // save() only stages the document; the row is untouched until flush.
            assertEquals(0, store.version())
            store.flush()
            assertEquals(1, store.version())
            store.save(snapshot.copy(cursor = Revision("g1", 1)))
            assertEquals(1, store.version())
            store.flush()
            assertEquals(2, store.version())
        } finally {
            db.close()
        }
    }

    @Test fun failedReplacementPreservesCommittedOutbox() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val db = Room.inMemoryDatabaseBuilder(context, AppDatabase::class.java).allowMainThreadQueries().build()
        try {
            val store = RoomSyncStore(db)
            val pending = Pending(write = StateWrite("later", 1, 2, "shelf", JsonPrimitive("later"), Revision("g1", 0), "2026-11-03T00:00:00Z", "explicit"))
            val original = SyncSnapshot(Revision("g1", 0), outbox = listOf(pending))
            store.save(original)
            store.flush()
            db.openHelper.writableDatabase.execSQL("CREATE TRIGGER fail_snapshot BEFORE UPDATE ON sync_snapshot BEGIN SELECT RAISE(ABORT, 'simulated persistence failure'); END")
            store.save(original.copy(cursor = Revision("g1", 1)))
            try {
                store.flush()
                fail("replacement unexpectedly succeeded")
            } catch (_: android.database.sqlite.SQLiteException) {
                assertEquals(original, RoomSyncStore(db).load())
            }
        } finally {
            db.close()
        }
    }
}

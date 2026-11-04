package ch.lkmc.earnest

import android.content.Context
import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import ch.lkmc.earnest.core.*
import ch.lkmc.earnest.db.AppDatabase
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
    @Test fun failedReplacementPreservesCommittedOutbox() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val db = Room.inMemoryDatabaseBuilder(context, AppDatabase::class.java).allowMainThreadQueries().build()
        try {
            val store = RoomSyncStore(db)
            val pending = Pending(write = StateWrite("later", 1, 2, "shelf", JsonPrimitive("later"), Revision("g1", 0), "2026-11-03T00:00:00Z", "explicit"))
            val original = SyncSnapshot(Revision("g1", 0), outbox = listOf(pending))
            store.save(original)
            store.flush()
            db.openHelper.writableDatabase.execSQL("CREATE TRIGGER fail_snapshot BEFORE INSERT ON sync_snapshot BEGIN SELECT RAISE(ABORT, 'simulated persistence failure'); END")
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

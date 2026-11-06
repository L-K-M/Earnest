package ch.lkmc.earnest.db

import android.content.Context
import androidx.room.Database
import androidx.room.Room
import androidx.room.RoomDatabase
import androidx.room.migration.Migration
import androidx.sqlite.db.SupportSQLiteDatabase

@Database(
    entities = [SyncSnapshotEntity::class, ContentEntity::class, BriefEntity::class],
    version = 2,
    exportSchema = false,
)
abstract class AppDatabase : RoomDatabase() {
    abstract fun snapshot(): SyncSnapshotDao
    abstract fun content(): ContentDao
    abstract fun briefs(): BriefDao

    companion object {
        private val instances = mutableMapOf<String, AppDatabase>()

        /** Adds the snapshot write counter used as a cheap change token. */
        private val MIGRATION_1_2 = object : Migration(1, 2) {
            override fun migrate(db: SupportSQLiteDatabase) {
                db.execSQL("ALTER TABLE sync_snapshot ADD COLUMN version INTEGER NOT NULL DEFAULT 0")
            }
        }

        fun get(context: Context, accountKey: String): AppDatabase = synchronized(this) {
            require(accountKey.matches(Regex("[0-9a-f]{64}"))) { "invalid account cache key" }
            val name = "earnest-$accountKey.db"
            val path = context.getDatabasePath(name).absolutePath
            instances.getOrPut(path) {
                Room.databaseBuilder(context.applicationContext, AppDatabase::class.java, name)
                    .addMigrations(MIGRATION_1_2)
                    .build()
            }
        }
    }
}

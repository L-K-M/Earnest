package ch.lkmc.earnest.db

import android.content.Context
import androidx.room.Database
import androidx.room.Room
import androidx.room.RoomDatabase

@Database(
    entities = [SyncSnapshotEntity::class, ContentEntity::class, BriefEntity::class],
    version = 1,
    exportSchema = false,
)
abstract class AppDatabase : RoomDatabase() {
    abstract fun snapshot(): SyncSnapshotDao
    abstract fun content(): ContentDao
    abstract fun briefs(): BriefDao

    companion object {
        private val instances = mutableMapOf<String, AppDatabase>()

        fun get(context: Context, accountKey: String): AppDatabase = synchronized(this) {
            require(accountKey.matches(Regex("[0-9a-f]{64}"))) { "invalid account cache key" }
            val name = "earnest-$accountKey.db"
            val path = context.getDatabasePath(name).absolutePath
            instances.getOrPut(path) {
                Room.databaseBuilder(context.applicationContext, AppDatabase::class.java, name)
                    .build()
            }
        }
    }
}

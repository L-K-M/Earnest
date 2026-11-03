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
        @Volatile
        private var instance: AppDatabase? = null

        fun get(context: Context): AppDatabase = instance ?: synchronized(this) {
            instance ?: Room.databaseBuilder(context, AppDatabase::class.java, "earnest.db")
                .fallbackToDestructiveMigration()
                .build()
                .also { instance = it }
        }
    }
}

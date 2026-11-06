package ch.lkmc.earnest.db

import androidx.room.Dao
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query

@Dao
interface SyncSnapshotDao {
    @Query("SELECT * FROM sync_snapshot WHERE id = 1")
    fun load(): SyncSnapshotEntity?

    /** The snapshot's monotonic write counter; callers compare it before decoding the row. */
    @Query("SELECT version FROM sync_snapshot WHERE id = 1")
    fun version(): Long?

    /** Atomically rewrites the snapshot and bumps the write counter; returns rows affected. */
    @Query("UPDATE sync_snapshot SET json = :json, version = version + 1 WHERE id = 1")
    fun rewrite(json: String): Int

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    fun save(entity: SyncSnapshotEntity)
}

@Dao
interface ContentDao {
    @Query("SELECT * FROM contents WHERE entryId = :entryId")
    suspend fun get(entryId: Long): ContentEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun save(entity: ContentEntity)

    @Query("DELETE FROM contents WHERE entryId = :entryId")
    suspend fun delete(entryId: Long)
}

@Dao
interface BriefDao {
    @Query("SELECT * FROM briefs ORDER BY rev DESC")
    suspend fun get(): List<BriefEntity>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun saveAll(briefs: List<BriefEntity>)

    @Query("DELETE FROM briefs")
    suspend fun clear()
}

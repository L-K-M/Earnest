package ch.lkmc.earnest.db

import androidx.room.Dao
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

@Dao
interface SyncSnapshotDao {
    @Query("SELECT * FROM sync_snapshot WHERE id = 1")
    fun load(): SyncSnapshotEntity?

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    fun save(entity: SyncSnapshotEntity)

    @Query("DELETE FROM sync_snapshot")
    fun clear()
}

@Dao
interface ContentDao {
    @Query("SELECT * FROM contents WHERE entryId = :entryId")
    fun entry(entryId: Long): Flow<ContentEntity?>

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
    fun all(): Flow<List<BriefEntity>>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun saveAll(briefs: List<BriefEntity>)

    @Query("DELETE FROM briefs")
    suspend fun clear()
}

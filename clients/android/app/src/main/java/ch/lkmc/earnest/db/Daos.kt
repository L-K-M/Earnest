package ch.lkmc.earnest.db

import androidx.room.Dao
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.Query
import androidx.room.Transaction

private const val SNAPSHOT_READ_CHUNK_BYTES = 256 * 1024

@Dao
abstract class SyncSnapshotDao {
    /**
     * Keep each cursor result below Android's CursorWindow limit, including for
     * oversized rows written by older versions. One transaction pins the row
     * across all reads so a concurrent flush cannot mix snapshot revisions.
     */
    @Transaction
    open fun load(): SyncSnapshotEntity? {
        val version = version() ?: return null
        val encoded = ByteArray(checkNotNull(byteCount()))
        var offset = 0
        while (offset < encoded.size) {
            val length = minOf(SNAPSHOT_READ_CHUNK_BYTES, encoded.size - offset)
            val chunk = checkNotNull(readChunk(offset + 1, length))
            check(chunk.size == length) { "Incomplete sync snapshot read" }
            chunk.copyInto(encoded, offset)
            offset += length
        }
        // Decode only after reassembly: a byte boundary can split UTF-8 text.
        return SyncSnapshotEntity(json = encoded.decodeToString(throwOnInvalidSequence = true), version = version)
    }

    @Query("SELECT length(CAST(json AS BLOB)) FROM sync_snapshot WHERE id = 1")
    protected abstract fun byteCount(): Int?

    @Query("SELECT substr(CAST(json AS BLOB), :start, :length) FROM sync_snapshot WHERE id = 1")
    protected abstract fun readChunk(start: Int, length: Int): ByteArray?

    /** The snapshot's monotonic write counter; callers compare it before decoding the row. */
    @Query("SELECT version FROM sync_snapshot WHERE id = 1")
    abstract fun version(): Long?

    /** Atomically rewrites the snapshot and bumps the write counter; returns rows affected. */
    @Query("UPDATE sync_snapshot SET json = :json, version = version + 1 WHERE id = 1")
    abstract fun rewrite(json: String): Int

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    abstract fun save(entity: SyncSnapshotEntity)
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

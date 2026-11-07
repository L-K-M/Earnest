package ch.lkmc.earnest.db

import androidx.room.ColumnInfo
import androidx.room.Entity
import androidx.room.PrimaryKey

/** The whole sync snapshot, persisted as one JSON document per user database. */
@Entity(tableName = "sync_snapshot")
data class SyncSnapshotEntity(
    @PrimaryKey val id: Int = 1,
    val json: String,
    /** Monotonic write counter, bumped by the writer on every flush; a change token for readers. */
    val version: Long = 0,
)

/** Cached reader content, written when a view opens an entry. */
@Entity(tableName = "contents")
data class ContentEntity(
    @PrimaryKey val entryId: Long,
    val html: String,
    val text: String,
    @ColumnInfo(name = "sanitizer_version") val sanitizerVersion: Long,
    val source: String,
    @ColumnInfo(name = "updated_at") val updatedAt: Long,
)

/** Cached Today briefs, so the top of the river survives offline. */
@Entity(tableName = "briefs")
data class BriefEntity(
    @PrimaryKey val principalId: Long,
    val value: String,
    val rev: Long,
)

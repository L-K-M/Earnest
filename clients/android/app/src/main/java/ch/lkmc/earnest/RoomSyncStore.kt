package ch.lkmc.earnest

import ch.lkmc.earnest.core.Revision
import ch.lkmc.earnest.core.SyncSnapshot
import ch.lkmc.earnest.core.SyncStore
import kotlinx.serialization.json.Json

internal class RoomSyncStore(private val dao: SyncDao) : SyncStore {
    override fun load(): SyncSnapshot = dao.load()?.let { Json.decodeFromString(SyncSnapshot.serializer(), it.json) }
        ?: SyncSnapshot(Revision("", 0))

    override fun save(snapshot: SyncSnapshot) {
        val row = SyncRow()
        row.id = 1
        row.json = Json.encodeToString(SyncSnapshot.serializer(), snapshot)
        dao.save(row)
    }
}

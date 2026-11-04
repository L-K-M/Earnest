package ch.lkmc.earnest

import ch.lkmc.earnest.core.*
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.*
import org.junit.Test

class ReadingStateTest {
    private class Store(private var data: SyncSnapshot) : SyncStore {
        override fun load() = data
        override fun save(snapshot: SyncSnapshot) { data = snapshot }
    }
    private fun subscription(id: Long, cadence: String) = SyncObject("subscription", id, rev = 1,
        raw = """{"cadence":"$cadence","initial_since":"2026-01-01T00:00:00Z","initial_through_seq":0}""")
    private val entries = (1L..20L).map { CachedEntry(it, subscriptionId = 3, seq = it, firstSeenAt = java.time.Instant.now().toString()) }
    private fun state() = UiState(
        snapshot = SyncSnapshot(Revision("g1", 0), entries = entries, objects = listOf(subscription(3, "today"))),
        principalId = 1,
        cadences = mapOf(3L to "today"),
    )

    @Test fun passiveSeenDoesNotDrainTodayBeforeRefresh() {
        var state = state()
        val members = todayEntries(state).map { it.id }
        repeat(20) {
            val rendered = todayEntries(state).take(5)
            state = state.copy(states = state.states + rendered.associate { entry ->
                val initial = SyncEngine.initialState(1, entry.id)
                entry.id to initial.copy(seen = initial.seen.copy(value = JsonPrimitive("seen")))
            })
        }
        assertEquals(members, todayEntries(state).map { it.id })
        assertEquals(5, state.states.size)
    }

    @Test fun explicitLaterAndDoneRemoveSessionMembers() {
        val initial = SyncEngine.initialState(1, 1)
        for (shelf in listOf("later", "done")) {
            assertFalse(todayEntries(state().copy(states = mapOf(1L to initial.copy(shelf = initial.shelf.copy(value = JsonPrimitive(shelf)))))).any { it.id == 1L })
        }
    }

    @Test fun coldRestoreDerivesCadencesWithoutNetwork() {
        val objects = listOf(
            subscription(3, "today"),
            subscription(4, "week"),
        )
        val engine = SyncEngine(Store(SyncSnapshot(Revision("g1", 0), entries = entries + CachedEntry(21, subscriptionId = 4, firstSeenAt = java.time.Instant.now().toString()), objects = objects)))
        val restored = restoredState(engine, 1)
        assertEquals(mapOf(3L to "today", 4L to "week"), restored.cadences)
        assertEquals(20, todayEntries(restored).size)
        assertEquals(1, restored.snapshot.entries.count { restored.cadences[it.subscriptionId] == "week" })
    }
    @Test fun refreshStartsNewMembershipFromEffectiveState() {
        val subscription = subscription(3, "today")
        val engine = SyncEngine(Store(SyncSnapshot(Revision("g1", 0), entries = entries, objects = listOf(subscription))))
        val initial = restoredState(engine, 1)
        engine.queue(StateWrite("seen", 1, 1, "seen", JsonPrimitive("seen"), Revision("g1", 0), "2026-11-03T00:00:00Z", "passive"))
        val refreshed = restoredState(engine, 1)
        val currentSession = initial.copy(snapshot = engine.snapshot(), states = refreshed.states)
        assertTrue(todayEntries(currentSession).any { it.id == 1L })
        assertFalse(todayEntries(refreshed).any { it.id == 1L })
    }

    @Test fun editionKeepsPassiveSeenMembersAndDropsShelvedRows() {
        val snapshot = SyncSnapshot(Revision("g1", 0), entries = entries.map { it.copy(subscriptionId = 4) },
            objects = listOf(subscription(4, "week")))
        val initial = restoredState(SyncEngine(Store(snapshot)), 1)
        val state = SyncEngine.initialState(1, 1)
        val seen = initial.copy(states = mapOf(1L to state.copy(seen = state.seen.copy(value = JsonPrimitive("seen")))))
        assertEquals(20, editionEntries(seen).size)
        val later = seen.copy(states = mapOf(1L to state.copy(shelf = state.shelf.copy(value = JsonPrimitive("later")))))
        assertEquals(19, editionEntries(later).size)
        assertTrue(todayEntries(initial).isEmpty())
    }

}

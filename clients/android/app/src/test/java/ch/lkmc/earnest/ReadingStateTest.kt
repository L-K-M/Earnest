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

    @Test fun coldRestoreDerivesCadencesWithoutNetwork() {
        val objects = listOf(
            subscription(3, "today"),
            subscription(4, "week"),
        )
        val engine = SyncEngine(Store(SyncSnapshot(Revision("g1", 0), entries = entries + CachedEntry(21, subscriptionId = 4, firstSeenAt = java.time.Instant.now().toString()), objects = objects)))
        val restored = restoredState(engine, 1)
        assertEquals(mapOf(3L to "today", 4L to "week"), restored.cadences)
        assertEquals(21, restored.riverEntryIds.size)
        assertEquals(1, restored.snapshot.entries.count { restored.cadences[it.subscriptionId] == "week" })
    }

    @Test fun refreshStartsNewMembershipFromEffectiveState() {
        val subscription = subscription(3, "today")
        val engine = SyncEngine(Store(SyncSnapshot(Revision("g1", 0), entries = entries, objects = listOf(subscription))))
        val initial = restoredState(engine, 1)
        engine.queue(StateWrite("seen", 1, 1, "seen", JsonPrimitive("seen"), Revision("g1", 0), "2026-11-03T00:00:00Z", "passive"))
        val refreshed = restoredState(engine, 1)
        // The fresh membership excludes the seen row; the session that was
        // already showing it keeps it until an intentional reload.
        assertFalse(1L in refreshed.riverEntryIds)
        val currentSession = initial.copy(snapshot = engine.snapshot(), states = refreshed.states)
        assertTrue(1L in currentSession.riverEntryIds)
    }

    @Test fun unreadVisitRetainsReadRowsAcrossBackgroundReloads() {
        val initial = state().copy(readerSettings = ReaderSettings(showOnlyUnread = true))
        val seen = SyncEngine.initialState(1, 1).let { it.copy(seen = it.seen.copy(value = JsonPrimitive("seen"))) }
        val fresh = initial.copy(states = mapOf(1L to seen), riverEntryIds = initial.riverEntryIds - 1L)
        val background = fresh.copy(riverEntryIds = initial.riverMembersAfter(fresh))
        assertTrue(1L in background.riverEntryIds)
        assertFalse(initial.riverMembersAfter(fresh, intentionalVisit = initial.listVisit).contains(1L))
        // A refresh for an older list cannot prune the current visit.
        assertTrue(initial.copy(listVisit = 1).riverMembersAfter(fresh, intentionalVisit = 0).contains(1L))
        assertFalse(initial.riverMembersAfter(fresh.copy(snapshot = fresh.snapshot.copy(cursor = Revision("g2", 0)))).contains(1L))
    }

    @Test fun unreadVisitRetainsRowsMarkedDoneUntilTheNextVisit() {
        val initial = state().copy(readerSettings = ReaderSettings(showOnlyUnread = true))
        val done = SyncEngine.initialState(1, 1).let { it.copy(shelf = it.shelf.copy(value = JsonPrimitive("done"))) }
        val fresh = initial.copy(states = mapOf(1L to done), riverEntryIds = initial.riverEntryIds - 1L)
        val background = fresh.copy(riverEntryIds = initial.riverMembersAfter(fresh))
        assertTrue(1L in background.riverEntryIds)
    }

    @Test fun restoredSettingsIncludeUnreadVisibility() {
        val initial = state().snapshot
        val settings = SyncObject("settings", 0, rev = 1, raw = """{"show_only_unread":true}""")
        val restored = restoredState(SyncEngine(Store(initial.copy(objects = initial.objects + settings))), 1)
        assertTrue(restored.readerSettings.showOnlyUnread)
    }
}

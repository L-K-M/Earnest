package ch.lkmc.earnest.core

import kotlinx.serialization.decodeFromString
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.*
import org.junit.Test
import java.time.Instant

class SyncEngineTest {
    private class Store : SyncStore {
        var data = SyncSnapshot(Revision("g1", 0))
        override fun load() = data
        override fun save(snapshot: SyncSnapshot) { data = snapshot }
    }
    private fun register(value: kotlinx.serialization.json.JsonElement, rev: Long = 0) = Register(value, rev, "2026-01-01T00:00:00Z", 1, "deliberate")
    private fun state() = State(1, 2, register(JsonPrimitive("seen"), 1), register(JsonPrimitive("done"), 1), register(JsonPrimitive(false), 1), 1)
    private fun write(id: String, value: String) = StateWrite(id, 1, 2, "seen", JsonPrimitive(value), Revision("g1", 1), "2026-01-01T00:00:00Z", "explicit")

    @Test fun coalescingPreservesDoneClearAndOldBase() {
        val engine = SyncEngine(Store())
        engine.mergeBootstrap(SyncChange(1, entry = CachedEntry(2, 3, 1, 1), states = listOf(state())))
        engine.queue(write("one", "unseen"))
        engine.queue(write("two", "seen").copy(base = Revision("g1", 9)))
        assertEquals(listOf("one/clear-done", "two"), engine.snapshot().outbox.map { it.write.opId })
        assertEquals(1L, engine.snapshot().outbox.last().write.base.rev)
        assertEquals(JsonPrimitive("none"), engine.state(1, 2)!!.shelf.value)
    }

    @Test fun inFlightAckCannotEraseLaterIntent() {
        val engine = SyncEngine(Store())
        engine.queue(write("one", "seen"))
        engine.markInFlight("one")
        engine.queue(write("two", "unseen"))
        engine.acknowledge("g1", "one", emptyList())
        assertEquals(listOf("two"), engine.snapshot().outbox.map { it.write.opId })
        engine.beginBootstrap(Revision("g2", 5))
        assertEquals(Revision("g2", 0), engine.snapshot().outbox.single().write.base)
        try { engine.acknowledge("g1", "two", emptyList()); fail() } catch (_: IllegalArgumentException) { }
    }

    @Test fun interruptedBootstrapRemainsIncomplete() {
        val store = Store()
        SyncEngine(store).beginBootstrap(Revision("g1", 4))
        val restarted = SyncEngine(store)
        assertTrue(restarted.snapshot().bootstrapping)
        try { restarted.applyPage(emptyList(), Revision("g1", 4)); fail() } catch (_: IllegalArgumentException) { }
        restarted.finishBootstrap()
        assertFalse(store.data.bootstrapping)
    }

    @Test fun partialPageDoesNotCommit() {
        val engine = SyncEngine(Store())
        try { engine.applyPage(listOf(SyncChange(1)), Revision("g1", 2)); fail() } catch (_: IllegalArgumentException) { }
        assertEquals(0L, engine.snapshot().cursor.rev)
        engine.applyPage(listOf(SyncChange(1), SyncChange(2)), Revision("g1", 2))
        assertEquals(2L, engine.snapshot().cursor.rev)
    }

    @Test fun expiryIsStrictlyAfterThirtyDays() {
        val engine = SyncEngine(Store())
        engine.queue(write("one", "seen"))
        assertTrue(engine.expire(Instant.parse("2026-01-31T00:00:00Z")).isEmpty())
        assertEquals(1, engine.expire(Instant.parse("2026-01-31T00:00:01Z")).size)
    }

    @Test fun bootstrapMergesRegistersIndependently() {
        val engine = SyncEngine(Store())
        val first = state()
        engine.mergeBootstrap(SyncChange(1, entry = CachedEntry(2, 3, 1, 1), states = listOf(first)))
        val older = first.copy(seen = register(JsonPrimitive("unseen"), 2), shelf = register(JsonPrimitive("later"), 0), stateRev = 2)
        engine.mergeBootstrap(SyncChange(2, states = listOf(older)))
        assertEquals(JsonPrimitive("unseen"), engine.state(1, 2)!!.seen.value)
        assertEquals(JsonPrimitive("done"), engine.state(1, 2)!!.shelf.value)
    }

    @Test fun rangeOverlayUsesFixedBoundaryAndSubscription() {
        val engine = SyncEngine(Store())
        engine.mergeBootstrap(SyncChange(1, entry = CachedEntry(2, 3, 1, 1), states = listOf(state().copy(seen = register(JsonPrimitive("unseen"))))))
        engine.queueRange(write("range", "seen"), RangeScope("subscriptions", listOf(3)), 1)
        assertEquals(JsonPrimitive("seen"), engine.state(1, 2)!!.seen.value)
        assertEquals(1, engine.snapshot().outbox.size)
        assertEquals(RangeScope("subscriptions", listOf(3)), engine.snapshot().outbox.single().scope)
    }

    @Test fun openApiLongIdsAndRegisterValuesRoundTrip() {
        val json = """{"principal":1,"entry_id":9007199254740993,"seen":{"value":"unseen","rev":0,"occurred_at":"0001-01-01T00:00:00Z","actor":0,"source":""},"shelf":{"value":"none","rev":0,"occurred_at":"0001-01-01T00:00:00Z","actor":0,"source":""},"kept":{"value":false,"rev":0,"occurred_at":"0001-01-01T00:00:00Z","actor":0,"source":""},"state_rev":0}"""
        val state = Json.decodeFromString<State>(json)
        assertEquals(9007199254740993L, state.entryId)
        assertEquals(JsonPrimitive(false), state.kept.value)
    }
}

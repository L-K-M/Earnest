package ch.lkmc.earnest.core

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import java.time.Instant

class SyncEngineTest {
    private class Store(var data: SyncSnapshot = SyncSnapshot(Revision("g1", 0))) : SyncStore {
        override fun load(): SyncSnapshot = data
        override fun save(snapshot: SyncSnapshot) {
            data = snapshot
        }
    }

    private fun register(value: kotlinx.serialization.json.JsonElement, rev: Long = 0) =
        Register(value, rev, "2026-01-01T00:00:00Z", 1, "deliberate")

    private fun state() = State(
        1,
        2,
        register(JsonPrimitive("seen"), 1),
        register(JsonPrimitive("done"), 1),
        register(JsonPrimitive(false), 1),
        1,
    )

    private fun write(id: String, value: String) = StateWrite(
        id,
        1,
        2,
        "seen",
        JsonPrimitive(value),
        Revision("g1", 1),
        "2026-01-01T00:00:00Z",
        "explicit",
    )

    @Test
    fun bootstrapIgnoresStatesForUnknownAndDeletedEntries() {
        val engine = SyncEngine(Store())
        engine.mergeBootstrap(SyncChange(1, states = listOf(state())))
        assertTrue("unknown entry retained bootstrap state", engine.snapshot().states.isEmpty())
        engine.mergeBootstrap(SyncChange(2, entry = CachedEntry(2, deleted = true), states = listOf(state())))
        assertTrue("deleted entry retained bootstrap state", engine.snapshot().states.isEmpty())
    }

    @Test
    fun goneAcknowledgementRemovesAllPrincipalsStates() {
        val engine = SyncEngine(Store())
        engine.mergeBootstrap(SyncChange(1, entry = CachedEntry(2), states = listOf(state(), state().copy(principal = 9))))
        engine.queue(write("gone", "seen"))
        engine.acknowledge("g1", listOf(Result("gone", "gone", state(), false)))
        assertTrue(engine.entry(2)!!.deleted)
        assertTrue("gone entry retained state", engine.snapshot().states.isEmpty())
        assertEquals(JsonPrimitive("unseen"), engine.state(1, 2).seen.value)
    }

    @Test
    fun coalescingPreservesDoneClearAndOldBase() {
        val engine = SyncEngine(Store())
        engine.mergeBootstrap(SyncChange(1, entry = CachedEntry(2, 3, 1, 1), states = listOf(state())))
        engine.queue(write("one", "unseen"))
        engine.queue(write("two", "seen").copy(base = Revision("g1", 9)))
        assertEquals(listOf("one/clear-done", "two"), engine.snapshot().outbox.map { it.operation.opId })
        assertEquals(1L, engine.snapshot().outbox.last().operation.base.rev)
        assertEquals(JsonPrimitive("none"), engine.state(1, 2).shelf.value)
    }

    @Test
    fun inFlightAckCannotEraseLaterIntent() {
        val engine = SyncEngine(Store())
        engine.queue(write("one", "seen"))
        engine.markInFlight("one")
        engine.queue(write("two", "unseen"))
        engine.acknowledge("g1", listOf(Result("one", "applied", engine.state(1, 2), false)))
        assertEquals(listOf("two"), engine.snapshot().outbox.map { it.operation.opId })
        engine.beginBootstrap(Revision("g2", 5))
        assertEquals(Revision("g2", 0), engine.snapshot().outbox.single().operation.base)
        try {
            engine.acknowledge("g1", emptyList())
            fail("stale generation acknowledged")
        } catch (_: IllegalArgumentException) {
        }
    }

    @Test
    fun inFlightItemIsNotReplacedByCoalescing() {
        val engine = SyncEngine(Store())
        engine.queue(write("one", "seen"))
        engine.markInFlight("one")
        engine.queue(write("two", "unseen"))
        assertEquals(listOf("one", "two"), engine.snapshot().outbox.map { it.operation.opId })
    }

    @Test
    fun partialPageDoesNotCommit() {
        val engine = SyncEngine(Store())
        try {
            engine.applyPage(listOf(SyncChange(1)), Revision("g1", 2))
            fail("partial page accepted")
        } catch (_: IllegalArgumentException) {
        }
        assertEquals(0L, engine.snapshot().cursor.rev)
        engine.applyPage(listOf(SyncChange(1), SyncChange(2)), Revision("g1", 2))
        assertEquals(2L, engine.snapshot().cursor.rev)
    }

    @Test
    fun replayingAPageIsSafe() {
        val engine = SyncEngine(Store())
        engine.applyPage(listOf(SyncChange(1), SyncChange(2)), Revision("g1", 2))
        engine.applyPage(listOf(SyncChange(1), SyncChange(2)), Revision("g1", 2))
        assertEquals(2L, engine.snapshot().cursor.rev)
    }

    @Test
    fun expiryIsStrictlyAfterThirtyDays() {
        val engine = SyncEngine(Store())
        engine.queue(write("one", "seen"))
        assertTrue(engine.expire(Instant.parse("2026-01-31T00:00:00Z")).isEmpty())
        assertEquals(1, engine.expire(Instant.parse("2026-01-31T00:00:01Z")).size)
    }

    @Test
    fun bootstrapMergesRegistersIndependently() {
        val engine = SyncEngine(Store())
        val first = state()
        engine.mergeBootstrap(SyncChange(1, entry = CachedEntry(2, 3, 1, 1), states = listOf(first)))
        val older = first.copy(
            seen = register(JsonPrimitive("unseen"), 2),
            shelf = register(JsonPrimitive("later"), 0),
            stateRev = 2,
        )
        engine.mergeBootstrap(SyncChange(2, states = listOf(older)))
        assertEquals(JsonPrimitive("unseen"), engine.state(1, 2).seen.value)
        assertEquals(JsonPrimitive("done"), engine.state(1, 2).shelf.value)
    }

    @Test
    fun rangeOverlayUsesFixedBoundaryAndSubscription() {
        val engine = SyncEngine(Store())
        engine.mergeBootstrap(
            SyncChange(1, entry = CachedEntry(2, 3, 1, 1), states = listOf(state().copy(seen = register(JsonPrimitive("unseen"))))),
        )
        engine.queueRange(
            RangeWrite(write("range", "seen"), Scope("subscriptions", listOf(3)), 1),
        )
        assertEquals(JsonPrimitive("seen"), engine.state(1, 2).seen.value)
        assertEquals(1, engine.snapshot().outbox.size)
        assertEquals(Scope("subscriptions", listOf(3)), engine.snapshot().outbox.single().range?.scope)
    }

    @Test
    fun resourceObjectsMergeByRevisionAndKeepTombstones() {
        val engine = SyncEngine(Store())
        engine.mergeObject(SyncObject("subscription", 1, folderId = 9, name = "first", rev = 2))
        engine.mergeObject(SyncObject("subscription", 1, folderId = 9, name = "older", rev = 1))
        engine.mergeObject(SyncObject("subscription", 1, deleted = true, rev = 3))
        assertEquals("", engine.findObject("subscription", 1)?.name)
        assertTrue(engine.findObject("subscription", 1)!!.deleted)
        engine.mergeObject(SyncObject("subscription", 1, deleted = true, rev = 2))
        assertEquals(3L, engine.findObject("subscription", 1)?.rev)
    }

    @Test
    fun positionsMergeByRevision() {
        val engine = SyncEngine(Store())
        engine.mergePosition(CachedPosition(1, "today", 5, 2))
        engine.mergePosition(CachedPosition(1, "today", 3, 3))
        engine.mergePosition(CachedPosition(1, "today", 9, 1))
        assertEquals(3L, engine.position(1, "today")?.seq)
        assertEquals(3L, engine.position(1, "today")?.rev)
    }

    @Test
    fun stateForDeletedEntryIgnoresOverlay() {
        val engine = SyncEngine(Store())
        engine.mergeBootstrap(
            SyncChange(1, entry = CachedEntry(2, 3, 1, 1), states = listOf(state().copy(seen = register(JsonPrimitive("unseen"))))),
        )
        engine.queue(write("one", "seen"))
        assertEquals(JsonPrimitive("seen"), engine.state(1, 2).seen.value)
        engine.mergeEntry(CachedEntry(2, 3, 1, 2, rev = 2, deleted = true))
        assertEquals(JsonPrimitive("unseen"), engine.state(1, 2).seen.value)
    }

    @Test
    fun acknowledgeRangeRemovesExactOperation() {
        val engine = SyncEngine(Store())
        engine.mergeBootstrap(SyncChange(1, entry = CachedEntry(2, 3, 1, 1)))
        engine.queueRange(RangeWrite(write("range", "seen"), Scope("all"), 1))
        engine.queue(write("later", "seen").copy(register = "shelf", value = JsonPrimitive("later")))
        val result = RangeResult("applied", 1, listOf(3), emptyList(), emptyList(), false)
        engine.acknowledgeRange("range", "g1", result)
        assertEquals(listOf("later"), engine.snapshot().outbox.map { it.operation.opId })
    }

    @Test
    fun rangeAcknowledgementTombstonesGone() {
        val engine = SyncEngine(Store())
        engine.mergeBootstrap(SyncChange(1, entry = CachedEntry(2, 3, 1, 1), states = listOf(state(), state().copy(principal = 9))))
        engine.mergeEntry(CachedEntry(3, 3, 2, 1))
        engine.queueRange(RangeWrite(write("range", "seen"), Scope("all"), 2))
        val result = RangeResult("applied", 2, listOf(3), emptyList(), listOf(
            Result("range", "gone", state(), false),
            Result("range", "applied", state().copy(entryId = 3), false),
            Result("range", "gone", state().copy(entryId = 4), false),
        ), false)
        try {
            engine.acknowledgeRange("range", "old", result)
            fail("stale response accepted")
        } catch (_: SyncException) {
            assertFalse(engine.entry(2)!!.deleted)
        }
        engine.acknowledgeRange("range", "g1", result)
        engine.mergeEntry(CachedEntry(2, 3, 1, 10, rev = 10))
        assertTrue(engine.entry(2)!!.deleted)
        assertFalse(engine.entry(3)!!.deleted)
        assertNull(engine.entry(4))
        assertEquals(listOf(3L), engine.snapshot().states.map { it.entryId })
        assertTrue(engine.snapshot().outbox.isEmpty())
    }

    @Test
    fun longIdentifiersSurviveSerialization() {
        val json = Json
        val snapshot = SyncSnapshot(
            cursor = Revision("g1", 1),
            entries = listOf(CachedEntry(9007199254740993, 9223372036854775807, 1, 1)),
        )
        val encoded = json.encodeToString(SyncSnapshot.serializer(), snapshot)
        val decoded = json.decodeFromString(SyncSnapshot.serializer(), encoded)
        assertEquals(9007199254740993L, decoded.entries.single().id)
        assertEquals(9223372036854775807L, decoded.entries.single().subscriptionId)
    }

    @Test
    fun invalidQueueValuesAreRejected() {
        val engine = SyncEngine(Store())
        listOf(
            write("ok", "seen").copy(register = "bogus"),
            write("ok", "seen").copy(value = JsonPrimitive("bogus")),
            write("ok", "seen").copy(opId = "x/clear-done"),
            write("ok", "seen").copy(base = Revision("g2", 0)),
            write("ok", "seen").copy(kind = "rule"),
        ).forEach { candidate ->
            try {
                engine.queue(candidate)
                fail("accepted $candidate")
            } catch (_: IllegalArgumentException) {
            }
        }
        assertNull(engine.snapshot().outbox.firstOrNull())
    }
}

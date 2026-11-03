package ch.lkmc.earnest.core

import kotlinx.serialization.json.*
import org.junit.Assert.*
import org.junit.Test
import java.io.File

/** Exercises local overlays before flush; this is not the complete shared-vector runner. */
class VectorSmokeTest {
    private class Store : SyncStore {
        var value = SyncSnapshot(Revision("g1", 1))
        override fun load() = value
        override fun save(snapshot: SyncSnapshot) { value = snapshot }
    }

    @Test fun bootstrapRangeRaceFixture() {
        val fixture = Json.parseToJsonElement(File("../../../testdata/vectors/13-bootstrap-range-race.json").readText()).jsonObject
        fun reg(value: JsonElement, rev: Long = 0) = Register(value, rev, "2026-11-02T12:00:00Z", 1, "deliberate")
        fun state(id: Long) = State(1, id, reg(JsonPrimitive("seen"), 3), reg(JsonPrimitive("none")), reg(JsonPrimitive(false)), 3)
        val devices = listOf(SyncEngine(Store()), SyncEngine(Store()))
        devices.forEach { engine ->
            engine.beginBootstrap(Revision("g1", 2))
            for (id in 1L..2L) engine.mergeBootstrap(SyncChange(id, entry = CachedEntry(id, 1, id, id), states = listOf(state(id))))
            engine.finishBootstrap()
            engine.applyPage(listOf(SyncChange(3, states = listOf(state(1), state(2)))), Revision("g1", 3))
        }
        for (element in fixture.getValue("final").jsonArray) {
            val step = element.jsonObject
            if (!step.containsKey("device")) continue // Server state is supplied by this simulation.
            val engine = if (step.getValue("device").jsonPrimitive.content == "online") devices[0] else devices[1]
            val actual = engine.state(1, step.getValue("entry_id").jsonPrimitive.long)!!
            val expected = step.getValue("expect").jsonObject.getValue("state").jsonObject
            assertEquals(expected.getValue("seen").jsonObject.getValue("value"), actual.seen.value)
            assertEquals(expected.getValue("shelf").jsonObject.getValue("value"), actual.shelf.value)
            assertEquals(expected.getValue("kept").jsonObject.getValue("value"), actual.kept.value)
        }
    }

    @Test fun clientOverlayFixtures() {
        val directory = File("../../../testdata/vectors")
        for (name in listOf("27-client-coalescing.json", "32-coalesced-done-clear.json")) {
            val fixture = Json.parseToJsonElement(File(directory, name).readText()).jsonObject
            val engine = SyncEngine(Store())
            fun reg(value: JsonElement, rev: Long = 0) = Register(value, rev, "2026-11-02T12:00:00Z", 1, "deliberate")
            val shelf = if (name.startsWith("32")) "done" else "none"
            val state = State(1, 1, reg(JsonPrimitive("unseen")), reg(JsonPrimitive(shelf), if (shelf == "done") 2 else 0), reg(JsonPrimitive(false)), if (shelf == "done") 2 else 0)
            val entry = CachedEntry(1, 1, 1, 1)
            engine.mergeBootstrap(SyncChange(1, entry = entry, states = listOf(state)))
            for (element in fixture.getValue("operations").jsonArray) {
                val step = element.jsonObject
                when (step.getValue("op").jsonPrimitive.content) {
                    "client_flush" -> break // Server conflict resolution is not implemented in this smoke test.
                    "client_queue" -> {
                        val w = step.getValue("write").jsonObject
                        engine.queue(StateWrite(w.getValue("op_id").jsonPrimitive.content, 1, w.getValue("entry_id").jsonPrimitive.long, w.getValue("register").jsonPrimitive.content, w.getValue("value"), Revision("g1", 0), "2026-11-02T12:00:00Z", "explicit"))
                    }
                    "assert_client" -> assertEquals(step.getValue("expect").jsonObject.getValue("outbox_size").jsonPrimitive.int, engine.snapshot().outbox.size)
                    "assert_state" -> {
                        val actual = engine.state(1, 1)!!
                        val expected = step.getValue("expect").jsonObject.getValue("state").jsonObject
                        for ((key, register) in listOf("seen" to actual.seen, "shelf" to actual.shelf, "kept" to actual.kept)) {
                            expected[key]?.let { assertEquals(it.jsonObject.getValue("value"), register.value) }
                        }
                    }
                    "bootstrap_begin" -> engine.beginBootstrap(Revision("g1", 1))
                    "bootstrap_page" -> engine.mergeBootstrap(SyncChange(1, entry = entry, states = listOf(state)))
                    "client_pull" -> Unit // No server changes were produced in these steps.
                    else -> error("Unsupported client step in $name")
                }
            }
        }
    }
}

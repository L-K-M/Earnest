package ch.lkmc.earnest

import ch.lkmc.earnest.core.*
import kotlinx.serialization.json.*
import org.junit.Assert.assertEquals
import org.junit.Test
import java.io.File

class RiverFixtureTest {
    @Test fun sharedRiverFixtures() {
        val directory = generateSequence(File(System.getProperty("user.dir"))) { it.parentFile }
            .map { File(it, "testdata/river") }.first { it.isDirectory }
        val files = directory.listFiles()!!.filter { it.extension == "json" }.sortedBy { it.name }
        check(files.isNotEmpty())
        for (file in files) {
            val fixture = Json.parseToJsonElement(file.readText()).jsonObject
            val entries = fixture.getValue("entries").jsonArray.map {
                Json { ignoreUnknownKeys = true }.decodeFromJsonElement(CachedEntry.serializer(), it)
            }
            val objects = fixture.getValue("subscriptions").jsonArray.map {
                SyncObject("subscription", it.jsonObject.getValue("id").jsonPrimitive.long, rev = 1,
                    deleted = it.jsonObject["deleted"]?.jsonPrimitive?.booleanOrNull ?: false, raw = it.toString())
            }
            val states = fixture.getValue("entries").jsonArray.associate {
                val item = it.jsonObject
                val id = item.getValue("id").jsonPrimitive.long
                val state = SyncEngine.initialState(1, id)
                id to state.copy(seen = state.seen.copy(value = item.getValue("seen")), shelf = state.shelf.copy(value = item.getValue("shelf")))
            }
            val mutes = fixture.getValue("mutes").jsonArray.map {
                val item = it.jsonObject
                SyncObject(River.MUTE, item.getValue("id").jsonPrimitive.long, rev = 1,
                    deleted = item["deleted"]?.jsonPrimitive?.booleanOrNull ?: false, raw = it.toString())
            }
            val settings = SyncObject(River.SETTINGS, 1, rev = 1,
                raw = """{"today_decay_days":${fixture["today_decay_days"] ?: JsonPrimitive(2)}}""")
            val snapshot = SyncSnapshot(Revision("g", 0), entries = entries, objects = objects + mutes + settings)
            val cadences = objects.associate { it.id to Json.parseToJsonElement(it.raw).jsonObject.getValue("cadence").jsonPrimitive.content }
            val now = java.time.Instant.parse(fixture.getValue("now").jsonPrimitive.content)
            val members = initialRiverIds(snapshot, states, now)
            val actual = members.filter { id -> cadences[entries.first { it.id == id }.subscriptionId] != "week" }.toSet()
            val expected = fixture.getValue("expected_ids").jsonArray.map { it.jsonPrimitive.long }
                .filter { id -> cadences[entries.first { it.id == id }.subscriptionId] != "week" }.toSet()
            assertEquals(file.name, expected, actual)
            val expectedRiver = fixture.getValue("expected_ids").jsonArray.map { it.jsonPrimitive.long }.toSet()
            assertEquals(file.name, expectedRiver, members)
        }
    }
}

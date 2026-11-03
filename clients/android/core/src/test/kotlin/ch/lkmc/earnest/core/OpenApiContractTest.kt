package ch.lkmc.earnest.core

import kotlinx.serialization.decodeFromString
import kotlinx.serialization.json.*
import org.junit.Assert.*
import org.junit.Test
import java.io.File

class OpenApiContractTest {
    @Test fun apiRequiresHttpsOrigin() {
        for (url in listOf("http://example.test", "https://user:pass@example.test", "https://example.test/path", "https://example.test/?token=x", "https://example.test/#fragment")) {
            try { EarnestApi(url); fail("Accepted $url") } catch (_: IllegalArgumentException) { }
        }
        EarnestApi("https://example.test:8443")
    }

    @Test fun changesPageSupportsNullChangesAndLongRevisions() {
        val page = Json.decodeFromString<ChangesPage>("""{"changes":null,"next":{"generation":"g1","rev":9007199254740993},"next_cursor":"g1:9007199254740993"}""")
        assertNull(page.changes)
        assertEquals(9007199254740993L, page.next.rev)
        assertEquals("g1:9007199254740993", page.nextCursor)
    }

    @Test fun stateWriteMatchesDocumentedRequiredFields() {
        val schema = Json.parseToJsonElement(File("../../../api/openapi.json").readText()).jsonObject
            .getValue("components").jsonObject.getValue("schemas").jsonObject.getValue("StateWrite").jsonObject
        val write = StateWrite("op", 1, 9007199254740993L, "kept", JsonPrimitive(true), Revision("g", 1), "2026-11-02T12:00:00Z", "explicit")
        val body = Json.encodeToJsonElement(write).jsonObject
        val required = schema.getValue("required").jsonArray.map { it.jsonPrimitive.content }.toSet()
        val allowed = schema.getValue("properties").jsonObject.keys
        assertTrue(body.keys.containsAll(required))
        assertTrue(allowed.containsAll(body.keys))
        assertEquals(9007199254740993L, body.getValue("entry_id").jsonPrimitive.long)
        assertEquals(JsonPrimitive(true), body.getValue("value"))
    }
}

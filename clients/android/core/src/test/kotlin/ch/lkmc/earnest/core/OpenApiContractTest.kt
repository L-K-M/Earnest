package ch.lkmc.earnest.core

import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerializationStrategy
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.int
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.long
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Checks every hand-written wire model against its schema in
 * ../../api/openapi.json. A server-side rename, a type change or a new field
 * fails here before the app ships against it.
 */
class OpenApiContractTest {
    private val json = Json { encodeDefaults = true }

    private fun <T> encode(serializer: SerializationStrategy<T>, value: T): JsonElement =
        json.parseToJsonElement(json.encodeToString(serializer, value))

    private val models: List<Pair<String, JsonElement>> = listOf(
        "Revision" to encode(Revision.serializer(), Revision("g1", 9007199254740993)),
        "Register" to encode(Register.serializer(), Register(JsonPrimitive("seen"), 2, "2026-11-02T12:00:00Z", 1, "deliberate")),
        "State" to encode(State.serializer(), state()),
        "StateWrite" to encode(StateWrite.serializer(), write()),
        "Result" to encode(Result.serializer(), Result("op", "applied", state(), false)),
        "RangeResult" to encode(
            RangeResult.serializer(),
            RangeResult("applied", 3, listOf(1), listOf(2), listOf(Result("op", "superseded", state(), false)), false),
        ),
        "ChangeRecord" to encode(ChangeRecord.serializer(), ChangeRecord(1, "entry.created", JsonNull)),
        "ChangesPage" to encode(ChangesPage.serializer(), ChangesPage(null, Revision("g1", 2), "g1:2")),
        "BootstrapPage" to encode(BootstrapPage.serializer(), BootstrapPage(emptyList(), 1, 2, "view", true)),
        "Scope" to encode(Scope.serializer(), Scope("subscriptions", listOf(1, 2))),
        "ThroughBody" to encode(ThroughBody.serializer(), ThroughBody(write(), Scope("all"), 4, "2026-11-02T12:00:00Z")),
        "Entry" to encode(Entry.serializer(), entry()),
        "EntriesPage" to encode(EntriesPage.serializer(), EntriesPage(listOf(entry()), "42", 42)),
        "FeedHealth" to encode(
            FeedHealth.serializer(),
            FeedHealth("2026-11-01T00:00:00Z", "2026-11-02T00:00:00Z", "2026-11-03T00:00:00Z", "dns", "boom", 3, 500, 12.5, true),
        ),
        "Subscription" to encode(Subscription.serializer(), subscription()),
        "CredentialsBody" to encode(CredentialsBody.serializer(), CredentialsBody("user", "pass", mapOf("X-Test" to "1"))),
        "SubscriptionBody" to encode(SubscriptionBody.serializer(), SubscriptionBody("https://example.test/feed", "op", "Title", 2, "today", "always", CredentialsBody("user", "pass", emptyMap()))),
        "SubscriptionPatch" to encode(SubscriptionPatch.serializer(), SubscriptionPatch("op", "Title", 2, "week", "never", CredentialsBody("user", "pass", emptyMap()), true)),
        "Folder" to encode(Folder.serializer(), Folder(1, "Folder", 2, false)),
        "Label" to encode(Label.serializer(), Label(1, "work", 2, false)),
        "LabelMembership" to encode(LabelMembership.serializer(), LabelMembership(1, 2, 3, 4, false)),
        "Position" to encode(Position.serializer(), Position(1, "today", 2, 3)),
        "Put-positionRequest" to encode(PutPosition.serializer(), PutPosition(2, "op")),
        "NameBody" to encode(NameBody.serializer(), NameBody("name", "op")),
        "OpBody" to encode(OpBody.serializer(), OpBody("op")),
        "Annotation" to encode(Annotation.serializer(), Annotation(1, 2, "Morning Agent", "2026-11-03T06:00:00Z", "note", listOf(3), "text", 4, true, false)),
        "AnnotationBody" to encode(AnnotationBody.serializer(), AnnotationBody("op", "text")),
        "MeResult" to encode(MeResult.serializer(), MeResult(1, 2, "human", listOf("entries:read"))),
        "Token" to encode(
            Token.serializer(),
            Token(
                1,
                2,
                "Morning Agent",
                "agent",
                "api",
                listOf("entries:read"),
                3,
                "2026-12-02T12:00:00Z",
                "2026-11-03T12:00:00Z",
                "2026-11-04T12:00:00Z",
                "2026-11-02T12:00:00Z",
            ),
        ),
        "DeviceStart" to encode(DeviceStart.serializer(), DeviceStart("Earnest Android")),
        "DeviceResult" to encode(DeviceResult.serializer(), DeviceResult("device", "AB-CD", "https://example.test/device", 5, 600)),
        "DevicePoll" to encode(DevicePoll.serializer(), DevicePoll("device")),
        "DeviceToken" to encode(DeviceToken.serializer(), DeviceToken("token", "Bearer")),
        "DeviceBody" to encode(DeviceBody.serializer(), DeviceBody("unifiedpush", "https://push.test/endpoint", "key", "secret", false, "op")),
        "Device" to encode(Device.serializer(), Device(1, "unifiedpush", "https://push.test/endpoint", false, 2, "2026-11-02T12:00:00Z")),
        "Content" to encode(Content.serializer(), Content(1, "<p>html</p>", "text", 2, "stored")),
        "Activity" to encode(Activity.serializer(), activity()),
        "ActivityGroup" to encode(ActivityGroup.serializer(), ActivityGroup(1, "Morning Agent", listOf(activity()))),
        "ActivityPage" to encode(ActivityPage.serializer(), ActivityPage(listOf(activity()), listOf(ActivityGroup(1, "Morning Agent", null)))),
        "NotificationBudget" to encode(NotificationBudget.serializer(), NotificationBudget(1, 3)),
        "NotificationWhy" to encode(NotificationWhy.serializer(), NotificationWhy("Morning Agent", NotificationBudget(1, 3), 4)),
        "Notification" to encode(Notification.serializer(), notification()),
        "Problem" to encode(Problem.serializer(), Problem("about:blank", "Invalid", 400, "invalid", "detail", null)),
        "Empty" to encode(Empty.serializer(), Empty()),
        "SearchFilter" to encode(SearchFilter.serializer(), SearchFilter(SearchMode.HYBRID, 9007199254740993, 2, 3)),
        "SearchMatch" to encode(SearchMatch.serializer(), searchMatch()),
        "SearchPage" to encode(SearchPage.serializer(), SearchPage(listOf(searchMatch()), "cursor", "enabled")),
        "SavedSearch" to encode(
            SavedSearch.serializer(),
            SavedSearch(9007199254740993, "Stored", "query", SearchFilter(SearchMode.HYBRID, 2, 3, 4)),
        ),
        "SavedSearchBody" to encode(
            SavedSearchBody.serializer(),
            SavedSearchBody("Stored", "query", SearchFilter(SearchMode.FULLTEXT, 2, 3, 4)),
        ),
        "SavedSearchRename" to encode(SavedSearchRename.serializer(), SavedSearchRename("Renamed")),
        "ReaderSettings" to encode(
            ReaderSettings.serializer(),
            ReaderSettings(
                ReadingMode.UNREAD_COUNTS,
                7,
                false,
                ReaderTheme.BLACK,
                72,
                ReaderFont.DYSLEXIC,
                mapOf("42" to ReaderOverride(ReaderTheme.EINK, 64, ReaderFont.SANS)),
                9007199254740993,
            ),
        ),
        "ReaderOverride" to encode(ReaderOverride.serializer(), ReaderOverride(ReaderTheme.EINK, 64, ReaderFont.SANS)),
        "UnreadCounts" to encode(
            UnreadCounts.serializer(),
            UnreadCounts(mapOf(3L to 9007199254740993, Long.MAX_VALUE to 4L), 2, 3, 9007199254740997),
        ),
        "SettingsBody" to encode(
            ReaderSettingsBody.serializer(),
            ReaderSettingsBody(
                ReadingMode.RIVER,
                2,
                true,
                ReaderTheme.SYSTEM,
                66,
                ReaderFont.SERIF,
                mapOf("42" to ReaderOverride(ReaderTheme.EINK, 64, ReaderFont.SANS)),
                "op-1",
            ),
        ),
    )

    @Test
    fun contractRejectsFractionalInteger() {
        val empty = JsonObject(emptyMap())
        for (number in listOf("12.5", "9007199254740993.1")) {
            val error = runCatching { assertType("id", json.parseToJsonElement("""{"type":"integer"}""").jsonObject, json.parseToJsonElement(number), empty) }.exceptionOrNull()
            assertTrue("integer schema accepted $number", error is AssertionError)
        }
        assertType("number", json.parseToJsonElement("""{"type":"number"}""").jsonObject, JsonPrimitive(2), empty)
    }

    @Test
    fun contractChecksInlineArrayObjects() {
        val empty = JsonObject(emptyMap())
        val schema = json.parseToJsonElement("""{"type":"array","items":{"type":"object","required":["id"],"properties":{"id":{"type":"integer"}}}}""").jsonObject
        for (invalid in listOf("[{}]", "[{\"id\":\"wrong\"}]")) {
            val error = runCatching { assertType("rows", schema, json.parseToJsonElement(invalid), empty) }.exceptionOrNull()
            assertTrue("inline schema accepted $invalid", error is AssertionError)
        }
        assertType("rows", schema, json.parseToJsonElement("""[{"id":9007199254740993}]"""), empty)
        assertType("number", json.parseToJsonElement("""{"type":"number"}""").jsonObject, JsonPrimitive(2), empty)
    }

    @Test
    fun everyModelMatchesItsSchema() {
        val document = json.parseToJsonElement(File("../../../api/openapi.json").readText()).jsonObject
        val schemas = document.getValue("components").jsonObject.getValue("schemas").jsonObject
        val checked = mutableListOf<String>()
        for ((name, value) in models) {
            val schema = schemas[name]?.jsonObject ?: error("model $name has no schema")
            assertSchema(name, schema, value, document)
            checked += name
        }
        assertTrue("the registry must not be empty", checked.size >= 30)
    }

    @Test
    fun apiRequiresHttpsOrigin() {
        for (url in listOf("http://example.test", "https://user:pass@example.test", "https://example.test/path", "https://example.test/?token=x", "https://example.test/#fragment")) {
            try {
                EarnestApi(url)
                error("Accepted $url")
            } catch (_: IllegalArgumentException) {
            }
        }
        EarnestApi("https://example.test:8443")
    }

    @Test
    fun changesPageSupportsNullChangesAndLongRevisions() {
        val page = Json.decodeFromString(ChangesPage.serializer(), """{"changes":null,"next":{"generation":"g1","rev":9007199254740993},"next_cursor":"g1:9007199254740993"}""")
        assertEquals(null, page.changes)
        assertEquals(9007199254740993L, page.next.rev)
        assertEquals("g1:9007199254740993", page.nextCursor)
    }

    @Test
    fun labelRowsUseCanonicalLowercaseKeys() {
        val label = Json.decodeFromString(Label.serializer(), """{"id":1,"name":"work","rev":2,"deleted":false}""")
        assertEquals(1L, label.id)
        assertEquals("work", label.name)
        val membership = Json.decodeFromString(
            LabelMembership.serializer(),
            """{"id":1,"label_id":2,"entry_id":3,"rev":4,"deleted":false}""",
        )
        assertEquals(2L, membership.labelId)
        assertEquals(3L, membership.entryId)
        assertEquals(setOf("id", "name", "rev", "deleted"), Json.parseToJsonElement(Json.encodeToString(Label.serializer(), label)).jsonObject.keys)
        assertEquals(setOf("id", "label_id", "entry_id", "rev", "deleted"), Json.parseToJsonElement(Json.encodeToString(LabelMembership.serializer(), membership)).jsonObject.keys)
    }

    private fun assertSchema(name: String, schema: JsonObject, value: JsonElement, document: JsonObject) {
        val required = schema["required"] as? JsonArray ?: JsonArray(emptyList())
        val properties = schema["properties"] as? JsonObject ?: JsonObject(emptyMap())
        if (schema["additionalProperties"] == JsonPrimitive(false)) {
            val obj = value as? JsonObject ?: error("$name: expected object, got $value")
            val extras = obj.keys - properties.keys
            assertTrue("$name: fields not in schema: $extras", extras.isEmpty())
        }
        if (value is JsonObject) {
            for (key in required.map { it.jsonPrimitive.content }) {
                assertTrue("$name: missing required field $key", value.containsKey(key))
            }
        }
        for ((key, rawSchema) in properties) {
            val field = value as? JsonObject ?: continue
            if (!field.containsKey(key)) continue
            val fieldValue = field.getValue(key)
            assertType("$name.$key", rawSchema.jsonObject, fieldValue, document)
        }
    }

    private fun assertType(path: String, schema: JsonObject, value: JsonElement, document: JsonObject) {
        val ref = schema["\$ref"]?.jsonPrimitive?.content
        if (ref != null) {
            val target = ref.removePrefix("#/components/schemas/")
            val resolved = document.getValue("components").jsonObject.getValue("schemas").jsonObject.getValue(target).jsonObject
            assertSchema(path, resolved, value, document)
            return
        }
        val oneOf = schema["oneOf"] as? JsonArray
        if (oneOf != null) {
            val matches = oneOf.any { option ->
                runCatching { assertType(path, option.jsonObject, value, document) }.isSuccess
            }
            assertTrue("$path: value $value matches none of the allowed types", matches)
            return
        }
        val types = when (val type = schema["type"]) {
            is JsonArray -> type.map { it.jsonPrimitive.content }
            is JsonPrimitive -> listOf(type.content)
            else -> return // an unconstrained field accepts anything
        }
        if (value is JsonNull) {
            assertTrue("$path: null is not allowed", types.contains("null"))
            return
        }
        val actual = when (value) {
            is JsonObject -> "object"
            is JsonArray -> "array"
            is JsonPrimitive -> when {
                value.isString -> "string"
                value.booleanOrNull != null -> "boolean"
                value.content.toBigDecimalOrNull()?.stripTrailingZeros()?.scale()?.let { it <= 0 } == true -> "integer"
                else -> "number"
            }
            JsonNull -> "null"
        }
        val matches = types.contains(actual) || (actual == "integer" && types.contains("number"))
        assertTrue("$path: ${value} is $actual, schema wants $types", matches)
        if (value is JsonObject && schema["properties"] != null) {
            assertSchema(path, schema, value, document)
        }
        if (actual == "array" && value is JsonArray) {
            val itemSchema = schema["items"]?.jsonObject ?: return
            value.forEach { assertType("$path[]", itemSchema, it, document) }
        }
    }

    private fun state() = State(
        1,
        2,
        Register(JsonPrimitive("seen"), 1, "2026-11-02T12:00:00Z", 1, "deliberate"),
        Register(JsonPrimitive("done"), 1, "2026-11-02T12:00:00Z", 1, "deliberate"),
        Register(JsonPrimitive(true), 1, "2026-11-02T12:00:00Z", 1, "deliberate"),
        1,
    )

    private fun write() = StateWrite(
        "op",
        1,
        2,
        "seen",
        JsonPrimitive("seen"),
        Revision("g1", 1),
        "2026-11-02T12:00:00Z",
        "explicit",
    )

    private fun entry() = Entry(
        1,
        2,
        3,
        4,
        "2026-11-02T12:00:00Z",
        "2026-11-01T12:00:00Z",
        "2026-11-02T13:00:00Z",
        "Title",
        "https://example.test/article",
        "Author",
        5,
        false,
        state(),
        "/v1/entries/1/thumbnail?v=0123abcd",
    )

    private fun subscription() = Subscription(
        "2026-11-01T00:00:00Z",
        0,
        true,
        1,
        2,
        "Title",
        "https://example.test/feed",
        "https://example.test",
        3,
        "today",
        "always",
        4,
        false,
        false,
        false,
        FeedHealth("2026-11-01T00:00:00Z", "2026-11-02T00:00:00Z", "2026-11-03T00:00:00Z", "", "", 0, 200, 1.5, false),
        "/v1/subscriptions/1/icon?v=0123abcd",
    )

    private fun activity() = Activity(
        1,
        2,
        "Morning Agent",
        "state.updated",
        JsonPrimitive("target"),
        JsonPrimitive("before"),
        JsonPrimitive("after"),
        "g1",
        "2026-11-02T12:00:00Z",
        "revert",
        "reason",
    )

    private fun searchMatch() = SearchMatch(
        "agent_note",
        9007199254740993,
        2,
        3,
        4,
        "Title",
        "https://example.test/a",
        "Snippet",
        1.25,
        true,
        84,
        "Morning Agent",
    )

    private fun notification() = Notification(
        1,
        2,
        false,
        "note",
        listOf(3),
        "alert",
        "delivered",
        NotificationBudget(1, 3),
        NotificationWhy("Morning Agent", NotificationBudget(1, 3), 4),
        "g1",
        "2026-11-02T12:00:00Z",
        false,
    )
}

package ch.lkmc.earnest

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class PushPayloadTest {
    private val json = Json { ignoreUnknownKeys = true }

    @Test fun decodesServerTextAndAgentText() {
        val payload = json.decodeFromString(
            PushPayload.serializer(),
            """{"title":"Server title","body":"Server body","notification_id":"42","category":"alert","text":"agent words"}""",
        )
        assertEquals("Server title", payload.title)
        assertEquals("Server body", payload.body)
        assertEquals("42", payload.notificationId)
        assertEquals("alert", payload.category)
        assertEquals("agent words", payload.text)
    }

    @Test fun agentTextIsOptional() {
        val payload = json.decodeFromString(
            PushPayload.serializer(),
            """{"title":"Server title","body":"Server body"}""",
        )
        assertNull(payload.text)
        assertEquals("", payload.notificationId)
        assertEquals("", payload.category)
    }

    @Test fun unknownFieldsAreIgnored() {
        val payload = json.decodeFromString(
            PushPayload.serializer(),
            """{"title":"Server title","body":"Server body","unexpected":"value"}""",
        )
        assertEquals("Server title", payload.title)
    }
}

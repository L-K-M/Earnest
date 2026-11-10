package ch.lkmc.earnest.core

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class SyncObjectHealthTest {
    private fun sub(raw: String) =
        SyncObject(kind = SyncEngine.OBJECT_SUBSCRIPTION, id = 1, name = "Feed", rev = 1, raw = raw)

    @Test fun healthIsReadFromTheRawProjection() {
        val health = sub(
            """{"health":{"last_success_at":"2026-11-09T00:00:00Z","last_attempt_at":"2026-11-10T00:00:00Z","next_fetch_at":"2026-11-10T01:00:00Z","error_class":"HttpError","error_message":"503","consecutive_failures":3,"http_status":503,"posts_per_day":1.5,"firehose":false}}"""
        ).health()
        assertEquals("HttpError", health?.errorClass)
        assertEquals("503", health?.errorMessage)
        assertEquals(3L, health?.consecutiveFailures)
        assertEquals("2026-11-10T00:00:00Z", health?.lastAttemptAt)
    }

    @Test fun absentOrUnreadableHealthIsNone() {
        assertNull(sub("""{"cadence":"today"}""").health())
        assertNull(sub("not json").health())
        assertNull(sub("""{"health":42}""").health())
    }
}

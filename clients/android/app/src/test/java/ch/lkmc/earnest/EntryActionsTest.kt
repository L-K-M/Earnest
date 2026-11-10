package ch.lkmc.earnest

import ch.lkmc.earnest.core.Register
import ch.lkmc.earnest.core.State
import ch.lkmc.earnest.core.SyncEngine
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class EntryActionsTest {
    private fun stateOf(seen: String, shelf: String, kept: Boolean = false) = State(
        principal = 1,
        entryId = 2,
        seen = Register(JsonPrimitive(seen), 1, "2026-11-03T00:00:00Z", 1, "explicit"),
        shelf = Register(JsonPrimitive(shelf), 1, "2026-11-03T00:00:00Z", 1, "explicit"),
        kept = Register(JsonPrimitive(kept), 1, "2026-11-03T00:00:00Z", 1, "explicit"),
        stateRev = 1,
    )

    private fun action(message: String, prior: Map<String, JsonPrimitive>, written: Map<String, JsonPrimitive>) =
        ReversibleAction(2, message, prior, written)

    @Test fun theUndoMessagesNameTheAction() {
        assertEquals("Moved to Later", EntryActionName.LATER.message)
        assertEquals("Moved to Done", EntryActionName.DONE.message)
        assertEquals("Kept", EntryActionName.KEEP.message)
        assertEquals("Removed from Kept", EntryActionName.UNKEEP.message)
        assertEquals("Marked as unread", EntryActionName.UNREAD.message)
    }

    @Test fun undoWritesBackThePriorValue() {
        val saved = action(
            "Moved to Later",
            mapOf(SyncEngine.REGISTER_SHELF to JsonPrimitive(SyncEngine.VALUE_NONE)),
            mapOf(SyncEngine.REGISTER_SHELF to JsonPrimitive(SyncEngine.VALUE_LATER)),
        )
        val intents = undoIntents(saved, stateOf(seen = "seen", shelf = "later"))
        assertEquals(listOf(SyncEngine.REGISTER_SHELF), intents.map { it.register })
        assertEquals(JsonPrimitive(SyncEngine.VALUE_NONE), intents.single().value)
        assertEquals(JsonPrimitive(SyncEngine.VALUE_LATER), intents.single().prior)
    }

    @Test fun undoLeavesARegisterChangedSinceTheActionAlone() {
        val saved = action(
            "Moved to Later",
            mapOf(SyncEngine.REGISTER_SHELF to JsonPrimitive(SyncEngine.VALUE_NONE)),
            mapOf(SyncEngine.REGISTER_SHELF to JsonPrimitive(SyncEngine.VALUE_LATER)),
        )
        // Another writer moved the article on to Done; undo must not drag it back.
        assertEquals(emptyList<RegisterIntent>(), undoIntents(saved, stateOf(seen = "seen", shelf = "done")))
    }

    @Test fun markUnreadClearsDoneWithTheShelfWriteFirst() {
        val intents = unreadIntents(stateOf(seen = "seen", shelf = "done"))
        assertEquals(
            listOf(SyncEngine.REGISTER_SHELF, SyncEngine.REGISTER_SEEN),
            intents.map { it.register },
        )
        assertEquals(JsonPrimitive(SyncEngine.VALUE_NONE), intents[0].value)
        assertEquals(JsonPrimitive(SyncEngine.VALUE_DONE), intents[0].prior)
        assertEquals(JsonPrimitive(SyncEngine.VALUE_UNSEEN), intents[1].value)
        assertEquals(JsonPrimitive(SyncEngine.VALUE_SEEN), intents[1].prior)
    }

    @Test fun markUnreadLeavesAnUnfinishedShelfAlone() {
        assertEquals(
            listOf(SyncEngine.REGISTER_SEEN),
            unreadIntents(stateOf(seen = "seen", shelf = "later")).map { it.register },
        )
    }

    @Test fun markUnreadUndoRestoresSeenAndDone() {
        // What Mark unread saved for a Done article: both registers moved.
        val saved = action(
            "Marked as unread",
            mapOf(
                SyncEngine.REGISTER_SHELF to JsonPrimitive(SyncEngine.VALUE_DONE),
                SyncEngine.REGISTER_SEEN to JsonPrimitive(SyncEngine.VALUE_SEEN),
            ),
            mapOf(
                SyncEngine.REGISTER_SHELF to JsonPrimitive(SyncEngine.VALUE_NONE),
                SyncEngine.REGISTER_SEEN to JsonPrimitive(SyncEngine.VALUE_UNSEEN),
            ),
        )
        val intents = undoIntents(saved, stateOf(seen = "unseen", shelf = "none"))
        // Seen comes first: restoring it cannot clear the Done shelf behind it.
        assertEquals(listOf(SyncEngine.REGISTER_SEEN, SyncEngine.REGISTER_SHELF), intents.map { it.register })
        assertEquals(JsonPrimitive(SyncEngine.VALUE_SEEN), intents[0].value)
        assertEquals(JsonPrimitive(SyncEngine.VALUE_DONE), intents[1].value)
    }

    @Test fun markUnreadUndoSkipsOnlyTheRegisterChangedSince() {
        val saved = action(
            "Marked as unread",
            mapOf(
                SyncEngine.REGISTER_SHELF to JsonPrimitive(SyncEngine.VALUE_DONE),
                SyncEngine.REGISTER_SEEN to JsonPrimitive(SyncEngine.VALUE_SEEN),
            ),
            mapOf(
                SyncEngine.REGISTER_SHELF to JsonPrimitive(SyncEngine.VALUE_NONE),
                SyncEngine.REGISTER_SEEN to JsonPrimitive(SyncEngine.VALUE_UNSEEN),
            ),
        )
        // Another writer moved the shelf on to Later; undo restores only seen.
        val shelfChanged = undoIntents(saved, stateOf(seen = "unseen", shelf = "later"))
        assertEquals(listOf(SyncEngine.REGISTER_SEEN), shelfChanged.map { it.register })
        assertEquals(JsonPrimitive(SyncEngine.VALUE_SEEN), shelfChanged.single().value)

        // The mirror: seen moved on, the shelf still holds what the action wrote.
        val seenChanged = undoIntents(saved, stateOf(seen = "seen", shelf = "none"))
        assertEquals(listOf(SyncEngine.REGISTER_SHELF), seenChanged.map { it.register })
        assertEquals(JsonPrimitive(SyncEngine.VALUE_DONE), seenChanged.single().value)
    }

    @Test fun canMarkUnreadOnlyForReadArticles() {
        assertTrue(canMarkUnread(stateOf(seen = "seen", shelf = "none")))
        assertTrue(canMarkUnread(stateOf(seen = "seen", shelf = "later")))
        assertTrue(canMarkUnread(stateOf(seen = "seen", shelf = "done")))
        assertFalse(canMarkUnread(stateOf(seen = "unseen", shelf = "none")))
        assertFalse(canMarkUnread(stateOf(seen = "none", shelf = "none")))
    }
}

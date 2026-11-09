package ch.lkmc.earnest

import ch.lkmc.earnest.core.State
import ch.lkmc.earnest.core.SyncEngine
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive

/** A reversible entry action, named for its operation and its undo snackbar. */
internal enum class EntryActionName(val id: String, val message: String) {
    LATER("later", "Moved to Later"),
    DONE("done", "Moved to Done"),
    KEEP("keep", "Kept"),
    UNKEEP("unkeep", "Removed from Kept"),
    UNREAD("unread", "Marked as unread"),
}

/** One register write, with the value it replaces, for one reversible action. */
internal data class RegisterIntent(val register: String, val value: JsonElement, val prior: JsonElement)

/**
 * A saved action: the registers it wrote and the values it replaced. Undo
 * writes back only registers still holding the value the action wrote, so a
 * change made elsewhere since the action is left alone.
 */
internal data class ReversibleAction(
    val entryId: Long,
    val message: String,
    val prior: Map<String, JsonElement>,
    val written: Map<String, JsonElement>,
)

/** The current value of one state register. Unknown registers are a programming error. */
internal fun valueOf(state: State, register: String): JsonElement = when (register) {
    SyncEngine.REGISTER_SEEN -> state.seen.value
    SyncEngine.REGISTER_SHELF -> state.shelf.value
    SyncEngine.REGISTER_KEPT -> state.kept.value
    else -> error("Unknown register: $register")
}

/** The revision one register's write is based on. Unknown registers are a programming error. */
internal fun revOf(state: State, register: String): Long = when (register) {
    SyncEngine.REGISTER_SEEN -> state.seen.rev
    SyncEngine.REGISTER_SHELF -> state.shelf.rev
    SyncEngine.REGISTER_KEPT -> state.kept.rev
    else -> error("Unknown register: $register")
}

/**
 * The writes marking a read article unread performs. Done is cleared as well
 * (the server rule the engine enforces behind this write), so the shelf write
 * goes first: the engine's own clear-done write then sees the shelf already
 * cleared and does not queue a second one.
 */
internal fun unreadIntents(state: State): List<RegisterIntent> {
    val seen = RegisterIntent(SyncEngine.REGISTER_SEEN, JsonPrimitive(SyncEngine.VALUE_UNSEEN), state.seen.value)
    val shelf = RegisterIntent(SyncEngine.REGISTER_SHELF, JsonPrimitive(SyncEngine.VALUE_NONE), state.shelf.value)
    return if (state.shelf.value == JsonPrimitive(SyncEngine.VALUE_DONE)) listOf(shelf, seen) else listOf(seen)
}

/**
 * The writes undo performs for [action]: registers still holding the value the
 * action wrote come back to the values it replaced. Seen comes first so that
 * restoring an unseen value cannot clear a Done shelf this same undo restores.
 */
internal fun undoIntents(action: ReversibleAction, state: State): List<RegisterIntent> =
    listOf(SyncEngine.REGISTER_SEEN, SyncEngine.REGISTER_SHELF, SyncEngine.REGISTER_KEPT).mapNotNull { register ->
        val written = action.written[register] ?: return@mapNotNull null
        if (valueOf(state, register) != written) return@mapNotNull null
        RegisterIntent(register, action.prior.getValue(register), written)
    }

/** Mark unread applies to a read article: one the reader has marked seen. */
internal fun canMarkUnread(state: State): Boolean =
    state.seen.value == JsonPrimitive(SyncEngine.VALUE_SEEN)

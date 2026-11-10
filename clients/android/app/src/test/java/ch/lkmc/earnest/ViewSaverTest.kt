package ch.lkmc.earnest

import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.SaverScope
import org.junit.Assert.assertEquals
import org.junit.Test

class ViewSaverTest {
    /**
     * The selected section must survive the saved-state round trip that a
     * rotation or process death performs, like the navigator's open article;
     * without it the app would reopen on Today whatever the user was reading.
     */
    @Test fun everyViewSurvivesSavedStateRoundTrip() {
        val scope = SaverScope { true }
        for (view in View.entries) {
            val saved = with(ViewSaver) { scope.save(mutableStateOf(view)) }
            // The persisted format is the enum name; changing it would
            // silently reset a reader's section after an app update.
            assertEquals(view.name, saved)
            assertEquals(view, ViewSaver.restore(requireNotNull(saved))?.value)
        }
    }

    /** A section a future build no longer knows falls back to Today. */
    @Test fun anUnknownViewNameRestoresToday() {
        assertEquals(View.TODAY, ViewSaver.restore("RetiredSection")?.value)
    }
}

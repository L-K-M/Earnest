package ch.lkmc.earnest

import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.SaverScope
import org.junit.Assert.assertEquals
import org.junit.Test

class AgentSelectedSaverTest {
    /**
     * The agent-selected set must survive the saved-state round trip that a
     * rotation or process death performs; otherwise the reopened reader would
     * load an agent-surfaced article's images automatically.
     */
    @Test fun agentSelectedIdsSurviveSavedStateRoundTrip() {
        val scope = SaverScope { true }
        val saved = with(AgentSelectedSaver) { scope.save(mutableStateOf(setOf(2L, 7L))) }
        val restored = AgentSelectedSaver.restore(requireNotNull(saved))
        assertEquals(setOf(2L, 7L), restored?.value)
    }
}

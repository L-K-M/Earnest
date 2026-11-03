package ch.lkmc.earnest

import org.junit.Assert.assertEquals
import org.junit.Test

class BriefsTest {
    @Test fun labelUsesTheAgentName() {
        assertEquals("Morning Agent: Shipping soon", briefLabel("Morning Agent", 7, "Shipping soon"))
    }

    @Test fun labelFallsBackToThePrincipalId() {
        assertEquals("Agent 7: Shipping soon", briefLabel(null, 7, "Shipping soon"))
    }

    @Test fun labelKeepsAgentTextVerbatim() {
        val brief = "Line one\nLine two <b>not html</b>"
        assertEquals("Scout: $brief", briefLabel("Scout", 3, brief))
    }
}

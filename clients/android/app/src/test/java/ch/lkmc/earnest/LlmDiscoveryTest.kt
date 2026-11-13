package ch.lkmc.earnest

import ch.lkmc.earnest.core.LlmDiscoveredModel
import ch.lkmc.earnest.core.LlmModel
import ch.lkmc.earnest.core.LlmProviderKind
import ch.lkmc.earnest.core.LlmTask
import ch.lkmc.earnest.core.LlmTokenField
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class LlmDiscoveryTest {
    private fun provider(
        hasApiKey: Boolean = false,
        apiKeyInput: String = "",
        clearApiKey: Boolean = false,
    ) = LlmProviderDraft(
        name = "openai",
        kind = LlmProviderKind.OPENAI,
        baseUrl = "https://api.openai.com/v1",
        apiKeyInput = apiKeyInput,
        clearApiKey = clearApiKey,
        hasApiKey = hasApiKey,
    )

    private val taken = setOf("" to "other-model")

    @Test
    fun theKeyBadgeReadsSavedEnteredRemovalAndNone() {
        assertEquals(LlmApiKeyStatus.SAVED, provider(hasApiKey = true).apiKeyStatus())
        assertEquals(LlmApiKeyStatus.ENTERED_UNSAVED, provider(hasApiKey = true, apiKeyInput = "sk-new").apiKeyStatus())
        assertEquals(LlmApiKeyStatus.ENTERED_UNSAVED, provider(apiKeyInput = "sk-new").apiKeyStatus())
        assertEquals(LlmApiKeyStatus.REMOVAL_PENDING, provider(hasApiKey = true, clearApiKey = true).apiKeyStatus())
        assertEquals(LlmApiKeyStatus.NONE, provider().apiKeyStatus())
    }

    @Test
    fun aWhitespaceOnlyKeyIsNotAnEnteredKeyAndATypedKeyWinsOverTheClearFlag() {
        // The badge reads the trimmed draft key: blanks never count as entered.
        assertEquals(LlmApiKeyStatus.SAVED, provider(hasApiKey = true, apiKeyInput = "   ").apiKeyStatus())
        assertEquals(LlmApiKeyStatus.NONE, provider(apiKeyInput = " \t ").apiKeyStatus())
        assertEquals(LlmApiKeyStatus.ENTERED_UNSAVED, provider(hasApiKey = true, apiKeyInput = "sk", clearApiKey = true).apiKeyStatus())
        // The clear flag alone still schedules a removal.
        assertEquals(LlmApiKeyStatus.REMOVAL_PENDING, provider(clearApiKey = true).apiKeyStatus())
    }

    @Test
    fun theKeyStatusLabelsMatchTheFourStates() {
        assertEquals("API key saved", LlmApiKeyStatus.SAVED.label())
        assertEquals("API key entered · unsaved", LlmApiKeyStatus.ENTERED_UNSAVED.label())
        assertEquals("Key removal pending", LlmApiKeyStatus.REMOVAL_PENDING.label())
        assertEquals("No API key", LlmApiKeyStatus.NONE.label())
    }

    @Test
    fun theDiscoveryFingerprintTracksEveryProviderEditButNeverHoldsTheKey() {
        val base = provider(hasApiKey = true, apiKeyInput = "sk-secret-value")
        val fingerprint = base.discoveryFingerprint()
        assertEquals(fingerprint, provider(hasApiKey = true, apiKeyInput = "sk-secret-value").discoveryFingerprint())
        assertFalse("the fingerprint must not carry the typed key", fingerprint.contains("sk-secret-value"))

        val edits = listOf(
            base.copy(name = "other"),
            base.copy(kind = LlmProviderKind.GEMINI),
            base.copy(baseUrl = "https://api.openai.com/v2"),
            base.copy(monthlyLimitUsd = "10"),
            base.copy(timeoutSeconds = "30"),
            base.copy(tokenField = LlmTokenField.MAX_TOKENS),
            base.copy(hasApiKey = false),
            base.copy(clearApiKey = true),
            base.copy(apiKeyInput = "sk-secret-value2"),
        )
        for (edit in edits) {
            assertNotEquals("an edit must stale the catalogue: $edit", fingerprint, edit.discoveryFingerprint())
        }
    }

    @Test
    fun collidingStringHashesStillInvalidateChangedCredentials() {
        assertEquals("Aa".hashCode(), "BB".hashCode())
        assertNotEquals(provider(apiKeyInput = "Aa").discoveryFingerprint(), provider(apiKeyInput = "BB").discoveryFingerprint())
    }

    @Test
    fun importsAreAtomicWhenSelectionsExceedCapacity() {
        val existing = (1..127).map { LlmModel("m$it") }
        assertEquals(existing, importReviewedModels(existing, listOf(LlmModel("first"), LlmModel("second"))))
    }

    @Test
    fun importingDoesNotOverwriteSavedPricingAndScopedIdentityWins() {
        val existing = LlmModel("shared", inputPer1m = "9", outputPer1m = "9", embeddingPer1m = "9", dimensions = 99)
        val imports = listOf(
            LlmModel("shared", inputPer1m = "1", outputPer1m = "1", embeddingPer1m = "1", dimensions = 1),
            LlmModel("fresh", inputPer1m = "0", outputPer1m = "0", embeddingPer1m = "0", dimensions = 0),
        )
        val merged = importReviewedModels(listOf(existing), imports)
        assertEquals(listOf(existing, imports[1]), merged)

        // A scoped row and a global row are different identities.
        val scoped = LlmModel("shared", provider = "openai", inputPer1m = "2")
        val withScoped = importReviewedModels(listOf(existing), listOf(scoped))
        assertEquals(listOf(existing, scoped), withScoped)
        assertEquals("openai/shared", scoped.identityLabel())
        assertEquals("fresh", imports[1].identityLabel())
    }

    @Test
    fun importingCapsTheTable() {
        val full = (1..128).map { LlmModel("m$it") }
        assertEquals(128, importReviewedModels(full, listOf(LlmModel("overflow"))).size)
        assertEquals(full, importReviewedModels(full, listOf(LlmModel("overflow"))))
    }

    @Test
    fun theCapabilityFilterHidesOnlyKnownIncapable() {
        val embeddable = LlmDiscoveredModel("e", embedding = true)
        val chatty = LlmDiscoveredModel("c", chat = true)
        val embedOnly = LlmDiscoveredModel("eo", embedding = true, chat = false)
        val chatOnly = LlmDiscoveredModel("co", chat = true, embedding = false)
        val unknown = LlmDiscoveredModel("u")
        for (task in listOf(LlmTask.CLASSIFY, LlmTask.BLURB, LlmTask.SUMMARY, LlmTask.EDITION, LlmTask.COMPILE_RULE, LlmTask.ASK)) {
            assertTrue("$task keeps chat-capable", chatty.compatibleWith(task))
            assertFalse("$task hides a known non-chat model", embedOnly.compatibleWith(task))
            assertTrue("$task labels unknown rather than hiding it", unknown.compatibleWith(task))
        }
        assertFalse("a known non-embedding model cannot embed", chatOnly.compatibleWith(LlmTask.EMBED))
        assertTrue("unknown embedding stays visible, labelled, for embed", chatty.compatibleWith(LlmTask.EMBED))
        assertTrue(embeddable.compatibleWith(LlmTask.EMBED))
        assertTrue("unknown capability stays visible for embed", unknown.compatibleWith(LlmTask.EMBED))
        // No task context: nothing is filtered.
        for (model in listOf(embeddable, chatty, embedOnly, chatOnly, unknown)) {
            assertTrue(model.compatibleWith(null))
        }
    }

    @Test
    fun searchMatchesIdOrName() {
        val model = LlmDiscoveredModel("gpt-5-mini", name = "GPT 5 Mini")
        assertTrue(model.matchesSearch(""))
        assertTrue(model.matchesSearch("gpt-5"))
        assertTrue(model.matchesSearch("MINI"))
        assertFalse(model.matchesSearch("llama"))
    }

    @Test
    fun reviewPrefillDefaultsOnlyKnownCapabilities() {
        // Chat-only: embedding price and geometry may default to zero.
        val chatOnly = LlmDiscoveredModel("co", chat = true, embedding = false)
        assertEquals(LlmModelReview("co", "", "", "0", "0", false), chatOnly.reviewPrefill())
        // Embedding-only: chat prices may default to zero.
        val embedOnly = LlmDiscoveredModel("eo", chat = false, embedding = true, embeddingPer1m = "0.02")
        assertEquals(LlmModelReview("eo", "0", "0", "0.02", "", false), embedOnly.reviewPrefill())
        // Unknown stays blank: an absent field is never read as free.
        val unknown = LlmDiscoveredModel("u")
        assertEquals(LlmModelReview("u", "", "", "", "", false), unknown.reviewPrefill())
        // Published fields prefill.
        val priced = LlmDiscoveredModel("p", chat = true, inputPer1m = "1.5", outputPer1m = "6", dimensions = 1536, truncatable = true)
        assertEquals(LlmModelReview("p", "1.5", "6", "", "1536", true), priced.reviewPrefill())
        // The provider scope and a chosen capability travel with the review.
        assertEquals("openai", chatOnly.reviewPrefill(provider = "openai").provider)
        assertEquals(
            LlmModelReview("eo", "0", "0", "0.02", "", false, provider = "openai"),
            embedOnly.reviewPrefill(provider = "openai", capability = LlmCapability.EMBEDDING),
        )
    }

    @Test
    fun chatSelectionZeroesNonapplicableEmbeddingFields() {
        val entry = LlmDiscoveredModel(
            "co",
            chat = true,
            inputPer1m = "1.5",
            outputPer1m = "6",
            embeddingPer1m = "0.02",
            dimensions = 1536,
            truncatable = true,
            supportsCustomDimensions = true,
        )
        val configured = entry.reviewPrefill("openai", LlmCapability.CHAT).toConfiguredModel(LlmCapability.CHAT)
        assertEquals("1.5", configured.inputPer1m)
        assertEquals("6", configured.outputPer1m)
        assertEquals("0", configured.embeddingPer1m)
        assertEquals(0, configured.dimensions)
        assertFalse(configured.truncatable)
        assertFalse(configured.supportsCustomDimensions)
        assertEquals("openai", configured.provider)
        assertTrue(entry.reviewProblems(entry.reviewPrefill("openai", LlmCapability.CHAT), taken, LlmCapability.CHAT).isEmpty())
    }

    @Test
    fun embeddingSelectionZeroesChatRatesAndKeepsGeometry() {
        val entry = LlmDiscoveredModel("eo", embedding = true, embeddingPer1m = "0.02", dimensions = 1536, truncatable = true)
        val configured = entry.reviewPrefill("openai", LlmCapability.EMBEDDING).toConfiguredModel(LlmCapability.EMBEDDING)
        assertEquals("0", configured.inputPer1m)
        assertEquals("0", configured.outputPer1m)
        assertEquals("0.02", configured.embeddingPer1m)
        assertEquals(1536, configured.dimensions)
        assertTrue(configured.truncatable)
        assertEquals("openai", configured.provider)
    }

    @Test
    fun reviewProblemsRequireApplicablePricesAndGeometry() {
        // A fully blank review for an unknown model reports every gap.
        val unknown = LlmDiscoveredModel("u")
        val gaps = unknown.reviewProblems(LlmModelReview("u"), taken)
        assertTrue(gaps.any { it.contains("input price") })
        assertTrue(gaps.any { it.contains("output price") })
        // A blank embedding dimension is missing, not a malformed integer.
        assertTrue(
            unknown.reviewProblems(LlmModelReview("u", "0", "0", "", ""), taken, LlmCapability.EMBEDDING)
                .any { it.contains("dimensions are missing") },
        )
        assertFalse(
            unknown.reviewProblems(LlmModelReview("u", "0", "0", "", ""), taken, LlmCapability.EMBEDDING)
                .any { it.contains("whole number") },
        )
        assertTrue(
            unknown.reviewProblems(LlmModelReview("u", "0", "0", "", "wide"), taken, LlmCapability.EMBEDDING)
                .any { it.contains("whole number") },
        )
        // A known embedding model must carry real geometry.
        val embedder = LlmDiscoveredModel("e", embedding = true)
        assertTrue(
            embedder.reviewProblems(LlmModelReview("e", "0", "0", "0.02", "0"), taken, LlmCapability.EMBEDDING)
                .any { it.contains("dimensions must be greater than zero") },
        )
        assertTrue(
            embedder.reviewProblems(LlmModelReview("e", "0", "0", "0.02", "1536", true), taken, LlmCapability.EMBEDDING).isEmpty(),
        )
        // A chat-only model needs no geometry, but its chat prices are
        // still required input: the prefill alone does not pass.
        val chatOnly = LlmDiscoveredModel("co", chat = true, embedding = false)
        assertTrue(chatOnly.reviewProblems(chatOnly.reviewPrefill(), taken).any { it.contains("input price") })
        assertTrue(chatOnly.reviewProblems(LlmModelReview("co", "1", "0.5", "0", "0", false), taken).isEmpty())
        // Names are unique against the table and sibling imports.
        assertTrue(
            unknown.reviewProblems(LlmModelReview("other-model", "0", "0", "0", "0"), taken)
                .any { it.contains("already configured") },
        )
        assertTrue(unknown.reviewProblems(LlmModelReview("bad name", "0", "0", "0", "0"), taken).any { it.contains("whitespace") })
        // A malformed price is not a price at all.
        assertTrue(unknown.reviewProblems(LlmModelReview("u", "abc", "0", "0", "0"), taken).any { it.contains("input price") })
    }

    @Test
    fun wideGeometryAcceptsTruncationOrCustomWidth() {
        val embedder = LlmDiscoveredModel("e", embedding = true)
        val wide = LlmModelReview("e", "0", "0", "0.02", "3072", truncatable = false)
        assertTrue(
            embedder.reviewProblems(wide, taken, LlmCapability.EMBEDDING).any { it.contains("truncatable") || it.contains("custom width") },
        )
        assertTrue(
            embedder.reviewProblems(wide.copy(truncatable = true), taken, LlmCapability.EMBEDDING).isEmpty(),
        )
        assertTrue(
            embedder.reviewProblems(wide.copy(supportsCustomDimensions = true), taken, LlmCapability.EMBEDDING).isEmpty(),
        )
        // The two flags are not interchangeable with the dimension ceiling.
        assertTrue(
            embedder.reviewProblems(wide.copy(dimensions = "65537", truncatable = true), taken, LlmCapability.EMBEDDING)
                .any { it.contains("at most") },
        )
    }

    @Test
    fun importingKeepsExistingPricesOnIdCollisionAndCapsTheTable() {
        val existing = LlmModel("shared", inputPer1m = "9", outputPer1m = "9", embeddingPer1m = "9", dimensions = 99)
        val imports = listOf(
            LlmModel("shared", inputPer1m = "1", outputPer1m = "1", embeddingPer1m = "1", dimensions = 1),
            LlmModel("fresh", inputPer1m = "0", outputPer1m = "0", embeddingPer1m = "0", dimensions = 0),
        )
        val merged = importReviewedModels(listOf(existing), imports)
        assertEquals(listOf(existing, imports[1]), merged)

        val full = (1..128).map { LlmModel("m$it") }
        assertEquals(128, importReviewedModels(full, listOf(LlmModel("overflow"))).size)
        assertEquals(full, importReviewedModels(full, listOf(LlmModel("overflow"))))
    }
}

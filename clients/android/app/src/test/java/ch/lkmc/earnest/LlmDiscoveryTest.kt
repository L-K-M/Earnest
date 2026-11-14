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
    @Test fun completionKeepsSavedCustomRatesAndTreatsDecimalZeroAsZero() {
        val existing = LlmModel("dual", inputPer1m = "0.000000", outputPer1m = "0.000000", embeddingPer1m = "3", dimensions = 1024)
        assertFalse(existing.usableFor(LlmCapability.CHAT))
        val review = LlmModelReview(name = "dual", input = "1", output = "2", embedding = "4", dimensions = "4096")
        val embedding = review.prefillSaved(existing, LlmCapability.EMBEDDING)
        assertEquals("3", embedding.embedding)
        assertEquals("1024", embedding.dimensions)
        val text = review.prefillSaved(existing.copy(inputPer1m = "9", outputPer1m = "8"), LlmCapability.CHAT)
        assertEquals("9", text.input)
        assertEquals("8", text.output)
    }

    @Test
    fun undoingFreeChoiceRestoresPublishedPrices() {
        val original = LlmModelReview(name = "chat", input = "1.25", output = "2.5", embedding = "0.1")
        val free = original.freePriceChoice(LlmCapability.CHAT, true, null)
        assertEquals("0", free.input)
        assertEquals("0", free.output)
        assertEquals(original, free.freePriceChoice(LlmCapability.CHAT, false, original))
        val embeddingFree = original.freePriceChoice(LlmCapability.EMBEDDING, true, null)
        assertEquals("0", embeddingFree.embedding)
        assertEquals("1.25", embeddingFree.input)
        assertEquals("0.1", embeddingFree.copy(dimensions = "4096")
            .freePriceChoice(LlmCapability.EMBEDDING, false, original).embedding)
    }
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
    fun reviewPrefillDefaultsOnlyInapplicableSides() {
        // Chat-only: embedding price and geometry are inapplicable for a
        // chat selection and may default to zero.
        val chatOnly = LlmDiscoveredModel("co", chat = true, embedding = false)
        assertEquals(LlmModelReview("co", "", "", "0", "0", false), chatOnly.reviewPrefill(capability = LlmCapability.CHAT))
        // Embedding-only: chat prices are inapplicable for an embedding
        // selection and may default to zero.
        val embedOnly = LlmDiscoveredModel("eo", chat = false, embedding = true, embeddingPer1m = "0.02")
        assertEquals(LlmModelReview("eo", "0", "0", "0.02", "", false), embedOnly.reviewPrefill(capability = LlmCapability.EMBEDDING))
        // Unknown stays blank: an absent field is never read as free.
        val unknown = LlmDiscoveredModel("u")
        assertEquals(LlmModelReview("u", "", "", "", "", false), unknown.reviewPrefill())
        // Published fields prefill.
        val priced = LlmDiscoveredModel("p", chat = true, inputPer1m = "1.5", outputPer1m = "6", dimensions = 1536, truncatable = true)
        assertEquals(LlmModelReview("p", "1.5", "6", "", "1536", true), priced.reviewPrefill(capability = LlmCapability.CHAT))
        // The provider scope travels with the review.
        assertEquals("openai", chatOnly.reviewPrefill(provider = "openai").provider)
        // An unspecified capability leaves both sides open: neither may be
        // read as free.
        assertEquals(LlmModelReview("co", "", "", "", "", false), chatOnly.reviewPrefill())
        assertEquals(LlmModelReview("eo", "", "", "0.02", "", false), embedOnly.reviewPrefill())
    }

    @Test
    fun chatSelectionNeverTreatsMissingTextPricesAsFree() {
        // A provider-known embedding-only entry has no published chat
        // prices. Selecting it for text must leave them blank, so the pane
        // demands explicit input or an explicit free choice.
        val embedOnly = LlmDiscoveredModel("eo", chat = false, embedding = true, embeddingPer1m = "0.02", dimensions = 1536)
        val review = embedOnly.reviewPrefill("openai", LlmCapability.CHAT)
        assertEquals("", review.input)
        assertEquals("", review.output)
        val problems = embedOnly.reviewProblems(review, taken, LlmCapability.CHAT)
        assertTrue(problems.any { it.contains("input price") })
        assertTrue(problems.any { it.contains("output price") })
        // Published chat prices are still used verbatim.
        val published = LlmDiscoveredModel(
            "eo", chat = false, embedding = true,
            inputPer1m = "1", outputPer1m = "2", embeddingPer1m = "0.02", dimensions = 1536,
        )
        assertEquals("1", published.reviewPrefill("openai", LlmCapability.CHAT).input)
        assertEquals("2", published.reviewPrefill("openai", LlmCapability.CHAT).output)
    }

    @Test
    fun savedGeometryGatesEmbeddingReadinessButTextRatesGateChatReadiness() {
        val textOnly = LlmModel("shared", inputPer1m = "1", outputPer1m = "2", provider = "openai")
        assertFalse(textOnly.usableFor(LlmCapability.EMBEDDING))
        assertTrue(textOnly.usableFor(LlmCapability.CHAT))

        val embedOnly = LlmModel(
            "vec", inputPer1m = "0", outputPer1m = "0", embeddingPer1m = "0.02",
            dimensions = 1536, truncatable = true, provider = "openai",
        )
        assertTrue(embedOnly.usableFor(LlmCapability.EMBEDDING))
        assertFalse(embedOnly.usableFor(LlmCapability.CHAT))

        // Legacy zero prices may be explicit free or placeholders. Ask before
        // using them for a new route; existing routes remain untouched.
        val local = LlmModel("local", provider = "openai")
        assertFalse(local.usableFor(LlmCapability.CHAT))
        assertFalse(local.usableFor(LlmCapability.EMBEDDING))
        assertFalse(local.copy(dimensions = 1024, embeddingPer1m = "0.000000").usableFor(LlmCapability.EMBEDDING))

        // One real text rate is enough; the other may be an explicit zero.
        val mixed = LlmModel("mixed", inputPer1m = "0", outputPer1m = "2", dimensions = 1536, provider = "openai")
        assertTrue(mixed.usableFor(LlmCapability.CHAT))
    }

    @Test
    fun confirmedCompletionKeepsTheOtherCapabilityAndFillsOnlyTheRequestedSide() {
        val textOnly = LlmModel("shared", inputPer1m = "1", outputPer1m = "2", provider = "openai")
        val embedded = textOnly.completedFor(
            LlmCapability.EMBEDDING,
            LlmModel("shared", embeddingPer1m = "0.03", dimensions = 4096, supportsCustomDimensions = true, provider = "openai"),
        )
        assertEquals("1", embedded.inputPer1m)
        assertEquals("2", embedded.outputPer1m)
        assertEquals("0.03", embedded.embeddingPer1m)
        assertEquals(4096, embedded.dimensions)
        assertTrue(embedded.supportsCustomDimensions)
        assertFalse(embedded.truncatable)

        val embedOnly = LlmModel(
            "vec", inputPer1m = "0", outputPer1m = "0", embeddingPer1m = "0.02",
            dimensions = 1536, truncatable = true, provider = "openai",
        )
        val chat = embedOnly.completedFor(
            LlmCapability.CHAT,
            LlmModel("vec", inputPer1m = "1", outputPer1m = "2", provider = "openai"),
        )
        assertEquals("1", chat.inputPer1m)
        assertEquals("2", chat.outputPer1m)
        assertEquals("0.02", chat.embeddingPer1m)
        assertEquals(1536, chat.dimensions)
        assertTrue(chat.truncatable)
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

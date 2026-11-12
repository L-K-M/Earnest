package ch.lkmc.earnest

import androidx.compose.material3.Text
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import ch.lkmc.earnest.core.Entry
import ch.lkmc.earnest.core.FrontPage
import ch.lkmc.earnest.core.FrontPageCoverage
import ch.lkmc.earnest.core.FrontPagePeriod
import ch.lkmc.earnest.core.FrontPageSection
import ch.lkmc.earnest.core.FrontPageStory
import ch.lkmc.earnest.core.FrontPageSummary
import ch.lkmc.earnest.core.GroundedClaim
import ch.lkmc.earnest.core.PassageCitation
import ch.lkmc.earnest.core.Register
import ch.lkmc.earnest.core.State
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class FrontPageScreenTest {
    @get:Rule
    val compose = createComposeRule()

    private val opened = mutableListOf<Long>()
    private val periods = mutableListOf<FrontPagePeriod>()
    private val refreshes = mutableListOf<Unit>()
    private val images = mutableListOf<String?>()

    private fun entry(
        id: Long,
        title: String = "Headline $id",
        deleted: Boolean = false,
        seen: String = "unseen",
        shelf: String = "none",
    ) = Entry(
        id = id,
        subscriptionId = 3,
        feedId = 5,
        seq = id,
        firstSeenAt = "2026-11-03T00:00:00Z",
        publishedAt = "2026-11-02T12:00:00Z",
        title = title,
        url = "https://news.test/$id",
        author = "Author",
        rev = 1,
        deleted = deleted,
        state = State(
            principal = 1,
            entryId = id,
            seen = Register(JsonPrimitive(seen), 1, "2026-11-03T00:00:00Z", 1, "passive"),
            shelf = Register(JsonPrimitive(shelf), 1, "2026-11-03T00:00:00Z", 1, "passive"),
            kept = Register(JsonPrimitive(false), 1, "2026-11-03T00:00:00Z", 1, "passive"),
            stateRev = 1,
        ),
    )

    private fun coverage(id: Long, deleted: Boolean = false) = FrontPageCoverage(entry(id, deleted = deleted), "Source $id")

    private fun story(
        id: String,
        entryId: Long,
        coverageIds: List<Long> = listOf(entryId),
        summary: FrontPageSummary? = null,
        imageUrl: String? = null,
        excerpt: String = "Excerpt text",
        deleted: Boolean = false,
        seen: String = "unseen",
        shelf: String = "none",
    ) = FrontPageStory(
        id = id,
        entry = entry(entryId, deleted = deleted, seen = seen, shelf = shelf),
        sourceTitle = "Source $entryId",
        publishedAt = "2026-11-02T12:00:00Z",
        feedCount = coverageIds.size,
        excerpt = excerpt,
        imageUrl = imageUrl,
        coverage = coverageIds.map { coverage(it) },
        summary = summary,
    )

    private fun page(
        stories: List<FrontPageStory>,
        revision: String = "rev-1",
        period: FrontPagePeriod = FrontPagePeriod.DAILY,
    ) = FrontPage(
        period = period,
        windowStart = "2026-11-02T10:00:00Z",
        windowEnd = "2026-11-03T10:00:00Z",
        selectedAt = "2026-11-03T10:00:00Z",
        revision = revision,
        sections = listOf(FrontPageSection("Across your feeds", stories)),
    )

    private fun summary(claims: List<GroundedClaim> = listOf(claim("Claim one."))) = FrontPageSummary(
        claims = claims,
        model = "model-x",
        promptVersion = "v1",
        generatedAt = "2026-11-03T09:00:00Z",
    )

    private fun claim(text: String, citationEntryId: Long? = null) = GroundedClaim(
        text = text,
        citations = citationEntryId?.let {
            listOf(PassageCitation(it, "hash", listOf("Passage one"), verified = true))
        },
        verified = true,
    )

    private fun setContent(state: FrontPageUi) {
        compose.setContent {
            EarnestTheme(ThemeSetting.LIGHT) {
                FrontPageScreen(
                    state = state,
                    states = emptyMap(),
                    principalId = 1,
                    briefs = emptyList(),
                    principalNames = emptyMap(),
                    entryTitles = emptyMap(),
                    agentChanges = emptyList(),
                    sessionChanges = emptyList(),
                    agentChangesError = null,
                    onRevert = {},
                    onOpen = { opened += it },
                    onOpenAgentSelected = {},
                    onPeriodChange = { periods += it },
                    onRefresh = { refreshes += Unit },
                    image = { path, modifier ->
                        images += path
                        if (path != null) Text("image:$path", modifier)
                    },
                )
            }
        }
    }

    @Test fun rendersSectionsAndStoriesInServerOrder() {
        setContent(
            FrontPageUi(
                page = page(
                    listOf(
                        story("story-lead", 2),
                        story("story-secondary", 4),
                    ),
                ),
            ),
        )
        compose.onNodeWithText("Across your feeds").assertExists()
        compose.onNodeWithTag("story-lead").assertExists()
        compose.onNodeWithTag("story-secondary").assertExists()
        val lead = compose.onNodeWithTag("story-lead").fetchSemanticsNode().positionInRoot.y
        val secondary = compose.onNodeWithTag("story-secondary").fetchSemanticsNode().positionInRoot.y
        assertTrue("the lead story must render above the secondary stories", lead < secondary)
    }

    @Test fun theHeadlineOpensTheRepresentative() {
        setContent(FrontPageUi(page = page(listOf(story("story-lead", 2)))))
        compose.onNodeWithText("Headline 2").performClick()
        assertEquals(listOf(2L), opened)
    }

    @Test fun summaryCitationsExpandAndOpenTheirCoverage() {
        setContent(
            FrontPageUi(
                page = page(
                    listOf(
                        story(
                            "story-lead",
                            2,
                            coverageIds = listOf(2, 3),
                            summary = summary(claims = listOf(claim("Claim one.", citationEntryId = 3))),
                        ),
                    ),
                ),
            ),
        )
        compose.onNodeWithText("Claim one.").assertExists()
        compose.onNodeWithText("Summary of linked coverage", substring = true).assertExists()
        compose.onNodeWithText("Citations").performClick()
        compose.onNodeWithText("Passage one").assertExists()
        compose.onNodeWithText("Passage one").performClick()
        assertEquals(listOf(3L), opened)
    }

    @Test fun anAttributedExcerptReplacesTheSummary() {
        setContent(FrontPageUi(page = page(listOf(story("story-lead", 2, excerpt = "Publisher excerpt")))))
        compose.onNodeWithText("Publisher excerpt").assertExists()
        compose.onNodeWithText("Excerpt from Source 2").assertExists()
        compose.onNodeWithText("Citations").assertDoesNotExist()
    }

    @Test fun aMissingPictureRendersTextOnly() {
        setContent(FrontPageUi(page = page(listOf(story("story-lead", 2, imageUrl = null)))))
        compose.onNodeWithText("image:/v1/entries/2/image?v=img").assertDoesNotExist()
        assertTrue("the story must ask for its absent picture", images.isNotEmpty())
        assertTrue("an absent picture must not render an image", images.all { it == null })
    }

    @Test fun aStoredPictureIsRequested() {
        setContent(FrontPageUi(page = page(listOf(story("story-lead", 2, imageUrl = "/v1/entries/2/image?v=img")))))
        compose.onNodeWithText("image:/v1/entries/2/image?v=img").assertExists()
        assertEquals(listOf("/v1/entries/2/image?v=img"), images)
    }

    @Test fun readShowsOnlyWhenAllCoverageIsRead() {
        val seen: (Long) -> Entry = { id -> entry(id, seen = "seen") }
        val readStory = story("story-read", 2, coverageIds = listOf(2, 3)).withCoverageEntries(seen)
        val partialStory = story("story-partial", 4, coverageIds = listOf(4, 5))
            .withCoverageEntries { id -> if (id == 4L) seen(id) else entry(id) }
        setContent(FrontPageUi(page = page(listOf(readStory, partialStory))))
        compose.onNodeWithTag("story-read").assertExists()
        compose.onNodeWithTag("story-partial").assertExists()
        compose.onAllNodesWithText("Read").assertCountEquals(1)
    }

    private fun FrontPageStory.withCoverageEntries(entryFor: (Long) -> Entry): FrontPageStory = copy(
        coverage = coverage.map { member -> member.copy(entry = entryFor(member.entry.id)) },
    )

    @Test fun coverageExpansionOpensTheOtherOriginals() {
        setContent(FrontPageUi(page = page(listOf(story("story-lead", 2, coverageIds = listOf(2, 3))))))
        compose.onNodeWithText("Other sources (1)").performClick()
        compose.onNodeWithText("Source 3 · Headline 3").assertExists()
        compose.onNodeWithText("Source 3 · Headline 3").performClick()
        assertEquals(listOf(3L), opened)
    }

    @Test fun coverageExpansionItselfDoesNotOpenAnArticle() {
        setContent(FrontPageUi(page = page(listOf(story("story-lead", 2, coverageIds = listOf(2, 3))))))
        compose.onNodeWithText("Other sources (1)").performClick()
        assertTrue(opened.isEmpty())
    }

    @Test fun theTimeTabsRouteToTheirWindows() {
        setContent(FrontPageUi(page = page(listOf(story("story-lead", 2)))))
        compose.onNodeWithText("Past 7 days").performClick()
        assertEquals(listOf(FrontPagePeriod.WEEKLY), periods)
    }

    @Test fun aNewRevisionWaitsForAnExplicitRefresh() {
        setContent(
            FrontPageUi(
                page = page(listOf(story("story-lead", 2)), revision = "rev-1"),
                pendingRevision = "rev-2",
            ),
        )
        compose.onNodeWithText("New coverage available").assertExists()
        compose.onNodeWithTag("front-apply").performClick()
        assertEquals(1, refreshes.size)
        compose.onNodeWithText("Headline 2").assertExists()
    }

    @Test fun anEmptyWindowIsHonest() {
        setContent(FrontPageUi(page = page(emptyList())))
        compose.onNodeWithText("No stories in this window").assertExists()
        compose.onNodeWithText("Nothing matched the past 24 hours.").assertExists()
    }

    @Test fun aLoadingWindowIsHonest() {
        setContent(FrontPageUi(loading = true))
        compose.onNodeWithText("Loading the front page…").assertExists()
    }

    @Test fun aFailedWindowIsHonestAndRetryable() {
        setContent(FrontPageUi(error = "Feed unreachable"))
        compose.onNodeWithText("Front page did not load").assertExists()
        compose.onNodeWithText("Feed unreachable").assertExists()
        compose.onNodeWithTag("front-try-again").performClick()
        assertEquals(1, refreshes.size)
    }

    @Test fun anOfflineWindowKeepsItsPageAndTime() {
        setContent(
            FrontPageUi(
                page = page(listOf(story("story-lead", 2))),
                updatedAt = System.currentTimeMillis() - 2 * 60 * 60_000,
                error = "Offline. Showing the last updated page.",
            ),
        )
        compose.onNodeWithText("Headline 2").assertExists()
        compose.onNodeWithText("Updated 2h ago").assertExists()
        compose.onNodeWithText("Offline. Showing the last updated page.").assertExists()
    }
}

package ch.lkmc.earnest

import androidx.compose.foundation.layout.Box
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.Density
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToIndex
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsNotDisplayed
import androidx.compose.ui.unit.dp
import ch.lkmc.earnest.core.Activity
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
import kotlinx.serialization.json.JsonObject
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

    private fun setContent(
        state: FrontPageUi,
        image: @Composable (String?, Modifier) -> Unit = { path, modifier ->
            images += path
            if (path != null) Text("image:$path", modifier)
        },
        agentChanges: List<Activity> = emptyList(),
        fontScale: Float? = null,
    ) {
        compose.setContent {
            CompositionLocalProvider(
                LocalDensity provides Density(LocalDensity.current.density, fontScale ?: LocalDensity.current.fontScale),
            ) {
                EarnestTheme(ThemeSetting.LIGHT) {
                    FrontPageScreen(
                        state = state,
                        states = emptyMap(),
                        principalId = 1,
                        briefs = emptyList(),
                        principalNames = emptyMap(),
                        entryTitles = emptyMap(),
                        agentChanges = agentChanges,
                        sessionChanges = emptyList(),
                        agentChangesError = null,
                        onRevert = {},
                        onOpen = { opened += it },
                        onOpenAgentSelected = {},
                        onPeriodChange = { periods += it },
                        onRefresh = { refreshes += Unit },
                        image = image,
                    )
                }
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

    @Test fun theStaticEmptyStateKeepsHorizontalInsetsWithLargeText() {
        setContent(FrontPageUi(), fontScale = 2f)
        val title = compose.onNodeWithText("No stories in this window", useUnmergedTree = true).fetchSemanticsNode()
        val minimum = with(compose.density) { 24.dp.toPx() }
        assertTrue("empty state must retain 24dp horizontal insets, got ${title.boundsInRoot}", title.boundsInRoot.left >= minimum - 1f)
    }

    @Test fun theMastheadDoesNotRepeatTheAppBarTitle() {
        setContent(FrontPageUi(page = page(listOf(story("story-lead", 2, imageUrl = null)))))
        // The outer app bar already reads "Front page"; the masthead must not
        // render a second one.
        compose.onAllNodesWithText("Front page").assertCountEquals(0)
    }

    @Test fun theMastheadKeepsTheWindowDateAndPeriodControls() {
        setContent(FrontPageUi(page = page(listOf(story("story-lead", 2, imageUrl = null)))))
        val expectedDate = java.time.Instant.parse("2026-11-03T10:00:00Z")
            .atZone(java.time.ZoneId.systemDefault())
            .format(java.time.format.DateTimeFormatter.ofPattern("MMMM d, yyyy"))
        compose.onNodeWithText(expectedDate).assertExists()
        compose.onNodeWithText("Past 24 hours").assertExists()
        compose.onNodeWithText("Past 7 days").assertExists()
        compose.onNodeWithTag("front-refresh").assertExists()
    }

    @Test fun longCoverageKeepsItsOwnCompactMetadataLine() {
        val manyFeeds = (2L..13L).toList()
        setContent(
            FrontPageUi(
                page = page(
                    listOf(
                        story(
                            "story-lead",
                            2,
                            coverageIds = manyFeeds,
                            imageUrl = null,
                        ).copy(sourceTitle = "An unusually long source name that used to share a row with the read state"),
                    ),
                ),
            ),
        )
        compose.onNodeWithText("Covered by 12 feeds").assertExists()
        compose.onNodeWithText(
            "An unusually long source name that used to share a row with the read state",
            substring = true,
        ).assertExists()
    }

    @Test fun readStateAndLongCoverageEachKeepTheirOwnLine() {
        val manyFeeds = (2L..13L).toList()
        val readStory = story("story-read", 2, coverageIds = manyFeeds, imageUrl = null)
            .withCoverageEntries { id -> entry(id, seen = "seen") }
        setContent(FrontPageUi(page = page(listOf(readStory))))
        compose.onNodeWithText("Read").assertExists()
        compose.onNodeWithText("Covered by 12 feeds").assertExists()
    }

    @Test fun storiesUseTheSharedTwentyDpGutter() {
        setContent(FrontPageUi(page = page(listOf(story("story-lead", 2, imageUrl = null)))))
        val card = compose.onNodeWithTag("story-lead").fetchSemanticsNode()
        val gutter = with(compose.density) { 20.dp.toPx() }
        assertEquals("stories must sit on the 20dp content gutter", gutter, card.positionInRoot.x, 1f)
    }

    @Test fun storiesHaveASixteenDpBreathingSpace() {
        setContent(
            FrontPageUi(
                page = page(
                    listOf(
                        story("story-lead", 2, imageUrl = null),
                        story("story-secondary", 4, imageUrl = null),
                    ),
                ),
            ),
        )
        val lead = compose.onNodeWithTag("story-lead").fetchSemanticsNode()
        val secondary = compose.onNodeWithTag("story-secondary").fetchSemanticsNode()
        val gap = secondary.positionInRoot.y - (lead.positionInRoot.y + lead.size.height)
        val expected = with(compose.density) { 16.dp.toPx() }
        assertEquals("stories must be separated by a 16dp gap", expected, gap, 1f)
    }

    @Test fun aMissingPictureDoesNotReserveAPictureHeight() {
        var missingSlotHeight = -1
        setContent(
            FrontPageUi(page = page(listOf(story("story-lead", 2, imageUrl = null)))),
            image = { path, modifier ->
                if (path == null) {
                    Box(modifier.onGloballyPositioned { missingSlotHeight = it.size.height })
                } else {
                    Box(modifier)
                }
            },
        )
        compose.waitForIdle()
        val pictureHeight = with(compose.density) { 200.dp.toPx() }.toInt()
        assertTrue(
            "a missing picture must not reserve a picture-height gap (measured $missingSlotHeight px)",
            missingSlotHeight in 0 until pictureHeight,
        )
    }

    @Test fun agentChangeNoticesAlignWithStoryContent() {
        val change = Activity(
            id = 9,
            principalId = 1,
            principalName = "Agent",
            action = "annotation.updated",
            target = null,
            before = null,
            after = JsonObject(mapOf("kind" to JsonPrimitive("note"))),
            generation = "g1",
            createdAt = "2026-11-03T09:00:00Z",
            nextAction = "revert",
        )
        setContent(
            FrontPageUi(page = page(listOf(story("story-lead", 2, imageUrl = null)))),
            agentChanges = listOf(change),
        )
        compose.onNodeWithText("While you were out").assertExists()
        val title = compose.onNodeWithText("While you were out").fetchSemanticsNode()
        val gutter = with(compose.density) { 40.dp.toPx() }
        assertEquals("notices must align with the lead story's content", gutter, title.positionInRoot.x, 1f)
    }

    @Test fun theMastheadScrollsWithTheFrontPage() {
        setContent(
            FrontPageUi(
                page = page((2L..12L).map { story("story-$it", it, imageUrl = null) }),
            ),
        )
        compose.onNodeWithTag("front-grid").performScrollToIndex(8)
        compose.onNodeWithTag("front-refresh").assertIsNotDisplayed()
        compose.onNodeWithTag("front-grid").performScrollToIndex(0)
        compose.onNodeWithTag("front-refresh").assertIsDisplayed()
    }
}

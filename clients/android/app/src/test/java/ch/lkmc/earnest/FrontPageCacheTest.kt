package ch.lkmc.earnest

import android.content.Context
import androidx.core.content.edit
import androidx.test.core.app.ApplicationProvider
import ch.lkmc.earnest.core.Entry
import ch.lkmc.earnest.core.FrontPage
import ch.lkmc.earnest.core.FrontPagePeriod
import ch.lkmc.earnest.core.FrontPageSection
import ch.lkmc.earnest.core.FrontPageStory
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.SQLiteMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@SQLiteMode(SQLiteMode.Mode.NATIVE)
class FrontPageCacheTest {
    private val context get() = ApplicationProvider.getApplicationContext<Context>()
    private val accountA = AccountIdentity("https://nas.test", 1, 1)
    private val accountB = AccountIdentity("https://nas.test", 2, 2)

    private fun page(revision: String, period: FrontPagePeriod = FrontPagePeriod.DAILY) = FrontPage(
        period = period,
        windowStart = "2026-11-02T10:00:00Z",
        windowEnd = "2026-11-03T10:00:00Z",
        selectedAt = "2026-11-03T10:00:00Z",
        revision = revision,
        sections = listOf(
            FrontPageSection(
                "Across your feeds",
                listOf(
                    FrontPageStory(
                        id = "story-1",
                        entry = Entry(
                            id = 2,
                            subscriptionId = 3,
                            feedId = 5,
                            seq = 4,
                            firstSeenAt = "2026-11-03T00:00:00Z",
                            title = "Headline",
                            url = "https://news.test/article",
                            author = "Author",
                            rev = 2,
                            deleted = false,
                        ),
                        sourceTitle = "Source A",
                        publishedAt = "2026-11-02T12:00:00Z",
                        feedCount = 1,
                        excerpt = "Excerpt",
                        coverage = emptyList(),
                    ),
                ),
            ),
        ),
    )

    @Test fun roundTripsPerPeriodWithItsFetchTime() {
        val cache = SessionCache(context, accountA)
        assertNull(cache.cachedFrontPage(FrontPagePeriod.DAILY))
        assertNull(cache.cachedFrontPageAt(FrontPagePeriod.DAILY))

        cache.cacheFrontPage(FrontPagePeriod.DAILY, page("rev-1"))
        cache.cacheFrontPage(FrontPagePeriod.WEEKLY, page("rev-week", FrontPagePeriod.WEEKLY))

        assertEquals("rev-1", cache.cachedFrontPage(FrontPagePeriod.DAILY)?.revision)
        assertEquals("rev-week", cache.cachedFrontPage(FrontPagePeriod.WEEKLY)?.revision)
        assertEquals(FrontPagePeriod.WEEKLY, cache.cachedFrontPage(FrontPagePeriod.WEEKLY)?.period)
        assertEquals("story-1", cache.cachedFrontPage(FrontPagePeriod.DAILY)?.sections?.single()?.stories?.single()?.id)
        assertEquals("Source A", cache.cachedFrontPage(FrontPagePeriod.DAILY)?.sections?.single()?.stories?.single()?.sourceTitle)
        assertEquals(true, cache.cachedFrontPageAt(FrontPagePeriod.DAILY) != null)
        assertEquals(true, cache.cachedFrontPageAt(FrontPagePeriod.WEEKLY) != null)
    }

    @Test fun accountsDoNotSharePages() {
        SessionCache(context, accountA).cacheFrontPage(FrontPagePeriod.DAILY, page("rev-a"))
        val other = SessionCache(context, accountB)
        assertNull("account B must not read account A's front page", other.cachedFrontPage(FrontPagePeriod.DAILY))
        other.cacheFrontPage(FrontPagePeriod.DAILY, page("rev-b"))
        assertEquals("rev-a", SessionCache(context, accountA).cachedFrontPage(FrontPagePeriod.DAILY)?.revision)
        assertEquals("rev-b", other.cachedFrontPage(FrontPagePeriod.DAILY)?.revision)
    }

    @Test fun clearFrontPagesDropsBothWindows() {
        val cache = SessionCache(context, accountA)
        cache.cacheFrontPage(FrontPagePeriod.DAILY, page("rev-1"))
        cache.cacheFrontPage(FrontPagePeriod.WEEKLY, page("rev-week", FrontPagePeriod.WEEKLY))
        cache.clearFrontPages()
        assertNull(cache.cachedFrontPage(FrontPagePeriod.DAILY))
        assertNull(cache.cachedFrontPage(FrontPagePeriod.WEEKLY))
        assertNull(cache.cachedFrontPageAt(FrontPagePeriod.DAILY))
        assertNull(cache.cachedFrontPageAt(FrontPagePeriod.WEEKLY))
    }

    @Test fun aCorruptStoredPageIsIgnored() {
        val cache = SessionCache(context, accountA)
        cache.preferences.edit { putString("front_page_daily", "not json") }
        assertNull(cache.cachedFrontPage(FrontPagePeriod.DAILY))
    }

    @Test fun aLateResponseCannotRecreatePagesAfterSignOut() {
        val oldSession = SessionCache(context, accountA)
        oldSession.cacheFrontPage(FrontPagePeriod.DAILY, page("before"))
        oldSession.clearFrontPages()
        assertFalse(oldSession.cacheFrontPage(FrontPagePeriod.DAILY, page("late")))
        val newSession = SessionCache(context, accountA)
        assertNull(newSession.cachedFrontPage(FrontPagePeriod.DAILY))
        newSession.cacheFrontPage(FrontPagePeriod.DAILY, page("new-session"))
        assertFalse(oldSession.cacheFrontPage(FrontPagePeriod.DAILY, page("stale")))
        assertEquals("new-session", newSession.cachedFrontPage(FrontPagePeriod.DAILY)?.revision)
    }
}

package ch.lkmc.earnest

import android.graphics.Color
import android.webkit.RenderProcessGoneDetail
import android.webkit.WebView
import android.view.View
import android.view.ViewGroup
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.unit.dp
import ch.lkmc.earnest.core.ReaderFont
import ch.lkmc.earnest.core.ReaderTheme
import ch.lkmc.earnest.core.ReaderTypography
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "night")
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class ReaderSurfaceTest {
    @get:Rule val compose = createComposeRule()

    @Test fun theNativeCanvasIsThemedBeforeTheFirstDocumentPaint() {
        val views = mutableMapOf<ReaderTheme, WebView>()
        compose.setContent {
            Column {
                ReaderTheme.entries.forEach { theme ->
                    Reader(
                        url = null, cachedHtml = "<p>Article</p>", typography = ReaderTypography(theme, 68, ReaderFont.SERIF),
                        modifier = Modifier.fillMaxWidth().height(80.dp),
                        onWebView = { if (it != null) views[theme] = it },
                    )
                }
            }
        }
        compose.runOnIdle {
            ReaderTheme.entries.forEach { theme ->
                val expected = when (theme) {
                    ReaderTheme.SYSTEM, ReaderTheme.DARK -> Color.rgb(36, 36, 36)
                    ReaderTheme.BLACK -> Color.BLACK
                    ReaderTheme.LIGHT, ReaderTheme.EINK -> Color.WHITE
                }
                assertEquals("$theme must not start with the default white WebView canvas", expected, shadowOf(views.getValue(theme)).backgroundColor)
            }
        }
    }

    @Test fun nativeDocumentPaintingCannotEscapeItsResizingViewport() {
        var view: WebView? = null
        compose.setContent {
            Reader(
                url = null, cachedHtml = "<p>Article</p>", typography = ReaderTypography(ReaderTheme.SYSTEM, 68, ReaderFont.SERIF),
                modifier = Modifier.fillMaxWidth().height(120.dp), onWebView = { view = it },
            )
        }
        compose.runOnIdle { assertTrue("a resizing WebView must clip its native draw layer", view!!.clipToOutline) }
    }

    @Test fun aRecreatedReaderClearsTheCachedBadgeWhenLiveContentCommits() {
        val state = InlineArticleState().apply { showCachedCopy() }
        val url = "https://content.test/signed"
        var root: View? = null
        compose.setContent {
            root = LocalView.current
            InlineArticleContent(
                ContentUrlResult(url, null, "<p>Article</p>"),
                ReaderTypography(ReaderTheme.DARK, 68, ReaderFont.SERIF), state,
            )
        }
        compose.runOnIdle {
            val view = checkNotNull(findWebView(checkNotNull(root)))
            view.webViewClient.onPageCommitVisible(view, url)
        }
        compose.runOnIdle { assertFalse("a previously cached source must not label a fresh live page", state.offline) }
    }

    @Test fun aLongArticleCanAdvanceAndReturnInsideTheFeed() {
        val state = InlineArticleState().apply { reportHeight(16000) }
        var root: View? = null
        compose.setContent {
            root = LocalView.current
            InlineArticleContent(
                ContentUrlResult(null, null, "<p>Body</p>"),
                ReaderTypography(ReaderTheme.DARK, 68, ReaderFont.SERIF), state,
            )
        }
        val view = compose.runOnIdle { checkNotNull(findWebView(checkNotNull(root))) }
        compose.onNodeWithText("Next page").performClick()
        compose.runOnIdle { assertTrue("Next page must scroll the native article", view.scrollY > 0) }
        compose.onNodeWithText("Previous page").performClick()
        compose.runOnIdle { assertEquals(0, view.scrollY) }
    }

    @Test fun aLostRendererIsRemovedAndRetryCreatesANewView() {
        var root: View? = null
        compose.setContent {
            root = LocalView.current
            Reader(
                url = null, cachedHtml = "<p>Body</p>",
                typography = ReaderTypography(ReaderTheme.DARK, 68, ReaderFont.SERIF),
            )
        }
        val old = compose.runOnIdle { checkNotNull(findWebView(checkNotNull(root))) }
        compose.runOnIdle {
            old.webViewClient.onRenderProcessGone(old, object : RenderProcessGoneDetail() {
                override fun didCrash() = false
                override fun rendererPriorityAtExit() = WebView.RENDERER_PRIORITY_BOUND
            })
        }
        compose.runOnIdle {
            assertEquals(null, findWebView(checkNotNull(root)))
            assertTrue(shadowOf(old).wasDestroyCalled())
        }
        compose.onNodeWithText("Reload article").performClick()
        compose.runOnIdle { assertTrue(checkNotNull(findWebView(checkNotNull(root))) !== old) }
    }

    private fun findWebView(view: View): WebView? {
        if (view is WebView) return view
        if (view is ViewGroup) {
            for (index in 0 until view.childCount) findWebView(view.getChildAt(index))?.let { return it }
        }
        return null
    }

    @Test fun cachedDarkDocumentsThemeTheCanvasAsWellAsTheBody() {
        assertTrue(offlineReaderHtml("<p>Article</p>", ReaderTypography(ReaderTheme.DARK, 68, ReaderFont.SERIF), false)
            .contains("html{color-scheme:dark;background:#242424;color:#eee}"))
    }
}

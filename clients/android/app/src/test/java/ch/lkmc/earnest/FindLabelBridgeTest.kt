package ch.lkmc.earnest

import android.content.Context
import android.webkit.WebView
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * The find results must name the article on screen: the bridge re-registers
 * its listener when the WebView or the article changes, and detaches the old
 * listener so a stale one cannot write a previous article's label. The sink
 * is per article here, as the pane's findLabel state is.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class FindLabelBridgeTest {
    @get:Rule
    val compose = createComposeRule()

    private class SpyWebView(context: Context) : WebView(context) {
        var listener: WebView.FindListener? = null
        override fun setFindListener(listener: WebView.FindListener?) {
            this.listener = listener
        }
    }

    @Test fun labelsFollowTheCurrentArticleAndWebView() {
        val app = ApplicationProvider.getApplicationContext<Context>()
        val first = SpyWebView(app)
        val second = SpyWebView(app)
        val sinks = mutableMapOf<Long, MutableList<String>>()
        val webView = mutableStateOf<WebView?>(first)
        val entryId = mutableStateOf(1L)
        compose.setContent {
            // A fresh label sink per article, like the pane's findLabel state.
            val sink = remember(entryId.value) { mutableListOf<String>() }
            sinks[entryId.value] = sink
            FindLabelBridge(webView.value, entryId.value) { sink += it }
        }

        compose.runOnIdle { first.listener?.onFindResultReceived(0, 3, true) }
        compose.runOnIdle { assertEquals(listOf("1/3"), sinks[1]) }

        // The same WebView serves the next article: its results must name
        // the new article's sink, not the previous one's.
        compose.runOnIdle { entryId.value = 2 }
        compose.runOnIdle {
            assertNotNull("the WebView lost its listener", first.listener)
            first.listener?.onFindResultReceived(0, 5, true)
        }
        compose.runOnIdle {
            assertEquals("a stale listener wrote the previous article", listOf("1/3"), sinks[1])
            assertEquals(listOf("1/5"), sinks[2])
        }

        // A new article gets its own WebView: the old listener is detached.
        compose.runOnIdle {
            webView.value = second
            entryId.value = 3
        }
        compose.runOnIdle {
            assertNull("the old WebView kept its listener", first.listener)
            assertNotNull(second.listener)
            second.listener?.onFindResultReceived(1, 2, true)
        }
        compose.runOnIdle { assertEquals(listOf("2/2"), sinks[3]) }
    }

    @Test fun anAbsentWebViewIsANoOp() {
        compose.setContent { FindLabelBridge(null, 1) { labels -> throw AssertionError("no WebView reported: $labels") } }
        compose.waitForIdle()
    }
}

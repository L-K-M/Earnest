package ch.lkmc.earnest

import android.app.Application
import android.view.View
import android.view.ViewGroup
import android.widget.FrameLayout
import android.widget.TextView
import androidx.compose.ui.unit.DpSize
import androidx.compose.ui.unit.dp
import androidx.glance.appwidget.compose
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class WidgetRenderTest {
    @Test fun theWidgetComposesIntoViewsALauncherCanShow() = runBlocking<Unit> {
        val context = ApplicationProvider.getApplicationContext<Application>()
        val views = withTimeout(RENDER_TIMEOUT_MS) {
            EarnestWidget().compose(context, size = DpSize(250.dp, 180.dp))
        }
        val shown = texts(views.apply(context, FrameLayout(context)))
        assertTrue("the widget showed $shown", "Front page" in shown)
        assertTrue("a widget without headlines must say how to get them", "No headlines yet. Open Earnest to sync." in shown)
    }

    private fun texts(view: View): List<String> = when (view) {
        is TextView -> listOf(view.text.toString())
        is ViewGroup -> (0 until view.childCount).flatMap { texts(view.getChildAt(it)) }
        else -> emptyList()
    }
}

private const val RENDER_TIMEOUT_MS = 15_000L

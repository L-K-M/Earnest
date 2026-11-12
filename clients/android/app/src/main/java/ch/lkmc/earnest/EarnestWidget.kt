package ch.lkmc.earnest

import android.content.Context
import android.content.Intent
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.core.net.toUri
import androidx.glance.GlanceId
import androidx.glance.GlanceModifier
import androidx.glance.appwidget.action.actionStartActivity
import androidx.glance.action.clickable
import androidx.glance.appwidget.GlanceAppWidget
import androidx.glance.appwidget.GlanceAppWidgetReceiver
import androidx.glance.appwidget.provideContent
import androidx.glance.appwidget.updateAll
import androidx.glance.background
import androidx.glance.layout.Column
import androidx.glance.layout.fillMaxSize
import androidx.glance.layout.padding
import androidx.glance.text.FontWeight
import androidx.glance.text.Text
import androidx.glance.text.TextStyle
import androidx.glance.unit.ColorProvider
import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.SyncSnapshot
import ch.lkmc.earnest.db.AppDatabase
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import kotlinx.coroutines.CancellationException

/**
 * The home-screen widget: the newest headlines and nothing else. It shows no
 * unread count, by design.
 */
class EarnestWidget : GlanceAppWidget() {
    override suspend fun provideGlance(context: Context, id: GlanceId) {
        val entries = withContext(Dispatchers.IO) { WidgetData.headlines(context) }
        provideContent {
            Headlines(entries)
        }
    }

    @Composable
    private fun Headlines(entries: List<CachedEntry>) {
        val context = LocalContext.current
        Column(
            modifier = GlanceModifier
                .fillMaxSize()
                .background(ColorProvider(Color(0xFFFFFFFF)))
                .padding(16.dp),
        ) {
            Text(
                "Front page",
                style = TextStyle(
                    fontWeight = FontWeight.Bold,
                    color = ColorProvider(Color(0xFF101418)),
                ),
            )
            entries.forEach { entry ->
                Text(
                    entry.title,
                    style = TextStyle(color = ColorProvider(Color(0xFF101418))),
                    modifier = GlanceModifier
                        .padding(top = 8.dp)
                        .clickable(
                            actionStartActivity(
                                Intent(context, MainActivity::class.java).apply {
                                    data = "earnest://entry/${entry.id}".toUri()
                                },
                            ),
                        ),
                )
            }
        }
    }

}

class EarnestWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = EarnestWidget()
}

internal object WidgetData {
    private val json = Json { ignoreUnknownKeys = true }

    /** Reads the cached snapshot once; the widget has no network access. */
    fun headlines(context: Context): List<CachedEntry> {
        val session = Secrets(context).current() ?: return emptyList()
        val row = runBlocking { AppDatabase.get(context, session.identity.cacheKey).snapshot().load() } ?: return emptyList()
        val snapshot = runCatching { json.decodeFromString(SyncSnapshot.serializer(), row.json) }
            .getOrNull() ?: return emptyList()
        if (Secrets(context).current() != session) return emptyList()
        return headlines(snapshot)
    }

    /** The newest live headlines, newest first; deleted entries never appear. */
    internal fun headlines(snapshot: SyncSnapshot): List<CachedEntry> =
        snapshot.entries
            .filterNot { it.deleted }
            .sortedBy { it.seq }
            .takeLast(HEADLINES)
            .reversed()

    private const val HEADLINES = 5
}

/**
 * Redraws every placed widget after a sync. Best effort: a widget failure must
 * not fail the sync, but cancellation still propagates so the caller's
 * coroutine or worker stops instead of reporting success.
 */
internal suspend fun refreshWidgets(context: Context) {
    try {
        EarnestWidget().updateAll(context)
    } catch (cancelled: CancellationException) {
        throw cancelled
    } catch (_: Exception) {
        // The next sync redraws the widget.
    }
}

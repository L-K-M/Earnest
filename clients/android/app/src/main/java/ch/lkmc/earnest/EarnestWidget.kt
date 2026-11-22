package ch.lkmc.earnest

import android.content.Context
import android.content.Intent
import androidx.compose.material3.ColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.net.toUri
import androidx.glance.GlanceId
import androidx.glance.GlanceModifier
import androidx.glance.Image
import androidx.glance.ImageProvider
import androidx.glance.action.clickable
import androidx.glance.appwidget.GlanceAppWidget
import androidx.glance.appwidget.GlanceAppWidgetReceiver
import androidx.glance.appwidget.action.actionStartActivity
import androidx.glance.appwidget.appWidgetBackground
import androidx.glance.appwidget.cornerRadius
import androidx.glance.appwidget.provideContent
import androidx.glance.appwidget.updateAll
import androidx.glance.background
import androidx.glance.color.ColorProvider
import androidx.glance.layout.Alignment
import androidx.glance.layout.Column
import androidx.glance.layout.Row
import androidx.glance.layout.Spacer
import androidx.glance.layout.fillMaxSize
import androidx.glance.layout.fillMaxWidth
import androidx.glance.layout.padding
import androidx.glance.layout.size
import androidx.glance.layout.width
import androidx.glance.text.FontFamily
import androidx.glance.text.FontWeight
import androidx.glance.text.Text
import androidx.glance.text.TextStyle
import ch.lkmc.earnest.core.CachedEntry
import ch.lkmc.earnest.core.SyncSnapshot
import ch.lkmc.earnest.db.AppDatabase
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json

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
        val paper = dayNight { it.surface }
        val ink = dayNight { it.onSurface }
        val quiet = dayNight { it.onSurfaceVariant }
        Column(
            modifier = GlanceModifier
                .fillMaxSize()
                .appWidgetBackground()
                .background(paper)
                .cornerRadius(WidgetCorner)
                .clickable(actionStartActivity(Intent(context, MainActivity::class.java)))
                .padding(horizontal = ReaderSpace.md, vertical = ReaderSpace.sm),
        ) {
            Row(GlanceModifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Image(ImageProvider(R.drawable.brand_icon), contentDescription = null, modifier = GlanceModifier.size(WidgetMark))
                Spacer(GlanceModifier.width(ReaderSpace.xs))
                Text(
                    "Front page",
                    style = TextStyle(color = ink, fontSize = WidgetTitle, fontWeight = FontWeight.Bold, fontFamily = FontFamily.Serif),
                    maxLines = 1,
                )
            }
            if (entries.isEmpty()) {
                Text(
                    "No headlines yet. Open Earnest to sync.",
                    style = TextStyle(color = quiet, fontSize = WidgetHeadline),
                    modifier = GlanceModifier.padding(top = ReaderSpace.sm),
                )
            }
            entries.forEach { entry ->
                Text(
                    entry.title,
                    style = TextStyle(color = ink, fontSize = WidgetHeadline, fontFamily = FontFamily.Serif),
                    maxLines = HEADLINE_LINES,
                    modifier = GlanceModifier
                        .fillMaxWidth()
                        .padding(top = ReaderSpace.sm)
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

// A widget lives on the home screen, so it follows the device rather than the app's theme.
private fun dayNight(role: (ColorScheme) -> Color) = ColorProvider(day = role(LightColors), night = role(DarkColors))

private val WidgetCorner = 24.dp
private val WidgetMark = 24.dp
private val WidgetTitle = 16.sp
private val WidgetHeadline = 14.sp
private const val HEADLINE_LINES = 2

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

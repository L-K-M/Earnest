package ch.lkmc.earnest

import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.compose.runtime.Composable
import androidx.compose.ui.unit.dp
import androidx.glance.GlanceId
import androidx.glance.GlanceModifier
import androidx.glance.appwidget.action.actionStartActivity
import androidx.glance.action.clickable
import androidx.glance.appwidget.GlanceAppWidget
import androidx.glance.appwidget.GlanceAppWidgetReceiver
import androidx.glance.appwidget.provideContent
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
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.Json

/**
 * The home-screen widget: the newest headlines and nothing else. It shows no
 * unread count, by design.
 */
class EarnestWidget : GlanceAppWidget() {
    override suspend fun provideGlance(context: Context, id: GlanceId) {
        provideContent {
            Headlines(context)
        }
    }

    @Composable
    private fun Headlines(context: Context) {
        val entries = rememberHeadlines(context)
        Column(
            modifier = GlanceModifier
                .fillMaxSize()
                .background(androidx.glance.unit.ColorProvider(android.graphics.Color.WHITE))
                .padding(16.dp),
        ) {
            Text(
                "Today",
                style = TextStyle(fontWeight = FontWeight.Bold, color = ColorProvider(android.graphics.Color.BLACK)),
            )
            entries.forEach { entry ->
                Text(
                    entry.title,
                    style = TextStyle(color = ColorProvider(android.graphics.Color.BLACK)),
                    modifier = GlanceModifier
                        .padding(top = 8.dp)
                        .clickable(
                            actionStartActivity(
                                Intent(context, MainActivity::class.java).apply {
                                    data = Uri.parse("earnest://entry/${entry.id}")
                                },
                            ),
                        ),
                )
            }
        }
    }

    @Composable
    private fun rememberHeadlines(context: Context): List<CachedEntry> {
        return androidx.compose.runtime.remember { WidgetData.headlines(context) }
    }
}

class EarnestWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = EarnestWidget()
}

private object WidgetData {
    fun headlines(context: Context): List<CachedEntry> {
        val row = runBlocking { AppDatabase.get(context).snapshot().load() } ?: return emptyList()
        val snapshot = runCatching { Json { ignoreUnknownKeys = true }.decodeFromString(SyncSnapshot.serializer(), row.json) }
            .getOrNull() ?: return emptyList()
        return snapshot.entries
            .filterNot { it.deleted }
            .sortedBy { it.seq }
            .takeLast(HEADLINES)
            .reversed()
    }

    private const val HEADLINES = 5
}

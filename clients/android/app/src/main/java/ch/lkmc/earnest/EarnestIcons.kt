package ch.lkmc.earnest

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.PathFillType
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.PathBuilder
import androidx.compose.ui.graphics.vector.path
import androidx.compose.ui.unit.dp
import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.sin

/**
 * The app's own icon family: a 24-unit grid, 2-unit round strokes and, for
 * destinations, a solid twin for the selected state. Drawing the few marks
 * the app needs avoids the extended Material icon pack in an unminified build.
 */
internal object EarnestIcons {
    private const val GRID = 24f
    private const val STROKE = 2f
    private const val HEAVY_STROKE = 3f
    private val Ink = SolidColor(Color.Black)

    // Destinations

    val FrontPage = icon("FrontPage") {
        line {
            box(3f, 4f, 21f, 20f, 3f)
            segment(7f, 8.5f, 17f, 8.5f)
            box(7f, 12f, 11f, 16f, 1f)
            segment(14f, 12.5f, 17f, 12.5f)
            segment(14f, 16f, 17f, 16f)
        }
    }
    val FrontPageSelected = icon("FrontPageSelected") {
        solid {
            box(2.5f, 3.5f, 21.5f, 20.5f, 3.5f)
            box(6f, 7f, 18f, 9.5f, 1f)
            box(6f, 12f, 11f, 17f, 1f)
            box(13f, 12f, 18f, 13.8f, 0.8f)
            box(13f, 15.2f, 18f, 17f, 0.8f)
        }
    }
    val Articles = icon("Articles", mirror = true) {
        line {
            box(3f, 4f, 21f, 10.5f, 2.5f)
            box(3f, 13.5f, 21f, 20f, 2.5f)
            segment(7f, 7.25f, 14f, 7.25f)
            segment(7f, 16.75f, 17f, 16.75f)
        }
    }
    val ArticlesSelected = icon("ArticlesSelected", mirror = true) {
        solid {
            box(2.5f, 3.5f, 21.5f, 11f, 3f)
            box(2.5f, 13f, 21.5f, 20.5f, 3f)
            box(6f, 6.3f, 14f, 8.3f, 1f)
            box(6f, 15.8f, 17f, 17.8f, 1f)
        }
    }
    val Saved = icon("Saved") { line { bookmark() } }
    val SavedSelected = icon("SavedSelected") { shape { bookmark() } }
    val Feeds = icon("Feeds", mirror = true) { signal(STROKE) }
    val FeedsSelected = icon("FeedsSelected", mirror = true) { signal(HEAVY_STROKE) }
    val Kept = icon("Kept") { line { star() } }
    val KeptSelected = icon("KeptSelected") { shape { star() } }
    val Later = icon("Later") {
        line {
            circle(12f, 12f, 9f)
            moveTo(12f, 7f)
            lineTo(12f, 12f)
            lineTo(15.2f, 14f)
        }
    }
    val LaterSelected = icon("LaterSelected") {
        solid {
            circle(12f, 12f, 10f)
            moveTo(11f, 6f)
            lineTo(13f, 6f)
            lineTo(13f, 11.4f)
            lineTo(16.3f, 13.5f)
            lineTo(15.2f, 15.2f)
            lineTo(11f, 12.5f)
            close()
        }
    }
    val Done = icon("Done") {
        line {
            circle(12f, 12f, 9f)
            moveTo(7.8f, 12.3f)
            lineTo(10.8f, 15.2f)
            lineTo(16.2f, 9.2f)
        }
    }
    val DoneSelected = icon("DoneSelected") {
        solid {
            circle(12f, 12f, 10f)
            moveTo(6.8f, 12.3f)
            lineTo(8.3f, 10.8f)
            lineTo(10.8f, 13.3f)
            lineTo(15.8f, 8.3f)
            lineTo(17.3f, 9.8f)
            lineTo(10.8f, 16.3f)
            close()
        }
    }
    val Activity = icon("Activity", mirror = true) { timeline(filled = false) }
    val ActivitySelected = icon("ActivitySelected", mirror = true) { timeline(filled = true) }
    val Folder = icon("Folder") { line { folder() } }
    val FolderSelected = icon("FolderSelected") { shape { folder() } }

    // Navigation and chrome

    val Menu = icon("Menu", mirror = true) {
        line {
            segment(4f, 6.5f, 20f, 6.5f)
            segment(4f, 12f, 20f, 12f)
            segment(4f, 17.5f, 14f, 17.5f)
        }
    }
    val More = icon("More") {
        solid {
            circle(12f, 5f, 1.75f)
            circle(12f, 12f, 1.75f)
            circle(12f, 19f, 1.75f)
        }
    }
    val Back = icon("Back", mirror = true) {
        line {
            segment(20f, 12f, 4.5f, 12f)
            moveTo(10.5f, 6f)
            lineTo(4.5f, 12f)
            lineTo(10.5f, 18f)
        }
    }
    val Close = icon("Close") {
        line {
            segment(6f, 6f, 18f, 18f)
            segment(18f, 6f, 6f, 18f)
        }
    }
    val Search = icon("Search") {
        line {
            circle(10.5f, 10.5f, 6.5f)
            segment(15.4f, 15.4f, 20f, 20f)
        }
    }
    val Refresh = icon("Refresh") {
        line {
            moveTo(19.5f, 12.5f)
            arcTo(7.5f, 7.5f, 0f, true, true, 17.3f, 6.7f)
            lineTo(19.5f, 8.8f)
            moveTo(19.5f, 4f)
            lineTo(19.5f, 9f)
            lineTo(14.5f, 9f)
        }
    }
    val Settings = icon("Settings") {
        line {
            segment(4f, 7f, 11f, 7f)
            circle(14.5f, 7f, 3f)
            segment(18f, 7f, 20f, 7f)
            segment(4f, 17f, 6f, 17f)
            circle(9.5f, 17f, 3f)
            segment(13f, 17f, 20f, 17f)
        }
    }
    val Theme = icon("Theme") {
        line { circle(12f, 12f, 8.5f) }
        solid {
            moveTo(12f, 3.5f)
            arcTo(8.5f, 8.5f, 0f, false, true, 12f, 20.5f)
            close()
        }
    }
    val Bell = icon("Bell") {
        line {
            moveTo(6f, 16f)
            lineTo(6f, 10.5f)
            arcTo(6f, 6f, 0f, false, true, 18f, 10.5f)
            lineTo(18f, 16f)
            lineTo(20f, 18f)
            lineTo(4f, 18f)
            close()
            segment(10f, 21f, 14f, 21f)
        }
    }
    val SignOut = icon("SignOut", mirror = true) {
        line {
            moveTo(12.5f, 4f)
            lineTo(6f, 4f)
            quadTo(4f, 4f, 4f, 6f)
            lineTo(4f, 18f)
            quadTo(4f, 20f, 6f, 20f)
            lineTo(12.5f, 20f)
            segment(10f, 12f, 20f, 12f)
            moveTo(16.5f, 8.5f)
            lineTo(20f, 12f)
            lineTo(16.5f, 15.5f)
        }
    }
    val ChevronDown = icon("ChevronDown") {
        line {
            moveTo(6.5f, 9.5f)
            lineTo(12f, 15f)
            lineTo(17.5f, 9.5f)
        }
    }
    val ChevronUp = icon("ChevronUp") {
        line {
            moveTo(6.5f, 14.5f)
            lineTo(12f, 9f)
            lineTo(17.5f, 14.5f)
        }
    }
    val ChevronForward = icon("ChevronForward", mirror = true) {
        line {
            moveTo(9.5f, 6.5f)
            lineTo(15f, 12f)
            lineTo(9.5f, 17.5f)
        }
    }

    // Actions

    val Add = icon("Add") {
        line {
            segment(12f, 5f, 12f, 19f)
            segment(5f, 12f, 19f, 12f)
        }
    }
    val Remove = icon("Remove") { line { segment(5f, 12f, 19f, 12f) } }
    val Check = icon("Check") {
        line {
            moveTo(5f, 12.5f)
            lineTo(9.8f, 17f)
            lineTo(19f, 7f)
        }
    }
    val External = icon("External") {
        line {
            moveTo(11f, 5f)
            lineTo(7f, 5f)
            quadTo(4f, 5f, 4f, 8f)
            lineTo(4f, 17f)
            quadTo(4f, 20f, 7f, 20f)
            lineTo(16f, 20f)
            quadTo(19f, 20f, 19f, 17f)
            lineTo(19f, 13f)
            segment(11f, 13f, 20f, 4f)
            moveTo(14.5f, 4f)
            lineTo(20f, 4f)
            lineTo(20f, 9.5f)
        }
    }
    val Share = icon("Share") {
        line {
            circle(18f, 5.5f, 2.5f)
            circle(6f, 12f, 2.5f)
            circle(18f, 18.5f, 2.5f)
            segment(8.2f, 10.8f, 15.8f, 6.7f)
            segment(8.2f, 13.2f, 15.8f, 17.3f)
        }
    }
    val Copy = icon("Copy") {
        line {
            box(8f, 8f, 20f, 21f, 2.5f)
            moveTo(16f, 4.5f)
            lineTo(16f, 4f)
            quadTo(16f, 3f, 15f, 3f)
            lineTo(6.5f, 3f)
            quadTo(4f, 3f, 4f, 5.5f)
            lineTo(4f, 15f)
            quadTo(4f, 16f, 5f, 16f)
        }
    }
    val TextSmaller = icon("TextSmaller") {
        line {
            letterA()
            segment(16f, 8f, 22f, 8f)
        }
    }
    val TextLarger = icon("TextLarger") {
        line {
            letterA()
            segment(16f, 8f, 22f, 8f)
            segment(19f, 5f, 19f, 11f)
        }
    }
    val MarkUnread = icon("MarkUnread") {
        line { circle(12f, 12f, 8.5f) }
        solid { circle(12f, 12f, 3.5f) }
    }
    val Import = icon("Import") {
        line {
            tray()
            segment(12f, 4f, 12f, 14f)
            moveTo(7.5f, 10f)
            lineTo(12f, 14.5f)
            lineTo(16.5f, 10f)
        }
    }
    val Export = icon("Export") {
        line {
            tray()
            segment(12f, 4f, 12f, 14.5f)
            moveTo(7.5f, 8.5f)
            lineTo(12f, 4f)
            lineTo(16.5f, 8.5f)
        }
    }
    val Delete = icon("Delete") {
        line {
            segment(4f, 6.5f, 20f, 6.5f)
            moveTo(9f, 6f)
            lineTo(9.5f, 3.5f)
            lineTo(14.5f, 3.5f)
            lineTo(15f, 6f)
            moveTo(6f, 7f)
            lineTo(7f, 19f)
            quadTo(7.1f, 21f, 9f, 21f)
            lineTo(15f, 21f)
            quadTo(16.9f, 21f, 17f, 19f)
            lineTo(18f, 7f)
            segment(10f, 11f, 10.3f, 17f)
            segment(14f, 11f, 13.7f, 17f)
        }
    }

    // States and subjects

    val Info = icon("Info") {
        line {
            circle(12f, 12f, 9f)
            segment(12f, 11f, 12f, 16.5f)
        }
        solid { circle(12f, 7.6f, 1.2f) }
    }
    val Alert = icon("Alert") {
        line {
            moveTo(10.3f, 4.5f)
            quadTo(12f, 2f, 13.7f, 4.5f)
            lineTo(21f, 17.5f)
            quadTo(22f, 20.5f, 19f, 20.5f)
            lineTo(5f, 20.5f)
            quadTo(2f, 20.5f, 3f, 17.5f)
            close()
            segment(12f, 9f, 12f, 13.5f)
        }
        solid { circle(12f, 17f, 1.2f) }
    }
    val Lock = icon("Lock") {
        line {
            box(5f, 10.5f, 19f, 21f, 3f)
            moveTo(8f, 10f)
            lineTo(8f, 8f)
            arcTo(4f, 4f, 0f, false, true, 16f, 8f)
            lineTo(16f, 10f)
            segment(12f, 14.5f, 12f, 17f)
        }
    }
    val Offline = icon("Offline") {
        line {
            moveTo(8f, 7f)
            arcTo(5.5f, 5.5f, 0f, false, true, 17.5f, 10f)
            arcTo(4.5f, 4.5f, 0f, false, true, 19.5f, 18.2f)
            moveTo(15.5f, 19f)
            lineTo(7f, 19f)
            arcTo(4.5f, 4.5f, 0f, false, true, 6f, 10f)
            segment(4f, 4f, 20.5f, 20.5f)
        }
    }
    val Sparkle = icon("Sparkle") {
        shape {
            moveTo(10.5f, 4f)
            quadTo(11.5f, 10.5f, 18f, 11.5f)
            quadTo(11.5f, 12.5f, 10.5f, 19f)
            quadTo(9.5f, 12.5f, 3f, 11.5f)
            quadTo(9.5f, 10.5f, 10.5f, 4f)
            close()
        }
        solid { circle(18.5f, 5.5f, 1.8f) }
    }
    val Book = icon("Book", mirror = true) {
        line {
            moveTo(12f, 6.5f)
            quadTo(8f, 3.5f, 3f, 5f)
            lineTo(3f, 18.5f)
            quadTo(8f, 17f, 12f, 20f)
            quadTo(16f, 17f, 21f, 18.5f)
            lineTo(21f, 5f)
            quadTo(16f, 3.5f, 12f, 6.5f)
            close()
            segment(12f, 6.5f, 12f, 20f)
        }
    }
    val LayoutHeadlines = icon("LayoutHeadlines", mirror = true) {
        line {
            segment(4f, 6f, 20f, 6f)
            segment(4f, 10f, 15f, 10f)
            segment(4f, 14f, 20f, 14f)
            segment(4f, 18f, 15f, 18f)
        }
    }
    val LayoutReading = icon("LayoutReading", mirror = true) {
        line {
            segment(4f, 4.5f, 20f, 4.5f)
            box(4f, 8.5f, 20f, 20f, 2.5f)
            segment(8f, 12.5f, 16f, 12.5f)
            segment(8f, 16f, 13f, 16f)
        }
    }

    private fun icon(name: String, mirror: Boolean = false, draw: ImageVector.Builder.() -> Unit): ImageVector =
        ImageVector.Builder(
            name = "Earnest.$name", defaultWidth = 24.dp, defaultHeight = 24.dp,
            viewportWidth = GRID, viewportHeight = GRID, autoMirror = mirror,
        ).apply(draw).build()

    private fun ImageVector.Builder.line(width: Float = STROKE, draw: PathBuilder.() -> Unit) {
        path(
            stroke = Ink, strokeLineWidth = width, strokeLineCap = StrokeCap.Round,
            strokeLineJoin = StrokeJoin.Round, pathBuilder = draw,
        )
    }

    /** Overlapping subpaths knock out of the fill. */
    private fun ImageVector.Builder.solid(draw: PathBuilder.() -> Unit) {
        path(fill = Ink, pathFillType = PathFillType.EvenOdd, pathBuilder = draw)
    }

    /** A filled shape with the same rounded corners as its outlined twin. */
    private fun ImageVector.Builder.shape(draw: PathBuilder.() -> Unit) {
        path(
            fill = Ink, stroke = Ink, strokeLineWidth = STROKE, strokeLineCap = StrokeCap.Round,
            strokeLineJoin = StrokeJoin.Round, pathBuilder = draw,
        )
    }

    private fun ImageVector.Builder.signal(width: Float) {
        line(width) {
            moveTo(4f, 11f)
            arcTo(9f, 9f, 0f, false, true, 13f, 20f)
            moveTo(4f, 4.5f)
            arcTo(15.5f, 15.5f, 0f, false, true, 19.5f, 20f)
        }
        solid { circle(5.5f, 18.5f, width) }
    }

    private fun ImageVector.Builder.timeline(filled: Boolean) {
        line {
            segment(6f, 8.5f, 6f, 15.5f)
            segment(11f, 6f, 20f, 6f)
            segment(11f, 18f, 18f, 18f)
        }
        if (filled) {
            shape {
                circle(6f, 6f, 2.5f)
                circle(6f, 18f, 2.5f)
            }
        } else {
            line {
                circle(6f, 6f, 2.5f)
                circle(6f, 18f, 2.5f)
            }
        }
    }

    private fun PathBuilder.segment(fromX: Float, fromY: Float, toX: Float, toY: Float) {
        moveTo(fromX, fromY)
        lineTo(toX, toY)
    }

    private fun PathBuilder.circle(x: Float, y: Float, radius: Float) {
        moveTo(x - radius, y)
        arcTo(radius, radius, 0f, true, true, x + radius, y)
        arcTo(radius, radius, 0f, true, true, x - radius, y)
        close()
    }

    private fun PathBuilder.box(left: Float, top: Float, right: Float, bottom: Float, radius: Float) {
        moveTo(left + radius, top)
        lineTo(right - radius, top)
        arcTo(radius, radius, 0f, false, true, right, top + radius)
        lineTo(right, bottom - radius)
        arcTo(radius, radius, 0f, false, true, right - radius, bottom)
        lineTo(left + radius, bottom)
        arcTo(radius, radius, 0f, false, true, left, bottom - radius)
        lineTo(left, top + radius)
        arcTo(radius, radius, 0f, false, true, left + radius, top)
        close()
    }

    private fun PathBuilder.bookmark() {
        moveTo(8f, 3.5f)
        lineTo(16f, 3.5f)
        quadTo(19f, 3.5f, 19f, 6.5f)
        lineTo(19f, 20.5f)
        lineTo(12f, 16.5f)
        lineTo(5f, 20.5f)
        lineTo(5f, 6.5f)
        quadTo(5f, 3.5f, 8f, 3.5f)
        close()
    }

    private fun PathBuilder.star() {
        repeat(10) { point ->
            val radius = if (point % 2 == 0) 9.2 else 4.6
            val angle = -PI / 2 + point * PI / 5
            val x = (12 + radius * cos(angle)).toFloat()
            val y = (12.6 + radius * sin(angle)).toFloat()
            if (point == 0) moveTo(x, y) else lineTo(x, y)
        }
        close()
    }

    private fun PathBuilder.folder() {
        moveTo(3f, 7f)
        quadTo(3f, 4.5f, 5.5f, 4.5f)
        lineTo(9f, 4.5f)
        lineTo(11.5f, 7.5f)
        lineTo(18.5f, 7.5f)
        quadTo(21f, 7.5f, 21f, 10f)
        lineTo(21f, 17f)
        quadTo(21f, 19.5f, 18.5f, 19.5f)
        lineTo(5.5f, 19.5f)
        quadTo(3f, 19.5f, 3f, 17f)
        close()
    }

    private fun PathBuilder.letterA() {
        moveTo(3f, 19.5f)
        lineTo(8.5f, 5.5f)
        lineTo(14f, 19.5f)
        segment(5.2f, 14.5f, 11.8f, 14.5f)
    }

    private fun PathBuilder.tray() {
        moveTo(4f, 15f)
        lineTo(4f, 18f)
        quadTo(4f, 20f, 6f, 20f)
        lineTo(18f, 20f)
        quadTo(20f, 20f, 20f, 18f)
        lineTo(20f, 15f)
    }
}

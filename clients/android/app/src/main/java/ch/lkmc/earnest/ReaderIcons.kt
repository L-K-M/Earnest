package ch.lkmc.earnest

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.path
import androidx.compose.ui.unit.dp

// These vectors are independent of navigation initialization. Keeping two
// small marks local avoids bringing in the extended Material icon pack.
internal val LaterIcon: ImageVector = ImageVector.Builder(
    name = "Later", defaultWidth = 24.dp, defaultHeight = 24.dp,
    viewportWidth = 24f, viewportHeight = 24f,
).apply {
    path(stroke = SolidColor(Color.Black), strokeLineWidth = 2f, strokeLineCap = StrokeCap.Round) {
        moveTo(21f, 12f)
        curveTo(21f, 17f, 17f, 21f, 12f, 21f)
        curveTo(7f, 21f, 3f, 17f, 3f, 12f)
        curveTo(3f, 7f, 7f, 3f, 12f, 3f)
        curveTo(17f, 3f, 21f, 7f, 21f, 12f)
        close()
        moveTo(12f, 7f)
        lineTo(12f, 12f)
        lineTo(15f, 14f)
    }
}.build()

internal val FeedIcon: ImageVector = ImageVector.Builder(
    name = "Feeds", defaultWidth = 24.dp, defaultHeight = 24.dp,
    viewportWidth = 24f, viewportHeight = 24f,
).apply {
    path(fill = SolidColor(Color.Black)) {
        moveTo(4f, 3f)
        curveTo(13.4f, 3f, 21f, 10.6f, 21f, 20f)
        lineTo(18f, 20f)
        curveTo(18f, 12.3f, 11.7f, 6f, 4f, 6f)
        close()
        moveTo(4f, 9f)
        curveTo(10.1f, 9f, 15f, 13.9f, 15f, 20f)
        lineTo(12f, 20f)
        curveTo(12f, 15.6f, 8.4f, 12f, 4f, 12f)
        close()
        moveTo(8f, 18f)
        curveTo(8f, 19.1f, 7.1f, 20f, 6f, 20f)
        curveTo(4.9f, 20f, 4f, 19.1f, 4f, 18f)
        curveTo(4f, 16.9f, 4.9f, 16f, 6f, 16f)
        curveTo(7.1f, 16f, 8f, 16.9f, 8f, 18f)
        close()
    }
}.build()

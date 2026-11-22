package ch.lkmc.earnest

import androidx.compose.material3.ExperimentalMaterial3ExpressiveApi
import androidx.compose.material3.Typography
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp

/*
 * Two voices. The system serif is for what you read: screen titles, headlines
 * and dialog titles. The system sans is for what you operate: controls,
 * labels, metadata and explanations. No fonts are bundled or downloaded.
 */

private fun editorial(size: Int, line: Int, weight: FontWeight = FontWeight.Bold) =
    TextStyle(fontFamily = FontFamily.Serif, fontWeight = weight, fontSize = size.sp, lineHeight = line.sp)

private fun interfaceText(size: Int, line: Int, weight: FontWeight = FontWeight.Normal) =
    TextStyle(fontFamily = FontFamily.SansSerif, fontWeight = weight, fontSize = size.sp, lineHeight = line.sp)

private val DisplayLarge = editorial(52, 60)
private val DisplayMedium = editorial(42, 50)
private val DisplaySmall = editorial(34, 40)
private val HeadlineLarge = editorial(30, 36)
private val HeadlineMedium = editorial(28, 34)
private val HeadlineSmall = editorial(24, 30)
private val TitleLarge = editorial(22, 28)
private val TitleMedium = interfaceText(16, 22, FontWeight.Medium)
private val TitleSmall = interfaceText(14, 20, FontWeight.SemiBold)
private val BodyLarge = interfaceText(16, 24)
private val BodyMedium = interfaceText(14, 20)
private val BodySmall = interfaceText(13, 18)
private val LabelLarge = interfaceText(14, 20, FontWeight.SemiBold)
private val LabelMedium = interfaceText(12, 16, FontWeight.Medium)
private val LabelSmall = interfaceText(11, 16, FontWeight.Medium)

// Expressive components reach for the emphasised styles on their own, so they
// are set too; otherwise those components would fall back to the baseline type.
@OptIn(ExperimentalMaterial3ExpressiveApi::class)
internal val EarnestTypography = Typography(
    displayLarge = DisplayLarge,
    displayMedium = DisplayMedium,
    displaySmall = DisplaySmall,
    headlineLarge = HeadlineLarge,
    headlineMedium = HeadlineMedium,
    headlineSmall = HeadlineSmall,
    titleLarge = TitleLarge,
    titleMedium = TitleMedium,
    titleSmall = TitleSmall,
    bodyLarge = BodyLarge,
    bodyMedium = BodyMedium,
    bodySmall = BodySmall,
    labelLarge = LabelLarge,
    labelMedium = LabelMedium,
    labelSmall = LabelSmall,
    displayLargeEmphasized = DisplayLarge,
    displayMediumEmphasized = DisplayMedium,
    displaySmallEmphasized = DisplaySmall,
    headlineLargeEmphasized = HeadlineLarge,
    headlineMediumEmphasized = HeadlineMedium,
    headlineSmallEmphasized = HeadlineSmall,
    titleLargeEmphasized = TitleLarge,
    titleMediumEmphasized = TitleMedium.copy(fontWeight = FontWeight.Bold),
    titleSmallEmphasized = TitleSmall.copy(fontWeight = FontWeight.Bold),
    bodyLargeEmphasized = BodyLarge.copy(fontWeight = FontWeight.SemiBold),
    bodyMediumEmphasized = BodyMedium.copy(fontWeight = FontWeight.SemiBold),
    bodySmallEmphasized = BodySmall.copy(fontWeight = FontWeight.SemiBold),
    labelLargeEmphasized = LabelLarge.copy(fontWeight = FontWeight.Bold),
    labelMediumEmphasized = LabelMedium.copy(fontWeight = FontWeight.Bold),
    labelSmallEmphasized = LabelSmall.copy(fontWeight = FontWeight.Bold),
)

/** Headline styles that Material's scale has no role for. */
internal object StoryType {
    val lead = editorial(28, 34)
    val secondary = editorial(19, 25, FontWeight.SemiBold)

    /** A list headline. Its weight, not its size, says whether it was read. */
    val row = editorial(18, 24, FontWeight.SemiBold)
}

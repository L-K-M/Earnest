package ch.lkmc.earnest

import androidx.compose.ui.semantics.SemanticsActions
import androidx.compose.ui.semantics.getOrNull
import androidx.compose.ui.test.SemanticsMatcher

/** A node whose click action tells a screen reader what [label] a tap does. */
internal fun hasClickLabel(label: String) = SemanticsMatcher("click label \"$label\"") {
    it.config.getOrNull(SemanticsActions.OnClick)?.label == label
}

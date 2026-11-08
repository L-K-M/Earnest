package ch.lkmc.earnest

/** Unsubmitted text for a nullable per-feed measure override. */
internal data class OverrideMeasureInput private constructor(val text: String) {
    val measure: Long? get() = text.trim().toLongOrNull()?.takeIf { it in 62L..72L }
    val canApply: Boolean get() = text.isBlank() || measure != null

    fun edit(input: String): OverrideMeasureInput = OverrideMeasureInput(input)

    companion object {
        fun fromValue(measure: Long?): OverrideMeasureInput = OverrideMeasureInput(measure?.toString().orEmpty())
    }
}

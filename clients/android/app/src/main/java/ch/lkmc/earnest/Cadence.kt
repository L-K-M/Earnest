package ch.lkmc.earnest

/** A feed's urgency bucket, named by when the reader wants to see it. */
enum class Cadence(val value: String, val label: String) {
    NOW("now", "Now"),
    TODAY("today", "Today"),
    WEEK("week", "This week"),
}

/** The cadence the server reports, defaulting to Today when it says nothing. */
internal fun cadenceOf(value: String?): Cadence =
    Cadence.entries.firstOrNull { it.value == value } ?: Cadence.TODAY

/** Selection state for the three cadence buttons; exactly one is selected. */
internal fun cadenceSelection(current: String?): Map<Cadence, Boolean> =
    Cadence.entries.associateWith { it == cadenceOf(current) }

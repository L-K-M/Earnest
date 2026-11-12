package ch.lkmc.earnest

/**
 * A feed's urgency bucket, named by when the reader wants to see it. The
 * hint is the explanatory copy shared with the web client; cadence no longer
 * decides front-page placement, which the server selects itself.
 */
enum class Cadence(val value: String, val label: String, val hint: String) {
    NOW("now", "Now", "Arrives in the river at once; rules may notify."),
    TODAY("today", "Today", "Arrives in the river, kept for the days you choose."),
    WEEK("week", "This week", "Arrives in the river and stays longer; the front page selects from every cadence."),
}

/** The cadence the server reports, defaulting to Today when it says nothing. */
internal fun cadenceOf(value: String?): Cadence =
    Cadence.entries.firstOrNull { it.value == value } ?: Cadence.TODAY

/** Selection state for the three cadence buttons; exactly one is selected. */
internal fun cadenceSelection(current: String?): Map<Cadence, Boolean> =
    Cadence.entries.associateWith { it == cadenceOf(current) }

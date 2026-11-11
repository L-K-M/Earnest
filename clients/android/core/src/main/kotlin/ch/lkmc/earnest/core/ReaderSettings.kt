package ch.lkmc.earnest.core

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Reader wire models for `/v1/settings`. Field names and defaults mirror
 * `internal/service/settings.go`; OpenApiContractTest checks them against
 * api/openapi.json. The raw `EarnestApi.settings()` JSON stays for sync.
 */

/** How the river presents unread work. */
@Serializable
enum class ReadingMode {
    @SerialName("river") RIVER,
    @SerialName("unread_counts") UNREAD_COUNTS,
}

/** The reading-view palette; `BLACK` is true black for OLED, `EINK` high contrast. */
@Serializable
enum class ReaderTheme {
    @SerialName("system") SYSTEM,
    @SerialName("light") LIGHT,
    @SerialName("dark") DARK,
    @SerialName("black") BLACK,
    @SerialName("eink") EINK,
}

/** The reading-view face. */
@Serializable
enum class ReaderFont {
    @SerialName("serif") SERIF,
    @SerialName("sans") SANS,
    @SerialName("dyslexic") DYSLEXIC,
}

/**
 * Per-feed reading-view override. Every member is optional; a null member
 * inherits the account-wide setting.
 */
@Serializable
data class ReaderOverride(
    val theme: ReaderTheme? = null,
    val measure: Long? = null,
    val font: ReaderFont? = null,
)

/**
 * The reader settings document. [rev] is the server's change-log revision and
 * is read-only for clients; it is never part of a PUT body.
 */
@Serializable
data class ReaderSettings(
    @SerialName("reading_mode") val readingMode: ReadingMode = ReadingMode.RIVER,
    @SerialName("today_decay_days") val todayDecayDays: Long = 2,
    @SerialName("single_key_shortcuts") val singleKeyShortcuts: Boolean = true,
    val theme: ReaderTheme = ReaderTheme.SYSTEM,
    val measure: Long = 66,
    val font: ReaderFont = ReaderFont.SERIF,
    @SerialName("feed_overrides") val feedOverrides: Map<String, ReaderOverride> = emptyMap(),
    val rev: Long = 0,
    @SerialName("show_only_unread") val showOnlyUnread: Boolean = false,
)

/** The effective reading view after per-feed inheritance. */
data class ReaderTypography(
    val theme: ReaderTheme,
    val measure: Long,
    val font: ReaderFont,
)

/**
 * Resolves the typography for [feedId]: each override member wins over the
 * account-wide setting, and a null member inherits. A null or non-positive
 * feed ID is the account-wide setting.
 */
fun ReaderSettings.typographyFor(feedId: Long?): ReaderTypography {
    val override = feedId?.takeIf { it > 0 }?.let { feedOverrides[it.toString()] }
    return ReaderTypography(
        theme = override?.theme ?: theme,
        measure = override?.measure ?: measure,
        font = override?.font ?: font,
    )
}

/**
 * Exact unread totals from `GET /v1/entries:counts`. [bySubscription] is
 * keyed by the decimal subscription ID; counts are int64 and never derived
 * by paging entries.
 */
@Serializable
data class UnreadCounts(
    @SerialName("by_subscription") val bySubscription: Map<Long, Long> = emptyMap(),
    val later: Long = 0,
    val kept: Long = 0,
    val total: Long = 0,
)

/**
 * PUT body for `/v1/settings`: every reader field plus the supplied op ID.
 * [ReaderSettings.rev] is deliberately absent.
 */
@Serializable
data class ReaderSettingsBody(
    @SerialName("reading_mode") val readingMode: ReadingMode,
    @SerialName("today_decay_days") val todayDecayDays: Long,
    @SerialName("single_key_shortcuts") val singleKeyShortcuts: Boolean,
    val theme: ReaderTheme,
    val measure: Long,
    val font: ReaderFont,
    @SerialName("feed_overrides") val feedOverrides: Map<String, ReaderOverride> = emptyMap(),
    @SerialName("op_id") val opId: String,
    @SerialName("show_only_unread") val showOnlyUnread: Boolean = false,
)

/** Server-side ranges from internal/service/settings.go. */
internal object ReaderSettingsLimits {
    const val MIN_MEASURE = 62L
    const val MAX_MEASURE = 72L
    const val MIN_TODAY_DECAY_DAYS = 1L
    const val MAX_TODAY_DECAY_DAYS = 365L
    const val MAX_FEED_OVERRIDES = 512
    const val MAX_FEED_OVERRIDE_KEY_BYTES = 128
    const val MAX_OP_ID_BYTES = 1024
}

internal fun validateReaderSettings(settings: ReaderSettings, opId: String) {
    require(opId.isNotEmpty() && opId.toByteArray(Charsets.UTF_8).size <= ReaderSettingsLimits.MAX_OP_ID_BYTES) { "op_id out of range" }
    require(settings.measure in ReaderSettingsLimits.MIN_MEASURE..ReaderSettingsLimits.MAX_MEASURE) { "measure out of range" }
    require(settings.todayDecayDays in ReaderSettingsLimits.MIN_TODAY_DECAY_DAYS..ReaderSettingsLimits.MAX_TODAY_DECAY_DAYS) {
        "today_decay_days out of range"
    }
    require(settings.feedOverrides.size <= ReaderSettingsLimits.MAX_FEED_OVERRIDES) { "feed_overrides too large" }
    for ((key, override) in settings.feedOverrides) {
        val id = key.toLongOrNull()
        require(key.length <= ReaderSettingsLimits.MAX_FEED_OVERRIDE_KEY_BYTES && id != null && id > 0 && id.toString() == key) {
            "invalid feed_overrides key"
        }
        require(override.measure == null || override.measure in ReaderSettingsLimits.MIN_MEASURE..ReaderSettingsLimits.MAX_MEASURE) {
            "feed_overrides measure out of range"
        }
    }
}

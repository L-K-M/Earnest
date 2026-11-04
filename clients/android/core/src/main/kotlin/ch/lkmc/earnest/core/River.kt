package ch.lkmc.earnest.core

import kotlinx.serialization.json.*
import java.time.Duration
import java.time.Instant

/** Pure membership over the persisted server projections and effective state. */
object River {
    private const val DEFAULT_DECAY_DAYS = 2L
    private const val WEEK_DECAY_DAYS = 7L
    private const val MAX_DECAY_DAYS = 365L
    const val MUTE = "mute"
    const val SETTINGS = "settings"

    fun contains(entry: CachedEntry, state: State, subscription: SyncObject?, resources: List<SyncObject>, now: Instant): Boolean {
        if (entry.deleted || subscription == null || subscription.deleted) return false
        if (state.seen.value != JsonPrimitive(SyncEngine.VALUE_UNSEEN) || state.shelf.value != JsonPrimitive(SyncEngine.VALUE_NONE)) return false
        val fields = fields(subscription)
        if (fields["muted"]?.jsonPrimitive?.booleanOrNull == true) return false
        val firstSeen = instant(entry.firstSeenAt) ?: return false
        val since = fields["initial_since"]?.jsonPrimitive?.contentOrNull?.let(::instant) ?: return false
        if (firstSeen < since) return false
        val through = fields["initial_through_seq"]?.jsonPrimitive?.longOrNull ?: return false
        val published = entry.publishedAt?.let(::instant)
        if (entry.seq <= through && published != null && published < since) return false
        if (resources.any { matchesMute(it, entry) }) return false
        val cadence = fields["cadence"]?.jsonPrimitive?.contentOrNull
        if (cadence == "now") return true
        val configured = resources.firstOrNull { it.kind == SETTINGS && !it.deleted }
            ?.let(::fields)?.get("today_decay_days")?.jsonPrimitive?.longOrNull
        val todayDays = configured?.takeIf { it in 1..MAX_DECAY_DAYS } ?: DEFAULT_DECAY_DAYS
        val days = if (cadence == "week") WEEK_DECAY_DAYS else todayDays
        return firstSeen >= now.minus(Duration.ofDays(days))
    }

    fun cadence(subscription: SyncObject): String = fields(subscription)["cadence"]?.jsonPrimitive?.contentOrNull ?: "today"

    private fun matchesMute(resource: SyncObject, entry: CachedEntry): Boolean {
        if (resource.kind != MUTE || resource.deleted) return false
        val fields = fields(resource)
        val value = fields["value"]?.jsonPrimitive?.contentOrNull ?: return false
        return when (fields["kind"]?.jsonPrimitive?.contentOrNull) {
            "author" -> entry.author.lowercase(java.util.Locale.ROOT) == value.lowercase(java.util.Locale.ROOT)
            "keyword" -> entry.title.lowercase(java.util.Locale.ROOT).contains(value.lowercase(java.util.Locale.ROOT))
            else -> false
        }
    }

    private fun fields(resource: SyncObject): JsonObject =
        runCatching { Json.parseToJsonElement(resource.raw).jsonObject }.getOrDefault(JsonObject(emptyMap()))

    private fun instant(value: String): Instant? = runCatching { Instant.parse(value) }.getOrNull()
}

package ch.lkmc.earnest

import java.time.Instant

internal fun Long.relativeTime(): String {
    val minutes = java.time.Duration.between(Instant.ofEpochMilli(this), Instant.now()).toMinutes()
    return when {
        minutes < 1 -> "just now"
        minutes < 60 -> "${minutes}m ago"
        minutes < 60 * 24 -> "${minutes / 60}h ago"
        else -> "${minutes / (60 * 24)}d ago"
    }
}

internal fun String.relativeTime(): String = runCatching {
    val instant = Instant.parse(this)
    val minutes = java.time.Duration.between(instant, Instant.now()).toMinutes()
    when {
        minutes < 1 -> "just now"
        minutes < 60 -> "${minutes}m ago"
        minutes < 60 * 24 -> "${minutes / 60}h ago"
        else -> "${minutes / (60 * 24)}d ago"
    }
}.getOrDefault("")

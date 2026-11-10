package ch.lkmc.earnest

import ch.lkmc.earnest.core.OpmlSummary

/** The summary line of an OPML import report, naming what was imported. */
internal fun opmlSummaryLine(summary: OpmlSummary): String {
    val parts = mutableListOf<String>()
    if (summary.subscriptions > 0) parts += "${summary.subscriptions} ${if (summary.subscriptions == 1) "feed" else "feeds"}"
    if (summary.folders > 0) parts += "${summary.folders} ${if (summary.folders == 1) "folder" else "folders"}"
    val done = if (parts.isEmpty()) "Nothing was imported." else "Imported ${parts.joinToString(" and ")}."
    return if (summary.errors.isEmpty()) done
    else "$done ${summary.errors.size} ${if (summary.errors.size == 1) "outline" else "outlines"} failed."
}

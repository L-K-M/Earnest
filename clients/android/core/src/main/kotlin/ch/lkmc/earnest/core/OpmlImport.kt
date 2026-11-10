package ch.lkmc.earnest.core

import kotlinx.serialization.Serializable

/** The largest file `POST /v1/opml:import` accepts; larger ones get 413. */
const val MAX_OPML_IMPORT_BYTES: Int = 8 * 1024 * 1024

/**
 * Wire models and summary for OPML import and export. Field names mirror the
 * server's OPMLResult schema: every input outline reports its own outcome, a
 * subscription or folder, a deferred initial fetch, or an error.
 */
@Serializable
data class OpmlResult(
    val path: List<Long>? = null,
    val deferred: Boolean = false,
    val subscription: Subscription? = null,
    val folder: Folder? = null,
    val error: String? = null,
)

/** What an OPML import did, counted for the report. */
data class OpmlSummary(
    val subscriptions: Int,
    val folders: Int,
    val deferred: Int,
    val errors: List<String>,
)

fun summarizeOpml(results: List<OpmlResult>): OpmlSummary = OpmlSummary(
    subscriptions = results.count { it.subscription != null },
    folders = results.count { it.folder != null },
    deferred = results.count { it.deferred },
    errors = results.mapNotNull { it.error },
)

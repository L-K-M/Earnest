package ch.lkmc.earnest.core

import java.io.ByteArrayOutputStream
import java.io.InputStream
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** The largest export `POST /v1/kept:import` accepts; larger ones get 413. */
const val MAX_KEPT_IMPORT_BYTES: Int = 64 * 1024 * 1024

/**
 * Reads [input] up to [limit] + 1 bytes, so a caller can tell an export over
 * the limit from one at it without loading a huge file into memory.
 */
fun readAtMost(input: InputStream, limit: Int): ByteArray {
    val out = ByteArrayOutputStream()
    val buffer = ByteArray(64 * 1024)
    while (out.size() <= limit) {
        val read = input.read(buffer, 0, minOf(buffer.size, limit + 1 - out.size()))
        if (read < 0) break
        out.write(buffer, 0, read)
    }
    return out.toByteArray()
}

/**
 * Wire models and summary for `POST /v1/kept:import`. Field names mirror the
 * server's KeptImportReport and KeptImportResult schemas. [KeptImportResult.labels]
 * is a later server addition that older servers simply omit.
 */

@Serializable
data class KeptImportReport(
    val results: List<KeptImportResult>? = null,
    val subscribed: List<Subscription>? = null,
)

@Serializable
data class KeptImportResult(
    val index: Long,
    val status: String,
    @SerialName("entry_id") val entryId: Long? = null,
    @SerialName("subscription_id") val subscriptionId: Long? = null,
    @SerialName("feed_url") val feedUrl: String? = null,
    val title: String? = null,
    val error: String? = null,
    val labels: List<String> = emptyList(),
)

/** A feed whose articles were skipped, with how many were left out. */
data class KeptImportSkip(
    val feedUrl: String,
    val count: Int,
)

/** The counts behind an import's user-facing summary. */
data class KeptImportSummary(
    val added: Int = 0,
    val existing: Int = 0,
    val unkept: Int = 0,
    val gone: Int = 0,
    val skipped: List<KeptImportSkip> = emptyList(),
    val failed: List<String> = emptyList(),
    val subscribed: Int = 0,
    /** Articles the import added at least one of the export's labels to. */
    val labelled: Int = 0,
)

/**
 * Counts an import's results the way the settings screen reports them. Null
 * lists are empty. Skipped feeds are ordered by count descending, then URL.
 */
fun summarizeKeptImport(report: KeptImportReport): KeptImportSummary {
    var added = 0
    var existing = 0
    var unkept = 0
    var gone = 0
    val skipped = mutableMapOf<String, Int>()
    val failed = mutableListOf<String>()
    var labelled = 0
    for (result in report.results.orEmpty()) {
        if (result.labels.isNotEmpty()) labelled++
        when (result.status) {
            "created" -> added++
            "matched", "unchanged" -> existing++
            "unkept" -> unkept++
            "gone" -> gone++
            "not_subscribed" -> {
                val feedUrl = result.feedUrl.orEmpty()
                skipped[feedUrl] = (skipped[feedUrl] ?: 0) + 1
            }
            "failed" -> {
                val title = result.title ?: "Article ${result.index + 1}"
                failed += "$title: ${result.error ?: "could not be imported"}"
            }
        }
    }
    return KeptImportSummary(
        added = added,
        existing = existing,
        unkept = unkept,
        gone = gone,
        skipped = skipped.map { (feedUrl, count) -> KeptImportSkip(feedUrl, count) }
            .sortedWith(compareByDescending<KeptImportSkip> { it.count }.thenBy { it.feedUrl }),
        failed = failed,
        subscribed = report.subscribed?.size ?: 0,
        labelled = labelled,
    )
}

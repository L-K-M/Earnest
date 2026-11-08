package ch.lkmc.earnest.core

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Search wire models. Field names mirror the server's search and saved-search
 * schemas; OpenApiContractTest checks them against api/openapi.json.
 */

/** Retrieval mode, encoded on the wire as `fulltext` or `hybrid`. */
@Serializable
enum class SearchMode {
    @SerialName("fulltext") FULLTEXT,
    @SerialName("hybrid") HYBRID,
    ;

    /** The exact value used by both JSON bodies and the `mode` query parameter. */
    val wireName: String
        get() = when (this) {
            FULLTEXT -> "fulltext"
            HYBRID -> "hybrid"
        }
}

/**
 * Search narrowing. Only [mode] has a default; absent subscription, folder
 * and limit mean "all" and "server default" on the server.
 */
@Serializable
data class SearchFilter(
    val mode: SearchMode = SearchMode.FULLTEXT,
    @SerialName("subscription_id") val subscriptionId: Long? = null,
    @SerialName("folder_id") val folderId: Long? = null,
    @SerialName("limit") val limit: Long? = null,
)

/**
 * One search hit. Entries and agent notes share the wire shape; a note is
 * distinguished by [kind] and carries [annotationId] and [agentName], and is
 * always untrusted because its text comes from an agent.
 */
@Serializable
data class SearchMatch(
    val kind: String,
    @SerialName("entry_id") val entryId: Long,
    @SerialName("subscription_id") val subscriptionId: Long,
    @SerialName("feed_id") val feedId: Long,
    val seq: Long,
    val title: String,
    val url: String,
    val snippet: String,
    val rank: Double,
    val untrusted: Boolean,
    @SerialName("annotation_id") val annotationId: Long? = null,
    @SerialName("agent_name") val agentName: String? = null,
) {
    /**
     * Stable identity for lists and deduplication. The kind separates an
     * entry from a note attached to the same entry, and the annotation id
     * distinguishes one note from another. A null annotation id is the same
     * identity as the server's omitted zero.
     */
    val key: String
        get() = "$kind:$entryId:${annotationId ?: 0}"
}

/**
 * A search page. [matches] is nullable because the server marks an
 * unavailable semantic path in-band with a null/empty match list and a
 * [status] of `disabled` or `paused`; a transport failure still throws.
 */
@Serializable
data class SearchPage(
    val matches: List<SearchMatch>?,
    @SerialName("next_cursor") val nextCursor: String? = null,
    val status: String? = null,
)

/** A named, stored search. */
@Serializable
data class SavedSearch(
    val id: Long,
    val name: String,
    val query: String,
    val filters: SearchFilter,
)

/** Creation body for a saved search; unlike most mutations it has no op_id. */
@Serializable
data class SavedSearchBody(
    val name: String,
    val query: String,
    val filters: SearchFilter,
)

/** Rename body; only the name is mutable. */
@Serializable
data class SavedSearchRename(
    val name: String,
)

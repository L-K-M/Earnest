package ch.lkmc.earnest.core

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement

/**
 * Wire models for the Earnest native API. Field names mirror
 * ../../api/openapi.json; OpenApiContractTest checks every model against that
 * document, so a server-side rename fails the build here.
 *
 * The endpoints this app needs are small enough to keep explicit.
 */

@Serializable
data class Revision(
    val generation: String,
    val rev: Long,
)

@Serializable
data class Register(
    val value: JsonElement,
    val rev: Long,
    @SerialName("occurred_at") val occurredAt: String,
    val actor: Long,
    val source: String,
)

@Serializable
data class State(
    val principal: Long,
    @SerialName("entry_id") val entryId: Long,
    val seen: Register,
    val shelf: Register,
    val kept: Register,
    @SerialName("state_rev") val stateRev: Long,
)

@Serializable
data class StateWrite(
    @SerialName("op_id") val opId: String,
    val principal: Long,
    @SerialName("entry_id") val entryId: Long,
    val register: String,
    val value: JsonElement,
    @SerialName("base_rev") val base: Revision,
    @SerialName("occurred_at") val occurredAt: String,
    val kind: String,
)

@Serializable
data class Result(
    @SerialName("op_id") val opId: String,
    val status: String,
    val state: State,
    val replay: Boolean,
)

@Serializable
data class RangeResult(
    val status: String,
    @SerialName("through_seq") val throughSeq: Long,
    val subscriptions: List<Long>?,
    val except: List<Long>?,
    val results: List<Result>?,
    val replay: Boolean,
)

@Serializable
data class ChangeRecord(
    val rev: Long,
    val type: String,
    val payload: JsonElement,
)

@Serializable
data class ChangesPage(
    val changes: List<ChangeRecord>?,
    val next: Revision,
    @SerialName("next_cursor") val nextCursor: String,
)

@Serializable
data class BootstrapPage(
    val items: List<JsonElement>?,
    @SerialName("next_id") val nextId: Long,
    @SerialName("next_principal") val nextPrincipal: Long,
    @SerialName("next_view") val nextView: String,
    @SerialName("has_more") val hasMore: Boolean,
)

@Serializable
data class Scope(
    val kind: String,
    val ids: List<Long>? = null,
)

@Serializable
data class RangeWrite(
    val write: StateWrite,
    val scope: Scope,
    @SerialName("through_seq") val throughSeq: Long,
)

@Serializable
data class ThroughBody(
    val write: StateWrite,
    val scope: Scope,
    @SerialName("through_seq") val throughSeq: Long? = null,
    val at: String? = null,
)

@Serializable
data class Entry(
    val id: Long,
    @SerialName("subscription_id") val subscriptionId: Long,
    @SerialName("feed_id") val feedId: Long,
    val seq: Long,
    @SerialName("first_seen_at") val firstSeenAt: String,
    @SerialName("published_at") val publishedAt: String? = null,
    @SerialName("updated_at") val updatedAt: String? = null,
    val title: String,
    val url: String,
    val author: String,
    val rev: Long,
    val deleted: Boolean,
    val state: State? = null,
    @SerialName("thumbnail_url") val thumbnailUrl: String? = null,
)

@Serializable
data class EntriesPage(
    val entries: List<Entry>?,
    @SerialName("next_cursor") val nextCursor: String? = null,
    /** Full match count for a dry run; may exceed `entries.size` when the sample is capped. */
    @SerialName("total") val total: Long? = null,
)

@Serializable
data class FeedHealth(
    @SerialName("last_success_at") val lastSuccessAt: String? = null,
    @SerialName("last_attempt_at") val lastAttemptAt: String? = null,
    @SerialName("next_fetch_at") val nextFetchAt: String,
    @SerialName("error_class") val errorClass: String? = null,
    @SerialName("error_message") val errorMessage: String? = null,
    @SerialName("consecutive_failures") val consecutiveFailures: Long,
    @SerialName("http_status") val httpStatus: Long,
    @SerialName("posts_per_day") val postsPerDay: Double,
    val firehose: Boolean,
)

@Serializable
data class Subscription(
    @SerialName("initial_since") val initialSince: String? = null,
    @SerialName("initial_through_seq") val initialThroughSeq: Long,
    @SerialName("initial_fetch_pending") val initialFetchPending: Boolean,
    val id: Long,
    @SerialName("feed_id") val feedId: Long,
    val title: String,
    val url: String,
    @SerialName("site_url") val siteUrl: String,
    @SerialName("folder_id") val folderId: Long,
    val cadence: String,
    val extract: String,
    val rev: Long,
    val deleted: Boolean,
    val private: Boolean,
    // Older servers do not project the flag; absent means not muted.
    val muted: Boolean = false,
    val health: FeedHealth,
    @SerialName("icon_url") val iconUrl: String? = null,
)

@Serializable
data class CredentialsBody(
    val username: String,
    val password: String,
    val headers: Map<String, String>? = null,
)

@Serializable
data class SubscriptionBody(
    val url: String,
    @SerialName("op_id") val opId: String,
    val title: String? = null,
    @SerialName("folder_id") val folderId: Long? = null,
    val cadence: String? = null,
    val extract: String? = null,
    val credentials: CredentialsBody? = null,
)

@Serializable
data class SubscriptionPatch(
    @SerialName("op_id") val opId: String,
    val title: String? = null,
    @SerialName("folder_id") val folderId: Long? = null,
    val cadence: String? = null,
    val extract: String? = null,
    val credentials: CredentialsBody? = null,
    @SerialName("credentials_set") val credentialsSet: Boolean? = null,
)

@Serializable
data class Folder(
    val id: Long,
    val name: String,
    val rev: Long,
    val deleted: Boolean,
)

@Serializable
data class Label(
    val id: Long,
    val name: String,
    val rev: Long,
    val deleted: Boolean,
)

@Serializable
data class LabelMembership(
    val id: Long,
    @SerialName("label_id")
    val labelId: Long,
    @SerialName("entry_id")
    val entryId: Long,
    val rev: Long,
    val deleted: Boolean,
)

@Serializable
data class Position(
    val principal: Long,
    val view: String,
    val seq: Long,
    val rev: Long,
)

@Serializable
data class PutPosition(
    val seq: Long,
    @SerialName("op_id") val opId: String,
)

@Serializable
data class NameBody(
    val name: String,
    @SerialName("op_id") val opId: String,
)

@Serializable
data class OpBody(
    @SerialName("op_id") val opId: String,
)

@Serializable
data class Annotation(
    val id: Long,
    @SerialName("principal_id") val principalId: Long,
    @SerialName("author_name") val authorName: String,
    @SerialName("created_at") val createdAt: String,
    val kind: String,
    @SerialName("entry_ids") val entryIds: List<Long>?,
    val value: String,
    val rev: Long,
    val untrusted: Boolean,
    val deleted: Boolean,
)

@Serializable
data class AnnotationBody(
    @SerialName("op_id") val opId: String,
    val value: String,
)

@Serializable
data class MeResult(
    @SerialName("user_id") val userId: Long,
    @SerialName("principal_id") val principalId: Long,
    val kind: String,
    val scopes: List<String>?,
)

@Serializable
data class Token(
    val id: Long,
    @SerialName("principal_id") val principalId: Long,
    val name: String,
    val kind: String,
    val audience: String,
    val scopes: List<String>? = null,
    @SerialName("daily_budget") val dailyBudget: Long = 0,
    @SerialName("expires_at") val expiresAt: String? = null,
    @SerialName("last_used_at") val lastUsedAt: String? = null,
    @SerialName("revoked_at") val revokedAt: String? = null,
    @SerialName("created_at") val createdAt: String = "",
)

@Serializable
data class DeviceStart(
    @SerialName("client_name") val clientName: String,
)

@Serializable
data class DeviceResult(
    @SerialName("device_code") val deviceCode: String,
    @SerialName("user_code") val userCode: String,
    @SerialName("verification_uri") val verificationUri: String,
    val interval: Long,
    @SerialName("expires_in") val expiresIn: Long,
)

@Serializable
data class DevicePoll(
    @SerialName("device_code") val deviceCode: String,
)

@Serializable
data class DeviceToken(
    @SerialName("access_token") val accessToken: String,
    @SerialName("token_type") val tokenType: String,
)

@Serializable
data class DeviceBody(
    val kind: String,
    val endpoint: String,
    val p256dh: String,
    @SerialName("auth_secret") val authSecret: String,
    @SerialName("show_text") val showText: Boolean,
    @SerialName("op_id") val opId: String,
)

@Serializable
data class Device(
    val id: Long,
    val kind: String,
    val endpoint: String,
    @SerialName("show_text") val showText: Boolean,
    val rev: Long,
    @SerialName("created_at") val createdAt: String,
)

@Serializable
data class Content(
    @SerialName("entry_id") val entryId: Long,
    val html: String,
    val text: String,
    @SerialName("sanitizer_version") val sanitizerVersion: Long,
    val source: String,
)

// Added on the server in parallel with this client; not yet in openapi.json.
@Serializable
data class ContentUrl(
    val url: String,
    @SerialName("expires_at") val expiresAt: String,
)

@Serializable
data class Activity(
    val id: Long,
    @SerialName("principal_id") val principalId: Long,
    @SerialName("principal_name") val principalName: String,
    val action: String,
    val target: JsonElement?,
    val before: JsonElement?,
    val after: JsonElement?,
    val generation: String,
    @SerialName("created_at") val createdAt: String,
    @SerialName("next_action") val nextAction: String,
    val reason: String? = null,
)

@Serializable
data class ActivityGroup(
    @SerialName("principal_id") val principalId: Long,
    @SerialName("principal_name") val principalName: String,
    val activities: List<Activity>?,
)

@Serializable
data class ActivityPage(
    val activities: List<Activity>? = null,
    val groups: List<ActivityGroup>? = null,
)

@Serializable
data class NotificationBudget(
    @SerialName("used_today") val usedToday: Long,
    @SerialName("limit_today") val limitToday: Long,
)

@Serializable
data class NotificationWhy(
    val sender: String,
    val budget: NotificationBudget? = null,
    @SerialName("matching_rule_id") val matchingRuleId: Long? = null,
)

@Serializable
data class Notification(
    val id: Long,
    @SerialName("principal_id") val principalId: Long,
    val muted: Boolean,
    val note: String,
    @SerialName("entry_ids") val entryIds: List<Long>?,
    val category: String,
    val status: String,
    val budget: NotificationBudget?,
    val why: NotificationWhy?,
    val generation: String,
    @SerialName("created_at") val createdAt: String,
    val replay: Boolean,
)

@Serializable
data class Problem(
    val type: String,
    val title: String,
    val status: Long,
    val error: String? = null,
    val detail: String? = null,
    val budget: NotificationBudget? = null,
)

@Serializable
class Empty {
    // The server returns an empty object; kept explicit so calls read clearly.
    override fun equals(other: Any?): Boolean = other is Empty
    override fun hashCode(): Int = 0
}

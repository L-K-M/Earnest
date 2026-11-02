# Earnest API — design research (2026-11-01)

Scope: survey of existing feed-sync APIs, then a concrete native design for a one-developer, self-hosted RSS reader with web/macOS/Android clients and an LLM/agent-facing API.

## Part 1 — Survey

| API | Auth | Sync model | Flaws | Clients |
|---|---|---|---|---|
| Google Reader (unofficial, reimplemented) | ClientLogin `Email`/`Passwd`; then `Authorization: GoogleLogin auth=…`; write token `T` | Stream IDs (`user/-/state/com.google/*`, `feed/N`, labels); `stream/items/ids` id-lists + continuation; batch `edit-tag`; `unread-count` | No canonical spec (original died 2013); implementations diverge: Miniflux documents `feed/` = numeric id in reads but absolute URL for `ac=subscribe`, `c` continuation is a SQL offset not opaque, unknown endpoints return `[]` with 200 ([docs](https://miniflux.app/docs/google_reader.html), [package doc](https://pkg.go.dev/miniflux.app/v2/internal/googlereader)) | Reeder, Fiery Feeds, NetNewsWire, Capy Reader, RSS Guard, FeedMe, FocusReader, Readrops, Read You, Newsflash, Newsboat, Vienna ([FreshRSS list](https://freshrss.github.io/FreshRSS/en/developers/06_GoogleReader_API.html)) |
| Fever | POST `api_key` = md5(email:password) to single endpoint; reads via query args | `since_id`/`max_id` watermark paging, 50 items/page | api_key is long-lived and unrevocable per device; writes limited to read/starred; no subscription management, no mark-unread; official docs only survive as mirrors ([FreshRSS mirror](https://freshrss.github.io/FreshRSS/en/developers/06_Fever_API.html)) | Reeder Classic, Unread, FeedMe, FocusReader, Newsflash, Fluent Reader, Lire, ReadKit, Read You |
| Feedbin v2 | HTTP Basic | Page pagination (100/page, `Link` headers) + `since` timestamp + `ids` batch; ETag/Last-Modified conditional GETs | No deletion tombstones or subscription delta; page offsets shift under concurrent writes; no push channel ([feedbin/feedbin-api](https://github.com/feedbin/feedbin-api/blob/master/README.md)) | NetNewsWire, Reeder, Fiery Feeds |
| Miniflux v1 | Basic or `X-Auth-Token` per-app keys | Full-list + `limit/direction/order/status` filters; bookmark toggle; refresh triggers; OPML import/export | No cursor or delta sync — clients re-fetch lists; no realtime; `flush-history` deletes read entries ([miniflux.app/docs/api](https://miniflux.app/docs/api.html)) | Official Go/Python clients, NewsFlash, PoweReader |
| NewsBlur | `newsblur_sessionid` cookie, or OAuth (client IDs by emailing the founder) | Page-number paging; story hashes as ids; form-encoded POSTs | No documented rate limits or idempotency; deprecated endpoints still live; unversioned paths ([newsblur.com/api](https://www.newsblur.com/api)) | Official apps mainly |
| Feedly Cloud | OAuth2; self-service tokens now Enterprise-only ([authorization](https://developers.feedly.com/reference/authorization)) | `continuation`+count paging; global resource ids; markers API; 100k req/month/token ([limits](https://developers.feedly.com/reference/request-limits)) | Closed to new free developers; ToS forbids competing on search/alerts and mandates showing sponsored results ([ToS](https://developers.feedly.com/reference/feedly-api-terms-of-service)) | Many (vendor's claim) |
| Inoreader | OAuth2 (`read`/`read write`) + `AppId`/`AppKey` app keys | Google Reader-compatible surface at `/reader/api/0` | App-key model; API requires Pro plan or public-app status ([developers](https://www.inoreader.com/developers/)) | News+, FeedMe, gReader clones |
| Microsub | Bearer (IndieAuth) | Single endpoint, `action=` dispatch; channels (reserved `notifications`); `before`/`after` cursors; JF2 items | Still an Editor's Draft, not a W3C REC; tiny client base ([indieweb.org](https://indieweb.org/Microsub-spec)) | Indigenous, Monocle |
| JSON Feed | n/a (content format) | n/a; top-level `hubs[]` advertises WebSub/rssCloud endpoints | Partial adoption; spec tells publishers to keep RSS primary ([jsonfeed.org/version/1.1](https://www.jsonfeed.org/version/1.1/)) | NetNewsWire, Feedly, others |
| OPML | none | Snapshot import/export of `type="rss"` outlines | 2.0 frozen since 2007; no sync semantics; folder structure not guaranteed preserved ([opml.org](http://opml.org/spec2.opml)) | Everything, for import only |
| WebSub | `hub.secret` → HMAC `X-Hub-Signature` | Publisher→hub→subscriber POST push; intent verification; leases (10 days recommended) | Hub dependency and availability; verification complexity; few feeds advertise hubs; carries no read state ([w3.org/TR/websub](https://www.w3.org/TR/websub/)) | Server-side (WordPress, YouTube) |
| rssCloud | `<cloud>` element; XML-RPC or REST notify | Subscriber registers at cloud; cloud pings on update; 24h renewal | Effectively dormant, near-zero modern use ([rsscloud.org](https://rsscloud.org/walkthrough/)) | Legacy |

## Part 2 — Recommendations

**(a) Compatibility APIs.** Ship none at first: you own all three clients, so compatibility only adds surface area. Second, ship a *documented subset* of the Google Reader API (the only compatibility layer with a live client ecosystem): `ClientLogin` (or better, token via header), `subscription/list`, `stream/items/ids`, `stream/items/contents`, `edit-tag` (read/starred), `unread-count`, `mark-all-as-read`. Copy Miniflux's honesty: document deviations rather than pretend. Skip Fever (dying, weak), Microsub (draft, different data model — but steal its channel + `before`/`after` cursor design), Feedly/Inoreader/NewsBlur compat (closed or pointless). On the *ingest* side: OPML import/export, parse JSON Feed 1.1, and subscribe to WebSub hubs where advertised (cheap polling reduction); ignore rssCloud.

**(b) Native REST shape.** Versioned path `/v1`, JSON only, RFC 9457 problem+json errors with stable `type` URIs. Delta sync is a **change feed**, not list re-fetching:

- Every mutation appends to a per-user log with a monotonic `revision` (logical clock, not wall time).
- `GET /v1/changes?cursor=…` returns ops (`entry.created`, `entry.read`, `entry.unread`, `entry.starred`, `subscription.created`, `subscription.deleted` …) with tombstones (`{id, deleted: true, revision}`), plus `next_cursor` and `has_more`. Cursor is opaque, encodes revision; server retains 30 days.
- **Read-state conflict rules:** read/starred are monotonic and idempotent (marking twice = one op). Mark-*unread* and un-star send `If-Match: "<entry-revision>"`; stale revision → `409` with current state, client re-syncs. Never silently resurrect.
- **Idempotency:** all unsafe requests accept `Idempotency-Key`; server stores key+response hash 24h and replays. Bulk endpoints (`POST /v1/entries/read`) accept up to 500 ids and are naturally idempotent.
- **Pagination:** cursor-based everywhere (`limit` ≤ 200 default 50); no offsets. **ETags** on single-resource GETs and `If-None-Match` → 304; `ETag` also usable as optimistic-concurrency token.

**(c) Auth.** Layered:
1. **Personal access tokens** (`Authorization: Bearer …`, stored hashed, per-token scopes, optional expiry) for your own clients and for agents — the primary path for a self-hosted single-user install.
2. **OAuth 2.1 + PKCE (S256)** for third-party apps, per MCP's current authorization spec: RFC 9728 protected-resource metadata, RFC 8707 resource indicators, RFC 9207 `iss` validation ([spec](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization)).
3. **Device flow (RFC 8628)** for CLI and TV-class clients.
Scopes are per-capability: `feeds:read entries:read entries:write notify:send admin`. **Agent tokens** are a distinct token class: created only through an explicit consent screen listing scopes and budget, cannot use cookie auth, get tighter rate limits and a hard notification budget, and are labeled in the UI so they're revocable at a glance.

**(d) Realtime.** SSE (`GET /v1/events`, `Last-Event-ID` resume, 25s heartbeats) is the default for web/macOS/Android clients: simple, works through Tailscale and reverse proxies, auto-reconnects. WebSocket only if you later need client→server acks. For server-to-server automation, **webhooks following Standard Webhooks**: `webhook-id`/`webhook-timestamp`/`webhook-signature` (HMAC-SHA256 over `id.timestamp.payload`), retry with backoff, endpoint secrets ([spec](https://github.com/standard-webhooks/standard-webhooks/blob/main/spec/standard-webhooks.md)).

**(e) Push.** Transport matrix: APNs for macOS/iOS (4KB payload, token JWT auth 1h rotation, priority 10/5, collapse-id, TTL — [Apple docs](https://developer.apple.com/library/archive/documentation/NetworkingInternet/Conceptual/RemoteNotificationsPG/CommunicatingwithAPNs.html)); FCM for Android (4096B, 600k msg/min per project default quota, per-device rate caps — [Firebase](https://firebase.google.com/docs/cloud-messaging/throttling-and-quotas)); Web Push for browsers (RFC 8030/8291/8292, 3993-byte max plaintext, 404/410 = delete subscription — [web.dev](https://web.dev/articles/push-notifications-web-push-protocol)); **UnifiedPush** for Android without Google ([spec](https://unifiedpush.org/developers/spec/android/)), with a self-hosted **ntfy** as distributor/relay — the natural fit for a NAS deployment; ntfy is also the generic relay when a device has no app ([ntfy docs](https://docs.ntfy.sh/publish/); the ntfy.sh free tier is 250 msg/day, vendor pricing — self-host to avoid the cap).

**Sending a notification safely** (this is where an agent can hurt you) — server-enforced, not client-enforced:
- `Idempotency-Key` + server-side `dedupe_key` (same key within N minutes collapses).
- **Quiet hours**: per-user local-time windows; sends outside are held (`status: scheduled`) or downgraded to silent, never dropped silently.
- **Per-token budget**: daily/monthly cap per token and per device, plus per-minute rate limit; over-budget → `429` with `Retry-After` and a budget body, not silent success.
- Priority (`high`/`normal`/`low`), TTL/expiry, collapse-id/thread-id mapping, and per-category muting are all parameters of the send call.
- Returns `202` with a delivery id; final states arrive via SSE/webhook (`sent`, `suppressed:quiet_hours`, `suppressed:budget`, `failed`).

**(f) MCP.** Current spec revision is **2026-07-28** ([versioning](https://modelcontextprotocol.io/specification/versioning), [changelog](https://modelcontextprotocol.io/specification/2026-07-28/changelog)). Recent changes that matter for a remote OAuth server: MCP is now **stateless** — no `initialize` handshake, no `Mcp-Session-Id`; `server/discover` is mandatory; results carry `resultType` and cache hints (`ttlMs`, `cacheScope`); `resources/subscribe` is replaced by `subscriptions/listen`; server-initiated requests (Roots, Sampling — both now deprecated) are replaced by the Multi-Round-Trip pattern; **Dynamic Client Registration is deprecated in favor of Client ID Metadata Documents**; clients must use PKCE, resource indicators, and validate `iss`; HTTP+SSE transport is deprecated. Recommendation: **yes, MCP should be a thin layer over the REST API** — one tool per REST operation, resources over the same read endpoints, no second implementation of business logic. Publish `openapi.json` as the source of truth and generate tool schemas from it. Discoverability: `/llms.txt` (+ `/llms-full.txt`, spec v2 at [llmstxt.org](https://llmstxt.org/)), a `## Skills`-style listing, an MCP **Skills** extension listing (`io.modelcontextprotocol/skills`, [overview](https://modelcontextprotocol.io/extensions/skills/overview)) and a listing in the official MCP registry ([registry](https://registry.modelcontextprotocol.io/)).

## Concrete draft

**Resources**

| Resource | Key fields | Notes |
|---|---|---|
| subscription | id, feed_url, site_url, title, category_id, icon_url, etag, last_modified, next_check_at, error | feed-level cache headers exposed for debugging |
| category | id, name, position | flat, ordered |
| entry | id, feed_id, title, url, author, summary, content, published_at, read_at, starred_at, revision, enclosures[] | `revision` = optimistic-concurrency token |
| change op | revision, type, resource_id, data/tombstone, at | append-only per-user log |
| device | id, platform (apns/fcm/webpush/unifiedpush/ntfy), token, endpoint, quiet_hours, budget | push credentials |
| notification | id, target, title, body, category, priority, dedupe_key, status, deep_link | status: scheduled/sent/suppressed/failed |
| webhook | id, url, secret, events[], active | Standard Webhooks headers |

**Endpoints (33)**

Auth/account: `GET /v1/me`; `GET /v1/tokens`, `POST /v1/tokens`, `DELETE /v1/tokens/{id}`.
Subscriptions: `GET /v1/subscriptions`; `POST /v1/subscriptions`; `GET /v1/subscriptions/{id}`; `PATCH /v1/subscriptions/{id}`; `DELETE /v1/subscriptions/{id}`; `POST /v1/subscriptions/{id}/refresh`; `GET /v1/subscriptions/{id}/icon`.
Categories: `GET /v1/categories`; `POST /v1/categories`; `PATCH /v1/categories/{id}`; `DELETE /v1/categories/{id}`.
Entries: `GET /v1/entries` (filters: subscription_id, category_id, status=unread/read/starred, since, q, order); `GET /v1/entries/{id}`; `GET /v1/entries/{id}/content`; `POST /v1/entries/{id}/read`; `DELETE /v1/entries/{id}/read`; `POST /v1/entries/{id}/star`; `DELETE /v1/entries/{id}/star`; `POST /v1/entries/read` (bulk ≤500); `POST /v1/entries/read-all` (scope: subscription/category/all).
Counts & sync: `GET /v1/unread-counts`; `GET /v1/changes`.
OPML: `POST /v1/opml/import`; `GET /v1/opml/export`.
Realtime & webhooks: `GET /v1/events` (SSE); `GET /v1/webhooks`; `POST /v1/webhooks`; `DELETE /v1/webhooks/{id}`.
Notifications: `POST /v1/notifications`; `GET /v1/notifications/{id}`; `POST /v1/notifications/{id}/cancel`; `GET /v1/devices`; `POST /v1/devices`; `DELETE /v1/devices/{id}`.

**Delta sync example**

```http
GET /v1/changes?cursor=c_9f3a&limit=50 HTTP/1.1
Authorization: Bearer earnest_pat_xxx
Accept: application/json
```

```json
{
  "changes": [
    {"revision": 1042, "type": "entry.created", "at": "2026-11-01T08:00:11Z",
     "data": {"id": "e_8813", "subscription_id": "s_12", "title": "…", "url": "https://…",
              "published_at": "2026-11-01T07:58:00Z", "read_at": null}},
    {"revision": 1043, "type": "entry.read", "at": "2026-11-01T08:00:40Z",
     "data": {"id": "e_8813", "read_at": "2026-11-01T08:00:40Z"}},
    {"revision": 1044, "type": "entry.unread", "at": "2026-11-01T08:01:02Z",
     "data": {"id": "e_8710", "revision": 1039}},
    {"revision": 1045, "type": "subscription.deleted", "at": "2026-11-01T08:02:00Z",
     "tombstone": {"id": "s_9"}}
  ],
  "next_cursor": "c_1045",
  "has_more": false
}
```

A mark-unread whose `If-Match` revision is stale returns `409` with the entry's current `revision`; the client replays from its last cursor.

**Notification-send example**

```http
POST /v1/notifications HTTP/1.1
Authorization: Bearer earnest_pat_agent
Idempotency-Key: agent-2026-11-01-digest-001
Content-Type: application/json

{
  "title": "3 unread in Rust Blog",
  "body": "Async cancellation, simplified",
  "deep_link": "earnest://entries/e_8813",
  "category": "digest",
  "priority": "normal",
  "dedupe_key": "digest:rust-blog:2026-11-01",
  "devices": ["d_mac", "d_android"],
  "quiet_hours": "respect",
  "ttl_seconds": 86400
}
```

`202 Accepted` → `{"id": "n_551", "status": "scheduled", "deliver_after": "2026-11-01T09:00:00+01:00"}` (held by quiet hours); final state arrives on `/v1/events` or a webhook. Over budget → `429` with `{"type": "https://earnest.dev/problems/budget-exceeded", "budget": {"daily_remaining": 0, "resets_at": "…"}}`.

**MCP tools (thin wrappers over the endpoints above)**

| Tool | Description |
|---|---|
| `subscriptions_list` | List subscriptions with unread counts and categories |
| `subscriptions_add` | Subscribe to a feed URL (with discovery) |
| `subscriptions_remove` | Unsubscribe |
| `subscriptions_refresh` | Force a feed refresh |
| `categories_list` / `categories_update` | List or rename/reorder categories |
| `entries_list` | Query entries (unread/starred/since/search) |
| `entries_get` | Fetch one entry with content |
| `entries_mark_read` / `entries_mark_unread` | Set read state (single or bulk) |
| `entries_star` / `entries_unstar` | Set starred state |
| `entries_search` | Full-text search over entries |
| `unread_counts` | Unread counts per feed/category |
| `changes_sync` | Delta sync from a cursor (tombstones included) |
| `notifications_send` | Send a notification (budget/quiet-hours enforced) |
| `notifications_list` / `notifications_cancel` | Inspect or cancel scheduled notifications |
| `devices_list` / `devices_register` / `devices_remove` | Manage push targets |
| `opml_export` / `opml_import` | Export/import subscription lists |
| `digest_summarize` | Summarize unread entries since a timestamp (agent helper) |

Resources: `earnest://subscriptions/{id}`, `earnest://entries/{id}`, `earnest://changes?cursor=…`. Prompts: `triage_inbox`, `reading_digest`, `weekly_review`.

## Gaps

- Google Reader's original spec no longer exists; sync semantics above come from Miniflux/FreshRSS implementation docs, not a primary source.
- Fever's official docs are offline; the mirrored copy (via FreshRSS) may be incomplete.
- BazQux Reader (a commercial Google Reader-compatible service, repo seen at github.com/bazqux/bazqux-api) was not investigated.
- Feedly's free developer program end-date was not confirmed beyond current Enterprise-only docs.
- APNs per-app send-rate limits are unpublished; only priority-throttling behavior is documented.
- Web Push per-service quotas (FCM/autopush) beyond 429 semantics are not fully documented publicly.
- MCP revision history before 2025-06-18 was not re-verified.

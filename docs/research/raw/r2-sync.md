# Adversarial review: sync & data model (api.md × ux.md)

Verified against Miniflux source ([migrations.go](https://github.com/miniflux/v2/blob/main/internal/database/migrations.go), [internal/googlereader/handler.go](https://github.com/miniflux/v2/blob/main/internal/googlereader/handler.go), [googlereader/README.md](https://github.com/miniflux/v2/blob/main/internal/googlereader/README.md)) and the [feedbin-api docs repo](https://github.com/feedbin/feedbin-api/tree/master/content).

## Blocking

### 1. Watermark ordering is never defined — and the obvious choice loses entries

"An entry is *past* … if it is before their watermark"; `position` stores "watermark timestamp and entry ID". Which timestamp orders the stream is never said, and the only per-entry timestamps in the draft are publisher-supplied. Failure: feed F sits on the 24 h adaptive clamp (or is in backoff); you mark-all-read at 12:00; at 18:00 F delivers items published 09:00–11:00. Under a `published_at` watermark they are born "past" and never surface. Same for backdated/scheduled posts and re-imported feeds; inverse poison: a feed emitting dates in 2030 sits above the watermark forever. Miniflux can afford `published before ts` because mark-all mutates rows — items stay unread regardless. A visibility-gating watermark cannot.

Fix: order by a server ingest sequence (`entry.seq` BIGSERIAL, or first_seen_at + id), never `published_at`. Keep `published_at` for display and decay only — and say decay measures age from `first_seen_at`, else a month-old backdated item decays on arrival. `seq` joins `hash` as load-bearing entry identity (tech-stack.md "expensive to reverse" #2).

### 2. "Exception" is self-contradictory, and `state` cannot express mark-unread

"Past … if before the watermark, **or if they have an explicit exception**" — then "marking unread writes an explicit exception." The first makes exceptions deepen past-ness; the second needs the opposite. And `state` lists only `done_at`, `later_at`, `kept_at`: no field can hold an unread exception, and there is no rule for which of three coexisting timestamps wins.

Fix: `state` row per (principal, entry): `kind ∈ {read, unread, done, later, keep}`, `(occurred_at, rev)`, `by`, `reason`. Watermark row per (principal, subscription): `(seq, occurred_at)`. Effective state = write with max `(occurred_at, rev)` among the entry's exception rows ∪ {the watermark advance as a `read` write covering `seq ≤ w.seq`}. `past` = winner ∈ {read, done, later, keep}; unread = winner `unread`, or seq > w.seq with no winning read/done. Otherwise three clients plus the GR adapter will implement four different rules — exactly the drift tech-stack.md names the plan's main risk.

## Major

### 3. Per-view vs per-subscription watermark: self-contradictory, and the GR subset needs per-subscription

api.md says "watermark **for its subscription**"; `position` and ux.md say "watermark **per view**". Verified: Miniflux's `mark-all-as-read` accepts `s = feed/<id>`, `user/.../label/<name>`, or `reading-list` — feed- and folder-scoped mark-all is what NNW/Capy send. On a river-wide watermark those force mass exception rows, the thing the model exists to avoid; `unread-count` likewise wants a per-feed unread predicate.

Fix: watermark per (principal, subscription); `position` per (principal, view) becomes a display-only cursor — matching ux.md's "position versus triage". Cadence changes (Today→This week) then don't perturb read state.

### 4. `occurred_at` "clamped to server time" is ambiguous in the direction that matters

Three readings, three outcomes for a 3-day-old offline queue: raised to now (a stale queued mark-all clobbers yesterday's mark-unread on another device); kept for the past, clamped for the future (LWW by true intent, modulo skew); rejected beyond a window (breaks long offline queues). Also unstated: the watermark advance must compete in the same per-entry `(occurred_at, rev)` ordering, or a stale position write silently re-reads items unread elsewhere. A slow clock loses every tie to a fast one — acceptable; write it down. GR/agent writes get server-time occurred_at and always beat skewed offline writes.

Fix: `occurred_at := min(client_value, receipt_time)`; never raised; beyond a bound (>24 h future, absurdly old) substitute receipt_time.

### 5. GR `read` must not map to `done`; `starred`/`kept-unread` unmapped

Verified: Miniflux `edit-tag` handles add/remove of `read`, `kept-unread`, `starred`. A mid-list "mark as read" fires `edit-tag` → under "read means … an explicit done row" it lands in the Done pile, the session-end summary ("41 moved to Done") and activity — when the user merely marked it seen. That inverts ux.md's own position-vs-triage split. `starred`, which every GR client sends, has no mapping.

Fix: `read`/`unread` as distinct state kinds; `done` stays deliberate triage. Map `starred` ↔ `keep` (and state whether kept counts as read for GR). Implement `kept-unread`.

### 6. Watermark must be declared monotonic

Verified: `mark-all-as-read` takes a client `ts` ("only unread entries published before ts"); clients routinely send a stale `ts` (last sync). Writing `watermark := ts` regresses read state wholesale — hundreds of entries reappear.

Fix: `watermark.seq := max(current, requested)` on every path — GR `ts` translated to a seq boundary, `PUT /v1/positions`, mark-all, `state:batch`; requests below the watermark are no-ops returning OK.

### 7. 410 resync is invoked but undefined; tombstone coverage is incomplete

"Runs a full resync" — of what? Subscriptions, cadences, watermarks, all open exception rows, positions? And how do cached entries purged by retention (features.md M2) die — there is no `entry.deleted`, nor `entry.updated` for the draft's own `content_hash` versioning (Feedbin precedent: its updated-entries API exists). Second gap: after restoring the M0 nightly dump the per-user rev counter rolls back; cursors now point *ahead* of max rev — `410` only covers cursor-behind. Three clients will implement three resyncs.

Fix: define a resync set (`/v1/subscriptions`, `/v1/state`, `/v1/positions`) with replace semantics; add `entry.deleted`/`entry.updated`; embed a generation id in the opaque cursor so cursor-ahead and post-restore both resolve to "resync".

### 8. State is per-entry; M5 clusters collide with it

features.md: clustering is "biggest volume win"; ux.md: "also in 3 others". River shows cluster rep E1 (Today); you mark it done; duplicates E2–E4 on This-week feeds keep state → the story resurfaces in the edition. Same for an agent's `triage`. Fix cheaply now: state writes accept `cluster_id` scope resolved to member entry IDs at write time (clusters recompute; resolve at write, keep per-entry rows). Endpoint shape is M0 — decide before clients exist.

## Minor

- `state` rows need `by`/`reason`, not just the change event: the session end ("41 moved to Done by your 'Noise' rule") needs attribution on the winning write; rules run as a server-side principal.
- "Adapt Miniflux's handler" caveat verified: its mux has **no `unread-count` route** (catch-all returns `[]`/200) — it must be built, and `[]` differs meaningfully from a real count. Its documented quirks (`ac=subscribe` takes `feed/<url>` vs `feed/<id>` elsewhere; `it` ignored) should be inherited deliberately.
- `state:batch`: state that entries resolve independently (partial wins) and how it interacts with `Idempotency-Key` replays from a revived offline queue.
- Dead exceptions (unread rows superseded by a newer watermark) accumulate forever; declare them compaction-eligible.
- `get_briefing` "since this agent's position" — over which view? Give agents a single seq cursor (or reuse their change-feed cursor), not per-view positions.
- Verified correct, no change: GR API "no canonical spec… no tombstones" stands, and Feedbin v2's documented API has no tombstone endpoint.

## Build vs fork — verified, draft is right; sharpen the reason

Verified: Miniflux `feeds` are per-user (`user_id`, `unique(user_id, feed_url)`); `entries` per-user with `status enum('unread','read','removed')` — no shared-feed dedupe, no change log, no actor/principal, no annotations. Migrations are an append-only Go array (`schema_version = len(migrations)`): a divergent schema cannot take upstream migrations, so a fork stops merging the day you touch `entries.status` — day one, since watermark + per-principal state is the project. The GR handler confirms it: it is routing + parameter parsing over a status-column storage layer; every storage call is rewritten anyway. The valuable borrowables are the fetcher (`internal/reader/*`, http client, sanitiser/rewrite/scraper) and the handler's shape, both file-level under Apache-2.0. A fork only wins by keeping `entries.status` — which forfeits the signature features. Agree with "borrow, don't fork"; raise confidence to High; add: the GR handler's storage layer is the fork's Trojan horse — it presumes the model you're replacing.

**Verified vs judgement.** From primary sources: Miniflux schema and GR handler semantics (edit-tag tags, mark-all scoping + `ts`, absent `unread-count`), feedbin-api doc contents. Mine: seq-ordering, the exception predicate, per-subscription watermarks, the read/done split, cluster-scope writes — proposals, not citations.

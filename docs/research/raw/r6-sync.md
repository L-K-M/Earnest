# Round 9 review: sync model

Reviewed 2026-11-02. Reviewer: GLM-5.3-Flash. Verifier: SWE-2 high, asked to refute each finding. The report is unedited; the verdicts follow it. At a glance: confirmed SYNC-1, SYNC-2, SYNC-3, SYNC-4, SYNC-5, SYNC-6, SYNC-7, SYNC-8; plausible SYNC-9.

ID: SYNC-1
Severity: blocking
Location: docs/research/api.md:135
Issue: Compacting a non-atomic `state:batch` into one list-valued change event breaks the serial-revision replay guarantee, so replaying clients end in a different state than the server.
Evidence: api.md:87 "It isn't atomic. Each item gets a result" vs api.md:135 "A batch of 2,000 marks is one list-valued `state.updated`, not 2,000 rows." Sequence: batch item k resolves entry E to seen at time t (register rev r1); before the batch's change row commits, device B deliberately marks E unread, committed at log rev 1050; the batch then commits one event at rev 1051 still containing E=seen. A third client replays 1050 then 1051 and ends with E seen while the server has E unseen.
Fix: Assign a log rev to each item at its own resolution instant (per-item events, or atomic batch resolution before compaction).

ID: SYNC-2
Severity: major
Location: docs/research/api.md:74
Issue: A native "mark unread" (`seen=unseen`) does not reset `shelf=done`, so the item still is not unread, while the same intent through Google Reader is defined to reset shelf — two outcomes for one action.
Evidence: api.md:74 "**Unread** means `seen` is unseen and `shelf` isn't done"; api.md:70 seen register "Marking it unread sets it deliberately"; api.md:100 gives only the compat path the shelf reset: "remove `read`, or add `kept-unread` | `seen = unseen`, deliberate, and `shelf = none` if it was done"; api.md:87's batch items carry a single register/value. Scenario: item at shelf=done; native client sends `{register: seen, value: unseen}` → per line 74 still read; same action in Capy → unread (vector 16).
Fix: Define deliberate `seen=unseen` writes to set `shelf=none` when it was `done`, in the native path too.

ID: SYNC-3
Severity: major
Location: docs/research/api.md:91
Issue: The `state.range` record's payload (subscription IDs + `through_seq` + `except`) cannot express a cluster or "all" scope, so clients cannot determine which entries the record covers.
Evidence: api.md:91 "writes one range record carrying **the resolved result**: the subscription IDs and an `except` list"; api.md:93 "`cluster` is accepted as a scope, resolved to member entries at write time"; sample api.md:121-122 `"subscriptions": ["s_3"], "through_seq": 90100`. A cluster spans entries across subscriptions, so no subscription-ID set represents it, and an empty `subscriptions` field is ambiguous between all/cluster/empty-folder.
Fix: Put the resolved scope kind plus resolved subscription or entry IDs into the range record.

ID: SYNC-4
Severity: major
Location: docs/research/api.md:136
Issue: The bootstrap merge rule "keeping the higher `rev` per object" is unimplementable because no bootstrap payload is specified to carry a per-object revision — entries carry `seq`, and subscriptions, folders, labels and annotations carry no rev at all.
Evidence: api.md:136 "Then page through every resource by stable ID... Replace the local cache, keeping the higher `rev` per object"; api.md:41 entry is "Immutable identity plus **`seq`**"; api.md:120 shows the state register's `state_rev: 7` is a separate numbering from log `rev`. Vectors 13 and 21 need exactly this comparison and cannot decide it.
Fix: Specify that every bootstrap payload embeds the object's last change-log rev (and per-register `state_rev`), and merge on those.

ID: SYNC-5
Severity: major
Location: docs/research/api.md:45
Issue: The `label` resource is bootstrapped and written through the Google Reader path, but has no native endpoints and no event types in the change log, so compat-written labels never reach native clients.
Evidence: api.md:45 "`label` | User labels on entries"; api.md:25 "Capy writes per-item user labels through `edit-tag`, so Earnest needs an entry-label resource"; api.md:103 maps `user/-/label/x` to `label`; the change-log sample (api.md:116-126) has no label events and the tombstone list (api.md:137) omits `label.deleted`; the endpoint table (api.md:171-190) has no label routes.
Fix: Add label created/updated/deleted events to the change log and native label endpoints.

ID: SYNC-6
Severity: major
Location: docs/research/api.md:89
Issue: Positions are last-writer-wins across devices but the change feed defines no position event, so resume positions propagate only at full bootstrap.
Evidence: api.md:89 "Positions are last-writer-wins per principal per view"; api.md:136 bootstraps "positions"; the only change types shown or listed (api.md:116-126, api.md:137) never include a position event. Scenario: phone PUTs `/v1/positions/river` at seq 700; the Mac's incremental `/v1/changes` never sees it and resumes at its stale 500 until re-bootstrap (or the "Display only" note at api.md:44 means no sync, which is never stated).
Fix: Add a `position.updated` change event, or state explicitly that positions sync only at bootstrap.

ID: SYNC-7
Severity: minor
Location: docs/research/api.md:134
Issue: The deadlock rule orders only the per-user rev counters, but an ingest transaction must also lock the global `seq` counter row, whose position in the lock order is unspecified.
Evidence: api.md:60 "`seq` comes from a counter row updated in a short commit step"; api.md:134 "Take counter locks in user ID order so concurrent ingests can't deadlock" — an ingest holds the seq row plus several users' counter rows (vector 11's shared-feed case), and two paths acquiring seq vs user counters in opposite orders can deadlock.
Fix: Pin a total lock order, e.g. seq counter first, then user counters ascending.

ID: SYNC-8
Severity: minor
Location: docs/research/api.md:112
Issue: `GET /v1/changes` is defined per user but nothing says its events are filtered to an agent token's feed allowlist, or how the cursor behaves under filtering.
Evidence: api.md:48 tokens carry a "**feed allowlist**"; api.md:204 "`entries:read` (limited to the feed allowlist)" and "Scopes are enforced in the service layer, so the same limits apply over REST, CLI and MCP"; api.md:112 defines `/v1/changes?cursor=...` with no filtering semantics, so an agent allowlisted to one feed receives `entry.created`/`subscription` events for every feed.
Fix: Define allowlist filtering for the change feed, including that denied events still advance the cursor.

ID: SYNC-9
Severity: minor
Location: docs/research/api.md:104
Issue: The Google Reader `mark-all-as-read` mapping "`ts` becomes the highest `seq` first seen by that time" is undefined: whose first-seen (this subscription's or the shared feed's ingest) and with what timestamp-to-seq mapping is unspecified.
Evidence: api.md:104 "| `mark-all-as-read`, if a client ever needs it | `state:through`; `ts` becomes the highest `seq` first seen by that time"; `seq` is assigned at ingest (api.md:58-60) with no defined index from time to seq, and a client that never streamed IDs sends a stale or absent `ts`.
Fix: Define it as the subscription's highest `seq` whose `first_seen_at` is at or before `ts`, with a documented fallback when `ts` is absent.

## Verification

- **SYNC-1: CONFIRMED.** api.md:87 declares `state:batch` non-atomic with per-item results while api.md:135 compacts it into one `state.updated` event, and nothing specifies the event is snapshotted atomically, so the described interleave (concurrent rev-1050 write overwritten by stale batch payload at rev 1051) is permitted by the text.
- **SYNC-2: CONFIRMED.** api.md:74 defines unread as `seen=unseen` AND `shelf!=done`, but only the Google Reader compat mapping (api.md:100) resets `shelf=none`; native batch items are single register/value (api.md:87) with no defined shelf reset, so a native mark-unread on a Done item leaves it not-unread.
- **SYNC-3: CONFIRMED.** api.md:91-93 accept `cluster` as a scope resolved to member entries, yet the range record (api.md:121-122) carries only subscription IDs + `through_seq` + `except`, which cannot express a cluster's resolved entry set (the "all"-scope sub-claim is weaker since enumerating all subscriptions works, but the cluster hole is real).
- **SYNC-4: CONFIRMED.** api.md:136's merge rule "keeping the higher `rev` per object" references a field defined nowhere: entries carry `seq` (api.md:41), state rows carry per-register `state_rev` (api.md:120), and subscriptions/folders/labels/annotations/positions have no rev field specified.
- **SYNC-5: CONFIRMED.** `label` is a resource (api.md:45) written via the Google Reader path (api.md:25, 103) and bootstrapped (api.md:136), but the endpoint table (api.md:171-190) has no label routes and neither the event list (api.md:116-126) nor the tombstone enumeration (api.md:137) includes label events.
- **SYNC-6: CONFIRMED.** api.md:89 makes positions last-writer-wins and api.md:136 syncs them at bootstrap, but no `position.*` event type appears in the change-feed example (api.md:116-126) or tombstone list (api.md:137), so cross-device resume propagates only at re-bootstrap.
- **SYNC-7: CONFIRMED.** api.md:134 orders only per-user counter locks while api.md:60 requires the same ingest transaction to also update the global `seq` counter row, leaving seq's position in the lock order — and thus the deadlock-freedom claim — unspecified.
- **SYNC-8: CONFIRMED.** Tokens carry a feed allowlist (api.md:48) enforced in the service layer (api.md:204), yet `/v1/changes` (api.md:112-129) defines no allowlist filtering or cursor semantics, so filtering behavior for agent tokens is genuinely undefined.
- **SYNC-9: PLAUSIBLE.** The api.md:104 mapping lacks a defined time-to-seq resolution and absent-`ts` fallback, though `first_seen_at` (api.md:58) does name the only first-seen concept, so part of the claimed ambiguity is answerable from the doc.

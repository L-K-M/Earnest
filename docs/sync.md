# Sync protocol

`internal/syncmodel` is the executable rule implementation. The service calls
`ResolveWrite` for each locked state row, using the allocated change revision,
and commits the result with its change event. It checks operation deduplication
before calling the resolver. `Model` is the single-user reference machine for
transactions, bootstrap, restore and clients. `Resources` exposes an isolated,
stably ordered bootstrap source for model consumers. Neither performs I/O or reads the
clock. Supply authenticated context and receipt time. Do not share a mutable
model between goroutines without external serialization.

The [JSON vectors](../testdata/vectors/README.md) specify observable results for
server and client implementations. Fixtures 01 through 21 correspond to the
research list; the remaining fixtures cover additional decisions.

## Identity, ordering and revisions

IDs are positive signed 64-bit integers, encoded as JSON numbers. Clients must
use integer-safe decoding. Assign `seq` during the short ingest commit, after
fetching and parsing. A supplied global sequence must advance visible order;
the reference model can allocate it locally. Order entries and resume positions
by `seq`. Compute decay from `first_seen_at`, clamping negative age to zero.
Publisher timestamps affect display only (06, 07, 14).

Take the global sequence-counter lock first, then user-counter locks in user ID
order. Allocate each user's gapless change `rev` inside the transaction. Commit
mutations, changes and delivery jobs together. The pure model represents these
commits sequentially (11); it cannot prove a database locking implementation.

Every entry, subscription, folder, label, annotation and position carries the
revision of its last change. State carries `state_rev` and each register's own
`rev`. Deletions retain revisioned tombstones, including token revocation.
Cursors and write `base_rev` are `{generation, rev}`. A generation mismatch
requires re-bootstrap, even if the numeric revision matches (09).

## State writes

For each target principal and entry, absence means `seen=unseen`, `shelf=none`,
`kept=false`, with zero revisions. The seen register accepts `seen` or `unseen`;
shelf accepts `none`, `later` or `done`; kept is a JSON boolean. Unread means
unseen and not Done. Kept never changes shelf (17).

Each accepted register stores its change revision, normalized `occurred_at`,
authenticated actor and server-derived source. Target principal and actor may
differ: an authorized agent can change your state, but provenance remains that
agent's. Progress in its own state stays separate (30). Scope checks belong to
the service before entering the model.

Derive source from trusted token kind and operation classification. A human or
compatibility explicit action is deliberate; scrolling is passive. Agent and
rule tokens always produce `rule_or_agent`, even for an explicit operation.
Never deserialize caller-supplied source into trusted context (04).

Resolve a write as follows:

1. Require the current database generation. Reject invalid fields before any
   batch mutation. Check `(actor principal, op_id)` deduplication first in the
   transaction. A repeated state operation returns its original outcome and the
   current effective state, with `replay=true`; a deleted target returns `gone`.
   It never resolves again (15).
2. A missing or retention-deleted entry returns `gone`, even for an expired
   operation. It cannot be resurrected (03).
3. Normalize time to `min(client time, receipt time)`. This includes clocks more
   than 24 hours ahead. Keep old times. Age strictly greater than 30 days returns
   `expired`; exactly 30 days is accepted (02, 28).
4. A matching register `base_rev` applies. Otherwise rank deliberate above
   rule/agent above passive. Same source uses later normalized time, then higher
   committed revision. Stale passive seen/unseen writes resolve to seen, even if both wrote unseen.
   Other passive registers use
   the ordinary time and revision comparison. An old
   deliberate clock can lose to another deliberate action; this is intentional
   (05). A current base still applies despite a slow clock (28).
5. Return `applied` or `superseded` with effective state. An accepted unseen
   action on a Done item also clears shelf to none, with the same revision and
   provenance. Apply this rule on every surface (16, 22).

The reference machine retains operation records indefinitely. Durable storage
must keep them longer than the maximum offline queue, including retry margin.
The service must atomically deduplicate resource and position mutations too;
`PutObject` and `PutPosition` are already-authorized transaction primitives.

## Batches, ranges and clusters

A batch validates all fields before changing state. Invalid input rejects the
whole batch, including dedupe records. Duplicate operation IDs or duplicate
register targets in a batch are invalid. Distinct registers are processed in
list order. A shelf reset by an earlier unseen item has the same allocated
event revision; it is a causal side effect, not an intervening device write.
A later shelf item can therefore apply after it, including Done again (35).
`gone`, `expired` and `superseded` are valid per-item outcomes. Applied registers
share one revision and one compact `state.updated`, containing all per-item
results with final effective state. A batch with no applied items emits no
change. Deduplication still records its results (12, 23).

`state:through` resolves subscription, folder or all scope at receipt. Persist
the resolved subscription IDs, `through_seq`, explicit `except` entry IDs and
resolved per-entry state stamps in one `state.range`. Only entries already
committed with `seq <= through_seq` participate. A range's shared base normally
is zero, so it competes as stale with existing registers. An expired range
emits nothing. A valid range emits one event even when all members are excepted.
Replays return its original resolved membership and current effective states.

Range preparation may use chunks, but their mutations and the range event must
become visible atomically. A crash during preparation publishes no partial
range. Clients apply the resolved results; they must not rerun the source/time
policy or infer state for subsequently ingested entries (01, 02, 06, 21).

Resolve cluster membership once, then use the ordinary atomic batch. Never
store a cluster range. A late-joining member stays unread until step 10 adds
read-time suppression (18).

## Google Reader compatibility

Use receipt time because the protocol supplies neither action times nor base
revisions. Map requests with a zero base, even if the service has a newer
register in memory. This deliberately treats them as stale (12, 20).

| Action | Register action and source |
| --- | --- |
| Add read | seen=seen, passive |
| Remove read or add kept-unread | seen=unseen, deliberate; shared rule clears Done |
| Add or remove starred | kept=true or false, deliberate |
| Read read-state | seen=seen or shelf=done |
| Add or remove user label | Native label membership and add/remove event |
| Mark all as read | Passive seen range |

Read never maps to Done. A fresh compatibility read can lose to a deliberate
native unread. Conversely, a stale compatibility unread, star or unstar can
win by its later receipt timestamp. The server cannot recover the missing
intent; this is the accepted compatibility trade-off (20).

For mark-all with `ts`, find the highest sequence first seen no later than that
time. Without `ts`, capture the highest visible sequence at receipt (26).

## Changes and bootstrap

The event vocabulary includes `entry.created`, `entry.updated`, `entry.deleted`,
`state.updated`, `state.range`, `label.added`, `label.removed`, `position.updated`,
`subscription.created`, `subscription.updated`, `subscription.deleted`,
`folder.updated`, `folder.deleted`, `annotation.updated`, `annotation.deleted`
and `token.revoked`. Entry-created events can carry IDs and sequence metadata;
fetch bodies on demand. Treat a body fetch returning 404 as a tombstone.

Labels are native revisioned objects. A label ID identifies one entry-label
membership; names are not IDs. Removal leaves a tombstone (24). Positions are
per principal and view, last committed writer wins, including backward moves.
`position.updated` propagates them live. A position never marks anything seen
(25). Folder and annotation update events complete their bootstrap lifecycle
(29).

Subscribing or rebinding to a shared feed atomically emits entry backfill events
with fresh entry revisions, including tombstones. This covers entries whose
IDs an ongoing bootstrap already passed. Feed title and site URL changes emit
subscription updates with fresh revisions for each affected subscriber;
an overridden title alone does not change when the feed title changes.

Read the current cursor first, then replace the local server cache and page all
resources by stable ID, including cached-window entries and state. Preserve the
pending outbox. Merge pages and replayed changes by the higher object revision;
merge state independently by each register revision, taking the maximum
`state_rev`. An older range can fill an older register while leaving newer
registers alone (13, 21, 38), and a stale response row cannot erase a register
a newer row already delivered (36). Do not create state for unknown or deleted
entry IDs when replaying state events.

Persist a whole change page and its cursor atomically. Check contiguous
revisions before applying it. A partial page with the final cursor is invalid;
replaying a page after a crash is safe (21). A cursor below the log retention
floor, or beyond the current revision, requires a 410-equivalent response. A
cursor exactly at the floor remains valid. Re-bootstrap and replay the outbox
(08).

Client state is server state plus the pending overlay. Coalesce pending intent
per principal, entry and register, keeping the oldest observed base and newest
action time and operation ID. Preserve ordering between registers. When an
unread overlay clears Done, queue
a shelf-none intent as well, using the unread operation ID plus `/clear-done`.
Retain it if a later seen action coalesces away that unread, so Done cannot
reappear (32). Reserve this generated ID suffix in client operation IDs. Do not
replace an operation already in flight: retain its exact payload for retries,
and queue a new ID for a subsequent action. An acknowledgement removes only its
own ID, so a late response cannot erase a newer replacement. Accept a response
only in the generation its request was sent under: a response from a superseded
generation is rejected whole, before merging results or removing the operation,
so the preserved outbox item still replays into the new generation (37). Ranges
are tagged outbox items with their original scope, action clock and fixed
boundary. Keep them in order with register writes and overlay them only on
locally cached members. Do not expand them into cached IDs because the server also holds
uncached members (31). Report expired or gone operations to the user when dropping them (27).

After generation rotation, clear server caches and bootstrap anew. Preserve
reversible pending intent but replace its base with `{new generation, 0}`. This
makes it stale against restored registers instead of granting a matching base
that forces it to win. Preserve its original action time. Never perform this
rewrite for irreversible notification requests.

## Restore and notification delivery

The restore process must supply a fresh, never-reused generation ID. The model
rejects reuse of the snapshot's generation; uniqueness across other historical
generations is the restore process's responsibility. Old cursors and writes
require re-bootstrap (09).

Notification requests carry a generation too. Check it before deduplication,
including when the snapshot predates both acceptance and its operation record
(19). Retried requests must retain their original generation. Record an attempt
in durable storage before calling the transport, and never retry an uncertain
attempt. Completion only changes uncertain to delivered. A crash between
recording and sending can lose a notification; this is the accepted price of
at-most-once delivery (10).

After restore, all delivery is withheld until explicit release. The release
permits only requests accepted in the current generation. Restored pending rows
stay withheld: their attempts may have occurred after the snapshot and been
forgotten. To send their content again, the user must authorize a new operation
in the current generation. This closes the duplicate-send hole when releasing
an old pending row (33). Deduplicated accepts carry `replay=true` so the service
can avoid creating duplicate jobs. Delivery attempts and generation checks are protocol gates; transport, budgets, quiet
hours and durable persistence belong to the service and push packages.

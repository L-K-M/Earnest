# Shared sync vectors, version 1

Run these JSON files unchanged in Go, Swift, Kotlin and TypeScript. The
[settled protocol](../../docs/sync.md) defines the rules. Files 01 through 21
match the research scenarios. Every file has a `decision` explaining its
settled result. All code and fixtures were written for Earnest.

A fixture has `version` (currently 1), `name`, `decision`, `setup`, `operations`
and `final`. Each of the last three is an ordered array of steps. Run them in
that order in a fresh environment; `final` contains read-only assertions. Every
step declares `expect`, including `{}` for a successful operation with no
asserted payload. `error`, when present, names the expected protocol error.
Expected objects are recursive subsets of actual objects. Arrays are exact,
ordered matches. Scalars and null match exactly. Missing expected keys fail.
Reject unknown fields, operations, versions and unregistered fixture files.
The Go runner keeps the filename manifest in `vectors_test.go`; adding a file
requires registering it. `TestUnknownVectorRejected` checks that guard.

## Values and defaults

IDs are positive signed 64-bit JSON numbers. Register revisions and seq are
nonnegative integers. Decode integers losslessly, including in JavaScript.
Time is RFC 3339 UTC. The runner compares IDs without floating-point conversion (34).
A kept register value is a JSON boolean; other register
values are strings. State has `seen`, `shelf`, `kept` register objects, each
with `value`, `rev`, `occurred_at`, `actor` and `source`, plus `principal`,
`entry_id` and `state_rev`.

A new named `server` creates one user's model with generation `g1`. The default
server is `main`. A `device` names a client cache/outbox attached to the selected
server. Server and device maps, snapshots, captured pages and prepared ingests
are local to the fixture. Names are case sensitive.

Common step defaults:

- `at`: `2026-11-02T12:00:00Z`, used as receipt time.
- `actor`: 1; `token`: `human`; `principal`: 1.
- `server`: `main`; omitted device means inspect the server.
- Write `principal`: step principal; `entry_id`: step entry_id.
- Write `base_rev`: `{generation: current server generation, rev: 0}`.
- Write `occurred_at`: step receipt time; `kind`: `explicit`.
- `op_id`, register and value have no implicit defaults for ordinary writes.
- Range register/value default to `seen`/`seen`; through defaults to `ThroughAt(ts)`.

Actor is trusted authentication; write principal is the state owner. Tokens
are `human`, `agent`, `rule`, `compat`. Operation kinds are `explicit`, `passive`.
No input supplies source. Invalid syntax rejects the entire transaction.

## Step operations

| `op` | Inputs | Actual result |
| --- | --- | --- |
| `ingest` | `entry`: id, subscription_id, optional seq/published_at; `at` | Committed Entry with seq, first_seen_at and rev |
| `repeat_ingest` | Entry template, `repeat` | Ingest consecutive IDs starting at template id; `{rev, seq}` |
| `prepare_ingest` | `name`, entry | Save parsed entry outside transaction; current `{rev, seq}` |
| `commit_ingest` | `name`, at | Commit saved entry, assign seq now; Entry |
| `update_entry` | entry_id, entry.published_at | `{}`; entry.updated event |
| `delete` | entry_id | `{}`; retention tombstone and event |
| `write` | `write` | One Result: op_id, status, effective state, replay |
| `batch` | `writes` | Ordered Results, one atomic commit |
| `cluster` | `writes` | Same as batch; list is membership resolved at receipt |
| `range` | write, scope, optional through_seq or ts | RangeResult: status, through_seq, subscriptions, except, results, replay |
| `google` | op_id, entry_id, action | Map and execute one compatibility write; Result |
| `google_batch` | op_id prefix, first entry_id, repeat, action | One batch; counts keyed applied/superseded/gone/expired |
| `object` | `object` | Revisioned object, including tombstones |
| `position` | `position` | Revisioned position, last commit wins |
| `changes` | cursor | `{changes, types, revs, next}`; limit 100000 |
| `capture_changes` | name, cursor | Same, and save the immutable page |
| `retain` | rev | `{}`; discard changes through rev |
| `checkpoint` | name | Snapshot all model state, `{rev}` |
| `restore` | name, generation | Restore that checkpoint into selected server, rotate generation, `{}` |
| `bootstrap_begin` | device | Create or reset cache, preserve outbox; return cursor read now |
| `bootstrap_page` | device | Merge current entries, state, objects, positions, `{}` |
| `client_pull` | device | Read from client cursor, atomically apply full page; return cursor |
| `client_apply` | device, name, optional count | Apply captured page and its final cursor; count truncates payload to simulate a crash; return cursor |
| `client_queue` | device, write | Coalesce register intent, `{}` |
| `client_queue_range` | device, write, scope, optional through_seq or ts | Queue a first-class range with fixed boundary, `{}` |
| `client_flush` | device, trusted context | Submit consecutive register writes as atomic batches and ranges individually in outbox order; ordered Results or RangeResults |
| `accept_notification` | actor, op_id, optional generation | Notification with pending/uncertain/delivered status |
| `attempt_notification` | actor, op_id, at | Record before sending; Notification, or attempted/withheld error |
| `complete_notification` | actor, op_id | Record successful transport completion, `{}` |
| `release_notifications` | none | Release restore hold for current-generation requests, `{}` |

Google actions are `read`, `unread`, `kept-unread`, `star`, `unstar`. Google
steps force `compat` authentication and receipt-time action clocks. Generated
batch IDs are `prefix-0`, `prefix-1`, etc. Range scope is `{kind: "all"}`,
`{kind: "subscriptions", ids: [...]}` or `{kind: "folder", ids: [folderID]}`.
Resolved results are ordered by entry ID, subscriptions are sorted and unique.
Absent `ts` and `through_seq` means highest committed seq at receipt.

Object fields are kind (`subscription`, `folder`, `label`, `annotation`,
`token`), id, principal, entry_id, folder_id, name, rev, deleted. Label IDs
identify entry-label memberships. Position fields are principal, view, seq,
rev. Caller-supplied revisions on objects/entries/positions are replaced at
commit. Deletion is a tombstone, not removal from the fixture map.

## Assertions and errors

| Read-only step | Inputs | Actual result |
| --- | --- | --- |
| `assert_state` | principal, entry_id, optional device | `{state, unread, google_read}` |
| `assert_entry` | entry_id, at, optional device | `{entry, exists, age_seconds}` |
| `assert_object` | object.kind/id, optional device | `{object, exists}` |
| `assert_position` | principal, position.view, optional device | `{position, exists}` |
| `assert_client` | device | `{outbox_size, cursor}` |
| `assert_log` | server | `{rev, seq, generation, revs, types, gapless, count}` |

Error names are `invalid`, `generation`, `cursor_gone`, `withheld`, `attempted`.
A write, batch or client-pull failing before a payload exists returns JSON null.
Other errors return their operation's zero-value or diagnostic result; fixture
expectations specify the fields that matter. Check the error before examining
that payload. `gone` and `expired` are per-item results, not protocol errors.

An outbox item is `{write: Write}` or `{range: RangeWrite}`. Queueing unread on
Done also queues a shelf-none intent with operation ID `unreadID/clear-done`.
This intent survives coalescing unread into seen (32). Within a batch, a
register carrying the same newly allocated event revision came from an earlier
item's side effect. A later explicit register action applies after that effect
in list order (35). Keep original generation
on notification retries; release does not enable old-generation pending rows
(33).

No runner should use wall-clock time or sleep. Prepared ingests and ordered
steps model transaction schedules, not actual SQL locks. Vector 11 proves
serial commit invariants for two users; it does not replace database deadlock
tests. Vector 21 rejects partial replay pages and proves idempotent restart;
store tests must also prove that prepared range chunks are never published.

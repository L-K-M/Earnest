# web/src/sync: client sync engine

TypeScript port of the sync protocol in `docs/sync.md` and the executable
model in `internal/syncmodel`. Everything here is owned by the sync layer;
network access is injected through the shared request seam.

## Pieces

- `json.ts`: lossless JSON codec. Integer literals decode to `bigint`,
  fractions/exponents to `number`. All protocol IDs and revisions are
  `bigint`; nothing in this layer passes through unsafe `number`s.
- `time.ts`: RFC3339 wall times (epoch ms internally, `formatTime`/`parseTime`).
- `types.ts`: protocol types: `Revision {generation, rev}`, registers
  (`seen`/`shelf`/`kept`), values, `Write`, `RangeWrite`, `Change`, `State`,
  `Entry`, `SyncObject`, `Position`, `Result`, `SyncError` codes.
- `wire.ts`: strict encode/decode between those types and JSON trees
  (every accessor rejects malformed payloads).
- `client.ts`: `SyncClient`: the pure engine. No I/O, no clock reads.
  Cursor/change replay, atomic contiguous pages, higher-revision merges
  (entries/objects whole; states per register), outbox coalescing with
  immutable in-flight groups, generation-aware acknowledgement, pending
  overlay, 30-day intent expiry, snapshots.
- `model.ts`: `Model`: in-memory port of the server's `internal/syncmodel`,
  used by tests to simulate every server-only vector step.
- `store.ts`: `SyncStore`: `IdbSyncStore` (one structured-clone record per
  save, so a snapshot write is a single transaction) plus `MemorySyncStore`.
- `runtime.ts`: `ReaderSync`: the browser orchestrator (below).
- `harness.ts` + `vectors.test.ts`: drift gate: all 35 files in
  `testdata/vectors` replayed through `SyncClient`; unregistered files and
  unknown ops fail the suite.
- `engine.test.ts`, `runtime.test.ts`: unit/integration coverage,
  including the late-generation-acknowledgement regressions that vectors
  36/37 would cover (absent in this checkout).

## Parent integration

```ts
import { ReaderSync } from "./sync/runtime.js";
import { requestText } from "./request";

const sync = new ReaderSync({ request: async <T>(path: string, options?: RequestInit) => await requestText(path, options) as T, principal: /* principal ID as bigint */ });
await sync.start();                          // loads IndexedDB, bootstraps once, leads the poll loop
const unsub = sync.subscribe((status) => { /* cursor, outboxSize, leader, bootstrapped, lastError */ });
```

Reads (synchronous, served from cache + pending overlay):

- `sync.state(principal, entryId) -> State`: effective registers.
- `sync.hasEntry(entryId)`, `sync.resource(kind, id)`: `resource` exposes
  the richer bootstrap projections (subscription title/url, folder name,
  label definitions) keyed `"subscription" | "folder" | "entry" | "label"
  | "labeldef"`.
- `sync.cursor()`, `sync.status()`.

Writes (durable before return; queued intent survives reloads):

- `await sync.queue(entryId, "seen" | "shelf" | "kept", value, kind?)` , 
  coalesces per principal/entry/register; returns the queued `Write`.
- `await sync.queueRange(scope, {throughSeq}, kind?)`
 : first-class ordered range intents (`/v1/state:through`).
- `await sync.observe(entry, state)`: adopt locally-observed rows from
  history pages before computing the register base.
- `await sync.stop()` on teardown.

Hooks in `ReaderSyncOptions`: `store`, `locks`, `channel`,
`openEvents` (SSE hint source; invoking `onHint` triggers a pull: hints
carry cursors, never payloads), `now`, `newOpId`, `pageLimit`,
`bootstrapLimit`, `onDropped` (gone/expired operations for user
visibility), `onError`, `device` (storage/lock namespace per device).

## Behavior contract

- Bootstrap captures `/v1/bootstrap` **before** paging; all seven kinds
  page to completion; `bootstrapped` persists so an interrupted pass
  restarts instead of trusting a partial cache. Commits landing during
  bootstrap replay through `/v1/changes` afterwards.
- Change pages apply atomically and only when contiguous at the cursor;
  gaps, partial pages or another generation trigger a re-bootstrap, as
  does HTTP 410 anywhere.
- Outbox: one pending intent per principal/entry/register keeping the
  oldest base revision and newest op id/time; generated `shelf:none`
  `/clear-done` intents persist as their own register slot. An in-flight
  group is frozen: a newer same-register intent queues behind it, never
  rewrites it: and the exact persisted payload is retried after errors.
- Acknowledgements bind to `requestGeneration`: a response from a
  superseded generation is ignored wholesale (no merge, no removal), and
  only the named op ids are ever cleared.
- Cross-tab: every reload→mutate→persist runs inside a Web Lock
  (`navigator.locks` when present); a second lock elects one leader that
  polls/flushes while followers share the snapshot; `BroadcastChannel`
  wakes peers on change.
- `/v1/events` is a hint channel only; payloads always come from
  `/v1/changes`.

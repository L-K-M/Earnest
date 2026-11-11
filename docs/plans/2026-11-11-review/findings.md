# Continuation findings

Review baseline: `aaf7719cfa3ce932d6ed9e289e431cc44ed2d0e4`.
The [plan](README.md) was committed and merged as
[#166](https://github.com/L-K-M/Earnest/pull/166) before implementation.
This is an execution checkpoint, not a clean bill of health. Linked PRs
record the latest CI, review and merge state.

## Coverage and limitations

| Slice | Evidence received and limitations |
| --- | --- |
| S5 | Two broad helpers timed out. Primary-thread review covered actor, annotations, activity/revert, triage, notifications and webhook paths; a separate LongCat refutation checked the selected webhook defect. No independent full-slice approval is claimed. |
| S6 | LongCat reviewed counters, core queries, locks, all eleven migrations and sync model. Primary review read the omitted generated/pipeline, cache, spend, embedding, clustering and entry-query files. Full database-backed Go tests ran. No query-plan benchmark was run. |
| S9 | Free DeepSeek reviewed adapters, CLI, MCP and boundary callers. Its session compacted before a final report; recovered static notes and actual package-test results remain usable partial evidence. Its database tests skipped; the primary full Go gate separately ran them with Postgres. |
| W2 | Free DeepSeek supplied a reader/routing report after the first LongCat timeout. A second LongCat review confirmed the selected passive-reading failure and rejected a local-handler-only fix as incomplete. No browser GUI test was run. |
| W3 | Tool-based and one-response LongCat attempts timed out. A bounded fallback report is pending; no clean result is inferred. |
| A1 | LongCat reviewed core sync, API, persistence and relevant app callers. No runtime suite was run by the reviewer; omissions include several import/search/settings tests. No consequential defect was established. |
| A2 | LongCat supplied a partial app review; independent verification confirmed the sign-in failure. Reader-load cancellation ordering and several larger test files remain unverified. |
| M1 | LongCat reviewed kit sync/network paths and selected app callers. Independent verification confirmed the clean-EOF hint failure. No Swift suite was run by the reviewers; vector-runner and several UI areas were not read in full. |
| M2 | LongCat supplied a partial window/reader/app report. Settings/Login internals and several tests were omitted. Its navigation/optimistic-cadence candidates are not runtime-confirmed. |
| O1 | LongCat checked deployment contracts and ran backup plus shell syntax probes. No local Compose/image gate was run by the reviewer because its Docker CLI lacked the Compose plugin. Context-isolation and restore/update candidates remain unverified. |
| O2 | Tool-based and one-response LongCat attempts timed out. A bounded fallback report is pending. |

All new review material is tied to the stated application baseline. The eight
older recovered reports and 134 historical assessments remain in the
[first batch record](../2026-11-10-review/findings.md) at `f8adbdb`.
They were not silently rerun or relabeled as current coverage. Recent work
by other agents is baseline code, not this continuation's implementation.
Some helper tools resolved the shared checkout; its HEAD matched the frozen
baseline and its only tracked modification was the pre-existing `.gitignore`.
That work was left untouched.

## Selected findings

### R1: Redirect responses can repeat a processed webhook POST

**Fixed:** [#167](https://github.com/L-K-M/Earnest/pull/167), two completed
review rounds. [Fix plan](webhook-see-other.md).

The worker treated redirects as definite refusals and retried them. A real
receiver consumed the POST and returned a result Location; the durable worker
sent it twice. Independent verification confirmed 303; review round 1
identified the same 302 path. Before the broader fix, additional receiver
regressions failed for 301, 302, 307 and 308. All 3xx responses now remain
uncertain, are never followed or retried, and do not increment refusal counts.

The public contract explicitly acknowledges the existing at-most-once trade-off:
an unknown attempt can be lost; an active destination is not an acknowledgement.
Additional delivery history/logging/metrics is deferred, not claimed present.
The stable `webhook-id` already exists; no new idempotency-key design was added.
The final full Postgres-backed Go gate passed at `7592673`, and all PR gates passed.

### R2: Publisher text can issue terminal commands through the CLI

**Fixed:** [#168](https://github.com/L-K-M/Earnest/pull/168), one completed
review round. [Fix plan](cli-terminal-controls.md).

Valid JSON Feed text preserves C0/C1 controls through parsing, HTML sanitization,
storage and JSON decoding. Human tables and `read` emitted those bytes directly.
The parser/sanitizer-to-CLI regression failed before the fix; a separate refutation
confirmed the real storage/API path. Terminal effects depend on the emulator;
this is not an automatic code-execution claim.

Human-readable stdout/stderr now neutralize unsafe controls while preserving
layout and Unicode. JSON, raw OPML, stored publisher text and article identities
are unchanged. Coverage includes machine-data round trips, import/error output,
cell truncation, Unicode and short/failing writers. Payloads were captured only
in buffers, never sent to a live terminal. Full Go gates passed before and after
integrating R1; current-head CI passed.

Review hypotheses about JSON stderr and export aliases were refuted: stderr is
human-only even under `--json`, and the only global extraction/dispatch paths
exactly match raw OPML routing. Future chunked UTF-8 output and bidi-display
policy are deferred; no current streamed human-output path was found.

### R3: A row's passive-reading cache survives a later unread

**Reproduced; submitted:** [#169](https://github.com/L-K-M/Earnest/pull/169).
[Fix plan](river-passive-reread.md). Review round 1 completed; round 2 pending.

River kept successful and already-seen IDs for an entire view mount. A later
keyboard, reader-toolbar or remote unread could not be followed by another
passive read until the view remounted. Five regressions failed first. The fix
deduplicates only outstanding writes, retaining later real reading events.

Review identified that an earlier failed intent could also be retried after a
newer unread. Two more regressions failed before extending the queue: pending
intent records its observed register revision and object identity, so unread
cancels stale intent and old callbacks cannot discard a newer event. This does
not change the sync conflict policy or invent optimistic register revisions.
All River tests, typecheck and lint pass; the complete final web gate passed.

### R4: Android device sign-in cannot restart after polling fails

**Candidate with independent confirmation; implementation in progress.**
[Fix plan](android-device-login-retry.md).

Expiry, denial and transport/API failures stop the polling effect while keeping
its authorization. That permanently disables the server field and Start button.
The planned fix releases the failed flow while keeping its error and server,
preserves pending/slow-down behavior, and propagates cancellation. Focused
Compose/Robolectric regressions and the native gate are not yet claimed complete.

### R5: A clean EOF permanently stops Mac live hints

**Candidate with independent confirmation; regression preparation in progress.**
[Fix plan](mac-event-reconnect.md).

The transport can finish a valid event stream without error. AppModel returns
instead of reconnecting; the two-minute poll converges data but never restarts
live hints. The intended fix uses the existing backoff and preserves session,
cancellation and 401 exits. Runtime verification is not yet claimed complete.

## Refutations and deferrals

- **Mac export truncation:** refuted. The pinned OpenAPIRuntime collector throws
  `TooManyBytesError` for both declared and streamed bodies exceeding the limit;
  `Data(collecting:upTo:)` does not silently return a prefix.
- **Google Reader repeated include filters:** recovered S9 notes identify the
  older B6-3 conjunction/union candidate. No new protocol regression was
  established here; it remains deferred for compatibility-contract verification.
- **MCP guide search text:** the old unavailable-search sentence in `llms.go`
  is a minor current-guide discrepancy, deferred rather than bundled into fixes.
- **Triage broker hints:** primary inspection found no immediate publication on
  Triage/RevertTriage, but periodic browser pulls still converge. A required
  latency failure was not established; no data-loss claim is made.
- **Native notes:** unavailable-entry deep-link recovery, non-media response
  bounds, overlapping loading indicators, very large notification-ID collisions,
  base-path validation and optimistic UI behavior remain unverified or minor.
  A 32-bit hash would not eliminate 32-bit notification-ID collisions.
- **Operations candidates:** missing Docker context exclusions, backup activity
  during restore, update health semantics and stale review-budget diagnostics
  require focused verification. No deployment, restore or release was performed.
- **Unresolved first-batch policy/performance:** agent summary authorization and
  spending, bounded-summary excerpt/citation/cache policy and large atomic range
  performance remain deferred. This continuation introduces none of those policies.

## Verification record

- Baseline `GOFLAGS=-p=1 make lint test` passed on `aaf7719` with an isolated
  Postgres 18.6/pgvector instance: complete uncached Go race suite, vet, gofmt,
  backup tests and all 18 packaging tests. The disposable server required an
  explicit `LC_ALL=C` after an initial locale-related startup failure.
- R1 and R2 have targeted red/green evidence and separate full Go gates; R2's
  integrated gate includes R1. Local checks are distinct from each PR's CI.
- Web verification uses isolated pnpm 10.15.1. The machine-wide corepack shim
  points to a missing cache entry; it was not altered or presented as working.
- No combined local `make check`, native full gate, real-provider call, production
  webhook delivery, restore, deployment or release is claimed by this checkpoint.
- Owned worktrees/test services remain while this execution is active; they must
  be saved and removed before the final task report.

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
| W3 | LongCat attempts and a bounded GLM fallback timed out. Primary review read all listed production entrypoints, feed helpers, selected boundary callers, Settings.test.tsx and Onboarding.test.tsx. Other adjacent tests and CSS were not read in full. No independent full-slice approval, browser GUI, accessibility or performance audit is claimed. |
| A1 | LongCat reviewed core sync, API, persistence and relevant app callers. No runtime suite was run by the reviewer; omissions include several import/search/settings tests. No consequential defect was established. |
| A2 | LongCat supplied a partial app review; independent verification confirmed the sign-in failure. Reader-load cancellation ordering and several larger test files remain unverified. |
| M1 | LongCat reviewed kit sync/network paths and selected app callers. Independent verification confirmed the clean-EOF hint failure. No Swift suite was run by the reviewers; vector-runner and several UI areas were not read in full. |
| M2 | LongCat supplied a partial window/reader/app report. Settings/Login internals and several tests were omitted. Its navigation/optimistic-cadence candidates are not runtime-confirmed. |
| O1 | LongCat checked deployment contracts and ran backup plus shell syntax probes. No local Compose/image gate was run by the reviewer because its Docker CLI lacked the Compose plugin. Context-isolation and restore/update candidates remain unverified. |
| O2 | After LongCat timeouts, GLM returned a static snapshot report comparing current product docs with CLI, config, MCP, compatibility and selected service/pipeline code. Most service code, HTTP handlers, deployment files and native/web implementation were absent from that snapshot. Primary inspection adjudicated the token-key claim. |

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

**Fixed:** [#169](https://github.com/L-K-M/Earnest/pull/169), two completed
review rounds. [Fix plan](river-passive-reread.md).

River kept successful and already-seen IDs for an entire view mount. A later
keyboard, reader-toolbar or remote unread could not be followed by another
passive read until the view remounted. Five regressions failed first. The fix
deduplicates only outstanding writes, retaining later real reading events.

Review identified that an earlier failed intent could also be retried after a
newer unread. Two more regressions failed before extending the queue: pending
intent records its observed register revision and object identity, so unread
cancels stale intent and old callbacks cannot discard a newer event. This does
not change the sync conflict policy or invent optimistic register revisions.
All 37 River tests and the complete final web gate passed on `27dca93`;
all CI gates passed. Minor follow-ups about timer coupling, a superseded-intent
error banner and a transient absent-row lookup were deferred, not turned into
another push.

### R4: Android device sign-in cannot restart after polling fails

**Fixed:** [#170](https://github.com/L-K-M/Earnest/pull/170), two completed
review rounds. [Fix plan](android-device-login-retry.md). All current-head CI
gates passed; minor assertion-message wording was deferred.

Expiry, denial and transport/API failures stopped polling while keeping the
authorization, permanently disabling the server field and Start button. Four
Compose/Robolectric regressions failed on the actual disabled form before the
fix. Terminal failures now release the flow while preserving its error and
server; pending/slow-down responses continue and cancellation is rethrown.

Review follow-ups added message-less transport errors and per-authorization
client reuse. Both additional checks failed before those changes. All five
focused tests and the full core/app unit tests, debug assembly and lint passed
on `50daa27`. No new dependency or automatic auth retry was added. Dedicated
cancellation-disposal coverage, concurrent Start prevention and local expiry
remain explicit gaps or separate work, not claimed implemented.

### R5: A clean EOF permanently stops Mac live hints

**Fixed:** [#171](https://github.com/L-K-M/Earnest/pull/171), one completed
review round. [Fix plan](mac-event-reconnect.md). All current-head CI gates,
including universal archive validation, passed.

The transport can finish a valid event stream without error. AppModel returned
instead of reconnecting; the two-minute poll converged data but never restarted
live hints. A real local SSE receiver and the public sign-in path reproduced
this: the second connection timed out before the fix. It now reconnects after
the existing ten-second backoff without an immediate retry loop, preserving
session, cancellation and 401 exits.

The focused regression, all 139 App tests and all 113 EarnestKit Swift Testing
tests passed on `1122258`. Local Debug tests used Apple Development signing
and `GENERATE_INFOPLIST_FILE=YES` for the test bundle. An initial signed-harness
plist failure and a later removable-volume permission interruption were not
counted as regression evidence. After allowing the prompt, a clean red run
preceded the production change.

Review's post-sleep cancellation guard is redundant with the loop condition
and session guard before the next stream. Exponential clean-close backoff and
persistent-proxy diagnostics would introduce a new retry/reporting policy,
contrary to this fix's bounded plan. They remain separate work, along with
optional timeout slack and test-seam naming. No immediately-closing proxy or
battery benchmark was run.

## Refutations and deferrals

- **Mac export truncation:** refuted. The pinned OpenAPIRuntime collector throws
  `TooManyBytesError` for both declared and streamed bodies exceeding the limit;
  `Data(collecting:upTo:)` does not silently return a prefix.
- **Google Reader repeated include filters:** recovered S9 notes identify the
  older B6-3 conjunction/union candidate. No new protocol regression was
  established here; it remains deferred for compatibility-contract verification.
- **Current-guide discrepancies:** the stale unavailable-search sentence in
  `internal/mcp/llms.go`, the agent notify example missing required `--entry`
  (`docs/cli.md:203`), the priced/capped Gemini embedding refusal not stated
  explicitly in the enable checklist, the idle worker's up-to-30-second polling
  backoff and the overbroad OPML-only idempotency exception remain documentation
  follow-ups. No unlimited-spend workaround or runtime policy change is proposed.
- **CLI token key claim:** no empty-key credential-minting defect was established.
  `Auth.CreateToken` uses a random opaque secret and its hash; the copied Auth
  secret-key field is not used by that path. The CLI documentation overstates
  the environment requirement, but the snapshot's suggested cryptographic
  impact does not follow from the implementation. Validation-policy changes
  and wording correction are deferred, not labeled a vulnerability.
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
- Native builds were serialized. Android's full local gate used JDK 21 and the
  installed SDK. Mac's signed Debug App tests and full Swift Testing suite passed;
  neither suite's empty/filtered wrapper is counted as substantive test coverage.
- No combined local `make check`, real-provider call, production webhook delivery,
  restore, deployment or release is claimed by this checkpoint. Native/Go/web
  checks are separate, commit-specific evidence, not a comprehensive app audit.
- Task-local worktrees and test services are removed after saving wanted work;
  the final task report records cleanup. Logs remain outside the checkouts.

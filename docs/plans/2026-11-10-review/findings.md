# Findings

Baseline: `f8adbdb`. This is a partial review, not a clean bill of health.
The first fan-out returned eight of nineteen review reports, with 28 candidate
findings. All 134 historical backlog items were assessed separately.

States: **open** means not fixed, **implemented** means regression-tested
code has been submitted, **fixed** explicitly records a merged PR,
**refuted** requires contrary evidence, and **deferred** gives the reason.
The linked PR is authoritative for CI, review and merge status. Unverified
claims are labeled explicitly.

## Coverage and recovery

| Slices | Result |
| --- | --- |
| S1, S2, S3, S4, S7, S8, S10, W1 | Complete helper reports recovered |
| S5 | Timed out without a report |
| S6, S9, W2 | Interrupted without a report |
| W3, A1, A2, M1, M2, O1, O2 | Relay stopped before a helper report was produced |
| B1 to B8 | All eight reports recovered, covering all 134 headings |

The previous assistant hit its weekly limit before it could collect and
verify the results. LongCat supplied seven review reports and every backlog
report. S3 used GLM 5.3 after LongCat returned no usable answer and Devin
reported a rate limit. No missing report is interpreted as zero findings.
Independent, targeted LongCat verification resumed directly, without relays.

Native-client coverage here is backlog reconciliation, not the planned new
native review. Other agents' web and Android PRs are independent work and are
not claimed as implementations by this review.

## Blocking and important

### S1-1: Retention duration overflow

**Implemented:** [#155](https://github.com/L-K-M/Earnest/pull/155),
branch `fix/retention-day-overflow`.

`config.loadRetentionDays` accepted arbitrary positive integers, while
`cmd/earnest/serve.go` multiplied them into `time.Duration`. For example,
213504 days wraps to about 25 minutes. The startup retention sweep can then
tombstone ordinary entries and delete their stored bodies. Independent code
verification confirmed the path through `DiscoverRetentionEntries`.

`TestLoadRetentionBounds` failed before the fix for 106752, 213504, 213505 and
MaxInt64. It passes after validation caps the value at 106751, preserving all
previously safe whole-day durations. Race tests and vet pass for
`internal/config` and `cmd/earnest`. The initial command-package run had no
Postgres configured, so its database tests skipped; config tests need none.

### S7-1: A feed redirect can shut down the server

**Implemented:** [#156](https://github.com/L-K-M/Earnest/pull/156),
branch `fix/feed-redirect-fragments`.

A permanent redirect preserves its fragment in `outbound.Result.FinalURL`.
`Fetcher.Fetch` copied it into the persisted feed URL, which the service
rejects. Its failure fallback copied the same invalid target; the resulting
scheduler error stops the server. The failure also occurs for HTTP and parse
errors that bypass the fallback. Independent verification confirmed this.

Four new Postgres integration cases failed before the fix: success, 304,
HTTP 500 and parse failure after fragment-bearing 301/308 redirects. A
separate regression covers the rejected-item fallback and encoded fragments.
They pass after clearing both fragment fields on a copy of the target.
`go test -race ./internal/fetcher ./internal/outbound` passes against an
isolated Postgres 18 database with pgvector; vet also passes. Query strings,
credential scoping and SSRF checks are unchanged.

### S10-1: Lost LLM responses can refund billable requests

**Implemented:** [#159](https://github.com/L-K-M/Earnest/pull/159), branch
`fix/llm-uncertain-billing`.

`billingUnknown` refunded every `outbound.Connect` failure, including
read/write failures after a provider may have received the request. The fix
refunds a connection-class error only when a failed dial proves it was not
sent. Completion and embedding regressions failed before the fix for resets
and unexpected EOF, then passed while preserving pre-send refunds. A real
local HTTP server receives the request and drops its response; Postgres
settlement now retains the estimate and blocks another call at the cap.
Race tests for `internal/llm` and `internal/pipeline` pass. No real provider
was contacted. The stale budget paragraph in `docs/llm-providers.md` now
describes atomic reservation and reconciliation.

### S2-1: Concurrent password checks bypass the failure limit

**Implemented:** [#161](https://github.com/L-K-M/Earnest/pull/161), branch
`fix/concurrent-login-throttle`.

`Authenticate` and `CompatLogin` checked the throttle before hashing, then
recorded failures afterward. A deterministic Postgres table lock demonstrated
seven password checks passing a five-attempt limit. The fix atomically
reserves username and client slots before I/O, without holding the mutex
during hashing or queries. It separately tracks in-flight checks so a window
boundary cannot discard them. Aborted queries release slots without counting
failures; success still forgives only account failures, not client spraying.

Mixed-endpoint bursts now run five checks and refuse two, for both username
and client limits. Concurrency, cleanup and cancellation race tests pass
three consecutive runs. Existing success/spray/expiry tests pass, as do the
full Go race suite, vet and all 18 packaging tests. See the validation note
below for the separately discovered shell portability failure.

### S4-1: Redirect-target subscriptions can duplicate a feed

**Implemented:** [#160](https://github.com/L-K-M/Earnest/pull/160), branch
`fix/subscription-redirect-dedup`.

`subscribeTx` matched only the canonical URL, creating a second feed and
subscription for an existing redirect target. It now matches recorded aliases
while preferring exact canonical matches and preserving ownership ordering.
With the original query restored, the new regressions failed for public
reuse, target resubscription and credential rebinding; they pass with the
fix. Additional cases cover exact-match precedence, deleted subscriptions,
foreign private feeds, one river item after repeated fetches, and secrets
remaining off the public feed. Subscription, rebinding and kept-import race
tests pass. Existing duplicates are not automatically merged or deleted.

### S1-2: Agents can request paid summaries

**Deferred, authorization policy unresolved.** The route requires only
`entries:read`; an agent can trigger a cached or paid summary. Independent
verification found no documented human-only boundary for summaries. Unlike
Ask, summaries accept stored publisher content, not caller-supplied prompts.
Do not silently remove an existing capability by copying Ask's restriction.
A dedicated scope or per-agent allowance requires a policy decision.

### S3-1: Large range marks hold the global core lock

**Deferred, unmeasured performance claim.** `Through` materializes every
member and embeds results in one event and deduplication row under
`LockCore`. Large backlogs may stall writes. The atomic range contract is
intentional; an arbitrary cap or non-atomic chunking would break semantics.
Measure a large backlog before designing compact atomic range events.

### S8-1: Frame messages are not bound to a document load

**Fixed by separate work:** another agent's
[#146](https://github.com/L-K-M/Earnest/pull/146) merged as `962c6da`.
The recovered report identified the missing per-load token and checked that
branch's implementation. This review did not duplicate or author that fix;
its validation and review belong to the linked PR.

### S10-2: Summary input is not bounded like other pipeline tasks

**Deferred, independently confirmed by code reading.** `generateClaims` sends
the full stored text, unlike tasks using `Bound`. Very large articles can
exceed a provider context or budget estimate. Blindly truncating to the
existing 2048-byte prefix would also degrade summaries and make them appear
complete. A fix needs an explicit excerpt policy, accurate user-facing
notice, citation validation and cache-version coverage.

## Minor and nits

These are candidate findings from code reading, not independently verified
regressions. They are deferred unless a focused fix verifies them.

| ID | Candidate | Location | Disposition |
| --- | --- | --- | --- |
| S1-3 | Wrong-method requests return 404 instead of 405 | `internal/api/api.go` | Deferred protocol polish |
| S1-4 | Batch IDs ignore other entry filters | `internal/api/reader.go` | Deferred; define supported parameter combinations first |
| S1-5 | OpenAPI omits validated enum constraints | `internal/api/reader.go` | Deferred schema coverage |
| S2-2 | Browser cookie is not renewed with the sliding session | `internal/api/identity.go` | Deferred; needs browser/API expiry regression |
| S2-3 | Every session resolution updates its row | `internal/service/session.go` | Deferred; preserve expiry semantics when optimizing |
| S4-2 | Kept-import lookup scans a feed per chunk | `internal/service/kept_import.go` | Deferred; measure the query plan before adding indexes |
| S7-2 | Host limiter map has no eviction | `internal/outbound/outbound.go` | Deferred; safe eviction must preserve active limiter ownership |
| S7-3 | DTD scan rejects literal declarations in content | `internal/feed/charset.go` | Deferred; retain XML attack rejection while narrowing scanning |
| S7-4 | Site URL can retain a non-HTTP scheme | `internal/feed/urlclean.go` | Deferred validation hardening; never turn article identity URLs into a generic empty fallback |
| S8-2 | Icon sizes with commas are ignored | `internal/media/html.go` | Refuted as a conformance defect: HTML sizes is a space-separated token set, not a comma-separated list |
| S8-3 | ICO directory dimensions can differ from decoded dimensions | `internal/media/ico.go` | Deferred malformed-icon selection polish |
| S8-4 | Image responses lack Referrer-Policy | `internal/imageproxy/imageproxy.go` | Deferred defense in depth; accepted raster formats make no subrequests |
| S10-3 | Trailing-dot host normalization | `internal/config/config.go` | Unverified; do not assume DNS equivalence implies browser same-origin equivalence |
| S10-4 | VAPID public/private mismatch is accepted at startup | `internal/push/webpush.go` | Deferred configuration validation |
| W1-1 | Kick received during sync can be lost | `web/src/sync/runtime.ts` | Deferred; needs deterministic in-flight-pull regression |
| W1-2 | Per-entry state scans the entire pending outbox | `web/src/sync/client.ts` | Deferred performance measurement |
| W1-3 | Recovered sync retains its error | `web/src/sync/runtime.ts` | Deferred; distinguish transient errors from dropped-operation notices |
| W1-4 | Model operation keys concatenate actor and operation ID | `web/src/sync/model.ts` | Deferred model parity regression |
| W1-5 | Offline README says no push worker exists | `web/src/offline/README.md` | Deferred documentation correction |

## Backlog reconciliation

[backlog.md](backlog.md) covers every heading from the preserved
[original analysis](original-analysis.md): 41 implemented, 92 still open and
one no longer applicable, assessed against the baseline. The root
`ANALYSIS.md` now points to these records rather than presenting old claims
as the live backlog.

New features, redesigns and novel ideas remain deferred. Open assessments
must be rechecked against current code before implementation. In particular,
backlog labels are not a claim that every historical security or concurrency
finding is correct.

## Additional execution finding: portable backup pruning

**Implemented:** [#162](https://github.com/L-K-M/Earnest/pull/162), branch
`fix/portable-backup-pruning`.

The full local `make test` run passed the Go suite, then failed because
`scripts/backup.sh` used GNU `find -printf`. BSD/macOS and BusyBox find reject
that option; the pipeline can report success while leaving old backups
unpruned. Replace it with `find -exec basename`, retaining the UTC-name
ordering and retention policy. A test shim rejects `-printf` even on GNU
hosts: the old implementation fails, and the fix passes on macOS and inside
Alpine 3.24. Only dummy dumps were used.

## Validation and review checkpoint

- Full `go test -race -count=1 ./...` passed with Postgres 18 and pgvector,
  with packages serialized using `GOFLAGS=-p=1`. Vet and gofmt checks passed.
- The first broad run hit database-clone/cleanup timeouts on slow storage;
  it was stopped and rerun using an isolated database on an external SSD.
  This was a test-harness failure, not a passing check.
- `make test` on the authentication branch then stopped at the pre-existing
  backup portability bug. Its shell regression passes with #162; all 18
  Python packaging tests also passed separately. Do not read this as a claim
  that the combined local `make check` gate ran successfully.
- Every submitted fix has targeted red/green regression evidence. CI and
  completed feedback on each linked PR still determine merge eligibility.
- #155 review round 1: the alleged Config formatting leak is refuted by
  `Config.String`/`GoString` redaction. A hypothetical future constructor
  guard is deferred, not implemented as duplicate validation.
- #156 review round 1: nil FinalURL is refuted by the concrete outbound
  client's sole validated result constructor; the previous `URL.String`
  was not nil-safe either. Extra redirect-chain coverage is deferred.
- #150 merged after one minor-only review. Suggestions about a future
  missing packageManager field or hash suffix were not necessary for the
  actual pinned value and verified Node 24/26 builds.

## Review boundaries

- No new features, theme redesign or blanket refactoring in this batch.
- No changes in another agent's worktree or PR.
- CI and completed review feedback are required on each fix's latest commit.
- A passing reviewer job is not approval until its actual comments are read.
- The unfinished slices above remain a validation gap after this batch.

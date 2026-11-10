# Findings

Baseline: `f8adbdb`. This is a partial review, not a clean bill of health.
The first fan-out returned eight of nineteen review reports, with 28 candidate
findings. All 134 historical backlog items were assessed separately.

States: **open** means not fixed, **implemented** means a fix is awaiting
merge, **fixed** requires a merged PR, **refuted** requires contrary evidence,
and **deferred** gives the reason. Unverified claims are labeled explicitly.

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

**Implemented, unmerged:** [#155](https://github.com/L-K-M/Earnest/pull/155),
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

**Implemented, unmerged:** [#156](https://github.com/L-K-M/Earnest/pull/156),
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

**Open, verification in progress.** `internal/llm/client.go:billingUnknown`
refunds every `outbound.Connect` failure. The outbound classification also
covers read/write failures after a provider may have received the request.
The push transport already distinguishes dial failures from uncertain
mid-request failures. Acceptance: completion and embedding calls must retain
the estimate on connection resets, while proven pre-send failures refund it.

### S2-1: Concurrent password checks bypass the failure limit

**Open, verification in progress.** `Auth.Authenticate` and `CompatLogin`
check the throttle before password hashing, then record failures afterward.
The two critical sections do not reserve in-flight attempts. A simultaneous
burst can pass the check before any failure is recorded, multiplying both
password guesses and Argon2 memory use. A fix must preserve successful-login
semantics and not hold a process-wide mutex during hashing or database I/O.

### S4-1: Redirect-target subscriptions can duplicate a feed

**Open, independently confirmed by code reading; runtime test pending.**
`subscribeTx` matches only `feeds.url`, unlike `resolveKeptFeed`, which also
matches `permanent_redirect_url`. Subscribing to the target of a previously
recorded permanent redirect can create a second subscription and split state
between duplicate articles. A fix must prefer exact matches and preserve
private-feed credential rebinding. Alias ambiguity needs regression coverage.

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

**Deferred to existing work:** another agent owns
[#146](https://github.com/L-K-M/Earnest/pull/146), branch
`fix/web-frame-channel`. The recovered report identified the missing
per-load token and checked that branch's implementation. This review does
not duplicate or claim that fix; its merge and validation remain separate.

### S10-2: Summary input is not bounded like other pipeline tasks

**Deferred, independent verification in progress.** `generateClaims` sends
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

## Review boundaries

- No new features, theme redesign or blanket refactoring in this batch.
- No changes in another agent's worktree or PR.
- CI and completed review feedback are required on each fix's latest commit.
- A passing reviewer job is not approval until its actual comments are read.
- The unfinished slices above remain a validation gap after this batch.

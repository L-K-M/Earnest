# Adversarial review: tech-stack.md

Reviewer: stack angle. Checked against primary sources on 2026-11-01/02 (GitHub API, crates.io, proxy.golang.org, Codeberg API, vendor docs). Judgement is marked; everything else was verified.

## Findings

### 1. [major] "Borrow, don't fork" — Miniflux is far less borrowable than claimed

**Claim (tech-stack.md "Server language" / "Borrow, don't fork"; README "borrowing Miniflux packages"; api.md "Adapt Miniflux's handler"):** copy Miniflux packages file by file into your own schema; adapt its Google Reader handler.

**Why wrong:** Miniflux's entire server codebase lives under `internal/` (top level is only `client/`, `contrib/`, `internal/`, `packaging/`), so nothing is importable — literal copying is the only mechanism, which the draft does say. But the coupling makes "file by file" realistic only for a subset:

- `internal/googlereader/handler.go` imports **13 internal packages**: `config`, `http/request`, `http/response`, `integration`, `mediaproxy`, `model`, `proxyrotator`, `reader/fetcher`, `reader/handler`, `reader/subscription`, `storage`, `urllib`, `validator`. The Google Reader handler is bound to Miniflux's storage API and its own read-status model — exactly the model Earnest replaces (watermark + per-entry exceptions). This is a rewrite against Earnest's service layer with Miniflux as a reference, not an adaptation.
- Even the most library-like piece, `internal/reader/sanitizer/sanitizer.go`, imports `internal/config` (it reads `config.Opts`), `internal/reader/urlcleaner` and `internal/urllib`.
- `internal/reader/rss` emits `model.Feed` / `model.NewEntry` carrying Miniflux's entry-identity semantics, which collide with Earnest's own `(feed_id, hash(...))` + `content_hash` decision.

Genuinely borrowable: the RSS/Atom/JSON parsers behind new adapters, charset/encoding handling, `urlcleaner`, the scraper-rule corpus, and (highest value) Miniflux's years of encoded feed quirks and its test corpus.

**Licence (verified):** Apache-2.0, per-file SPDX headers, no NOTICE file at root — so obligations are keep headers, ship the licence, and state changes. The draft's handling is correct and easy here; this is not the problem.

**Evidence:** https://github.com/miniflux/v2/tree/main/internal ; https://github.com/miniflux/v2/blob/main/internal/googlereader/handler.go ; https://github.com/miniflux/v2/blob/main/internal/reader/sanitizer/sanitizer.go

**Proposed change:** re-scope the claim to "borrow parsers, encoding handling, URL cleaning, scraper rules and test corpus; write the greader subset from scratch against the service layer"; re-cost M0 accordingly (the Google Reader subset is new code, the "M" estimate in features.md is optimistic); restate the Go rationale (see below) around the library ecosystem and iteration speed, not "borrowing Miniflux directly". This also weakens README's stated chief reason for Go over Rust.

### 2. [minor] "load average past 1000 during this research" (tech-stack.md, §Server language)

A 1-minute load average above 1000 on an 8-core machine means the box is effectively frozen, not "saturated by Rust compiles"; either the number measures something else or the machine was broken (in which case Go builds suffer too). As written it cannot carry the Go-vs-Rust decision. Verified: nothing (local claim). Propose: drop the number or restate it (e.g. measured `cargo build` wall time vs `go build`), and let agent iteration latency — the honest argument — carry the point.

### 3. [minor] Data volume band hides a soft assumption (tech-stack.md, §Storage)

Arithmetic verified: 1000 feeds × 3/day × 5 yr ≈ 5.48M entries; × 11 KB ≈ 60 GB; 1024-dim `halfvec` × 5.48M ≈ 11.3 GB; FTS "adds a third" is plausible order. But 10 KB average sanitised content is the load-bearing assumption: full-text extraction of long-form feeds routinely yields 30–80 KB per item, and features.md makes full-text an M0 per-feed option. If a third of feeds go full-text, 5-year content lands nearer 150–250 GB before FTS. Still NAS-viable, so not blocking — but state the assumption and sensitivity where the "100 to 150 GB" figure is given, because the same assumption drives the retention-cap decision. Also make explicit that the 11 GB vectors assume embedding *every* ingested entry (llm.md's pipeline implies it; it is a policy choice, not a necessity).

### 4. [minor] Broken reference

README links `draft/validation.md`; no such file exists in draft/. Either add it or remove the row.

## Strongest honest case against each choice (judgement, informed by verified facts)

- **Go vs Rust.** Against Go: agent-written Go fails at runtime, not compile time — nil derefs, ignored errors, zero-value `time.Time` — precisely in the sync/state code that is hardest to test; the draft's own table concedes Rust's sqlx, ammonia and feed tooling are competitive (all verified current: sqlx 0.9.0, ammonia 4.2.4, feed-rs 3.0.0 with commits 2026-10-27, official rmcp 3.8.0); and Miniflux prior art is weaker than claimed (finding 1). For Rust: the compiler genuinely corrects agents, and the two Rust proposals' "weakest call" self-assessment may reflect the prompt's anchoring more than the merits.
- **Miniflux.** See finding 1. The realistic asset is reference code plus its quirk corpus, plus gofeed (2,878★, v1.5.0 2026-09-25) versus feed-rs (214★) — a 13× gap in battle-testing on pathological feeds, and feed quirks are the project's main bug source.
- **Postgres vs SQLite.** For SQLite: one user, ~3000 items/day (a write every ~29 s), zero admin, same engine already shipped inside two of three clients (GRDB, Room), trivial Litestream backups. Verified against: sqlite-vec is still v0.1.10-alpha.4 (2026-05-18), README says "pre-v1, expect breaking changes", and has no ANN index — brute force over 5.5M × 1024-dim vectors is seconds per query. The draft's conclusion stands.
- **River/huma/sqlc.** Verified: River v0.51.0 (2026-11-01), very active, pre-1.0; huma v2.40.0 (2026-10-27); sqlc v1.31.1 with commits 2026-10-30. Against: a pre-1.0 queue in the ingest hot path means migration churn for a solo maintainer. Mitigation below.
- **React.** Against: offline PWA is the web platform's weakest corner, and optimistic sync UIs are where the complexity lives; agents write React boilerplate well but state sync less well. The draft already shrinks offline scope, which is why this survives.
- **SwiftUI + NSTableView.** Against: maintaining an `NSViewRepresentable` bridge, selection/keyboard plumbing and diffable data source glue is real work that SwiftUI List would give free; the cited evidence (troz.net, verified to exist) is honestly labelled blog-level. The draft's own plan to re-measure is right; NSTableView is cheap insurance, not a commitment.
- **No shared client core.** Against: three sync engines (cursor, outbox, conflict rules, `410 Gone` resync) written in three languages is the single most drift-prone element of the plan, and JSON test vectors test what you thought to test. For: UniFFI means a permanent Rust FFI build across Xcode and Gradle for one person — worse. KMP imposes Kotlin on the Mac client — worse.
- **Arithmetic.** See finding 3; internally consistent.

## What I would pick (judgement)

**Go — same conclusion, corrected rationale.** The decisive verified facts are ecosystem depth where the risk actually lives (gofeed's exposure to pathological feeds; bluemonday is fine for this renderer's needs even if its last tagged release is 2023 — repo commits continued to 2025-04), iteration latency for agent-assisted solo development, single static binary + cross-compile, and an official MCP SDK at v1.8.0. "Borrow Miniflux directly" is not among them (finding 1). Postgres, React, SwiftUI-with-NSTableView-fallback, and no shared core: all as drafted, with two additions — wrap River behind a ~50-line enqueue/worker interface so the draft's own SKIP LOCKED fallback can replace it if pre-1.0 churn bites; and log client protocol anomalies server-side (the change log already exists) so drift is caught in production, not only in CI. Make the Mac sync engine a standalone, headless-testable Swift package from day one.

## Verified correct (spot-checks, no finding)

Versions/dates: gofeed v1.5.0; feed-rs v3.0.0/214★; readeck go-readability v2.1.3 and go-shiori repo archived (isArchived true); bluemonday last release 2023-10-12; ammonia 4.2.4; River v0.51.0/5.8k★/pre-1.0; apalis-postgres 1.0.0 2026-10-12; sqlx 0.9.0; huma v2.40.0; utoipa 6.0.0; go-sdk v1.8.0; rmcp 3.8.0; apns2 v0.25.0 (2024-10); webpush-go v1.4.0 (2025-01); a2 0.10.0 (2024-05); web-push 0.11.0 (2025-02); dom_smoothie 0.19.0 active; Miniflux 2.3.4 (2026-10-20), Apache-2.0, ~9.9k★, "Minimalist and opinionated"; pgvector 0.8.7 (2026-10-01); PG 19 exists; sqlite-vec v0.1.10-alpha.4 pre-v1; Sparkle 2.10(.1); swift-openapi-generator 1.14.x; Compose BOM 2026.09.00; Tailscale Funnel limited to 443/8443/10000 with non-configurable bandwidth limits; UnifiedPush adopting Web Push formats (F-Droid post); Capy native-reader post; NNW conditional-GET post; openrss.org/work/144; rachelbythebay.com/fs/ score project (enrolment by key). MCP go-sdk licence is MIT→Apache-2.0 in transition (permissive either way).

## Not verified

GRDB "7.12" exact patch (network blocked Swift Package Index and GitHub tags; choice of GRDB over SwiftData is sound regardless); Apple capability-table per-column checkmarks (page lists Push notifications and Time Sensitive Notifications as macOS capabilities, but the Developer ID column markers did not survive extraction — re-check before relying on the APNs-without-App-Store claim); the load-average claim (finding 2).

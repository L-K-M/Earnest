# Earnest tech stack proposal

## Architecture

```
                        ┌─────────────────────────────────────────────────┐
                        │              NAS (x86_64, Docker)               │
                        │                                                 │
   RSS/Atom feeds ─────►│  earnestd (single Rust binary, musl)            │
   WebSub hubs ────────►│   ├─ axum API  (/v1, OpenAPI via utoipa)        │
   (push)               │   ├─ fetcher   (tokio, per-host limits,         │
                        │   │            conditional GET, backoff,        │
                        │   │            WebSub callbacks, autodiscovery) │
                        │   ├─ pipeline  (dom_smoothie extract → ammonia  │
                        │   │            sanitize → embed → index)        │
                        │   ├─ jobs      (apalis-postgres, SKIP LOCKED)   │
                        │   ├─ imgproxy  (fetch/resize/cache to volume)   │
                        │   └─ embedded web build (React SPA)             │
                        │                                                 │
                        │  postgres:18 + pgvector  (metadata, FTS,        │
                        │                        embeddings, job queue) │
                        │  ntfy            (UnifiedPush distributor)      │
                        └──────┬──────────────────────────────────────────┘
                               │ HTTPS over Tailscale
            ┌──────────────────┼───────────────────┐────────────┐
            ▼                  ▼                   ▼            ▼
      Web (React PWA,    macOS (SwiftUI +     Android (Compose   LLM/API
      service worker,    NSTableView list,    + Room +           clients
      IndexedDB cache)   GRDB, WKWebView,     WorkManager,       (OpenAPI
                         Sparkle, APNs)       UnifiedPush)       token)
```

## Decisions at a glance

| Area | Choice | Runner-up | Why the winner |
|---|---|---|---|
| Server | Rust + axum 0.8, sqlx 0.9 | Go | best-in-class feed/extract/sanitize crates, lowest RAM, strong types constrain agent output |
| Storage | Postgres 18/19 | SQLite+Litestream | pgvector, SKIP LOCKED jobs, FTS in one engine, scales to hosted tier |
| Search | Postgres `tsvector`/pg_trgm | Tantivy 0.27 | zero extra infra; Meilisearch overkill |
| Embeddings | pgvector 0.8.7 | sqlite-vec | mature, same DB; sqlite-vec still alpha (v0.1.10) |
| Jobs | apalis-postgres 1.0 on PG | Redis+sidekiq-ish | no extra service; transactional enqueue |
| Web | React 19 + Vite + PWA | Svelte 5 | ecosystem, agent fluency, offline machinery |
| macOS | SwiftUI + AppKit list + GRDB | pure SwiftUI | NSTableView survives 100k rows; GRDB predictable |
| Android | Compose + Room + WorkManager | SQLDelight | first-party tooling; Room 2.8.5 |
| Push (Android) | UnifiedPush via self-hosted ntfy | FCM | self-hosted ethos, no Play dependency |
| Push (macOS) | APNs via Developer ID | polling | works outside App Store (Apple docs) |
| Shared code | none; generated clients | Rust+UniFFI | FFI tax exceeds payoff for one dev |
| API | code-first utoipa 6 → openapi.json | spec-first | less ceremony, contract still enforced in CI |

## 1. Server language: Rust (axum)

| | Rust | Go | TypeScript (Bun/Node) | Kotlin/JVM |
|---|---|---|---|---|
| Feed parsing | feed-rs 3.0 (2026-09) | gofeed v1.5.0 (2026-09) | @extractus/feed-extractor 8.0.3 (2026-08); rss-parser stale since 2023 | ROME 2.1.0 — dormant since Mar 2023 |
| Sanitizing | ammonia 4.2.4 (2026-10) | bluemonday 1.0.27 (2024-07, mature) | dompurify 3.4 — reference impl | jsoup 1.23.2 whitelist |
| Readability | dom_smoothie 0.19 (Mozilla port, 2026-10); readability-rs stale | go-readability archived Dec 2025; fork lives at codeberg.org/readeck/go-readability/v2 | @mozilla/readability 0.6 — *the* upstream, best fidelity | jsoup approximations only — real gap |
| NAS memory | ~tens of MB idle | moderate, GC | higher (runtime+deps) | highest (JVM) |
| Agent code | strict compiler rejects bad agent output; slower builds | simplest to generate well | good; loose typing hurts | verbose, fine |
| Deploy | one static musl binary | one static binary | bun single binary possible, node_modules reality | fat jar + JRE image |

Sources: https://crates.io/crates/feed-rs, https://crates.io/crates/ammonia, https://crates.io/crates/dom_smoothie, https://crates.io/crates/readability, https://proxy.golang.org/github.com/mmcdole/gofeed/@latest, https://github.com/go-shiori/go-readability (archived, deprecation notice), https://codeberg.org/readeck/go-readability, https://repo1.maven.org/maven2/com/rometools/rome/maven-metadata.xml, https://registry.npmjs.org/@mozilla%2Freadability.

Rust wins on library fitness and resource use; the strict compiler is the best defense against agent-generated subtle bugs. Go is a defensible second — its readability library just got orphaned-and-reforked, which is exactly the churn you don't want in your content pipeline. TS has the *best* extraction fidelity (Mozilla's own code) but the worst runtime profile and weakest type safety. Kotlin is eliminated by ROME's dormancy — you'd write your own parser.

## 2. Storage: Postgres. Search: PG FTS. Embeddings: pgvector.

Volume estimate, 1 user × 1000 feeds × 5 yr: mixed feeds average ~2–5 items/day → 2–8M items. At ~1 KB metadata, ~8 KB cleaned text, 30–50% FTS overhead, and 384–768-dim float32 embeddings (1.5–3 KB, or half with `halfvec`): roughly **50–110 GB total**, dominated by stored text — trivial for a NAS but enough that you want real vacuum/backup tooling, i.e. Postgres, not a file you rsync.

- PG FTS (`tsvector` + `websearch_to_tsquery`, pg_trgm for fuzzy feeds) covers reader search; add Tantivy only if relevance disappoints — it's embeddable in the same binary (0.27, 2026-10, https://crates.io/crates/tantivy). Meilisearch is a 340 MB separate service (v1.56, https://github.com/meilisearch/meilisearch/releases) — wrong size class for this deployment.
- sqlite-vec is still `0.1.10-alpha.4` (2026-05, https://github.com/asg017/sqlite-vec/releases) vs pgvector v0.8.7 (2026-10, https://github.com/pgvector/pgvector/tags). Postgres 19 is current (https://www.postgresql.org).
- SQLite's honest appeal (one file, trivial backup) loses on: concurrent fetch/API/job writers, immature vector extension, and it forecloses the hosted-tier path.

## 3. Fetcher (in-binary tokio subsystem)

- **Scheduling**: jobs table (`next_run_at`, host, priority); apalis-postgres workers (`SELECT … FOR UPDATE SKIP LOCKED`). Adaptive interval per feed from observed post frequency; clamp 15 min–24 h.
- **Conditional GET**: persist ETag/Last-Modified; treat 304 as free. Respect `Retry-After`, Cache-Control `max-age` floor.
- **Per-host limits**: semaphore per host (2 conns), per-host RPM budget, global pool cap.
- **WebSub**: subscribe when `Link rel=hub` present — spec is again a W3C Recommendation (June 2026, https://www.w3.org/TR/websub/) and Google's hub still runs (https://websub.appspot.com/). Real-time for YouTube/WordPress feeds; polling remains the default.
- **Backoff**: exponential w/ jitter; separate failure classes (DNS, TLS, HTTP 4xx = dead feed → quarantine; 429/5xx = retry).
- **Autodiscovery**: parse `<link rel=alternate>`; probe `/feed`, `/rss`, `/atom.xml`, `/.well-known/` on failure.
- **Cloudflare**: accept loss. FlareSolverr is a losing arms race (v3.5.2 still shipping but issues show repeated breakage into 2026, e.g. https://github.com/FlareSolverr/FlareSolverr/issues/1678). Mark CF-blocked feeds, rely on feed body, offer optional user-supplied solver endpoint.
- **Full-text extraction**: per-feed toggle; dom_smoothie on demand per item, cache sanitized result; never at ingest for all items (storage cost, latency).
- **Image proxy**: `/img/{hash}` — fetch, resize (image crate), WebP, LRU disk cache on volume; fixes mixed-content and dead-host images; dedupe by content hash.

## 4. Jobs and LLM pipeline

apalis 1.0 (https://crates.io/crates/apalis) with the Postgres backend — job rows live next to the data they mutate, enqueue is transactional with ingest. Named queues: `fetch`, `extract`, `embed`, `notify`, `llm.*`. LLM calls are jobs with idempotency keys, per-provider rate config (Ollama/OpenAI-compatible endpoint), prompt+model version stored on the job for replay. No Redis, no extra container — same failure domain as the DB.

## 5. Web: React PWA

React 19.3 + Vite + TanStack Query + service worker (Workbox) with IndexedDB mirror of unread items and a pending-actions outbox — that covers the only offline features a reader needs (list + article bodies + mark-read queue). React over Svelte 5 (5.57, active) and Solid purely for hiring-pool-of-agents: models write React most fluently, and the ecosystem (PWA tooling, reader-mode components) is deepest. htmx 2.0 rejected: offline/PWA forces client-side state anyway; you'd rebuild half a SPA inside a server-rendered app. SPA is embedded in the Rust binary — zero separate web infra.

## 6. macOS: hybrid, native all the way

- **Shell**: SwiftUI everywhere except the article list → wrap `NSTableView` via `NSViewRepresentable`. Pure-SwiftUI `List`/`LazyVStack` has a documented history of choking past ~10⁴ rows on macOS [partly unverified on macOS 26]; NSTableView virtualization is solved since the 90s.
- **Local DB**: GRDB 7.12 (released today — hyperactive, https://github.com/groue/GRDB.swift/releases) over SwiftData (still carries migration/CloudKit quirks [unverified]) and Point-Free's SQLiteData (1.12, promising but younger, https://github.com/pointfreeco/sqlite-data/releases). GRDB's explicitness matches sqlx on the server.
- **Article rendering**: WKWebView + sanitized HTML from the server + reader CSS. Native text layout for plain-text feeds. Don't re-sanitize client-side.
- **iOS later**: SwiftUI views, GRDB schema, and the WKWebView wrapper all port directly; the AppKit table wrapper is the only mac-only code.
- **Distribution**: Developer ID + notarization + Sparkle 2.10 (https://github.com/sparkle-project/Sparkle/releases). APNs works for Developer-ID apps outside the store per Apple (https://developer.apple.com/developer-id/); needs a paid dev account. Alternatively fall back to silent-fetch polling.

## 7. Android

Compose (BOM 2026.09), Room 2.8.5 over SQLDelight 2.4.1 (both current: https://dl.google.com/dl/android/maven2/androidx/room/room-runtime/maven-metadata.xml, https://repo1.maven.org/maven2/app/cash/sqldelight/runtime/maven-metadata.xml) — Room's KSP codegen and agent familiarity win; SQLDelight only if you later go KMP. WorkManager 2.12 for sync. **UnifiedPush via self-hosted ntfy** (spec now standardized on Web Push/VAPID — https://f-droid.org/2026/01/08/unifiedpush-5-years.html, https://unifiedpush.org/users/distributors/ntfy/) with an FCM product-flavor for Play builds later. Article view: WebView with server-sanitized HTML.

## 8. Shared code: none — generated clients instead

UniFFI (0.32, active under Mozilla, https://crates.io/crates/uniffi) and KMP both solve "write logic once" at the price of permanent FFI/multiplatform build complexity — a tax a solo dev pays forever. The actually-shared logic is thin (API calls, sync cursors, read-state), while the risky logic (extraction, sanitize, dedup) already lives server-side only. Generate clients from openapi.json: openapi-typescript + openapi-fetch (web), swift-openapi-generator 1.14 (macOS), openapi-generator 7.26 Kotlin/JVM (Android) — all active (releases above). Keep the *types* shared, not the code.

## 9. API contract: code-first, enforced contract

utoipa 6 (2026-09, https://crates.io/crates/utoipa) emits openapi.json from axum handlers; commit the spec, CI fails on drift, clients generate in CI. Pure spec-first adds a hand-maintained YAML with no payoff at this size; pure code-first with no exported spec invites client drift. Key contract decisions to get right *before* clients ship: cursor-based sync tokens (not timestamps), immutable item IDs from `(feed_id, guid-hash)` dedup, pagination envelope, API-key auth for LLM/agent consumers. Expose the same surface as an MCP server later — it's just a thin adapter over `/v1`.

## 10. Testing, CI, deploy

Golden-corpus tests: recorded feeds + HTML pages through parse→extract→sanitize (insta snapshots). sqlx tests against ephemeral Postgres; contract test that openapi.json matches committed spec. Playwright for web; xcodebuild unit/UI on a mac runner; Android instrumented on emulator. GitHub Actions → `Dockerfile` producing a ~30 MB scratch/distroless image (static musl, embedded SPA); compose = `earnestd` + `postgres` (+`ntfy`). Nightly `pg_dump` into restic. Expose only on Tailscale — no TLS/ingress work at all.

## The 5 most expensive-to-reverse decisions

1. **Postgres vs SQLite** — schema, ops, backup, and every query decision compounds.
2. **Item identity & dedup model** (guid hashing, canonicalization) — baked into DB, sync protocol, and every client's cache.
3. **Ingest-time vs render-time content pipeline** — extracting/sanitizing server-side commits you to stored blobs and reprocessing; moving it later is a full backfill.
4. **Sync API shape** (cursors vs timestamps, state model) — three clients plus external agents depend on it; versioning is permanent.
5. **Embedding model + vector schema** — re-embedding millions of items is the costliest migration in the system.

## Weakest part of this proposal

Rejecting a shared core and instead triplicating sync/read-state logic in TS, Swift, and Kotlin. Generated API clients share *types*, not *behavior* — dedup edge cases, cursor handling, and conflict resolution will drift between clients, and there's no mechanical check catching it. Mitigations: golden sync test-vectors consumed by all three client test suites, and pushing every possible rule server-side. The other proposer's Rust-core/UniFFI or KMP answer is genuinely better on this axis; I bet the slower development of three thin-but-redundant clients costs less than years of FFI plumbing — but it's a bet.

## Gaps

- SwiftUI List performance and SwiftData reliability on macOS 26 not verified on current releases (marked [unverified] where claimed).
- Did not check: axum OpenAPI ecosystem churn (utoipa 6 API stability), `image` crate WebP resize quality, ntfy battery cost vs FCM on modern Android, MCP spec details, PG `halfvec` support in stock pgvector Docker images, Meilisearch idle RAM (claimed "wrong size class" on binary size alone).
- Embedding dims/GB estimates are arithmetic, not benchmarked. Feed-per-day rate (~3 avg) is an assumption; real corpora vary 10×.
- No verification that `dom_smoothie` tracks Readability.js 0.6 behavior, only release activity.

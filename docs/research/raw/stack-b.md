# Earnest tech stack — proposal B (Rust core, thin native clients)

Compiled 2026-11-01. Versions checked against primary sources (crates.io API, proxy.golang.org, npm registry, Google Maven, GitHub releases) unless marked [unverified].

## Architecture

```
  macOS app (SwiftUI + AppKit list, GRDB, Sparkle) ──┐
  Android app (Compose, Room, WorkManager, ntfy)  ──┼──REST/JSON──▶ earnest (one Rust bin, axum 0.8)
  Web/PWA (React 19, service worker, Web Push)  ───┘                │  /api  (OpenAPI 3.1 via utoipa)
                                        Tailscale                  │  /web  (static React build)
                                                                   ├─ fetcher: scheduler │ cond-GET │ per-host
                                                                   │  limits │ WebSub │ backoff │ autodiscovery
                                                                   ├─ pipeline: readability → ammonia →
                                                                   │  store → LLM summarize → embed → FTS
                                                                   ├─ jobs: apalis 1.0 (Postgres storage)
                                                                   └─ image proxy: resize, cache to volume
                                                                          │
                                              PostgreSQL 18 (FTS GIN + pgvector) ── volume: DB + image cache
```

Deployment: `docker-compose.yml` on the x86_64 NAS — earnest (distroless static image), Postgres, ntfy (optional); reached via Tailscale.

## 1. Server language

| Criterion | Rust (axum 0.8.9) | Go | TypeScript | Kotlin/JVM |
|---|---|---|---|---|
| Feed parsing | feed-rs 3.0.0, updated 2026-09 (crates.io/crates/feed-rs) | gofeed v1.5.0, 2026-09-25 (proxy.golang.org) — excellent | rss-parser; fine | Rome — stale [unverified] |
| HTML sanitising | ammonia 4.2.4, 2026-10 (crates.io/crates/ammonia); best-in-class | bluemonday v1.0.27, last release 2024-07 (stable, slow) | DOMPurify/sanitize-html 2.18.1; canonical | Jsoup cleaner; good |
| Readability | dom_smoothie 0.19.0, 2026-10 (young but active; old `readability` crate dead, 2023) | go-shiori/go-readability, active 2025-12; battle-tested | @mozilla/readability 0.6.0 — the reference impl | none canonical |
| Memory on NAS | ~30–60 MB RSS | ~20–40 MB | Node/Bun baseline higher, GC | JVM 200 MB+, GraalVM to fix it is work |
| Agent-written code | Good, but borrow-checker loop slows agents | Best: fast compiles, huge corpus, simple toolchain | Fastest drafts, weakest boundary discipline | Good |
| Single image | ~15–25 MB static musl | similar static binary | ~150 MB+ node image | heavy or native-image complexity |

Recommendation: **Rust + axum + sqlx 0.9**. Ammonia + feed-rs + a maintained readability crate is the strongest safety-to-effort package, memory fits a NAS alongside other services, and it is the builder's stack. Go is the honest runner-up and wins on agent ergonomics and go-readability maturity — see "weakest part".

## 2. Storage, search, embeddings

**PostgreSQL 18 + pgvector.** One user's volume is tiny but the write pattern (parallel fetchers) and query shape (FTS + vector + feed filters in one SQL statement) suit Postgres. SQLite is a legitimate single-file alternative, but FTS5 + sqlite-vec concurrency under parallel fetch writes is a WAL-fight, and sqlite-vec is one weekend-maintained project (maintainer admits slow pace, issue #226; upstream resumed at v0.1.10-alpha.4, May 2026; a community fork exists: github.com/asg017/sqlite-vec, github.com/vlasky/sqlite-vec). pgvector stays active (github.com/pgvector/pgvector; 0.8.x [unverified exact]).

Search comparison:

| Option | Verdict |
|---|---|
| **Postgres FTS (tsvector+GIN)** | Chosen. Zero extra services, one query language with filters; ranking adequate for one user. Upgrade path: Tantivy index later. |
| SQLite FTS5 | Great for on-device (Mac/Android), not the server choice here. |
| Tantivy 0.27 (crates.io, active) | Best relevance/speed, in-process Rust; costs a second index to keep synced. Defer. |
| Meilisearch v1.56.0 (github.com/meilisearch/meilisearch/releases — active) | Lovely typo-tolerance, but a ~340 MB binary and RAM for one user. Vendor's own marketing overstates need. |

Embeddings: pgvector `halfvec` (1024-dim ≈ 2 KB/item) with binary quantization option; embeddings regenerate cheaply if the model changes, so don't over-engineer.

**Data volume, 1000 feeds, 5 years** (assumes ~2 items/feed/day ≈ 3.6M items; images capped):

| Component | Estimate |
|---|---|
| Sanitized article HTML+text (~18 KB/item) | ~65 GB |
| Metadata + indexes | ~15 GB |
| FTS GIN index | ~25–40 GB |
| Embeddings halfvec 1024-d | ~7 GB (binary ~0.5 GB) |
| Image cache (capped, LRU) | 50–200 GB, user-tunable |
| **Total ex-images** | **~110–130 GB** — trivial for a NAS; vacuum discipline matters more than engine choice. |

## 3. Fetcher

- Scheduling inside the Rust binary (tokio): adaptive per-feed interval from observed publish frequency + jitter; no cron.
- Conditional GET (ETag/If-Modified-Since) always; store 304s as health signal.
- Per-host semaphore + token bucket (≤1 concurrent, ≥2 s spacing) so 50 feeds on one host can't hammer it.
- Retry with exponential backoff + `Retry-After`, circuit-breaker a feed after N failures, dead-letter visible in UI.
- WebSub: subscribe when the feed advertises a hub (Google's hub still runs: pubsubhubbub.appspot.com; W3C spec). Needs a **public** callback — Earnest lives behind Tailscale, so expose one funnel route (Tailscale Funnel) or skip WebSub and poll at 5–15 min. Decide per comfort; not core.
- Cloudflare: Bot Fight Mode blocks feed fetchers outright and site owners must opt out (openrss.org/blog/using-cloudflare-on-your-website-could-be-blocking-rss-users; community.cloudflare.com/t/rss-feed-being-blocked/783062). Cloudflare now defines an `cf-rss-feed` label that Bot Fight Mode won't block (developers.cloudflare.com/security/web-assets/label-operations/), but configuring it is the *publisher's* job. Mitigate: honest UA with contact URL, per-feed manual override, "open original" fallback. Accept residual failures; do not build a headless-browser arms race.
- Autodiscovery: `<link rel="alternate">` + feed-rs sniffing of the URL itself.
- Full text: fetch article page under the same politeness rules, extract with dom_smoothie, sanitize with ammonia, store only sanitized HTML.
- Images: server-side proxy `/img?u=…` — allowlist, size cap, `image` crate resize, disk cache with TTL+LRU. Clients never load remote images directly (privacy + mixed-content).

## 4. Jobs & LLM pipeline

**apalis 1.0** (crates.io, hit 1.0 2026-10) backed by the same Postgres (github.com/geofmureithi/apalis). Queues: `fetch`, `extract`, `summarize`, `embed`. LLM jobs carry provider, model, prompt-version, cost, and item-hash idempotency; per-provider rate limits and token budgets; retries with backoff; dead-letter table surfaced in the admin UI. Re-running a prompt version over history is a job-fanout, not a migration. Runner-up: plain `SELECT … FOR UPDATE SKIP LOCKED` on a jobs table — equally valid, apalis just supplies retries/cron/monitoring.

## 5. Web client

**React 19.3.1 + TypeScript + Vite 8.3.5 SPA** (npm registry), built statically and served by axum; TanStack Query/Router. PWA with a service worker caching read articles (IndexedDB) for offline reading — the main offline need is "read on the train", not offline sync. Push: Web Push/VAPID through the server. Svelte 5.57.3 is smaller but every AI agent and hire knows React; Solid 1.9.18 is fine but ecosystem-thinner; htmx is attractive for server-rendered speed but forfeits the offline story and duplicates the article rendering path. React's ecosystem wins for a solo builder.

## 6. Mac client

- **Hybrid: SwiftUI shell, AppKit `NSTableView` for the article list** (wrapped in `NSViewRepresentable`). SwiftUI `List` on macOS remains poor at 10k–30k rows: all rows initialized, unusable scroll/selection (troz.net/post/2024/swiftui_lists/); one 2026 profile claims 100k rows at 22 FPS/800 MB for `List` vs 60 FPS/45 MB for `NSTableView` (rockxy.io/blog/virtual-scrolling-swiftui — single blog, treat as directional). Apple's WWDC26 performance lab fields large-list questions and hints at macOS 27 improvements [unverified] (developer.apple.com/videos/play/wwdc2026/8003/). With lists capped per-feed, `List` may suffice — keep the AppKit escape hatch.
- Local DB: **GRDB 7.11.1** (github.com/groue/GRDB.swift — active, June 2026). SwiftData in 2026 still bites on migrations, `#Predicate` edge cases, and large imports (nativefirstapp.com/learn/field-notes/swiftdata-production-2026-cloudkit-gotchas/, vburojevic.dev/blog/swiftdata-2026-production-ready/); third-party benchmarks put GRDB ~10–20× faster on bulk inserts (fractal-dev.com/blog/ios-databases — blog, not audited). "SQLiteData" (GRDB-based SwiftUI layer) exists but couples you to a small project; plain GRDB + a thin repository layer is safer. SQLite.swift: effectively dormant [unverified].
- Rendering: **WKWebView** rendering server-sanitized HTML with an injected stylesheet. Fidelity beats re-layout in SwiftUI `AttributedString`, and iOS reuses the same pipeline. Native text view later as a reader-mode option only.
- iOS reuse: SwiftUI views + GRDB schema port into a Swift package from day one; share the generated API client.
- Distribution: Developer ID + notarization + **Sparkle 2.10.1** for updates (github.com/sparkle-project/Sparkle/releases — active, Oct 2026; EdDSA-signed appcasts). Consequence: no APNs push on Mac — the app polls on launch/focus and fires **local** notifications; Web Push covers the browser. That is acceptable for RSS; revisit if an iOS App Store build happens.

## 7. Android

- **Kotlin + Jetpack Compose** (BOM 2026.09.00, dl.google.com/android/maven2). `LazyColumn` with stable keys handles tens of thousands of rows; no macOS-style cliff.
- DB: **Room 2.8.5** over SQLDelight 2.4.1 — first-party, KSP, KMP-ready if ever needed; builder knows it.
- Sync: **WorkManager 2.12.0** for periodic fetch of deltas (expedited when possible), plus push-triggered sync.
- Push: **UnifiedPush + self-hosted ntfy** as default (fits the NAS/Tailscale setup; no Google account) with FCM as a compile-time alternative for a future hosted tier. UnifiedPush is a real standard adopted by Element/Molly-class apps [unverified today; see unifiedpush.org].
- Rendering: WebView fed the same sanitized HTML; reader-mode text path shared with list views.

## 8. Shared code

**None across platforms; OpenAPI-generated API clients only.** The expensive logic (parsing, sanitising, rendering HTML) lives server-side; clients stay thin. UniFFI 0.32.2 (crates.io, active) would let a Rust core be shared, but FFI build pain across Xcode/Android Studio for marginal gain at n=1 user is a bad trade. Kotlin Multiplatform only makes sense if iOS later adopts Compose — then the Android domain layer moves to KMP; keep ViewModels platform-thin to preserve that option.

## 9. API contract

**Code-first with utoipa 6.0** (crates.io/crates/utoipa, active): axum handlers annotated, spec served at `/api/openapi.json`, CI diffs the committed spec to catch accidental breaks. Clients generate: Swift via **swift-openapi-generator 1.14.3** (github.com/apple/swift-openapi-generator — active, Oct 2026, Swift 6.2+), Kotlin via openapi-generator (its Kotlin output is uneven [unverified] — wrap it behind a repository layer and hand-fix if needed). Spec-first (oapi-codegen v2.8.2, active 2026-10) is the runner-up: better contract discipline, worse solo-dev velocity — the spec always lags the code unless you let it generate handlers.

## 10. Testing, CI, deployment

- Server: `cargo nextest`, testcontainers-Postgres integration tests, proptest fuzzing of feed-rs/dom_smoothie/ammonia pipeline over a golden corpus of pathological feeds (real-world junk feeds are the #1 bug source), insta snapshots of extracted HTML.
- Clients: swift-testing + snapshot tests; JUnit + Compose UI tests against a mock server generated from the same OpenAPI spec.
- CI: GitHub Actions — fmt/clippy/test matrix, `sqlx prepare` check, spec-diff gate, image build; Xcode notarize job; Android release SIGNED-AAB (side-load APK for personal use).
- Deploy: multi-stage build → distroless static image; docker compose pull + up (or watchtower); nightly `pg_dump` + weekly restore test; tracing logs to the container log, healthcheck endpoints, and a `/metrics` page.

## Choices vs runner-up

| Area | Choice | Runner-up | Why not runner-up |
|---|---|---|---|
| Server | Rust + axum 0.8 | Go + net/http | Go's agent ergonomics + go-readability; loses on ammonia+memory+single image |
| Storage | Postgres 18 | SQLite (WAL) | concurrency + FTS/vector maturity; sqlite-vec bus factor |
| Search | Postgres FTS | Tantivy | second index to sync; relevance sufficient for 1 user |
| Jobs | apalis + Postgres | Redis queue | one less service to run |
| Web | React SPA + PWA | htmx SSR | offline/PWA requirement |
| Mac UI | SwiftUI + AppKit table | pure SwiftUI | long-list performance record |
| Mac DB | GRDB 7.11.1 | SwiftData | migrations/insert-perf field reports |
| Android | Compose + Room | Views + SQLDelight | builder skill; first-party tooling |
| Push | ntfy/UnifiedPush + Web Push | FCM | no Google dependency; self-host fit |
| Shared | none; generated clients | UniFFi core | FFI tax ×3 platforms at n=1 user |
| API | utoipa code-first | spec-first oapi-codegen | solo velocity; CI diff preserves safety |

## Five most expensive decisions to reverse

1. **Postgres vs SQLite** — every FTS/vector query and the concurrency model rewrite; a migration is a full reindex.
2. **Item identity & sync protocol** (entry-hash scheme, read/star state, tombstones) — get it wrong and you get duplicate articles and lost read-state across 3 clients; retrofitting tombstones is brutal.
3. **Stored article representation** (sanitized HTML schema, summary/embed columns) — all three renderers and every cached item depend on it.
4. **Per-platform persistence + sync journal** (GRDB/Room schemas, delta cursors) — client migrations for data you no longer have upstream.
5. **Push channel per platform** (ntfy endpoints, Web Push subscriptions) — silently installed on devices; rotating later means users re-onboarding.

## Weakest part of this proposal

Choosing Rust over Go. Every runtime argument for Rust (memory, image size) is marginal at 1000 feeds — Miniflux proves Go+Postgres runs this workload in production for years (miniflux.app; vendor's own docs [Go+Postgres not re-verified today]). Go's iteration speed with AI agents (instant compiles, no borrow-checker loop) and the more battle-tested go-readability would likely ship v1 faster. I bet on Rust because the builder already runs axum/sqlx and ammonia is the best sanitizer story; if velocity slips, this is the decision to revisit — switching before clients exist is a 2-week port.

## Gaps

- pgvector's exact current version (GitHub releases page empty; CHANGELOG fetch 404).
- UnifiedPush/ntfy current adoption claims; openapi-generator Kotlin output quality — not verified hands-on.
- rockxy.io NSTableView-vs-List numbers and fractal-dev GRDB benchmarks: single blogs, not reproduced.
- macOS 27 SwiftUI list improvements: WWDC26 Q&A hint only.
- Tailscale Funnel viability/limits for a WebSub callback not tested.
- Bun vs Node for a TS server; Solid/htmx depth — not investigated (not chosen).
- LLM model/provider choice and embedding dimensions assumed (1024-d), not researched.
- Miniflux's current architecture not re-verified today.

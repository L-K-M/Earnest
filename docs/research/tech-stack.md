# Tech stack

Versions checked 2026-11-01 against crates.io, proxy.golang.org and the GitHub API.

## Architecture

```
                    tailnet                         NAS (Docker Compose)
 Web (React PWA) ─────────┐             ┌──────────────────────────────────────┐
 Mac (SwiftUI + AppKit) ──┤   REST/SSE  │ earnest (one Go binary)              │
 Android (Compose) ───────┼────────────►│  api      /v1, OpenAPI, SSE          │
 Agents (MCP, REST, CLI) ─┤             │  compat   Google Reader subset       │
 Capy, NetNewsWire ───────┘             │  mcp      Streamable HTTP            │
                                        │  ──────── service layer ───────────  │
                                        │  fetcher  schedule, GET, per-host    │
 Feeds, sites  ◄── egress proxy rules ──│  pipeline parse, extract, sanitise   │
 LLM providers ◄────────────────────────│  llm      queue, cache, budgets      │
 APNs, FCM, Web Push, ntfy ◄────────────│  push     one transport interface    │
                                        │  web      embedded SPA               │
                                        └───────────────┬──────────────────────┘
                                                        ▼
                                  Postgres + pgvector (data, FTS, vectors, jobs)
```

Every entry point (REST, compatibility API, MCP, CLI) goes through one service layer. None touches the database directly, and none implements its own rules. Budgets and scopes live in the service layer, so a new surface cannot bypass them.

## Server language: Go, provisionally

Both research proposals chose Rust and both named that as their weakest decision. Their prompt also mentioned axum and sqlx, which may have anchored them. Two reviewers from other labs (GPT-6.1 Sol and GLM-5.3) also picked Go when asked to attack the choice, but for narrower reasons than this draft first claimed.

| Factor | Go | Rust |
| --- | --- | --- |
| Feed parsing, where most bugs live | [gofeed](https://github.com/mmcdole/gofeed) v1.5.0, 2.9k stars | [feed-rs](https://github.com/feed-rs/feed-rs) v3.0.0, 214 stars |
| Readability | [readeck/go-readability](https://codeberg.org/readeck/go-readability) v2.1.3, used by Readeck. The original go-shiori repository is archived | [dom_smoothie](https://github.com/niklak/dom_smoothie) 0.19, 231 stars, active |
| Sanitising | `golang.org/x/net/html` tree-based, as [Miniflux does](https://github.com/miniflux/v2/blob/main/internal/reader/sanitizer/sanitizer.go). [bluemonday](https://github.com/microcosm-cc/bluemonday) v1.0.27 (2024) is a [non-validating tokeniser](https://github.com/microcosm-cc/bluemonday#limitations), so prefer the tree | [ammonia](https://github.com/rust-ammonia/ammonia) 4.2.4 on html5ever, better |
| Jobs on Postgres | [River](https://github.com/riverqueue/river) v0.51, 5.8k stars, very active, pre-1.0 | [apalis-postgres](https://crates.io/crates/apalis) 1.0, released 2026-10-12 |
| SQL checking | [sqlc](https://github.com/sqlc-dev/sqlc) at generate time | [sqlx](https://github.com/launchbadge/sqlx) 0.9 at compile time, better |
| OpenAPI from code | [huma](https://github.com/danielgtaylor/huma) v2.40 | [utoipa](https://github.com/juhaku/utoipa) 6.0 |
| MCP SDK | [go-sdk](https://github.com/modelcontextprotocol/go-sdk) v1.8.0, official, serves 2026-07-28 | [rmcp](https://github.com/modelcontextprotocol/rust-sdk) 3.8.0, official |
| Prior art under a permissive licence | [Miniflux](https://github.com/miniflux/v2), Go and Postgres, Apache-2.0 | None comparable |
| Compile and iteration time | Seconds, small cache | Minutes, `target/` runs to gigabytes per worktree |
| Compiler as agent supervisor | Weak. Nil, ignored errors and zero-value times fail at runtime, in exactly the sync code that is hardest to test | Strong |
| Runtime memory | Fine | Better, irrelevant at this scale |

**The case against this choice is real.** Agent-written Go fails at runtime where agent-written Rust fails at compile time, and the sync engine is where that hurts. Rust also has the better sanitiser and SQL checking. If you have materially more experience in Rust, that could reverse the decision. A later port would be expensive, so don't treat this as easily reversible.

**Mitigations.** `errcheck`, `staticcheck` and `nilaway` in CI. Wrap River behind a small enqueue and worker interface, so a plain `SKIP LOCKED` table can replace it if pre-1.0 churn bites.

**Miniflux: reference, not a parts bin.** All its server code sits under `internal/` and can't be imported. Coupling is real: [the Google Reader handler imports 13 internal packages](https://github.com/miniflux/v2/blob/main/internal/googlereader/handler.go), and even the sanitiser reads global config. Worth copying, with a bounded spike to confirm: RSS, Atom and JSON parsers behind new adapters, encoding handling, `urlcleaner`, scraper rules and test cases. Write the rest against your own service layer. Files keep their Apache-2.0 headers, and modified files must say so. [Decided since](decisions.md#licence-and-code-reuse): nothing is copied, and Miniflux is reference only.

**Don't fork it.** [Miniflux entries are per-user with a `status` column](https://github.com/miniflux/v2/blob/main/internal/database/migrations.go), and feeds are unique per user. There is no change log, actor model or annotation layer. Migrations are an append-only list, so a diverged schema stops taking upstream migrations the day you change `entries`, and that change is the project.

## Storage: Postgres

Postgres 18 with pgvector 0.8.7. [Postgres 19 is at release candidate](https://www.postgresql.org/about/news/postgresql-19-rc-1-released-3409/), with general availability planned for 19 November. Volume estimate at 1000 feeds, about 3 items per feed per day, for 5 years: 5.5M entries. Assume 10 KB of sanitised content and 1 KB of metadata per entry, roughly 60 GB before indexes; FTS adds a third. 1024-dimension `halfvec` embeddings for every entry add about 11 GB before HNSW overhead. Expect **100 to 150 GB**, excluding images.

The 10 KB figure carries this estimate. Extracted long-form articles often run 30 to 80 KB, so if a third of feeds use full text, content alone could reach 150 to 250 GB. That's still fine for a NAS. Retention and embedding only what you might search are policy levers.

The honest case for SQLite: one file, no service, trivial backups, and write contention at 3000 items a day is a non-issue, whatever the research reports implied. It loses on vector search (sqlite-vec is still `v0.1.10-alpha.4`, pre-v1, and [its ANN indexes exist only in those alphas](https://github.com/asg017/sqlite-vec/releases), so a stable release means brute force over 5.5M vectors), on job handling, and on any hosted future. Choose Postgres.

**Search.** `websearch_to_tsquery` with a `tsvector` per entry, weighted title over body, and per-feed language with a `simple` fallback. Postgres has no BM25, so ranking is adequate rather than good. Hybrid retrieval via reciprocal rank fusion with vectors should help, unmeasured. If it disappoints, add an embedded index later. Meilisearch would be a second service with little payoff at one user.

## Fetcher

The hardest code in the project, and where Miniflux helps most.

- **Polite by construction.** Always send conditional GET, ask for compression, and honour `Cache-Control` and `Retry-After`. Default no more often than hourly unless the feed earns it. Consider [Rachel Kroll's feed reader score project](https://www.goldenhillsoftware.com/2024/08/feed-polling-for-unread-cloud/) as an external test: it has caught bugs even in [NetNewsWire](https://inessential.com/2024/08/03/netnewswire_and_conditional_get_issues.html). Her site wasn't reachable from here, so whether it still enrols readers is unchecked.
- **Adaptive schedule.** Interval from recent publishing frequency with jitter, clamped between 15 minutes and 24 hours. Per-host concurrency of one and a minimum spacing, so 50 feeds on one host behave as one client.
- **Honest failure classes.** DNS, TLS, 4xx gone, 429, 403 challenge, parse error, each visible in feed health, with a backoff suited to the class.
- **Cloudflare.** [Bot Fight Mode blocks feed fetchers](https://openrss.org/work/144), and the fix belongs to the publisher. Send an honest user agent, show the failure and move on. Don't build a headless-browser arms race. A residential IP may fare better than a datacentre one, as [NewsBlur's IP-level blocks](https://forum.newsblur.com/t/feed-retrieval-problems-403-errors/10761) suggest. That is untested.
- **Entry identity.** `(feed_id, hash(guid or canonical link or title+date))` plus a server ingest `seq`; see [api.md](api.md). Decide once: it is in every client's cache. Track `content_hash` separately to detect edits without creating duplicates. Internal IDs are 64-bit integers so Google Reader's long-form item IDs map without a lookup table.
- **Full text on demand.** Per-feed setting, extract once, cache. Store sanitised HTML only, and keep the raw feed payload briefly for debugging.
- **Authenticated feeds.** Per-feed HTTP auth and custom headers, encrypted at rest. These feeds are private to their owner; see [api.md](api.md).

## Security

A home NAS that fetches attacker-supplied URLs deserves more than a bullet. Minimum controls for a one-person project, in rough order of likelihood times damage:

**One outbound client, used by every sink.** Feeds, discovery, OPML import, full-text extraction, favicons, image proxy, push and webhook delivery all go through it.

- Check the address at connect time with `net.Dialer.Control`, which sees the IP actually being dialled. That closes the DNS rebinding gap that a check-then-fetch leaves open. Don't reuse connections across hosts.
- Deny table with a test per range: `0.0.0.0/8`, `10/8`, `100.64/10`, `127/8`, `169.254/16`, `172.16/12`, `192.168/16`, `198.18/15`, `240/4`, `::/128`, `::1/128`, `::/96`, `fc00::/7`, `fe80::/10`, `::ffff:0:0/96`, `64:ff9b::/96`. [Go treats `[::]` as the local system](https://pkg.go.dev/net#Dial), and [`netip.Prefix.Contains` returns false for zoned addresses](https://pkg.go.dev/net/netip#Prefix.Contains), so reject any address with a zone first, or `[fe80::1%eth0]` passes every range. [MCP's guidance warns against hand-rolled IP parsing](https://modelcontextprotocol.io/specification/2026-07-28/basic/security_best_practices), for reasons like these, so seriously consider [Smokescreen](https://github.com/stripe/smokescreen).
- No proxies from the environment: `Transport.Proxy` is nil, since a dial check would validate the proxy rather than the destination.
- Schemes `http` and `https` only. Ports 80 and 443 by default, with per-feed exceptions you approve. Validate every redirect hop the same way. OPML import is rate-limited, since one request becomes hundreds of fetches.
- **Credentials never cross origins.** Authenticated feeds drop basic auth and custom headers on any cross-origin redirect. [Go's client only strips some headers](https://pkg.go.dev/net/http#Client) on its own.
- The per-host limiter lives in this client, so extraction and image fetches can't burst at a host.
- Allowed private destinations (a local model server, webhook targets on the tailnet) are configured explicitly by you, per destination.
- Run the fetcher on a Docker network with no route to your LAN if practical.
- Conformance tests: a resolver returning public then private addresses, IPv4-mapped IPv6, zoned and unspecified IPv6 literals, redirects to private ranges, credentials on cross-origin redirects.

**Image proxy.** A signature proves Earnest issued a URL, not that its destination is safe, and a feed author chooses that destination. [Miniflux shipped exactly this SSRF](https://github.com/miniflux/v2/security/advisories/GHSA-xwh2-742g-w3wp): valid HMAC, no check on target. Signing prevents open-proxy abuse only. The destination check comes from the outbound client. Add size caps, content sniffing with `nosniff`, and never SVG.

**HTML, with three renderers.** One server sanitiser and three parsers (WKWebView, Android WebView, browser) invite mutation XSS.

- Parse to a tree, allowlist, serialise, re-parse until stable. Forbid `template`, `noscript`, `svg`, `math`, `style` and `data:` URIs.
- Web: a sandboxed `iframe` on a separate origin, with its own CSP (the app's doesn't govern a cross-origin frame) and a strict CSP on the app too. [Sandboxing doesn't stop a frame navigating itself](https://html.spec.whatwg.org/multipage/browsers.html#sandboxing), so the sanitiser sets `target="_blank"` and `rel="noopener noreferrer"` on every link, and the frame allows only `allow-popups allow-popups-to-escape-sandbox`.
- Mac: `WKWebView` with JavaScript off, a custom scheme, a strict CSP, and a `WKNavigationDelegate` that allows only that scheme. With JavaScript off, `meta refresh` and forms can still navigate.
- Android: `javaScriptEnabled=false`, `allowFileAccess=false`, `allowContentAccess=false` (defaults to true), `WebViewAssetLoader`, and allowlisted navigation.
- Test with mutation XSS payloads against each real renderer, not just the sanitiser.

**Agent text** is plain text in all three clients, never rendered as HTML or Markdown. See [llm.md](llm.md#threat-model).

**Parser abuse.** XML without DTD or entity expansion. Limits on size, time and decompression ratio.

**Deep links.** `earnest://` and Android intent filters accept internal IDs only. Any app can send one.

**Tokens.** See [api.md](api.md). A log-scrubbing test keeps them out of logs.

**Supply chain.** `govulncheck` in CI, actions pinned to commit SHAs, image digests pinned, Renovate or Dependabot.

## Reachability

"Tailnet only" is the right start and has consequences the research reports missed or contradicted.

| Needs inbound from the public internet | Works on tailnet only |
| --- | --- |
| WebSub callbacks | Polling, all clients on tailnet |
| Cloud-hosted LLM clients, with the exception below | Local agents and CLIs on the tailnet |
| Inbound email | IMAP polling of a mailbox |
| Public shared feeds | Outbound push: APNs, FCM, Web Push, ntfy |
| | Outbound webhooks to agents on the tailnet |

Browsers require HTTPS for service workers and Web Push; `tailscale serve` gives a MagicDNS name and certificate. Phones and Macs must be on the tailnet to fetch, though push still arrives without it.

**Exception.** OpenAI's [Secure MCP Tunnel](https://developers.openai.com/api/docs/guides/secure-mcp-tunnels) lets ChatGPT, Codex and the Responses API reach a private MCP server through an outbound-only client. Plan eligibility isn't documented on that page, and no equivalent was confirmed for Claude.

When public access is wanted, run a **second listener in the binary**, bound to its own loopback-only port, exposing only `/mcp`, `/.well-known/*`, `/oauth/*` and `/websub/*`. Put Tailscale Funnel or a Cloudflare Tunnel in front of that port and nothing else. [Funnel is all or nothing per port](https://tailscale.com/docs/features/tailscale-funnel): if the last command on a port was `funnel`, the port is completely public, so one stray command on the main port publishes `/v1/tokens`. Check for this at startup. Tokens issued for one listener must not work on the other.

## Web

React 19, TypeScript, Vite, TanStack Query and Virtual. The SPA is embedded in the Go binary. React because agents write it reliably and the fleet already uses it. htmx is ruled out by offline and optimistic-update needs. Offline covers reading plus a queue of actions; this is the web platform's weakest corner, so keep the scope small.

## Mac

- **SwiftUI shell, `NSTableView` for the entry list**, wrapped in `NSViewRepresentable`. SwiftUI `List` has a [history of struggling at 10k rows and beyond on macOS](https://troz.net/post/2024/swiftui_lists/), though the evidence is blog-level and worth re-measuring on macOS 27 with a real list. Start with the table; it is cheap.
- **GRDB** 7.12 over SwiftData (migration and bulk-insert complaints) and SQLiteData (younger, and adds a dependency).
- **WKWebView** for articles, JavaScript off, served through a custom scheme with a strict CSP. Injected typography stylesheet.
- **Distribution.** Developer ID, notarised, [Sparkle 2.10](https://github.com/sparkle-project/Sparkle). [Apple's capability table](https://developer.apple.com/help/account/reference/supported-capabilities-macos) lists Push Notifications and Time Sensitive Notifications for Developer ID, so one research report's "no APNs outside the App Store" is wrong.
- **Keep Swift packages portable.** Make the sync engine a headless, testable package from day one. iOS would then cost less.

## Android

Compose BOM 2026.09, Room, WorkManager, `ListDetailPaneScaffold`. WebView with JavaScript off, reusing Kararead's `ReaderHtmlBuilder` approach. Capy Reader recently [replaced WebView with a native HTML-flattening renderer](https://jocmp.com/2026/10/07/reader-refresh-e-ink-improvements), which is worth studying later. Distribution as APK through GitHub Releases, matching the fleet.

## Push

| Platform | Transport | Credential |
| --- | --- | --- |
| Browser | Web Push (RFC 8030, 8291, 8292) | VAPID key on your server |
| Android | [UnifiedPush](https://unifiedpush.org) via ntfy; FCM flavour later | None for UnifiedPush |
| Mac | APNs token-based | Your `.p8` key on your server |
| Anything | ntfy direct | Topic token |

UnifiedPush [now follows the Web Push format](https://f-droid.org/2026/01/08/unifiedpush-5-years.html), so browser and UnifiedPush can share an encoder, provided it really does RFC 8291 encryption. [Follow UnifiedPush's server guidance](https://unifiedpush.org/news/20250513_push_security_privacy/): reject endpoints on private addresses and don't follow redirects. Payload rules are in [api.md](api.md).

**The relay problem.** The APNs key and Firebase credentials belong to whoever publishes the app. On your own NAS with your own builds, that's you. If strangers install your signed binaries against their own servers, they can't hold your key, and you'd need a publisher-run push relay, as Bitwarden and Home Assistant do. Hide transports behind one interface and defer this until a public release is real. UnifiedPush avoids it on Android.

## Shared client code: none

The tricky logic (parsing, sanitising, ranking) is on the server. What remains client-side is sync: cursors, an outbox and conflict rules. Three implementations will drift, and that is this plan's main risk. Mitigate with:

- OpenAPI-generated clients: `openapi-fetch` for web, [swift-openapi-generator](https://github.com/apple/swift-openapi-generator) 1.14 for Mac, `openapi-generator` for Kotlin, wrapped behind a repository layer because its output is uneven.
- **Language-neutral sync test vectors**: JSON fixtures of server states, client outbox contents and expected results, run by the server and all clients in CI. [api.md](api.md#change-feed) lists a starting set.
- Pushing every decision that can live on the server to the server.
- Logging protocol anomalies on the server, so drift is noticed in use and not only in CI.

A Rust core via UniFFI would replace all three sync engines with one, at the cost of a permanent FFI build across Xcode and Gradle, plus a language you'd no longer use on the server. Kotlin Multiplatform would impose Kotlin on Swift clients. Revisit only if the test vectors fail to contain the drift.

## API contract

Code-first with huma: the spec is generated from Go types, committed, and diffed in CI. Client generation runs from the committed file. Spec-first with oapi-codegen is the honest runner-up with better discipline. Either way, the committed `openapi.json` is the thing clients and agents depend on.

## Testing and CI

- **Golden corpus.** A checked-in pile of pathological real feeds and pages run through parse, extract and sanitise with snapshot tests. Real-world junk is the main bug source.
- **Fetcher conformance.** A local server scripted to send 304s, 429s with `Retry-After`, redirects to private IPs, gzip bombs and malformed XML.
- **Sanitiser tests** with mutation XSS payloads, run against each real renderer.
- **Sync test vectors** per [api.md](api.md), plus a soak test with concurrent clients.
- **Mac builds** on the planned Mac mini VM or GitHub macOS runners. Private repositories pay for those runners by the minute, and the fleet's Actions budget has already run out once. Prefer making this public, or local builds.
- Fleet conventions: `scripts/build.sh`, `scripts/release.sh`, `update.sh`, hardening trio in CI, once there is code.

## Expensive to reverse

1. **Entry identity, `seq` and dedupe.** In every cache, every client and the Google Reader mapping.
2. **State registers, conflict policy and change-log protocol.** See [api.md](api.md).
3. **Postgres.**
4. **Stored content representation.** Three renderers depend on the sanitised HTML subset. Define and version it.
5. **Embedding model and dimension.** Re-embedding millions of items costs money or days. Store the model version with every vector.

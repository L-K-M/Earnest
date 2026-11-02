# Earnest Feature Matrix

**Date:** 2026-11-01  
**Scope:** Personal-first, self-hostable RSS reader with Web, macOS, Android clients + API for LLM agents  
**Builder:** Solo dev, comfortable with Rust/Postgres/React/SwiftUI/Kotlin/Docker, self-hosts on NAS via Tailscale  
**Sources:** Competitor feature pages (Feedly, Inoreader, NewsBlur, Feedbin, Miniflux, FreshRSS, NetNewsWire, Reeder, Readwise Reader) + first principles

---

## Feature Matrix

### Ingest

| Feature | Tier | Clients | Cost | Reason |
|---------|------|---------|------|--------|
| RSS 1.0/2.0, Atom 0.3/1.0, JSON Feed 1.0/1.1 | MVP | All | S | Table stakes; all competitors support |
| WebSub (PubSubHubbub) push | v1 | Web, API | M | Real-time without polling; Feedbin, NewsBlur, FreshRSS have it |
| Newsletter ingestion (unique email addresses, plus-addressing, deactivation) | MVP | All | M | Feedbin/Inoreader/NewsBlur/Readwise do this; solves inbox clutter |
| YouTube channels/playlists (RSS + Invidious fallback) | MVP | All | S | Feedbin, Reeder, Readwise, Inoreader support; no API key needed |
| Reddit (via RSS endpoints) | v1 | All | S | Reeder, Inoreader, NewsBlur support; Reddit killed free API |
| Mastodon/Bluesky timelines & search feeds | v1 | All | M | Reeder, Inoreader, NewsBlur have native support |
| Podcasts (RSS enclosures + inline player) | MVP | All | M | Feedbin, Inoreader, Reeder, Readwise, Miniflux all support |
| Full-text extraction for partial feeds (Readability + custom CSS selectors) | MVP | All | M | Feedbin, NewsBlur, Miniflux, FreshRSS do this; essential for partial feeds |
| Web feeds (scrape any site via CSS/XPath/JSON) | v1 | Web, API | L | NewsBlur, Inoreader, FreshRSS, Miniflux support; high maint burden |
| Karakeep / read-later import | v1 | Web, API | S | User already uses Karakeep; one-way import is low effort |
| OPML import/export | MVP | All | S | Universal interchange; all competitors support |

### Organise

| Feature | Tier | Clients | Cost | Reason |
|---------|------|---------|------|--------|
| Folders (hierarchical) | MVP | All | S | Universal baseline |
| Tags (flat, multi-assign) | MVP | All | S | More flexible than folders; Feedbin uses tags |
| Saved searches / smart folders (dynamic queries) | v1 | All | M | NewsBlur, FreshRSS, Inoreader have powerful saved searches |
| Filters/rules (auto-tag, auto-read, notify on match) | v1 | All | M | Inoreader Rules, NewsBlur training, Feedbin Actions all do this |
| Mute/block (keyword, author, feed, regex) | MVP | All | S | Basic triage; NewsBlur visual training is gold standard |
| Regex classifiers (title, body, URL) | v1 | Web, API | M | NewsBlur Pro feature; high power-to-weight for power users |
| Cross-folder "River of News" unified view | MVP | All | S | NewsBlur, NetNewsWire, Reeder all have this |

### Reading

| Feature | Tier | Clients | Cost | Reason |
|---------|------|---------|------|--------|
| Full-text extraction (Readability + site-specific rules) | MVP | All | M | Feedbin, NewsBlur, Miniflux, FreshRSS; core reading experience |
| Typography: font choices, size, line-height, width, themes | MVP | Web, Mac, Android | S | Feedbin (Hoefler fonts), NetNewsWire, Reeder, Readwise all invest here |
| Reader view toggle per-feed | MVP | All | S | NetNewsWire, Feedbin, Reeder allow per-feed override |
| Media: inline images, YouTube embed (youtube-nocookie), audio player | MVP | All | M | Feedbin/Reeder/Readwise have good players; Miniflux uses Invidious |
| Code blocks with syntax highlighting | v1 | Web, Mac | S | Readwise, Reeder 2025.4 added this; dev audience expects it |
| Text-to-speech (TTS) | v1 | Mac, Android | M | Inoreader, NewsBlur, Readwise have TTS; accessibility + commute use |
| Offline cache (articles + media) | v1 | Mac, Android | M | NetNewsWire, Reeder, Readwise sync for offline; expected on native |
| Scroll-position sync (not unread count) | v1 | All | M | Reeder's innovation; more robust than read/unread across devices |

### Triage

| Feature | Tier | Clients | Cost | Reason |
|---------|------|---------|------|--------|
| "Today" / "Unread" smart feeds | MVP | All | S | Universal |
| Mark read on scroll (configurable threshold) | MVP | All | S | NetNewsWire, Reeder, Feedbin basics |
| Keyboard shortcuts (j/k, o, s, etc.) | MVP | Web, Mac | S | Power-user expectation; NewsBlur, NetNewsWire, Feedbin all have |
| Swipe actions (mobile) | MVP | Android | S | Reeder, NetNewsWire iOS standard |
| "Read later" / star / bookmark / tags | MVP | All | S | Universal; Feedbin stars, Inoreader tags, Readwise highlights |
| Bulk actions (mark folder read, apply tag) | v1 | Web, Mac | S | Essential for high-volume feeds |

### Search

| Feature | Tier | Clients | Cost | Reason |
|---------|------|---------|------|--------|
| Full-text search (PostgreSQL tsvector / Meilisearch) | MVP | All | M | NewsBlur (Elasticsearch), Feedbin, Miniflux, FreshRSS all have |
| Saved searches as virtual feeds | v1 | All | M | FreshRSS user queries, NewsBlur saved search feeds |
| Search operators (feed:, tag:, date:, has:, is:) | v1 | All | M | Feedbin expressive syntax, FreshRSS dynamic operators |
| Global search across public feeds (discovery) | later | Web, API | L | Inoreader Global Search, NewsBlur Discover; needs corpus |

### Sharing & Export

| Feature | Tier | Clients | Cost | Reason |
|---------|------|---------|------|--------|
| OPML export | MVP | All | S | Data portability; all competitors |
| JSON Feed export (tags, saved items) | MVP | Web, API | S | Reeder, NewsBlur do this; modern interchange |
| Share to Karakeep (read-later) | MVP | All | S | User's existing workflow; API call |
| Share to Instapaper/Pocket/Readwise/Notion/Obsidian | v1 | All | S | Feedbin, Inoreader, NewsBlur have 20+ integrations |
| Public shared feeds (JSON Feed) from tags | v1 | Web, API | S | Reeder Shared Feeds, NewsBlur blurblog; low effort, high value |
| Email digests (daily/weekly, curated) | v1 | Web, API | M | Inoreader, NewsBlur Daily Briefing; good for LLM consumption |

### Notifications

| Feature | Tier | Clients | Cost | Reason |
|---------|------|---------|------|--------|
| Push notifications (per-feed, filtered by rules) | v1 | Mac, Android, Web | M | NewsBlur, Inoreader, Feedbin; filtered avoids noise |
| Native macOS notifications (click → open article) | v1 | Mac | S | NewsBlur macOS app does this well |
| Background refresh (iOS/Android) | v1 | Android | M | NetNewsWire, Reeder use BG refresh; expected on native |

### Multi-user

| Feature | Tier | Clients | Cost | Reason |
|---------|------|---------|------|--------|
| Multi-user (isolated accounts, shared instance) | v1 | All | M | FreshRSS, NewsBlur self-hosted support; family/small team |
| Teams / shared folders with permissions | later | Web, API | L | Inoreader Teams, NewsBlur blurblog; complex auth |
| Anonymous read-only mode | never | Web | S | FreshRSS has it; not needed for personal-first |

### Admin & Operations

| Feature | Tier | Clients | Cost | Reason |
|---------|------|---------|------|--------|
| Feed health dashboard (errors, last fetch, HTTP status, redirect detection) | MVP | Web, API | M | Feedbin "Fixable Feeds", NewsBlur Feed Statistics; critical for self-host |
| Dead feed auto-detection + suggested replacement | v1 | Web, API | M | Feedbin does this; reduces maintenance |
| Per-feed fetch frequency config (5min–24h) | v1 | Web, API | S | NewsBlur Pro every 5min; respect publisher + server load |
| Structured logging + metrics (Prometheus / OpenTelemetry) | MVP | API | S | Self-host observability; solo dev needs visibility |
| Admin CLI (db backup, reindex, user mgmt) | v1 | API | S | FreshRSS CLI, NewsBlur CLI; essential for NAS ops |
| Docker Compose one-command deploy | MVP | — | S | Miniflux, FreshRSS, NewsBlur all provide; table stakes |

### LLM Features & API

| Feature | Tier | Clients | Cost | Reason |
|---------|------|---------|------|--------|
| REST API (OpenAPI spec) for all CRUD + search | MVP | API | M | NewsBlur, Inoreader, Feedbin, Miniflux have APIs; LLM agents need it |
| MCP server (Model Context Protocol) | v1 | API | M | NewsBlur has MCP server; lets Claude/Codex read feeds, triage, train |
| Article summarization (per-article, on-demand) | v1 | All | M | Inoreader Intelligence, NewsBlur Ask AI, Readwise Ghostreader |
| Daily AI briefing (custom sections, scheduled) | v1 | Web, API | M | NewsBlur Daily Briefing, Readwise Themed Reviews; high value for LLM |
| Chat with highlights / saved items (RAG over user corpus) | v1 | Web, API | L | Readwise Chat With Highlights; differentiator for "LLM-native" reader |
| Custom prompt templates (saved, shareable) | v1 | Web, API | S | Inoreader Intelligence, Readwise Ghostreader v2; power-user leverage |
| LLM-powered feed classification (auto-tag on ingest) | later | API | L | NewsBlur training is manual; auto-classify reduces friction |
| Webhook events (article.created, article.tagged, etc.) | v1 | API | M | Miniflux, Inoreader, Feedbin have webhooks; enables reactive agents |

---

## Smallest MVP (Replace Current Reader)

A user switches from Feedbin/NetNewsWire/Reeder when Earnest delivers:

1. **RSS/Atom/JSON Feed + WebSub** — core ingestion
2. **Newsletter email addresses** (unlimited, plus-addressing, deactivate) — replaces Feedbin/Inoreader
3. **YouTube + podcasts** — unified timeline
4. **Full-text extraction** (Readability + custom rules) — reads partial feeds
5. **Folders + tags + River of News** — organisation
6. **Full-text search (Postgres)** — finds anything
7. **OPML import/export + JSON Feed export** — no lock-in
8. **Keyboard shortcuts + swipe actions** — speed
9. **iCloud-style sync** (read position, tags, subscriptions) across Web/Mac/Android
10. **Docker Compose deploy + Prometheus metrics** — self-host confidence
11. **REST API + MCP server** — LLM agent access from day one

**Explicitly NOT in MVP:** Web feeds (scraping), regex classifiers, TTS, offline cache, teams, AI summarization, chat-with-highlights, webhooks, custom scrapers.

---

## Explicit Non-Goals

| Non-Goal | Reason |
|----------|--------|
| Social features (follow users, public profiles, blurblog) | Crowded; not personal-use; adds moderation burden |
| Algorithmically curated "For You" / discovery feed | Contradicts user control; Feedly/Inoreader territory |
| Monetisation / hosted tier / billing | Builder has no commercial plan; adds infra complexity |
| iOS app (separate from macOS Catalyst) | Solo dev; macOS + Android covers personal devices; iOS later if Catalyst insufficient |
| Windows/Linux native apps | Web PWA + Mac/Android covers builder's devices |
| Plugin/extension marketplace | FreshRSS extensions exist; high maint, low solo-dev ROI |
| Federation (ActivityPub, AT Protocol) | Spec unstable; Reeder/NetNewsWire integrate as clients, not servers |
| Image proxy / media transcoding | Miniflux media proxy is complex; serve originals via Tailscale |
| Epub/PDF library management | Readwise owns this; Earnest is feed-first, not document-first |

---

## Three Signature Features (Why Build Earnest?)

| Feature | Why It Wins |
|---------|-------------|
| **MCP-native API** — Every action (read, tag, train, search, brief) exposed as MCP tools with pre-built prompt templates for "daily briefing", "triage inbox", "research topic". LLMs become first-class users, not afterthought. NewsBlur added MCP in 2025 but as bolt-on; Earnest designs for it from schema up. |
| **Scroll-position sync, not unread-count sync** — Reeder proved this model: position is durable, works across content types (articles, videos, podcasts), survives feed reordering. No "mark read" ambiguity. Works with LLM agents that "read" by seeking positions. |
| **Feed health as first-class UI** — Self-hosters lose feeds silently. Earnest surfaces: HTTP errors, redirect chains, content hash changes, fetch latency percentiles, "last successful fetch" badges per feed, and one-click "find replacement" (Feedbin-style). Turns ops from anxiety into visibility. |

---

## Gaps (Unverified / Could Not Check)

- **Feedbin API rate limits & WebSub support details** — pricing page says "API updated for feed search" but no public OpenAPI spec found
- **Inoreader Intelligence token pricing** — "1M tokens/month on Pro" cited from blog; exact overage cost not on public page
- **NewsBlur self-hosted Docker resource requirements** — "single command install" claimed; actual RAM/CPU for 10k feeds unverified
- **Readwise Reader API public docs** — "API-first" claimed; no public OpenAPI spec or auth flow documented (beta)
- **Miniflux Fever/Google Reader API compatibility scope** — claims compatibility; which endpoints exactly not enumerated
- **FreshRSS WebSub hub discovery** — supports WebSub but hub auto-discovery reliability on self-host unclear
- **NetNewsWire iCloud sync conflict resolution** — "syncs read/starred status"; merge strategy for concurrent edits not documented
- **Reeder iCloud sync schema** — "syncs subscriptions, timeline position, tagged items"; no public schema for interop
- **Android background fetch constraints (2026)** — Doze mode, app standby buckets; exact min interval for feed polling unverified
- **PostgreSQL full-text search vs Meilisearch tradeoffs at 1M+ articles** — Miniflux uses PG; NewsBlur uses Elasticsearch; no recent benchmarks found

---

*Sources inline above. Vendor marketing pages (feedly.com, inoreader.com, newsblur.com, feedbin.com, miniflux.app, freshrss.org, netnewswire.com, reeder.app, readwise.io) treated as feature claims, not independent verification.*
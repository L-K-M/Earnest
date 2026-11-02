# Earnest — competitor research: native clients and open-source / self-hosted readers

Research date: 2026-11-01. All claims checked today unless marked [unverified]. Vendor claims are labelled.
**Legend:** [V] = vendor's own marketing/pricing page. Star counts from the GitHub API today.

---

## 1. Native Mac / iOS clients

### Open source

| App | License | Language / storage | Last release | Stars | Sync backends / API | AI / LLM | Standout idea | Main complaint |
|---|---|---|---|---|---|---|---|---|
| **NetNewsWire** (Ranchero/ Brent Simmons) | MIT (verify) | Swift + SwiftUI, AppKit; Core Data/SQLite on Mac, feed parsing in Rust (`FeedKit`?) [unverified] | see below | repo `RancheroSoftware/NetNewsWire` returned **404** today | iCloud, Feedly, Feedbin, FreshRSS, Inoreader, NewsBlur, The Old Reader, BazQux, Miniflux (Feedly + self-hosted) | none | The default free, native, multi-account Mac reader; also ships on iOS. Feedly/self-hosted accounts. | Accounts and sync are basic; the UI is deliberately plain; iOS app is secondary to the Mac app. |

NetNewsWire's repo returned 404 today — see Gaps.

### Commercial / closed

| App | License | Language / storage | Last release | Sync backends / API | AI / LLM | Standout idea | Main complaint |
|---|---|---|---|---|---|---|---|
| **Reeder** (Silvio Rizzi) — current "Reeder 6" era | Proprietary | Swift, native | [unverified] | Feedbin, Feedly, Inoreader, NewsBlur, FreshRSS, Miniflux, BazQux, The Old Reader, Readwise Reader, local | none (Readwise Reader is a separate paid product with AI) | The reference design for feed reading on Apple platforms; extremely polished list/article/gesture model | Closed source; no self-hostable server; slow-moving |
| **Reeder Classic** | Proprietary | older | discontinued (kept on sale) | Google Reader API, Feedly, local | none | Legacy Google Reader clone | Dead |
| **Unread** (Golden Hill Software, John Hays) | Proprietary | Swift/SwiftUI + AppKit | ships alongside Inkwell (Mar 2026) [V: manton.org] | Feedbin, Inoreader, NewsBlur, FreshRSS, Miniflux, BazQux, The Old Reader, **Inkwell/Micro.blog** | none | New "Inkwell" sync service added in 2026 — evidence of an *open third-party API* winning clients fast [V: manton.org 2026-03-09] | Closed; small team |
| **ReadKit** (Marc Vaillant?) | Proprietary | native | [unverified] | Feedly, Feedbin, Miniflux, FreshRSS, NewsBlur, Inoreader, local | none | Long-lived share-sheet/bookmarklet target | Niche |
| **Lire** | Proprietary | native | [unverified] | Inoreader, Feedly, Feedbin, local | none | Mobile-first | Small |
| **Fiery Feeds** (Dmitriy Kireev) | Proprietary | native | [unverified] | Feedly, Feedbin, Inoreader, NewsBlur, Miniflux, FreshRSS, BazQux, The Old Reader, local | none | Deep per-feed control | Small |
| **CMRSS** (Charlie Monroe Software) | Proprietary, $9.99 one-time IAP; free up to 5 feeds [V: software.charliemonroe.net] | native Swift, iCloud sync | macOS 26 / iOS 26 | iCloud only | **On-device Apple Intelligence summaries** [V] | iCloud-only sync + private on-device AI | Young; iCloud-only |
| **Current** ($9.99 one-time, iOS/iPad/Mac) | Proprietary [V: TechCrunch 2026-02-19] | native | Feb 2026 | iCloud, plus Feedbin / NewsBlur / FreshRSS accounts [V: MakeUseOf 2026-03-29] | none | **The "river"**: no unread count, content auto-expires by velocity class (breaking 3h, news 8h, article 18h, essay 3d, evergreen 7d) [V] | No free trial; novelty |
| **Beacon** (Wells Code, Oct 2026) | Proprietary, £11.99/yr, £1.49/mo, or £29.99 pay-once [V: wellscode.com] | native | Oct 2026 | iCloud; also Mastodon + Bluesky | none | "Newspaper" digest view; Smart Folders (saved searches); feed health; Mastodon/Bluesky as feeds | Very new |
| **Newsairy** | Proprietary, Pro tier | native | 1.22.0 Oct 2026 [V: presskit] | iCloud, The Old Reader, **Miniflux, FreshRSS, Feedbin, Inoreader** (Pro) | **On-device offline AI summaries (iOS 26+)** [V] | Anti-Spoiler keyword hiding with expiry; three independent retention rules (fetch/unread/read) | Young; sync gated behind Pro |
| **Feedler** | Proprietary | native | App Store since 2026-10-28 [V: feedlerapp.com/press.html] | Local, iCloud, Feedbin, Inoreader, NewsBlur, BazQux, The Old Reader, **FreshRSS, Miniflux, Tiny Tiny RSS**; several accounts side by side [V] | Optional AI summaries + TTS [V] | **On-device story clustering** of the same news across your feeds, "no AI" [V]; user-correctable in-house article extractor; true offline | Very new |
| **Inkwell** (Manton Reece / Micro.blog, Mar 2026) | Proprietary | — | Mar 2026 | Micro.blog account | Reading Recap (groups + summarizes posts, pulls quotes) | "Fading" tab (posts expire after a week) + Micro.blog conversation integration | Tied to Micro.blog |
| **Silverleaf** (Jon Hays) | Proprietary | iOS | Mar 2026 | Inkwell open API | — | Third-party client built fast because Inkwell's API is open [V: manton.org] | Inkwell only |

**The pattern worth copying:** the *closed* Apple clients all monetise the same way now — free/cheap app, iCloud sync for personal use, and a **Pro IAP that unlocks third-party sync APIs (Miniflux, FreshRSS, Feedbin, Inoreader, TT-RSS)**. Newsairy and Feedler both ship that exact split [V]. So a well-documented, stable REST sync API is a *market requirement*, not a nice-to-have.

---

## 2. Android clients

| App | License | Language / storage | Last release | Stars | Sync backends / API | AI / LLM | Standout idea | Main complaint |
|---|---|---|---|---|---|---|---|---|
| **Capy Reader** (jocmp) | GPL-3.0 | Kotlin, Jetpack Compose, SQLite/Room | 2026.10.1215 stable (2026-10-27); nightlies to 2026-10-31 | 1,441 | **Feedbin, FreshRSS, Miniflux, Google Reader API**, local | none | Very actively developed; full-text extraction; "mark above/below as read" | Small team |
| **Read You** | GPL-3.0 | Kotlin, Compose, Room | 0.16.2 (2026-06-03) | 7,633 | **Fever API, Google Reader API, Feedbin**, local | none | Best Material You implementation; the biggest OSS Android reader | Dev pace slowed sharply: only 4 commits/3 releases in 2026 so far; 491 open issues |
| **Feeder** (formerly FeedMe, spacecowboy) | GPL-3.0 | Kotlin, Compose, Room | 2.25.0 (2026-10-23) | 3,102 | **local only** (no server sync) + Nostr topic | none | Fastest, most battery-friendly local reader; no account | **No server sync at all** — its 237 open issues include years of sync requests |
| **FocusReader** | [unverified] | Kotlin | [unverified] | [unverified] | [unverified] | none | [unverified] | [unverified] |
| **Readrops** | GPL-3.0 | Kotlin | v2.1.1 (2025-07-07) | 583 | **Fever API, Nextcloud News, Google Reader API**, local | none | Multi-service in one app | Effectively unmaintained: last release Jul 2025 |
| **Twine** (msasikanth) | GPL-3.0 | Kotlin Multiplatform + Compose | v3.9.0 (2026-09-09) | 2,437 | local / KMP | none | **Only actively-maintained KMP reader** — one codebase for Android + iOS + desktop | No server sync yet |
| **Fluent Reader Lite** (yang991178) | BSD-3-Clause | Dart / Flutter | 1.0.8+20 (2026-10-22) | 1,779 | **Fever API, Google Reader API, Inoreader, Feedbin, local** | none | One Flutter codebase for Android + iOS; broadest API coverage of any OSS mobile client | 90 open issues; Flutter UI is not truly native |
| **Nunti** (Ondřej Foltýn, GitLab not GitHub) | GPL-3.0-only | Kotlin? [unverified] | v2.10.0 (2026-08-21, F-Droid) | n/a (GitLab) | **local only** — "no data ever leaves your device" [V: F-Droid] | "adaptive learning of preferences" (on-device ranking) [V] | Smart grouping of similar titles/topics; per-feed download limits; topic-based discovery | Local-only by design |
| **Inoreader app** | Proprietary | native | — | — | Inoreader only | Paid AI features (rules, summaries) | Powerful server-side rules/filters | Expensive tiers; data hostage to Inoreader |
| **Feedly app** | Proprietary | native | — | — | Feedly only | Leo AI (paid tiers) [V] | Best-in-class content discovery, teams | Expensive; AI is the paid upsell; not self-hostable |

---

## 3. Self-hosted servers

| Project | License | Language / storage | Last release | Stars | Sync APIs it speaks | AI / LLM | Standout idea | Main complaint |
|---|---|---|---|---|---|---|---|---|
| **Miniflux** | Apache-2.0 | Go, Postgres (or SQLite) | 2.3.4 (2026-10-20) | 9,867 | **Fever API, Google Reader API**, full REST, WebSub, scraper rules, full-content | none | The cleanest, fastest self-hosted reader; Postgres-native; explicit, opinionated API | No built-in native apps (you bring your own); Go/Postgres, so no PHP stack |
| **FreshRSS** | AGPL-3.0 | PHP, MySQL/Postgres/SQLite | 1.30.4 (2026-10-21) | 16,480 | **Google Reader API, Fever API**, full REST + WebSub | none | Huge multi-user install base, many themes/extensions, works on cheap PHP hosting | PHP; the UI feels dated; extension API is PHP |
| **Tiny Tiny RSS** | GPL-3.0 (fork `tt-rss/tt-rss`); original `fox/tt-rss` **404 today** | PHP, Postgres/MySQL | fork has no releases/tags; see Gaps | 874 (fork) | **Google Reader API**, Fever via plugin, JSON API | Plugins | The deepest plugin ecosystem and the most Google-Reader-like UI | **Effectively orphaned upstream**: the canonical repo 404s; the community fork has no tagged releases; official Android app was removed from Play |
| **CommaFeed** | Apache-2.0 | Java, HSQLDB/Postgres/MySQL | 7.4.1 (2026-10-22) | 3,640 | Fever API, Google Reader API (partial), REST | none | Google-Reader clone that is simple to run | JVM memory footprint; small community |
| **Yarr** | MIT | Go, SQLite, single binary | v2.9 (2026-09-07) | 4,081 | Fever API, own REST | none | One static binary, zero config, SQLite | Minimal: no multi-user, no real API surface |
| **Stringer** | MIT | Ruby, Postgres/SQLite | no GitHub releases (see Gaps) | 4,126 | none (web only) | none | Anti-social, Heroku-era simplicity | Stalled: no tagged releases |
| **Selfoss** | GPL-3.0 | PHP + JS, SQLite | 2.19 (2022-10-12) | 2,473 | own REST/JSON | none | Multipurpose aggregator (feeds + streams + tweets) | **Effectively abandoned** — 4 years without a release |
| **Glance** | AGPL-3.0 | Go, YAML config | v0.8.7 (2026-10-15) | 37,953 | none | none | Dashboard/homepage that renders feeds — the fastest-growing project here | Not a reader: no read state, no accounts |
| **Feedbin server** | MIT (partial) | Ruby on Rails, Postgres | no releases | 3,783 | Feedbin's own REST + JSON Feed | none | The reference implementation of a *commercial-grade* sync API; well documented | Not a complete self-hostable product (the task is not the code) |
| **RSSHub** | AGPL-3.0 | TypeScript, Redis | (see Gaps) | 46,753 | produces feeds | none | Turns thousands of sites into feeds; huge rule library | A generator, not a reader; heavy; Chinese-site-heavy coverage |
| **RSS-Bridge** | Unlicense | PHP, no DB | active (pushed 2026-10-31) | 9,325 | produces feeds | none | Same idea as RSSHub, self-hostable, simpler | Fragile; per-site "bridges" break often |
| **Kill the Newsletter** | MIT | TypeScript | pushed 2026-10-28 | 3,119 | produces Atom from email | none | Email newsletters → Atom feed | A generator, not a reader |

---

## 4. Answers

### 4.1 Which sync APIs must a new Earnest server speak?

Priority order, by what existing native clients already implement:

| API | Who speaks it | Priority for Earnest |
|---|---|---|
| **Google Reader API** (`/reader/api/...`, auth via ClientLogin/`Authorization: GoogleLogin auth=` or the modern `accounts.google.com` flow) | Read You, Capy Reader, Fluent Reader Lite, FreshRSS, Miniflux, CommaFeed, TT-RSS, Readrops | **Must-have.** It is the de-facto standard. Implementing it is the single highest-leverage thing Earnest can do. |
| **Fever API** (simple `api_key`-based, POST JSON) | Read You, Fluent Reader Lite, Readrops, Miniflux, FreshRSS, CommaFeed, Yarr | **Must-have** — trivially easy to implement, and it's the fallback when a client's Google Reader support is partial. |
| **Feedbin API** (REST, token auth, JSON Feed) | Capy Reader, Feedler, Unread, Reeder, Lire, Fiery Feeds | High. It is the cleanest documented REST sync API and the one independent devs copy. |
| **Miniflux REST API** (token auth, documented at miniflux.app/docs/api.html) | Feedler, Newsairy, Reeder, others | Medium-high. Already token-based and stable; cheap to implement. |
| **FreshRSS API / Google Reader compat** | same clients | Medium-high. |
| **Inoreader API** | Unread, Reeder, Fiery Feeds, Feedler, Newsairy | Medium — mostly relevant if Earnest wants to be an Inoreader *client*, not a server. |
| **iCloud / CloudKit** | NetNewsWire, CMRSS, Current, Newsairy, Beacon | Not applicable to a self-hosted server. But note: the *pattern* of "free local app, paid sync" is exactly the Apple-client monetisation model [V]. |
| **Own REST + LLM-friendly docs** | Earnest's own clients | **Differentiator.** None of the servers above have a first-class "let an agent read my summaries" story. |

**Recommendation:** ship **Google Reader API + Fever API + a clean documented REST API** on day one. That combination alone makes Capy Reader, Read You, Fluent Reader Lite, Reeder, Unread and Feedler usable with an Earnest backend with zero work from Earnest. Feedbin's API doc is the best template to copy.

### 4.2 What the best native clients do that web readers cannot

1. **Retention as a first-class concept.** Current's "river" with velocity-based expiry (3h → 7d) and Inkwell's "Fading" tab both solve the unread-count anxiety problem by *deleting* on a schedule [V]. Newsairy ships three independent retention rules (fetch history / unread / read). Web readers almost never do this because the server is the one holding the data.
2. **On-device AI, not cloud AI.** CMRSS and Newsairy both market *private, offline, Apple Intelligence* summaries [V]. This is a trust feature that a self-hosted web reader cannot easily claim, because the browser has no stable on-device model and the server is where the data lives.
3. **Offline-first caching + full-text extraction with user correction.** Feedler's in-house extractor is user-correctable per feed [V]; Capy Reader and Feeder cache aggressively.
4. **OS integration.** Widgets, Siri/Shortcuts, share sheets, tabbed reading, Listen/TTS, system font settings, pull-to-refresh, notification-per-feed. These are the things users actually pay for.
5. **Story clustering across feeds, on device.** Feedler groups several feeds' articles about the same news into one story with no AI [V]. No self-hosted server in the table does this.
6. **Reading-position memory** (Beacon's Timeline returns you to the article you were reading [V]) and per-feed "open with" rules.

### 4.3 What could Earnest build on or fork?

| Option | Verdict |
|---|---|
| **Use Miniflux as the backend** | **Strongest option.** Apache-2.0, Go, Postgres, actively released (2.3.4, 2026-10-20), 9.9k stars, already speaks Google Reader + Fever APIs, has full-content extraction and WebSub. Earnest's own clients (native Mac/Android/web/API) could consume its API, and *every existing client in the tables above* would work immediately. But it is a finished, opinionated product with a fixed data model and a "minimalist" ethos — adding Earnest's API/LLM/notification features means forking and diverging, and you inherit its roadmap. |
| **Use FreshRSS as the backend** | Best if Earnest wants a large existing install base and a PHP plugin ecosystem. AGPL-3.0 is compatible with a commercial hosted tier but forces source release of Earnest's own additions. 16.5k stars, 1.30.4 released 2026-10-21. Also a finished product; forking it means fighting a large legacy codebase. |
| **Fork an existing client, not a server** | **Twine** (KMP, GPL-3.0, actively released to v3.9.0 in Sep 2026) is the only well-maintained multiplatform native reader — a genuine head start for a Kotlin client that covers Android + iOS + desktop. **Capy Reader** (GPL-3.0, Kotlin, very active) is the best Android-only starting point and already speaks Google Reader/Fever/Feedbin/FreshRSS/Miniflux. **Fluent Reader Lite** (BSD-3-Clause, Flutter) if Earnest prefers one non-native codebase over two native ones — and it already speaks the widest set of sync APIs. |
| **Start fresh in Rust/axum + sqlx + Postgres** | Defensible, and it fits the builder's stack. The risk is not the server — it is the client fleet. Writing a Mac and an Android client from scratch is where the months go. |
| **Recommended combination** | Build the **Earnest server fresh** in Rust/axum/sqlx/Postgres (small, focused: ingest, dedupe, full-text extraction, retention, notifications, and an LLM-facing API), implement **Google Reader + Fever + Feedbin-compatible REST** for third-party clients, and **fork Capy Reader** for Android and either **Twine or NetNewsWire** for Apple. Use Miniflux's and Feedbin's API docs as the spec, and RSSHub/RSS-Bridge as the ingest escape hatch for sites with no feed. Do not fork Miniflux or FreshRSS — you would be maintaining a foreign codebase to add features they explicitly don't want. |

**One more honest note:** the biggest competitive gap in the table is not a reader, it is **feed generation**. RSSHub (46.8k stars) and RSS-Bridge (9.3k stars) both exist because half the sites people want to follow have no feed. Earnest should treat "generate a feed for anything" as an ingest-side feature, not a client feature.

---

## Gaps

- **NetNewsWire**: repo `RancheroSoftware/NetNewsWire` returned **404** on the GitHub API today. I could not confirm license, stars, language or last release. Needs a direct check of netnewswire.com / its current repo location.
- **Tiny Tiny RSS**: canonical repo `fox/tt-rss` returned **404** today; the community fork `tt-rss/tt-rss` (874 stars) has **no tagged releases and no GitHub releases at all**. I could not confirm whether upstream is archived, moved, or renamed, nor the fork's actual status or a date for its latest commit beyond `pushed_at` 2026-11-01.
- **Stringer**: no GitHub releases or tags found; latest activity inferred from `pushed_at` (2026-10-31), which may be branch activity rather than a release.
- **RSSHub**: GitHub API returned 403 (rate limit) for its releases; last release date unconfirmed.
- **FocusReader**: could not locate the project. No GitHub repo matched; it may be a renamed or defunct project, or not on GitHub.
- **Reeder, Unread, ReadKit, Lire, Fiery Feeds, and the Inoreader/Feedly apps**: closed-source, no public repos. Version numbers, prices, sync lists and AI features above come from their App Store pages and third-party articles, and are marked [unverified] where I could not confirm them from a primary source.
- **Nunti**: hosted on GitLab (`gitlab.com/ondrejfoltyn/nunti`), not GitHub, so no GitHub star count; language/stack not confirmed.
- **Selfoss**: last release 2.19 (2022-10-12) confirmed via the GitHub API, but I did not verify whether development continued on `main` without a release.
- **Feedbin's open-source server**: MIT-licensed per the API, but I did not verify how much of the service is actually in that repo versus the closed hosted product.
- No pricing/feature verification for commercial Apple apps beyond their own marketing pages, which are labelled [V].

# Earnest — RSS Reader User Sentiment (2024–2026)

Scope: why users abandon, praise, pay, migrate; AI opinions; heavy-user needs. Sources: HN Algolia (2024–2026), r/rss, r/selfhosted, GitHub FreshRSS/Miniflux/Folo (2024–2026), Mastodon/web (indirect), product forums (Feedly/Inoreader pricing pages, The Verge 2024-01-22). Today 2026-11-01; claims unchecked marked [unverified].

---

## Method / sources (brief)
- HN: https://hn.algolia.com/api/v1/search queries “Current – An RSS reader with no unread counts” (2026-02-17, 7pts, https://www.terrygodier.com/current), “Ask HN: Where to put AI summary” (2024-04-19, 4pts, https://news.ycombinator.com/item?id=40082096), “How many feeds” (2024-09-23, https://news.ycombinator.com/item?id=41626359).
- Reddit r/rss: “Is the read/unread feature necessary” (2024-01-10, https://www.reddit.com/r/rss/comments/1931jd0/), “drowning in RSS feeds” (2025-07-13, https://www.reddit.com/r/rss/comments/1lyx0c9/), “calm RSS reader” (2024-09-11, https://www.reddit.com/r/rss/comments/1fe7xar/), “Love RSS but hate 1000+ unread” / Folo AI build (2025-11-20, https://www.reddit.com/r/rss/comments/1p222hw/), “Seeking Feedback on AI-Powered RSS Reader” (2024-09-12, https://www.reddit.com/r/rss/comments/1fez2d4/), “Feeeed summary unavailable” (2025-04-20, https://www.reddit.com/r/rss/comments/1k3hshw/), “How many feeds” (2025-04-18, https://www.reddit.com/r/rss/comments/1k28gm1/).
- GitHub: FreshRSS full-content bugs #6773 (2024-09-04), #6588 CSS-selector regression (2024-06-24), #6674 partial headings (2024-08-01); Miniflux #2570 full-content fail (2024-03-26); Folo #887 full-text missing (2024-10-11); FreshRSS→Inoreader migration #6762 (2024-09-02).
- Self-host / migration: Wet-Dry World Mastodon note (TT-RSS→FreshRSS, Docker/Proxmox, “a bit buggy and Docker containers were throwing errors”) https://akko.wetdry.world/notice/116771192476567130; Petra 5-yr FreshRSS (589 feeds, 34 cats, “delightful mess”) https://petras.space/blog/2025/5-years-freshrss.html; MereCivilian Feedbin (>600 feeds, 30-min sync) https://merecivilian.com/my-rss-workflow/.
- Pricing / pay: The Verge “RSS readers” (Feedly Pro $72/yr, Pro Plus $144/yr AI; Inoreader Supporters $20.04/yr 500 feeds, Pro $90/yr unlimited) https://www.theverge.com/24036427/rss-feed-reader-best.

---

## 1. Pain points (ranked, widely held first)

### P1 — Unread anxiety / volume overload (widely held)
Evidence 1: r/rss “drowning in RSS feeds” (2025-07-13): “Feeds pile up so fast, and if I don’t stay on top of them for a few days, it’s suddenly +999 unread.” https://www.reddit.com/r/rss/comments/1lyx0c9/
Evidence 2: r/rss “Look for a calm RSS reader” (2024-09-11): “my RSS reader was bombarding them with new entries to read, not skimming every single entry was like missing out, it felt like a second inbox next to your email.” https://www.reddit.com/r/rss/comments/1fe7xar/
Evidence 3: HN “Current – no unread counts” (2026-02-17, 7pts) removes counters to eliminate anxiety; comment asks for mobile clients: https://news.ycombinator.com/item?id=47047754
Design for Earnest: make unread counts optional per-feed / global; add “calm mode” that hides badges and surfaces only starred/new items; default to chronological not “unread first” for heavy users.

### P2 — Broken / partial feeds, full-text failure (widely held among self-hosters)
Evidence 1: FreshRSS #6773 (2024-09-04): selector didn’t match; fallback to original truncated feed; fix merged for broken-root HTML. https://github.com/FreshRSS/FreshRSS/issues/6773
Evidence 2: FreshRSS #6588 (2024-06-24): “Posts in some feeds can be partial/truncated… With 1.24.1 this stopped working.” User workaround: delete all items, clear cache, redownload. https://github.com/FreshRSS/FreshRSS/issues/6588
Evidence 3: Miniflux #2570 (2024-03-26) “Not able to fetch full content”; another reader OK: https://github.com/miniflux/v2/issues/2570. Evidence 4: Folo #887 (2024-10-11) “Some RSS subscription sources cannot load the full text… other readers able”: https://github.com/RSSNext/Follow/issues/887
Design: per-feed CSS-selector + Mozilla Readability fallback; cache full-text independently; expose partial/full status.

### P3 — Discovery / filtering / noise (widely held; vocal minority wants AI ranking)
Evidence 1: r/rss “Love RSS but hate 1000+ unread” / Folo build (2025-11-20): “The ‘unread anxiety,’ the difficulty of discovering new feeds, and the sheer volume of noise can be exhausting.” https://www.reddit.com/r/rss/comments/1p222hw/
Evidence 2: r/rss “Seeking Feedback” (2024-09-12): “subscribe to too many sources, overwhelming backlog… Using AI to filter”: https://www.reddit.com/r/rss/comments/1fez2d4/. Evidence 3: HN “Where to put AI summary” (2024-04-19): readers ignore namespaces, injection “hacky”: https://news.ycombinator.com/item?id=40082096
Design: keyword/highlight rules (Inoreader-style); optional AI layer that never hides original; offline filtering trained on saved/starred (Banquet model).

### P4 — Migration fatigue / data lock-in (vocal minority, but high cost when it happens)
Evidence 1: FreshRSS migration guide: OPML import from Inoreader added in 1.25.0 (2024-09-02) to preserve “liked articles and their tags”; user confirms import works. https://github.com/FreshRSS/FreshRSS/discussions/6762
Evidence 2: Mastodon (Wet-Dry World, ~2026-07): “Migrated from TT-RSS to FreshRSS… TT-RSS buggy, Docker errors, no time to troubleshoot”: https://akko.wetdry.world/notice/116771192476567130
Evidence 3: NewsFlash docs: “If you are moving from Feedly, Inoreader… connect to service directly rather than importing OPML, since NewsFlash will pull the full subscription list.” Migration path choice depends on service API access, not just OPML. https://newsflash.dev/
Design: OPML + favorites/tags/read-state import/export; direct API sync to FreshRSS/Miniflux; document exit path.

### P5 — Heavy-user performance / scale (vocal minority, but they pay and build)
Evidence 1: Petra (2025-03-07): 589 feeds in 34 categories on FreshRSS; “Performance was perfectly fine on the rather oversubscribed linode… now on my home ‘server’ (a now decade-old mini PC) it may as well not even be running for all the CPU it seems to use.” https://petras.space/blog/2025/5-years-freshrss.html
Evidence 2: HN “How many feeds” (2024-09-23): “Anybody out there with 500 feeds?”; answers 163, ~100 with filters, 400+ with Inoreader. https://news.ycombinator.com/item?id=41626359
Evidence 3: r/rss “How many feeds” (2025-04-18): 700+ (yellowduckbe), ~600 (kevincox_ca: “if you can handle 10k feeds decently you will comfortable handle almost every user”), ~500 growing. https://www.reddit.com/r/rss/comments/1k28gm1/
Evidence 4: MereCivilian Feedbin workflow (2024-09-05): “Over 600 RSS feeds that Feedbin tracks. It’s super quick too, so if there is a new blog post, generally, I see it pretty quickly (within 30 minutes).” https://merecivilian.com/my-rss-workflow/
Design: parallel refresh + per-feed intervals; DB indexing for 500+; mobile lazy-load; folders/filters reduce surface.

---

## 2. What people praise (compact, with evidence)
- **Calm / no-counter design**: “Current” (HN, 2026) + r/rss “calm RSS reader” ask both value absence of badges.
- **Self-host / privacy / no ads**: FreshRSS 14K stars; Mastodon “no tracking”; Feedlight “local-first, no accounts.”
- **Full-text extraction / readability**: Feedlight uses Mozilla Readability; FreshRSS article CSS selectors (when working) praised.
- **Speed / reliability of paid sync**: Feedbin (600 feeds, 30-min) praised over iCloud/Feedly sync; Inoreader tracked for 400+ feeds with IFTTT→Telegram.
- **Keyboard / power-user navigation**: FreshRSS, NewsFlash, Feedlight all emphasize shortcuts.

---

## 3. AI summaries & ranking — split opinion
**Pro (vocal minority / builder cohort)**: Folo (2025-11-20) “TL;DR of all your unread items… game-changer”; Banquet (r/rss 2024-11-14) “filters according to your instructions… after months of tweaking, it actually works”; Tidyread / Telegram bot summaries (2024/2025) all advertise “save 2 hours/day.”
**Caution / against (wider)**: r/rss “AI-Powered RSS Reader MVP” (2024-08-04) is promotional; critical reply in “Feeeed summary unavailable” (2025-04-20) shows feature rot (“Summary unavailable”). HN “Where to put AI summary” (2024-04-19) notes standard feed readers ignore custom namespaces, making AI injection brittle and “hacky.” r/rss “RSS is not the solution” (2024-08-30): “there is no algorithm to determine which articles are trending or which articles are important… Every RSS reader has this problem, there is stagnation and no innovation.” So users want filtering, not necessarily opaque ranking.
Design implication: AI summary should be optional, per-feed or global, with user-editable prompts (like Banquet’s “Save/Dismiss” training); never hide original source; provide “why this summary?” transparency; allow local-only inference (Ollama) for privacy, matching Feedlight’s model.

---

## 4. Most requested features (ranked by evidence frequency)
1. Optional unread counts / calm mode (HN “Current”, r/rss calm, r/rss no-counters ask).
2. Full-text / readability fallback with per-feed selector (FreshRSS issues #6773/#6588, Miniflux #2570, Folo #887).
3. AI summary / keyword filter (r/rss Folo, Banquet, Tidyread, but also HN hacky-injection warning).
4. OPML + favorites + tags import/export (FreshRSS #6762, NewsFlash docs, self-host migration notes).
5. Heavy-user performance / parallel refresh / folders (Petra 589 feeds, r/rss 500+ users, MereCivilian 600).
6. Mobile apps (HN “Current” comment; r/rss iOS Feeder/Feeeed/Banquet all mobile-focused).
7. Notification / digest (Telegram bot, Tidyread, Folo morning email).

---

## 5. What triggers migration (summary)
- Technical failure / maintenance burden: TT-RSS→FreshRSS (Docker errors); FreshRSS version regressions (#6588).
- Feature need: full-text, AI filtering, unread-count removal, mobile sync.
- Service decline/shutdown: MetaFilter drops FeedBurner (2024-01-14 HN, 10pts); Google Reader history (older, but mentioned implicitly in “drowning” posts).
- Cost / limits: Feedly free=100 feeds; Inoreader free=150; users cross to self-host or Pro at ~400–600 feeds (The Verge pricing; HN 400+ Inoreader; r/rss 700+).

---

## 6. What makes someone pay
Evidence: The Verge (2024-01-22) pricing table; r/rss “How many feeds” notes 600+ users choose Feedbin/inoreader paid; FreshRSS is free/self-host so pay is for sync/reliability, not software.
- **Feed limits**: free tiers at 100–200 feeds; 500+ users need Pro/unlimited (Inoreader Pro $90/yr; Feedly Pro Plus $144/yr for AI + 2500 feeds).
- **Full-text / AI / rules / filters**: Inoreader Pro adds rules/filters/IFTTT; Feedly Pro Plus adds AI summary + up to 2500 feeds.
- **Reliability / speed**: MereCivilian says Feedbin “fast, reliable”; others try iCloud sync, find it “slower and less reliable.”
- **No ads / privacy**: self-host free, but hosted tiers can charge for convenience + privacy (no tracking).
Design: Earnest should have a generous free tier (e.g., 200 feeds, full-text, basic filters) and a paid tier for 1000+ feeds, AI summarization/embedding, and cross-device sync (since self-hosted users already pay with hardware/time; hosted tier can sell convenience).

---

## 7. Heavy users (500+) vs light users — differences
| Need | Light (20–100) | Heavy (500–700+) |
|---|---|---|
| Unread anxiety | High; counts matter | Low if curated; need “calm” + folder filters |
| Discovery | Subscribing is fun | Need rules/keyword filters; AI optional |
| Full-text | Nice-to-have | Critical; broken selectors = huge time loss |
| Performance | Not noticed | Parallel refresh, DB indexing, mobile caching |
| Migration | Rare | Frequent (service limits, technical failures) |
| Pay trigger | Free okay | Unlim feeds + sync + reliability |
Evidence: Peterson 589 feeds; r/rss 700+; MereCivilian 600; HN “400+ feeds (and counting)” with Inoreader; kevincox_ca “10k feeds decently = almost every user.”

---

## 8. Design implications for Earnest (one-line per point)
- Calm-first UI: optional unread badges; chronological default; folder-based “topic calm.”
- Readability-first: per-feed CSS selector + Mozilla Readability fallback; show partial vs full status.
- Filter > rank: user-defined keyword/highlight rules + optional AI summary with editable prompts; never hide original.
- Migration-first: OPML + favorites/tags + read-state import/export; direct sync APIs to FreshRSS/Miniflux/Feedbin; document exit path.
- Scale-first: parallel refresh, per-feed intervals, efficient DB; mobile lazy-load; folders/filters reduce surface.
- Paid = convenience + scale: free up to ~200 feeds with full-text + filters; paid for unlimited + AI + sync + notifications.
- Local-first option: Docker/Compose self-host (builder is comfortable); optional Ollama-only AI for privacy.

---

## Gaps / unverified
- Lobsters direct posts: could not retrieve JSON (site requires session/auth); no direct quotes included.
- Mastodon: only one note (Wet-Dry World) retrieved; broader Mastodon sentiment [unverified].
- Current Feedly/Inoreader pricing: from The Verge 2024-01-22; may have changed [unverified].
- Non-builder AI-ranking sentiment: mostly builder posts (Folo/Banquet); mainstream critical reactions sparse [unverified].
- Heavy-user mobile benchmarks (600 feeds): app complaints noted, no detailed perf data [unverified].
- Earnest revenue model: no primary data; inference from pricing patterns only [unverified].

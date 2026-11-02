# Competitor research: hosted feed readers & adjacent reading products

## Live products

**Feedly** — Free: 100 sources, 3 folders, ads. Pro $6/mo annual ($72) or ~$8/mo: 1,000 feeds, search, notes. Pro+ $99 first year then $144/yr (~$12/mo): 2,500 feeds, "Feedly AI" (formerly Leo): AI Feeds, mute filters, dedup, newsletter intake, RSS Builder. Generative "AI Actions" live only on B2B Market Intelligence plans (vendor pays tokens). Platforms: web, iOS, Android; no native Mac. API effectively enterprise-only (100k req/mo, sales-gated): https://developers.feedly.com/reference/authorization. Third-party clients (Reeder, NetNewsWire) still sync via OAuth. Push via mobile apps [unverified]. No consumer MCP. Pricing: https://feedly.com/i/pricing (JS-gated; figures via https://hushread.com/blog/feedly-pricing). Sharpest weakness: the consumer reader is a marketing arm of a threat-intel company — API closed, AI gated behind $144/yr, opaque renewal pricing.

**Inoreader** — Free: 150 subs, Google News alerts. Pro €6.67/mo annual (€8.99 monthly): 2,500 feeds, rules (30), filters (50), monitoring feeds, web feeds, newsletters, social feeds (Telegram/Bluesky/YouTube), push notifications via rules. Intelligence: summaries, mentions, podcast transcripts, suggested tags, reports — bundled in Pro with limits; supports custom inference (OpenAI/Mistral/Anthropic) and BYOK keys. Public OAuth REST API requires Pro and is quota-starved (~100 reads + 100 writes/day per registered app, expandable by request — per community MCP docs). No official MCP; community servers exist: https://github.com/justmytwospence/inoreader-mcp. Platforms: web, iOS, Android. https://www.inoreader.com/pricing. Weakness: upsell maze and an API too rate-limited for agent polling.

**Feedbin** — $7/mo or $70/yr, 30-day trial, no free tier. Web + own iOS app; the canonical third-party client backend (Reeder, NetNewsWire, Unread, ReadKit, lire, Capy, FeedMe…): https://feedbin.com/apps. Documented REST API: https://github.com/feedbin/feedbin-api. Actions can star/mark-read/send push notifications. Newsletters, YouTube, podcasts, full-text extraction, search. No AI features at all; no official MCP (community: https://github.com/hypermoose/FeedbinMCP). Weakness: zero AI; you live or die by third-party clients.

**NewsBlur** — Free: 64 sites. Premium $36/yr (1,024 sites, search, river-of-news). Premium Archive $99/yr (4,096 sites, full archive, Ask AI, daily briefing, story clustering). Premium Pro $29/mo (10,000 sites, 5-min fetch, regex classifiers). Ask AI offers Claude/GPT/Gemini/Grok — vendor pays, bundled. Official MCP server (HTTP+OAuth, 28 tools incl. subscribe, mark-read, save/tag, train classifiers, manage notifications) requires Archive tier or above; also a CLI: https://newsblur.com/features/mcp. Native iOS/Android apps; open source, self-hostable via Docker: https://newsblur.com/pricing. Weakness: dense, dated UI; essentially a one-developer bus factor. Closest existing product to Earnest's agent vision.

**Readwise Reader** — No free tier (30-day trial). Reader-only $7.99/mo annual; Full Readwise $9.99/mo annual; Highlights Lite $5.59. Web, iOS, Android, Mac app. RSS is one input among PDFs/EPUBs/newsletters/YouTube. Ghostreader AI: summarize/ask/translate — vendor pays tokens, no BYOK. Best-in-class agent surface: official MCP server + CLI + REST API + ChatGPT connector on all plans: https://readwise.io/pricing. Weakness: read-later first, feed reader second — weak per-feed push/notification story; subscription lock-in.

**The Old Reader** — Free ≤100 feeds; Premium $3/mo or $30/yr: 500 feeds, full-text search, 1-year post storage. Web only; unofficial Google-Reader-style API lets some third-party clients connect [unverified]. No AI, no push, no MCP. Sources: https://blog.theoldreader.com/post/77390229437/premium-accounts-faq, https://en.wikipedia.org/wiki/The_Old_Reader. Weakness: near-stagnant; Google Reader nostalgia without an ecosystem.

**BazQux** — $30/yr flat, 30-day trial, no free tier. Web (desktop + mobile web); third-party iOS/Mac/Android clients via a Google-Reader-compatible API: https://github.com/bazqux/bazqux-api. Differentiators: in-reader comments, filters/smart streams, Facebook/Telegram/VK feeds, full-text conversion, image proxy. No AI, no push, no MCP. https://bazqux.com. Weakness: solo-dev, dated, no AI — but the flat-fee "you're the client" model is worth copying.

**Feeder** — Free: 200 feeds, 30-min refresh, 2 AI summaries/day. Plus $7.99/mo ($95.88/yr): 2,500 feeds, 5-min refresh, 10 summaries/day, daily brief, rules/filters. Professional $14.99/user/mo: 10k feeds, 1-min refresh, integrations, create-RSS, 60 summaries/day. Vendor pays tokens via daily quotas. Web + iOS/Android apps + browser extension + desktop notifications. No public API; "Feed your AI" is marketing for generating feeds for AI pipelines, not an agent API. https://feeder.co/pricing. Weakness: paywalled refresh speed; no real API — a hosted fetcher, not a platform.

**Folo** (ex-Follow) — Open source (GPL-3.0, https://github.com/RSSNext/Folo), self-described "AI RSS reader" with a literal "I'm an Agent" mode. Free: 150 subs, 3 AI summaries/day. Basic $4.17/mo ($49.99/yr). Plus $8.33/mo ($99.99/yr): unlimited summaries/translations, BYOK (OpenAI/Google/OpenRouter/Vercel AI Gateway), 5M AI credits, external MCP-server connections inside the app's AI (OAuth-secured). Pro $83.33/mo. Web, desktop (macOS/Win/Linux), iOS, Android. Community folo-mcp server exposes the account externally (session token): https://github.com/hyoban/folo-mcp. https://folo.is/pricing. Weakness: credit/token accounting complexity, young codebase, Electron heft.

**AI-first since 2024** — **Particle** (May 2024, ex-Twitter engineers): free iOS/Android/web app; AI groups articles into stories with multi-source summaries; new "Radar" podcast-search feature; no RSS, no API. https://particle.news (vendor marketing). **News Minimalist** (2023, solo): Gemini scores ~30k articles/day for 0–10 significance; free site + newsletter; proof that LLM-based triage is the feature users actually want. https://www.newsminimalist.com/about. **Reeder** (Sep 2024): kills unread counts in favor of synced timeline position; iOS/Mac only, iCloud sync, ~$1/mo or $10/yr; no AI. https://reeder.app, https://techcrunch.com/2024/09/23/the-new-reeder-app-is-built-for-rss-youtube-reddit-mastodon-and-more/. **Tapestry** (Feb 2025, Iconfactory): unified chronological timeline across RSS/Mastodon/Bluesky/YouTube/podcasts via connectors; iOS/iPadOS (Mac via Apple Silicon); free + premium; deliberately anti-algorithm, no AI. https://usetapestry.com. **Digg** relaunch: open beta Jan 2026, hard-reset and dead by Mar 2026, killed by AI bot spam — a warning for any social layer: https://www.theverge.com/tech/894803/digg-beta-shutdown-layoffs-ai. Minor: SmartRSS AI mobile client, Readless AI digest, Cubox AI summaries.

## Graveyard

- **Google Reader** (d. July 1, 2013): officially "declining usage" + consolidation around Google+ — https://googleblog.blogspot.com/2013/03/a-second-spring-of-cleaning.html. Users fled to Feedly (biggest winner), The Old Reader, NewsBlur, Feedbin, Digg Reader (itself dead 2018). Lesson: the API's de-facto standard still shapes the market.
- **Artifact** (d. Feb–Apr 2024): wound down because "the market opportunity isn't big enough" — https://medium.com/artifact-news/shutting-down-artifact-1e70de46d419. Tech sold to Yahoo → Yahoo News personalization: https://www.theverge.com/2024/4/2/24118436/yahoo-news-artifact-acquisition. Users scattered to SmartNews/Apple News; its niche was re-attempted by Particle.
- **Omnivore** (d. Nov 15, 2024): free + donations couldn't sustain it; team hired by ElevenLabs, service deleted within ~2 weeks' notice. https://elevenlabs.io/blog/omnivore-joins-elevenlabs, https://techcrunch.com/2024/10/29/elevenlabs-has-hired-the-team-behind-omnivore-a-reader-app/. Users went to Readwise Reader, Karakeep, Wallabag. Lesson: free OSS + hosting = acquisition bait.
- **Pocket** (d. July 8, 2025; exports till Oct 8): Mozilla cut costs to focus on Firefox; "the way people save and consume content has evolved." https://blog.mozilla.org/en/mozilla/building-whats-next/. Users → Karakeep, Wallabag, Instapaper, Readwise Reader, Matter, Raindrop. Lesson: even 17 years and Mozilla money doesn't protect a feature-product.

## Comparison table

| Product | Price / free tier | Platforms | AI (who pays) | API / 3rd-party | Push | MCP/agent |
|---|---|---|---|---|---|---|
| Feedly | Free 100/3; Pro $6/mo; Pro+ ~$12/mo | Web,iOS,And | AI Feeds, dedup (vendor); gen-AI = B2B only | Enterprise-only API; many clients sync | App push | None |
| Inoreader | Free 150; Pro €6.67/mo | Web,iOS,And | Intelligence suite; BYOK option | Public API, ~100 calls/day | Rules→push | Community only |
| Feedbin | $7/mo, no free | Web,iOS (+3rd-party all platforms) | None | Excellent REST API; huge client ecosystem | Actions→push | Community only |
| NewsBlur | Free 64; $36/$99/$29mo tiers | Web,iOS,And; OSS self-host | Ask AI + briefing (vendor, Archive tier) | Public API + own apps | Per-feed alerts | **Official, 28 tools** |
| Readwise Reader | $7.99/mo, no free | Web,iOS,And,Mac | Ghostreader (vendor) | REST API + CLI + ChatGPT conn. | Weak | **Official** |
| Old Reader | Free 100; $30/yr | Web | None | Unofficial greader-ish API | None | None |
| BazQux | $30/yr, no free | Web + 3rd-party clients | None | greader-compat API | None | None |
| Feeder | Free 200; $7.99–14.99/mo | Web,iOS,And,ext | Summaries/brief, daily caps (vendor) | No public API | Desktop+app | None |
| Folo | Free 150; $4.17–83.33/mo | Web,Desk,iOS,And; OSS | Unlimited on Plus; BYOK or credits | Internal API; community MCP server | App | In-app MCP client; community server |
| Particle | Free | iOS,And,Web | Story synthesis (VC pays) | None | App | None |

## Gaps nobody fills (especially for an LLM agent working your feeds)

1. **Agent-write is missing or crippled.** Only NewsBlur's official MCP covers read *and* act (subscribe, tag, train, notify) — and it's $99/yr. Inoreader's ~100-calls/day API quota makes agent-driven triage a joke. Feedly's API is sales-gated. Feedbin's API is great but has no push-to-agent story — agents must poll.
2. **No push-to-agent channel.** Every reader pushes to humans (mobile push, email, IFTTT/Zapier). Nobody offers webhooks/SSE/streaming of new items filtered semantically, so an LLM can be woken by "something matching this description" rather than by keyword rules. Inoreader rules + Feeder integrations are the closest — human-grade only.
3. **No dual read-state.** Agents marking items read stomp on the human's unread queue. Nobody exposes agent-specific read state, agent-annotated triage (scores, clusters, reasons), or an "agent triaged, human approved" split view.
4. **No standardized agent API.** NewsBlur, Readwise, and community MCPs are all bespoke. There's no "Google Reader API for agents" a client could implement once.
5. **Token economics are wrong.** Bundled AI (NewsBlur/Feeder/Folo free tiers) forces daily quotas and vendor-chosen models; only Inoreader and Folo offer BYOK, and Folo buries it under a credit system.
6. **No product combines: self-hostable + BYOK + official MCP with write actions + native Mac/Android clients + notification routing.** NewsBlur is closest (OSS + MCP) but has no native Mac app and an aging UI. That's Earnest's opening.

## What Earnest should copy

- **Feedbin**: a small, clean, fully documented REST API designed so third-party clients can sync — and consider shipping a Google-Reader/Fever-compatible endpoint so existing clients work on day one.
- **NewsBlur**: the MCP surface (read + act + train + notify), classifier training, per-feed notification rules, open-source + Docker self-host + paid-hosted hybrid.
- **Inoreader**: rules→actions (push, tag, webhook), web-feed generation, monitoring feeds; BYOK for AI.
- **Folo**: BYOK and an explicit human/agent split in the product model.
- **Readwise**: first-class agent triad — API + CLI + MCP — as a launch feature, not an afterthought.
- **BazQux**: honest flat pricing, "you're the client, not the product."
- **Reeder/Tapestry**: timeline-position sync and connector-style source extensibility as an answer to unread-count anxiety.

## What Earnest should refuse to copy

- Feedly's enterprise-locked API, ad-supported free tier, and bait-and-switch annual pricing.
- Vendor-pays-tokens bundling that forces daily AI quotas (Feeder caps, Folo credits) — prefer BYOK from day one with the hosted tier reselling tokens at cost.
- Folo's credit/token economy and feature sprawl (25+ source types before core reading is polished).
- Omnivore's free-OSS-without-revenue endgame, and Artifact's VC-scale-or-die premise.
- Algorithmic black-box ranking presented as inevitable (Artifact, Digg's bot collapse) — agent triage should be inspectable: every suppressed item needs a stored reason.
- Unread-count inbox-zero anxiety as the only model — offer Reeder-style timeline mode.

## Gaps (unverified)

- Feedly mobile push-notification specifics; exact Pro+ renewal flow (pricing page is JS-gated; prices from third-party archives dated Oct 2026).
- The Old Reader's API capabilities (undocumented; inferred greader compatibility).
- Tapestry premium price (~$20/yr, from memory — App Store page not fetched).
- BazQux notification/digest features — assumed none.
- Whether NewsBlur Ask AI has hidden usage caps beyond the plan boundary.
- Readwise Reader push-notification behavior on new RSS items.

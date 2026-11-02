# Competitors

Checked 2026-11-01. GitHub figures come from the GitHub API that day. Prices come from vendor pages unless marked. Vendor pages are marketing: they say what is sold, not how well it works.

## The market in one paragraph

Hosted readers split into platforms with an API (Feedbin, Inoreader, NewsBlur) and closed products (Feedly's API is enterprise-gated, Feeder has none). Native Apple clients had a burst in 2024 to 2026, nearly all on the same model: cheap app, iCloud sync, optional third-party sync. Android is thinner and open source. Agent access moved fast: NewsBlur and Readwise now ship official MCP servers, so it isn't a differentiator on its own.

**NewsBlur is the closest thing to Earnest that exists.** It is [MIT-licensed, 7.7k stars](https://github.com/samuelclay/NewsBlur) and self-hostable, [its native apps connect to your own instance](https://www.newsblur.com/alternative/self-hosted-rss-reader), and [it has a Mac app, apparently Catalyst](https://blog.newsblur.com/2026/03/05/newsblur-v14-for-ios-and-mac/). Its [README documents a self-hosted MCP container](https://github.com/samuelclay/NewsBlur/blob/main/README.md#self-hosted-mcp-server), and [its Docker settings carry AI provider keys and Ask AI and briefing model selection](https://github.com/samuelclay/NewsBlur/blob/main/newsblur_web/docker_local_settings.py). All documented; none of it has been run for this research. Try it before building.

## Hosted

| Product | Price | Clients | AI, and who pays | API and third-party clients | Agent surface | Weakness |
| --- | --- | --- | --- | --- | --- | --- |
| [NewsBlur](https://newsblur.com/pricing) | Free 64 sites; $36/yr; $99/yr; $29/mo | Web, iOS, Android, Mac; open source, Docker | Ask AI, briefing; vendor pays, Archive tier | Public API | [Official MCP, 28 tools, OAuth, plus CLI](https://newsblur.com/features/mcp), $99/yr and up. Acts as you | Dense UI; Mac app is not AppKit-first |
| [Readwise Reader](https://readwise.io/pricing) | $7.99/mo yearly, no free tier | Web, iOS, Android, Mac | Ghostreader; vendor pays | REST API, [webhooks](https://docs.readwise.io/highlights/docs/webhooks) | [Official hosted MCP](https://docs.readwise.io/tools/mcp), CLI, ChatGPT connector. Acts as you | Read-later first, feeds second |
| [Folo](https://folo.is/pricing) | Free 150 feeds; $49.99 and $99.99/yr; $999.99/yr | Web, desktop, iOS, Android; [AGPL-3.0, 39k stars](https://github.com/RSSNext/Folo); no official self-host path, community forks exist | Summaries, translation, AI actions that filter and notify, scheduled briefings; credits, BYOK on Plus and Pro | Internal API | In-app MCP client; community server | Credit accounting, feature sprawl |
| [Inoreader](https://www.inoreader.com/pricing) | Free; Pro €6.67/mo yearly; Custom | Web, iOS, Android | Intelligence; own key or custom inference | Google Reader-compatible API; [docs](https://www.inoreader.com/developers/) require a paid plan, [pricing](https://www.inoreader.com/pricing) lists API under Custom | Community MCP only | Upsell maze, tight API quota (reported) |
| [Feedbin](https://feedbin.com/pricing) | $7/mo or $70/yr | Web, iOS, plus many third-party | None | [Excellent documented REST API](https://github.com/feedbin/feedbin-api) | Community MCP only | No AI, no agent story |
| Feedly | Free 100 sources; Pro about $6/mo, Pro+ about $12/mo (pricing page is script-gated, third-party figures) | Web, iOS, Android | Vendor-paid, gated | [Tokens are Enterprise-only](https://developers.feedly.com/reference/authorization) | None for consumers | A threat-intelligence company's side product |
| [Feeder](https://feeder.co/pricing) | Free 200 feeds; $7.99/mo; $14.99/user/mo | Web, mobile, extension | Daily summary caps; vendor pays | None | None | Refresh speed is paywalled |
| BazQux, The Old Reader | About $30/yr | Web, third-party clients | None | Google Reader-compatible | None | Stagnant |

AI-first news products ([Particle](https://particle.news), [News Minimalist](https://www.newsminimalist.com/about)) are not feed readers, but they show demand for "tell me what matters". The [Digg relaunch collapsed under AI bot spam in 2026](https://www.theverge.com/tech/894803/digg-beta-shutdown-layoffs-ai): do not build a social layer.

## Graveyard

| Product | Died | Cause | Lesson |
| --- | --- | --- | --- |
| Google Reader | 2013 | Declining use, strategy | Its unofficial API still defines interoperability |
| [Artifact](https://medium.com/artifact-news/shutting-down-artifact-1e70de46d419) | 2024 | "Market opportunity isn't big enough" | AI news personalisation is not a venture-scale business |
| [Omnivore](https://elevenlabs.io/blog/omnivore-joins-elevenlabs) | 2024 | Team acqui-hired, two weeks' notice | Free hosted open source has no durable funding |
| [Pocket](https://blog.mozilla.org/en/mozilla/building-whats-next/) | 2025 | Mozilla cut it | Hosted services vanish; export and self-hosting are features |
| [Tiny Tiny RSS](https://github.com/tt-rss/tt-rss) | 2025 | Author [retired all infrastructure](https://linuxiac.com/tt-rss-shuts-down-but-the-project-lives-on-under-a-new-fork) | Bus factor one; a community fork continues |

## Native clients

| App | Model | Worth noting |
| --- | --- | --- |
| [NetNewsWire](https://github.com/Ranchero-Software/NetNewsWire) | MIT, 10.5k stars, Mac 7.2.1 on 2026-10-20 | The reference three-pane reader. Speaks several sync APIs, so it is a free test client for Earnest |
| [Reeder](https://reederapp.com) | iCloud sync | [No unread counts; synced timeline position](https://reederapp.com), though it [shows a count of newer items](https://reederapp.com/help/) |
| [Current](https://www.terrygodier.com/current) | [$9.99 once; works with Feedbin, NewsBlur, FreshRSS, Miniflux, Google Reader services](https://www.currentreader.app/docs/faq) | No unread counts, ever. Each source has a half-life and items fade. A waterline marks where new begins, and "Ambient Closure" says when you're caught up. **Most of Earnest's calm reading ideas already ship here** |
| [Feedler](https://feedlerapp.com/press.html) | Released 2026-10-28; many sync services | [On-device story clustering with no AI](https://feedlerapp.com/press.html); optional AI summaries |
| [Unread](https://apps.apple.com/us/app/unread-an-rss-reader/id1363637349) | Subscription; nine sync services | Typography and gestures |
| Tapestry, Beacon, Newsairy, CMRSS, Inkwell | Reported, not individually checked | Timelines across sources, newspaper views, on-device summaries, posts that fade. Same themes |
| [Capy Reader](https://github.com/jocmp/capyreader) | Android, GPL-3.0, 1.4k stars, very active | Best current Android sync client; a test client for Earnest |
| [Read You](https://github.com/ReadYouApp/ReadYou) | Android, GPL-3.0, 7.6k stars | Best Material You look; development has slowed |
| [Feeder (Android)](https://github.com/spacecowboy/Feeder) | GPL-3.0, 3.1k stars | Local only. Not related to Feeder.co or FeedMe |
| [Twine](https://github.com/msasikanth/twine) | Kotlin Multiplatform, GPL-3.0, 2.4k stars | Proof KMP works for a reader |

Do not fork these. The GPL clients conflict with your usual licences, and their designs assume a different reading model. Read them; test against them. Your own [Kararead](https://github.com/L-K-M/Kararead) reader view (`ReaderHtmlBuilder.kt`, MIT) is the better Android starting point. [RSS Spy](https://github.com/L-K-M/rssspy) can grow a "subscribe in Earnest" action.

## Self-hosted

| Project | Licence, stack | Stars, last release | Speaks | Note |
| --- | --- | --- | --- | --- |
| [Miniflux](https://github.com/miniflux/v2) | Apache-2.0, Go, Postgres | 9.9k, 2.3.4 on 2026-10-20 | Google Reader, Fever, REST | Closest technical relative. Borrow from it. [Decided since](decisions.md#licence-and-code-reuse): reference only, no copied code |
| [FreshRSS](https://github.com/FreshRSS/FreshRSS) | AGPL-3.0, PHP | 16.5k, 1.30.4 on 2026-10-21 | Google Reader, Fever | Largest install base |
| [CommaFeed](https://github.com/Athou/commafeed) | Apache-2.0, Java | 3.6k, 7.4.1 on 2026-10-22 | Reported | JVM footprint |
| [Yarr](https://github.com/nkanaev/yarr) | MIT, Go, SQLite | 4.1k, v2.9 on 2026-09-07 | Fever | One binary; proves the SQLite route for small use |
| [Glance](https://github.com/glanceapp/glance) | AGPL-3.0, Go | 38k | None | A dashboard, not a reader, but its popularity shows appetite for glanceable feeds |
| [RSSHub](https://github.com/DIYgod/RSSHub), [RSS-Bridge](https://github.com/RSS-Bridge/rss-bridge), [Kill the Newsletter](https://github.com/leafac/kill-the-newsletter) | Generators | 47k, 9.3k, 3.1k | Produce feeds | Use them as ingest escape hatches rather than rebuilding them |

## What's actually open

An earlier draft listed several gaps. Checking narrowed them to one.

| Claimed gap | Status |
| --- | --- |
| Self-hosted with native apps and agent access | **Taken** by NewsBlur |
| Calm reading: no counts, decay, end states | **Taken** by Current and Reeder |
| Waking an agent on new items | Mostly taken: [Miniflux](https://miniflux.app/docs/webhooks.html) and [Readwise](https://docs.readwise.io/highlights/docs/webhooks) have webhooks. Filtered watches remain rarer |
| Automated AI triage and briefings | Taken by Folo, NewsBlur, Inoreader |
| **Agents as distinct principals** | **Open, as far as this research found.** Nothing found gives an agent its own read position, annotations, budget and revert log. NewsBlur, Readwise and community Miniflux servers all act as the authorising user |
| AppKit-first Mac app plus Android, on your own server | Open, but it's a preference and not a market gap |

**A warning about that one open gap.** Every demand signal found ([Readwise](https://readwise.io/reader/update-aug2026), NewsBlur) is for agents acting *as you*. Nobody was found asking for separate agent identities. It may be sound engineering nobody wants until something goes wrong. This research wasn't demand research, so hold that loosely in either direction.

## Copy and refuse

**Copy.** Feedbin's API documentation discipline. NewsBlur's classifiers. Inoreader's rules-to-action model. Current's per-source half-life and closure. Feedler's clustering without an LLM. Readwise's API plus CLI plus MCP as a set. BazQux's flat honest pricing, if a hosted tier ever happens.

**Refuse.** AI summaries on by default. Credit systems. Vendor-paid AI with daily caps. Black-box ranking. Social features. A feature-gated API.

## What users say

Hard evidence on abandonment is thin; most of it is forum posts by people who self-select as heavy users. Reddit blocks automated verification, so Reddit quotes from the research run are excluded here.

- **Overload and unread debt** is the classic complaint, going back well over a decade: [Marco Arment](https://marco.org/2013/03/26/power-of-rss), [Brent Simmons and Nick Bradbury](https://nickbradbury.com/2014/04/02/unread-counts/). Recent products are built on it: [Current](https://www.terrygodier.com/current), [Reeder](https://reederapp.com).
- **But many people want counts.** In [Feeder's 2020 survey of 800+ users](https://feeder.co/blog/484-state-of-rss-survey-the-results), a vendor source, 67% rated unread counts positively and 28% called them the most important feature. An [August 2026 analysis of 1,811 App Store reviews](https://lantery.app/blog/read-state-in-feed-readers), by someone building a competing sync service, found read state in 24% of Current's and 22% of Reeder's negative reviews, against 0 to 6% for inbox-model apps. Among Reeder reviews taking a side, 55% opposed the change and 45% favoured it. Recent reviews skew negative, so read direction, not magnitude. The problem may be making one model mandatory.
- **Full-text extraction breaks often**, and self-hosters feel it: [FreshRSS #6588](https://github.com/FreshRSS/FreshRSS/issues/6588), [Miniflux #2570](https://github.com/miniflux/v2/issues/2570), [Folo #887](https://github.com/RSSNext/Folo/issues/887). Show full or partial status per feed; allow a per-feed override.
- **Maintenance is a migration trigger.** Self-hosters leave when a reader becomes a chore. Feed fetchers are also [being blocked by Cloudflare](https://forum.newsblur.com/t/feed-retrieval-problems-403-errors/10761), and readers need to [show why a feed is failing](https://openrss.org/work/144).
- **Heavy users are not rare among enthusiasts.** [One self-hoster runs 589 feeds on a decade-old mini PC](https://petras.space/blog/2025/5-years-freshrss.html) without noticing the load; [another keeps over 600 in Feedbin](https://merecivilian.com/my-rss-workflow/). [HN answers range from 100 to 400+](https://news.ycombinator.com/item?id=41626359).
- **AI sentiment is split and poorly measured.** Builders are enthusiastic. Complaints about forced AI in mainstream readers are reported in a secondary roundup only. Treat "people want AI summaries" as unproven: make it opt-in and test it on yourself.

## The name

No RSS reader called Earnest turned up in search. [Earnest](https://www.earnest.com) is a US student-loan lender, a different trademark class but a search-result competitor, so expect poor discoverability on the bare word. Treat this as a non-lawyer's search, not clearance.

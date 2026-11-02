# Adversarial review — product & UX angle (Earnest draft, 2026-11-01)

Reviewer angle: README.md positioning, competitors.md claims, features.md signature features, ux.md concepts. All files in ./draft/ were read. Web checks done 2026-11-01 against primary sources; anything not re-verified is marked. Verified items and judgement are separated at the end.

## Findings

### 1. MAJOR — "No self-hosted server pairs with first-party native apps" is wrong; the "combination" gap is much smaller than claimed
- **Claim:** competitors.md:7 ("Self-hosting is healthy but no server pairs with first-party native apps") and competitors.md:69 gap #4 ("Self-hosted, native Mac and Android, bring-your-own-agent. NewsBlur is closest and has no native Mac client").
- **Why wrong:** Folo (RSSNext/Folo, AGPL-3.0, 39,163 stars on 2026-11-01) ships first-party clients for web, iOS, Android, macOS, Windows, Linux, with server code in the same AGPL repo; community self-hosted Docker backends exist (e.g. jhangyu/Folo "Forage"). Caveat verified: upstream's README documents no official self-host path (hysenlabs.com review, Oct 2026). But Folo also has AI Actions (rules that filter/star/**notify**/summarize new entries), scheduled AI Tasks (daily/weekly briefings to chat/email), BYOK, and an in-app MCP client (confirmed in repo code). So "self-hosted + native Mac/Android + automated AI triage and briefings" is partially filled; only **agent identity/budget/undo** remains genuinely open (see #4).
- **Evidence:** https://github.com/RSSNext/Folo (README, licence), https://grasppy.com/radar/repo/RSSNext/Folo/ (39,194 stars, AGPL, checked 2026-10-29), https://hysenlabs.com/projects/rssnext-folo ("the README documents no deployment path for your own instance"), https://github.com/jhangyu/Folo (Docker self-host fork).
- **Change:** rewrite competitors.md:62-69 to a single gap — agents as first-class principals (own read position, budget, annotations, undo) — and add "Folo self-hosted via fork" as closest competitor, with caveats (AGPL, no official self-host docs, Electron/RN clients, no agent identity). This changes the positioning math in README.md:17.

### 2. MAJOR — Two of the three "signature features" already ship in a current competitor
- **Claim:** features.md:7-10 presents cadences/decay and "reading with an end" as signature differentiators; README.md:22.
- **Why wrong:** Current (terrygodier.com/current, launched Feb 2026, TechCrunch 2026-02-19) already ships per-source half-life decay, five per-source speeds (Breaking/News/Article/Essay/Evergreen — i.e. cadences), no unread counts ever, and "Ambient Closure", an explicit end-of-session "you're caught up" state — the draft's "sessions end". Reeder ships position-not-counts. These are validated ideas but not novel, and an indie iCloud-only app shipped them in months, so they are not a moat.
- **Evidence:** https://www.terrygodier.com/current , https://www.currentreader.app/docs/faq ("Ambient Closure tells you when you're caught up"), https://techcrunch.com/2026/02/19/current-is-a-new-rss-reader-thats-more-like-a-river-than-an-inbox/ , https://reederapp.com/help/ .
- **Change:** in README.md:19-23 and features.md:7, stop presenting cadences/decay/session-end as the differentiator. The honest signature is: self-hosted + third-party-client sync + agents as supervised principals + inspectable automation. Keep cadences/decay as table stakes that Current proves work.

### 3. MAJOR — "Position, not debt" overstates the evidence; the user population is split, and read-state removal has a measurable backlash
- **Claim:** ux.md:7 ("Show how far you've read, never how much you owe"), ux.md:37 ("This, not a badge, is the answer to unread debt"), competitors.md:81.
- **Why wrong:** Feeder's State of RSS survey (800+ respondents, 2020): 67.5% rated "keeping count of unread posts" positively, 28.5% called it the most important feature. A 2026-08-24 analysis of 1,811 App Store reviews across 8 readers found river-model apps draw read-state complaints from ~1 in 5 recent reviewers (Current 24%, Reeder 22%), split 55% against / 45% for; inbox apps draw 0-6%. The functional loss is real: a scroll position cannot answer "have I seen this?" when moving between folders or skimming. Even Reeder shows a tappable count of newer items (competitors.md:39 omits this).
- **Evidence:** https://feeder.co/blog/484-state-of-rss-survey-the-results , https://lantery.app/blog/read-state-in-feed-readers , https://reederapp.com/help/ , https://danq.me/2026/02/06/phantom-obligation-vs-freshrss/ (real users hide counts selectively via CSS — counts kept for designated feeds only).
- **Change:** keep counts off by default, but (a) soften ux.md:37 from "the answer" to "our default, with a first-class opt-in"; (b) make the opt-in counts view (ux.md:166) an onboarding setting, not a buried risk note; (c) treat Reeder's ~55/45 backlash as the warning for removing read state entirely — Earnest's sparse per-entry exceptions (api.md:47) are the right hedge, say so explicitly.

### 4. Verified open gap — no product gives agents their own identity, budget, annotations or undo; but the demand signal is for agents acting *as you*
- **Claim:** competitors.md:66 gap #1, README.md:17, features.md:9.
- **Verified:** NewsBlur ships an official MCP server, now 28 tools, gated at Premium Archive $99/yr or Pro $29/mo (feature page; the 2026-04-01 launch blog said 22 tools — it has grown). Readwise ships a hosted MCP at mcp2.readwise.io/mcp; both act as the authorising user via OAuth — no separate agent identity, budget, or undo anywhere. Community Miniflux MCP servers all run on one user token ("One server process uses one configured Miniflux identity, so every connected MCP client has that identity's permissions"). So the gap is real as of 2026-11-01.
- **Evidence:** https://www.newsblur.com/features/mcp , https://blog.newsblur.com/2026/04/01/mcp-server-and-cli/ , https://www.newsblur.com/pricing , https://docs.readwise.io/tools/mcp , https://readwise.io/reader/update-aug2026 ("pleasantly surprised how exciting and popular this has been… game-changer"), https://github.com/tssujt/miniflux-mcp .
- **Change (judgement, not fact):** "supervised colleagues" is your framing; every demand signal found (NewsBlur, Readwise) is for agents acting as you. Keep M1 as the test, but define success as "the author reads the agent's brief daily for two weeks", not "the tools exist".

### 5. MAJOR — Missing feature: authenticated/private feeds
- **Claim:** features.md ingest table (features.md:17-29) has no entry for feeds requiring HTTP Basic auth, cookies, or custom headers.
- **Why it matters:** a self-hosted single user routinely has paid newsletters, private trackers and auth-gated feeds; without credential support those feeds fail at M0 and land in "feed health" as 401/403. Existing self-hosted readers support credentials embedded in feed URLs (Miniflux documents `username:password@` URL credentials).
- **Evidence:** https://miniflux.app/docs/webhooks.html (URL credential format; Miniflux fetcher accepts them in feed URLs — partially verified; treat exact per-feed UI as my judgement).
- **Change:** add "per-feed HTTP auth / custom headers, stored encrypted" to M0 ingest.
### 6. MINOR — Missing: user-facing export of reading data
- **Claim:** the draft's own graveyard lesson (competitors.md:31, Pocket: "export and self-hosting are features") is not applied to Earnest. OPML export covers subscriptions only; the nightly dump (features.md:108) is a backup, not a portable export of entries/annotations/saves.
- **Change:** add "export library (entries, annotations, saved) as JSON/Markdown" to Later. Judgement, no external evidence needed.

### 7. MINOR — Reeder claim omits its newer-items count
- **Claim:** competitors.md:39 "No unread counts; synced timeline position."
- **Why:** Reeder does show a count of newer items as a tappable button (can be made icon-only) — https://reederapp.com/help/ . Not decision-changing, but Earnest's "never a count anywhere" stance is stricter than the precedent it cites; Current's "no counts ever" (verified at currentreader.app/docs/faq) is the cleaner precedent. Change: cite Current for the strict version, or soften the claim.

## Verified as accurate (no action)
NewsBlur MCP 28 tools + Archive/Pro gate; NewsBlur pricing $36/yr, $99/yr, $29/mo; Readwise Reader-only $7.99/mo billed yearly, no free tier (readwise.io/pricing); Readwise MCP at mcp2.readwise.io/mcp acting as you; Folo AGPL-3.0, ~39.2k stars, in-app MCP client; Feedler on App Store since 2026-10-28, on-device no-AI clustering (feedlerapp.com/press.html); Digg beta shutdown 2026-03-13 over AI bot spam, relaunched 2026-05-08 at di.gg (theverge.com/tech/894803, /tech/927230); Marco.org 2013-03-26 post exists (argues unread counts are fine if you curate feeds); Android LOW importance for subscribed new content (developer.android.com); Current's no-counts-ever and half-life (currentreader.app).

## Judgement (not verified facts)
- Cadences, decay, session ends, activity log: not fantasies — Current ships three of the four since Feb 2026 — but they are hypotheses with one shipped precedent, not unmet needs. The time box has no supporting evidence found; ux.md:168's paternalism risk stands, keep it opt-in.
- "Agents as supervised colleagues": nobody found asking for agents as separate principals; all demand signals are BYO-agent-as-you. Activity-log-with-undo is sound engineering but its *desirability* is unproven.
- Most likely abandonment point: not the agent thesis — the three-client sync matrix with no shared core (tech-stack.md:131 calls it "this plan's main risk"), built solo across M2-M4. Consider cutting to two surfaces initially to halve drift.

## Single riskiest assumption and cheapest test
**Riskiest assumption:** that the M1 agent surface (an external agent writing a morning brief as a "supervised colleague") will be used daily by its only user — the positioning in README.md:19 rests on it and it is built before any client exists.
**Cheapest test:** the plan's own M1 proof, time-boxed: run one external agent over MCP against the Google-Reader-subset server, one morning-brief annotation plus one ntfy notification, for two weeks; record whether the brief gets read. No UI needed. Runner-up assumption (decay/cadences beat counts for this user) is testable free: hide counts selectively in FreshRSS/Miniflux via the published CSS recipe (danq.me 2026-02-06) for two weeks before building anything.

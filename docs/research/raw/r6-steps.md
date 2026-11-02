# Round 9 review: steps 0 and 1

Reviewer: Mistral Large 4. Verifier: MiMo V2.6 Pro, asked to refute each finding. The report is unedited; the verdicts follow it.

ID: ACT-1
Severity: major
Location: README.md:61
Issue: Step 1's constraint "No database, no HTTP" makes several of its own proof vectors unmeasurable, so "the model passes them" cannot be satisfied as written.
Evidence: README.md:61 says "No database, no HTTP" and proof is "The listed vectors (api.md#change-feed) have decided expected results, and the model passes them"; but api.md:155 (vector 11) demands "no deadlock, gapless `rev` per user" and api.md:158 (vector 14) demands "A slow ingest transaction never exposes a higher `seq` before a lower one", both of which are properties of real Postgres row locks/transaction visibility that api.md:134 grounds in "Row locks are held until transaction end" and lock ordering — a no-database model can only encode those assumptions, not test them; vectors 9/10/19 (api.md:153,154,163) likewise require real backup/restore with generation rotation.
Fix: Either scope step 1's vectors to what an in-memory model can prove and move vectors 9-11, 14, 19 to the step-2/3 Postgres-backed conformance suite, or drop the "no database" constraint.

ID: ACT-2
Severity: major
Location: README.md:60
Issue: Step 0's ntfy paging never says the ntfy server must be the self-hosted one on the NAS, so a literal reading pages the agent's brief through public ntfy.sh, contradicting llm.md's invariant 3.
Evidence: README.md:60 only fixes "a fixed server and topic" in the wrapper; llm.md:116 invariant 3: "Agent text doesn't leave the tailnet by default"; api.md:232 calls for "a self-hosted, default-deny ntfy" and docs.ntfy.sh/publish states "the topic is essentially a password" on the public server; tech-stack.md:123 adds "Phones and Macs must be on the tailnet to fetch", which step 0 never mentions.
Fix: Add to step 0: self-host ntfy on the NAS with default-deny auth, put the phone on the tailnet, and point the wrapper's fixed server at it.

ID: ACT-3
Severity: major
Location: README.md:60
Issue: Step 0's "enforced tool allowlist" names the tools to exclude but never says how exclusion is enforced, so someone starting today has no mechanism to build.
Evidence: README.md:60 says "with an enforced tool allowlist... exclude those" (newsblur_get_original_text and subscription/share tools — verified present in NewsBlur's 28-tool MCP at newsblur.com/features/mcp and in stories.py); no doc in the repo states NewsBlur's self-hosted MCP supports server-side tool filtering, and no client config/proxy pattern is specified; llm.md:115 invariant 2 ("No agent-reachable operation can cause an outbound request to a URL influenced by agent input") is what makes this enforcement load-bearing.
Fix: Specify the enforcement point (e.g. an MCP client config that only registers the read-only tools, or a filtering proxy in front of the MCP endpoint) before running the step-0 agent.

ID: ACT-4
Severity: minor
Location: README.md:60
Issue: Step 0 depends on the unanswered question 1 (current reader and its OPML) without saying so — "your real feeds" cannot be imported until that answer exists.
Evidence: README.md:60 "Deploy NewsBlur on the NAS with your real feeds"; README.md:80 "What reader do you use now, and can you share its OPML?" with only a bracketed assumption (300 feeds); the Sequence (README.md:54-72) never marks step 0 as blocked on Q1.
Fix: State in step 0 that it starts only after exporting the OPML from the current reader per Q1.

ID: ACT-5
Severity: minor
Location: README.md:60
Issue: Step 0 tells you to "read with counts hidden" in NewsBlur but gives no method, while the only documented counts-hiding technique in the repo targets different products.
Evidence: README.md:60 "read with counts hidden"; ux.md:168: "hide counts selectively in FreshRSS or Miniflux with CSS" (danq.me recipe) — not NewsBlur, the product step 0 deploys; no doc says how to hide unread counts in NewsBlur.
Fix: Name the concrete NewsBlur setting (or equivalent) that hides counts for the two-week experiment.

ID: ACT-6
Severity: minor
Location: README.md:60
Issue: Step 0's proof "You know whether NewsBlur is enough" has no measurable criteria, unlike the brief criterion.
Evidence: README.md:60 proof column: "You read the brief daily for two weeks. You know whether you want counts and decay. You know whether NewsBlur is enough" — the first is measurable, the last has no stated threshold (which gaps — Mac client, MCP behaviour, sync — would disqualify NewsBlur); raw/r2-product.md:29 sets the measurable bar only for the brief ("reads the agent's brief daily for two weeks").
Fix: List the concrete NewsBlur capabilities step 0 must demonstrate (self-hosted MCP with allowlist, native clients, sync behaviour) as pass/fail criteria.

ID: ACT-7
Severity: minor
Location: api.md:162
Issue: Vector 18 requires a decided expected result for a cluster-scoped write at step 1, but cluster membership and read-time suppression are deferred to step 10, so the vector's expected result rests on an undefined notion of cluster at model time.
Evidence: api.md:162 "Late-joining cluster member after a cluster-scoped done: expected result documented" (no result is recorded anywhere); api.md:93 "A duplicate arriving next week needs read-time suppression in the view, which belongs with clustering at step 10"; features.md:66 only says "Reserve the `cluster` state scope in the model".
Fix: Either define cluster membership abstractly enough for the vector (e.g. a set of entry IDs supplied in the fixture) or move vector 18 to the step-10 clustering work.

ID: ACT-8
Severity: minor
Location: README.md:47
Issue: The Decisions table rates "Agent permissions" confidence High, but the topic doc itself states the underlying threat model is "reviewed, not proven".
Evidence: README.md:47 "| Agent permissions | Allowlisted feeds, stored content only... | High |"; llm.md:123 "That has been reviewed, not proven; keep attacking it"; validation.md:102 lists "The threat model... reviewed, not proven, and each round of attack found something new" under Not checked.
Fix: Lower the confidence to Medium (or annotate High as "high as a design default, unproven as a guarantee") to match llm.md's own caveat.

## Verification

- **ACT-1: REFUTED.** Step 1's model scope explicitly includes restore (README.md:61: "registers, conflict policy, range records, bootstrap and restore"), deadlock-freedom and seq-ordering are protocol properties an executable model can check under the lock discipline api.md:134 prescribes (counter-row locks taken in user ID order), api.md:143 says undecided expected results are settled "while building the model", and real-database verification is already assigned to step 3 ("The vectors pass against the real server", README.md:62).
- **ACT-2: CONFIRMED.** README.md:60 fixes only "a fixed server and topic" and never requires a self-hosted ntfy or the tailnet for the step-0 phone/wrapper, while the docs' own rules demand exactly that (llm.md:116 invariant 3, api.md:232 "a self-hosted, default-deny ntfy", features.md:73 "Self-hosted, default deny", tech-stack.md:123 "Phones and Macs must be on the tailnet to fetch"), so the actionable step-0 text leaves public ntfy.sh open.
- **ACT-3: CONFIRMED.** README.md:60 demands "an enforced tool allowlist" but no repo doc names an enforcement point, and the NewsBlur MCP page (https://www.newsblur.com/features/mcp, checked today) documents only OAuth connection of all "28 tools" ("Anything you can do") with no server-side tool filtering, so the mechanism llm.md:115 relies on is unspecified.
- **ACT-4: REFUTED.** Exporting the OPML from the current reader is a routine user action that needs no answer recorded in the doc, and README.md:78–80 explicitly runs the plan on bracketed defaults ("These change the plan. Defaults are in brackets." / "[Assume 300 feeds…]"), so step 0 is not blocked on question 1.
- **ACT-5: CONFIRMED.** README.md:60 says "read with counts hidden" with no NewsBlur method given, and the only counts-hiding technique documented anywhere in the repo is the FreshRSS/Miniflux CSS recipe at ux.md:168, which does not apply to the product step 0 deploys.
- **ACT-6: CONFIRMED.** README.md:60's proof "You know whether NewsBlur is enough" states no threshold and no other doc defines disqualifying gaps (competitors.md:9 says only "Try it before building"), while raw/r2-product.md:29 sets the measurable bar only for the brief ("the author reads the agent's brief daily for two weeks").
- **ACT-7: REFUTED.** api.md:93 already defines `cluster` at model time as "accepted as a scope, resolved to member entries at write time" (with features.md:66 reserving that scope in the model), api.md:143 says "Some expected results are still decisions to make while building the model", and vector 18 itself (api.md:162) demands a documented decision ("expected result documented"), so step-10's read-time suppression (api.md:93) is not a prerequisite.
- **ACT-8: REFUTED.** The Confidence column rates the recommendation — which README.md:3 frames as "defaults to overturn, not commitments" — not the strength of the security guarantee, and the threat model's unproven status is already recorded where caveats live (llm.md:123 "reviewed, not proven", validation.md:102 under Not checked, README.md:23 pointing at the threat model's limits).

# Round 9 review: internal consistency

Reviewer: SWE-2 max. Verifier: GLM-5.3, asked to refute each finding. The report is unedited apart from shortened local paths; the verdicts follow it.

ID: CON-1
Severity: minor
Location: features.md:106
Issue: The provider-layer feature names only OpenAI-compatible and Anthropic, omitting Gemini, which llm.md's description of the same provider layer includes.
Evidence: features.md:106 "| Provider layer: OpenAI-compatible, Anthropic | 10 | S |"; llm.md:77 "**Provider layer.** OpenAI-compatible as the default dialect ... plus Anthropic and Gemini for their batch and caching features."
Fix: Align features.md with llm.md by adding Gemini (or note it is covered via the OpenAI-compatible dialect, which llm.md does not rely on since it wants Gemini's batch/caching features).

ID: CON-2
Severity: minor
Location: README.md:38
Issue: The Decisions table says Miniflux is "reference and parts" while tech-stack.md's heading for the same decision says "reference, not a parts bin" — opposite characterisations.
Evidence: README.md:38 "Miniflux is reference and parts, not a base"; tech-stack.md:49 "**Miniflux: reference, not a parts bin.**" (though its body does list files "Worth copying")
Fix: Reword one side, e.g. README "Miniflux is reference plus selected copied files, not a base", to match tech-stack.md's substance.

ID: CON-3
Severity: minor
Location: features.md:75
Issue: The notification policy "realtime only for rules you wrote" excludes realtime agent alerts, which the agent surface (step 6, one step earlier) and api.md's notify flow explicitly deliver.
Evidence: features.md:75 "Digest by default; realtime only for rules you wrote"; api.md:268 notify tool sends "within budget" (realtime push, held only by budget/quiet hours); README.md:66 step 6 ships "ntfy with budgets".
Fix: Scope the row to feed-driven notifications, e.g. "realtime for rules you wrote and agent alerts within budget".

ID: CON-4
Severity: minor
Location: api.md:204
Issue: Agents "track progress with [their] own `seen` rows and position", but the enumerated agent scopes contain no scope permitting writes to an agent's own state registers — only `position:own` (position) and `state:write-human` (the human's state).
Evidence: api.md:108 "An agent tracks its progress with its own `seen` rows and position"; api.md:204 scope list "`entries:read` ... `position:own` ... `state:write-human`" with no own-state scope.
Fix: State explicitly whether `position:own` covers an agent's own `seen`/`shelf` writes or add a `state:write-own` scope.

ID: CON-5
Severity: minor
Location: features.md:52
Issue: The only offline feature is tiered "Phone client" with "Web and Mac can start with less", yet README ships web "offline reading" at step 7 and tech-stack.md describes the web offline scope as "reading plus a queue of actions" — nearly the full feature's wording — leaving the web's step-7 scope undefined.
Evidence: features.md:52 "Offline reading and action queue | Phone client"; README.md:67 "Web complete: ... offline reading"; tech-stack.md:131 "Offline covers reading plus a queue of actions ... keep the scope small."
Fix: Give the reduced web offline scope its own tier or state explicitly what "less" means at step 7 (e.g. reading only, no action queue).

ID: CON-6
Severity: minor
Location: api.md:3
Issue: api.md says the sync model went through "three rounds of attack" while validation.md says "two rounds of review found real defects each time" — different counts for the same review history.
Evidence: api.md:3 "The sync model has been through three rounds of attack"; validation.md:101 "Two rounds of review found real defects each time" (rounds 2 and 4 per validation.md:11,13; rounds 6 and 8 found no sync defects).
Fix: Reconcile the counts, e.g. "three rounds of review, two of which found real defects", in both files.

## Verification

- **CON-1: CONFIRMED.** features.md:106 lists the provider layer as "OpenAI-compatible, Anthropic" while llm.md:77 includes Gemini in the same layer ("plus Anthropic and Gemini for their batch and caching features"), and validation.md records nothing about this omission.
- **CON-2: REFUTED.** The quotes are accurate but the characterizations are not opposite: tech-stack.md:49's "not a parts bin" rejects the already-fixed draft claim that Miniflux "can be borrowed from file by file" (validation.md, "What the reviews changed"), while its own body lists files "Worth copying", which is exactly the "reference and parts" substance README.md:38 states.
- **CON-3: CONFIRMED.** features.md:75 allows realtime "only for rules you wrote", but the agent `notify` tool (api.md:268, tier 6 per README.md:66) delivers agent alerts immediately subject only to budget and quiet hours (api.md:225-238, llm.md action tier 2), so realtime agent alerts exist outside the row's scope.
- **CON-4: CONFIRMED.** api.md:108 has agents "track progress with [their] own `seen` rows and position", but the scope list at api.md:204 contains only `position:own` (position moves only, api.md:266) and `state:write-human` (the human's state), with no scope for an agent's own `seen` writes.
- **CON-5: CONFIRMED.** features.md:52 tiers "Offline reading and action queue" to the phone client with "Web and Mac can start with less", yet README.md:67 ships web "offline reading" at step 7 and tech-stack.md:131 says web offline "covers reading plus a queue of actions" — the full tiered feature, leaving the web's reduced scope undefined.
- **CON-6: REFUTED.** The two statements count different things and are compatible: api.md:3's "three rounds of attack" matches the adversarial rounds (2, 4, 6 in validation.md's process table), while validation.md:101's "two rounds of review found real defects each time" counts only the defect-finding rounds (2 and 4); neither says the other count is wrong.

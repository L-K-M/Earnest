# Earnest draft consistency — findings

Files read: README.md, features.md, api.md, llm.md, ux.md, tech-stack.md, validation.md, competitors.md. No style review; no agents/git/gh; no edits.

## Tier / sequence (features.md vs README.md sequence)

- `features.md` “Folders | 3 | S | Maps onto Google Reader tags” — folders delivered at step 4. Severity: medium.
- `features.md` “Feed autodiscovery from any URL | 3” — autodiscover likely step 2. Severity: low.
- All others match: river/unread/cadences/determined/read view/mute = 5 (step 5); agent surface = 6 (step 6); native REST/change feed = 3 (step 3); Google Reader subset = 4 (step 4); search/saved searches/rules/webhooks/session end = 7 (step 7); clustering/embeddings/classifier/summaries/LLM edition = 10 (step 10); compiled rules/watch/ask = 11 (step 11). OK.

## Dependencies / step order

- `api.md` line 93: “A duplicate arriving next week needs read-time suppression in the view, which belongs with clustering at **step 9**.” README: clustering is **step 10** (pipeline: embeddings, clustering, semantic search, classifier, lazy summaries, LLM editions). Severity: medium.
- `validation.md` confirms prior sequence rebuilds (“M1 tokens ‘in the UI’, before any UI… Dependency errors. Sequence rebuilt”). Not a new contradiction, but step-order remains the active risk.
- No step requires a later step’s output to run (step 3 API works without step 4 subset; step 5 client uses step 3). OK.

## Tools / scopes vs tiers / invariants (api.md vs llm.md)

- `api.md` agent scopes (line 199): `entries:read`, `annotations:write`, `position:own`, `notify:send`, `proposals:write`, `state:write-human`, `notify:urgent`. `llm.md` tiers (line 137): 0 = annotations/cluster; 1 = tag/shelf; 2 = notify; 4 = subscribe/refresh/delete/tokens/rules/webhooks. Mapping correct; no agent scope triggers fetch; `notify:urgent` stays tier 2 (quiet hours only). OK.
- `llm.md` invariant 5: “Tiers are enforced in the service layer, over REST, CLI and MCP alike.” `api.md` line 199: “Scopes are enforced in the service layer, so the same limits apply over REST, CLI and MCP.” OK.
- Tool count ~8: `README.md` decision (line 46) “About 8 tools to start”; `api.md` line 252; `features.md` line 92 “about 8 tools”. OK.
- `notify` tool (line 263) scope `notify:send`, “Irreversible once delivered” — matches `llm.md` tier 2 “No, once delivered. Cancellable while held.” OK.

## Notification rules vs UX flows

- Digest default: `api.md` implied + `features.md` line 90 + `ux.md` line 23 “digest is the default.” OK.
- Budget: `api.md` response `budget` object; `ux.md` “Budget: 1 of 3 today”. OK.
- Server-generated push payload / no agent text: `api.md` 227 (“Agent text never enters a push payload by default… payload is server-generated”); `ux.md` 131 (“The lock screen shows only what the server generated”). OK.
- “Why” block server-gen, agent words plain separate: `api.md` 229; `ux.md` 131. OK.
- No caller URLs / only item tap: `api.md` 232; `ux.md` 131. OK.
- At-most-once delivery / cancel while held / mute agent: `api.md` 140, 234; `ux.md` 140 “Revert, Cancel, or nothing, since a delivered notification can’t be recalled.” Minor: “Revert” option label vs clarification; severity low.
- `notify:urgent` bypasses quiet hours only: `api.md` 233; `ux.md` Android “Now alerts rise above IMPORTANCE_LOW.” OK.
- Channels: `api.md` 230 (agent notifications in agent’s own Android channel) vs `ux.md` 147 “Channels per user-visible category.” Minor naming mismatch; severity low.

## Numbers repeated across docs

- “8 tools”: consistent (README 46, api.md 252, features.md 92).
- “28 tools”: NEWSBLUR MCP only (README 17, competitors.md 15). Not contradictory — different product.
- “300 feeds / ~1000 items / 1000/3000 design”: README 80 only; no conflict.
- “40 to 55 calendar weeks” steps 2–11: README 74 only.

## Relative links / anchors

- OK: README→llm.md#threat-model (`### Threat model`); README→api.md#change-feed (`### Change feed`); README→competitors.md#the-market-in-one-paragraph; tech-stack.md→llm.md#threat-model + api.md#change-feed; ux.md→competitors.md#what-users-say (`## What users say`).
- **Dangling:** `api.md` line 269: “The full threat model is in [llm.md](llm.md#safety).” Actual heading `### Threat model` → anchor `#threat-model`; `#safety` missing. Severity: medium.

## Other exact-quote checks

- `api.md` line 91 bulk/state scope uses `cluster` accepted; `llm.md` tier mapping consistent.
- `api.md` lines 140/145/154 notification replay/restore rules match `llm.md` invariant 3 (agent text inert by default) and invariant 6 (provenance server-generated).
- `features.md` tier definitions (“Later / Never”) used consistently (e.g., Fever API = Never; Mastodon/Bluesky = Later).
- No contradictions in `tech-stack.md`, `validation.md`, `competitors.md` with the others (competitors cited in README 17/76 correctly; validation documents prior fixes, not new errors).

## Severity tally

- Medium: folders tier 3 vs step 4; clustering step 9 vs 10; `api.md` `#safety` anchor dangling.
- Low: autodiscovery tier 3 vs step 2; UX “Revert” label nuance; Android channel wording.
- High: none.

Word count ~580. No document edited. Nothing launched externally.

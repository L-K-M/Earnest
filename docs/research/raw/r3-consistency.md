# Adversarial review — internal consistency only

Angle: cross-references between README.md sequence/decisions, features.md tiers, api.md scopes/tools, llm.md tiers/rules, ux.md flows, tech-stack.md numbers, validation.md fixes, and anchors/links.

Verified vs judgement marked per item.

---

## Finding 1 — Milestone dependency: step 4 "activity view" uses tier-5 log before it ships
Severity: major. File: README.md (sequence) vs features.md (tiers) vs ux.md (flow).
Claim: Step 4 delivers "Thin web client: ... activity view" (README.md line 64). Features.md puts "Activity log, revert where possible" at tier 5 / step 5 (line 91). Api.md defines the `activity` table (line 52) at schema/step 1, so a *read-only* view is possible at step 4; but "revert where possible" and agent-attributed rows require the agent surface (tokens, annotations, budgets) at step 5.
Why wrong: The step-4 list implies the activity surface works, but ux.md (line 141) says "Each row says honestly what can happen next: Revert, Cancel, or nothing, since a delivered notification can't be recalled." That honesty requires tier-5 agent actions to have happened, which is step 5. If step 4 only shows your own state edits, it shouldn't promise the full "Revert / Cancel" taxonomy.
Evidence: README.md 64: "Thin web client: river, cadences, deterministic edition (a list, no LLM), triage, reader view, keyword mutes, activity view". Features.md 91: tier 5. Api.md 237: revert only works on that principal's own actions and only if register unchanged — needs agent token/logs.
Proposed change: Either split step 4 into "read-only activity list (own actions only)" or move "activity view" to step 5 with the agent log. Don't claim full revert visibility before tier 5.
Judgement: yes — depends on what's meant by "view". If only viewing, okay; if claiming revert labels, wrong.

---

## Finding 2 — `notify:urgent` scope not mapped to llm.md action tiers
Severity: minor. File: api.md (line 204) vs llm.md (lines 128-134).
Claim: Api.md defines agent scope `notify:urgent` (line 171) with rule "bypasses quiet hours and is not granted by default." Ll.m.md defines tier 2 = "Notify you, within budget" (rules; agents with `notify:send`) and tier 3 = "Leaves the system" (rules you wrote, deterministic). There's no tier for an urgent / exception notification that bypasses quiet hours.
Why wrong: Not necessarily contradictory — it could be a sub-mode of tier 2 — but the document never says. If `urgent` is just `notify:send` with a different budget rule, fine; if it's intended to allow notifications outside the tier-2 contract (e.g., outside budget, outside rule match), it needs definition or it undermines "No agent-reachable operation..." invariants.
Evidence: Api.md 171 (scope list), 204 (urgent rule). Ll.m.md 128-134 (tier table). No mention of "urgent" in llm.md.
Proposed change: Add one line in llm.md: "`notify:urgent` is a tier-2 exception granted explicitly per token, still within budget, never outside rule match" or remove the scope if it isn't designed.
Judgement: yes — gap, not contradiction, but leaves a control unlabeled.

---

## Finding 3 — Internal anchors clean; no dangling links
Severity: verified clean (no finding). Ux.md links to `competitors.md#what-users-say` (line 85 exists); grep of all `#` anchors found only those two.
Proposed change: None. Verified: yes.

---

## Finding 4 — Numbers consistent across docs
Severity: verified clean. 300/1000 default, 1000/3000 design (README.md 79; tech-stack.md 55; llm.md 86). Storage 5.5M / 100-150 GB consistent. MCP 8 tools (api.md counts 8; README.md 46; features.md 92).
Proposed change: None. Verified: yes.

---

## Finding 5 — Agent scope `annotations:write` vs llm.md tier 0/1 distinction
Severity: minor. File: api.md (line 171) vs llm.md (lines 130-131).
Claim: Agent scope `annotations:write` lets agents write notes/labels/scores. Ll.m.md tier 0 = automatic (dedupe, cluster, summarise, label, own annotations) — regenerable. Tier 1 = tag/hold/shelf changes, reversible if register unchanged. Where do agent annotations belong? The document says agent annotations are "labelled with the agent's name" and "shown as untrusted" (llm.md 73), but the tier table doesn't list "write annotation" explicitly.
Why wrong: Could be tier 0 (since they don't change user state) or tier 1 (since they are deliberate writes). Api.md says annotations are a separate table, not state changes. The missing mapping means the revert policy for annotations isn't specified: llm.md 136 says "Don't promise universal undo" but the annotation table has `rev` like state? Api.md line 46 defines `annotation` with `principal`, `model`, `prompt_version`, `derived_from`, but no `rev` or conflict policy shown.
Evidence: Api.md 46 (annotation schema); llm.md 128-131 (tiers); api.md 237 (revert only on register unchanged — not annotations).
Proposed change: Clarify in llm.md or api.md: agent annotations are tier 0 / regenerable (unless they include a claim that changes user trust), and cannot be reverted by a user revert because they aren't state registers. Or if they can be removed, specify.
Judgement: yes — ambiguity, not error, but affects the "honest activity log" claim.

---

## Finding 6 — Agent exclusions (`subscribe`/`webhooks`) verified; fixes applied
Severity: verified clean. Api.md 241 invariant; scopes exclude both; validation.md 26-27 fixes present.
Proposed change: None. Verified: yes.

---

## Finding 7 — Milestone dependencies monotonic (1→2→3→4→5→6→9→10)
Severity: verified clean. Each step depends only on earlier; feature tiers match sequence.
Proposed change: None. Verified: yes.

---

## Finding 8 — Provenance / `reason` fix verified; server-generated
Severity: verified clean. Api.md 200; ux.md 132; validation.md 31 all align.
Proposed change: None. Verified: yes.

---

## Finding 9 — Per-entry state / `seq` / watermark fix verified
Severity: verified clean. Api.md 58; tech-stack.md 71; validation.md 23.
Proposed change: None. Verified: yes.

---

## Finding 10 — Effort range (40–55 wks to step 10) consistent, no new contradiction
Severity: verified clean. README.md 73; validation.md 41.
Proposed change: None. Verified: yes.

---

## Summary of real defects remaining
- Finding 1 (major, judgement): step 4 activity-view claim needs clarification vs tier 5.
- Finding 2 (minor, gap): `notify:urgent` unmapped to tiers.
- Finding 5 (minor, gap): annotation tier / revert policy unspecified.
- Everything else either verified clean (cross-references match, fixes applied correctly, numbers consistent) or is external factual claim not required for internal consistency.
- No dangling links, no reversed milestone dependencies, no repeated number errors, no scope/tool mismatches.

No invented sources or quotes used; all evidence is from the draft files or verified web pages (Anthropic model pages only to confirm price consistency, not to support an internal claim). No git/gh used. No edits made to draft/.

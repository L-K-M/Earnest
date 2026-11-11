# Review continuation, 2026-11-11

The plan was merged before implementation. Bounded coverage, findings and
execution state are in [findings.md](findings.md).

Baseline: `aaf7719cfa3ce932d6ed9e289e431cc44ed2d0e4`, current `origin/main`
at the start of this continuation. The [first batch](../2026-11-10-review/README.md)
merged six regression fixes, the Docker/pnpm fix, dependency updates and the
134-item backlog reconciliation. It recovered eight of nineteen review
reports at an older baseline. This continuation targets the eleven missing
review areas; incomplete reports and omitted files remain explicit. It does
not claim the older eight were rerun against this baseline.

## Scope

Review the existing app, confirm consequential defects and fix them in
focused pull requests. Do not implement product ideas, redesign clients,
change unresolved authorization or LLM spending policies, or rewrite the
preserved historical analysis. Recently merged web and Android fixes are
part of this baseline, not work authored by this continuation.

| Slice | Scope | Main risks |
| --- | --- | --- |
| S5 | Service agents, annotations, briefs, notifications, activity, revert, webhooks and triage; their tests and boundary callers | Authorization, ownership, idempotency, budgets, delivery |
| S6 | Store queries, migrations and sync model; their tests and service callers | Persistent invariants, migration safety, transactions, sync consistency |
| S9 | MCP, CLI and Google Reader compatibility; their tests and service boundaries | Scope checks, protocol behavior, input validation, documented options |
| W2 | App, routing, river, reader, article frame, sidebar, entry media, range marks and menus; adjacent tests and callers | Wrong state, navigation, races, resource ownership |
| W3 | Settings, feed settings, onboarding, login, rules, search, labels, agents, palette, pipeline, account, requests and styles; adjacent tests | Error handling, authorization assumptions, lost edits, forms |
| A1 | Android core and tests | Sync, persistence, authentication, request construction |
| A2 | Android app and tests | Account switching, lifecycle, user actions, background work |
| M1 | Mac EarnestKit and tests | Sync, persistence, authentication, request construction |
| M2 | Mac app and tests | Per-window ownership, lifecycle, reader, user actions |
| O1 | Dockerfile, Compose, updater, scripts, CI, release and operations docs | Deployment contract, secrets, portability, failure handling |
| O2 | Current product docs against their implementation | Incorrect commands, guarantees, configuration and API descriptions |

Production entrypoints are not exhaustive file lists. Each slice includes
adjacent tests and the callers needed to establish reachability. The detailed
[original slice map](../2026-11-10-review/method.md) supplies additional paths.
Backlog assessments remain code-reading dispositions at their stated
baseline, not independent regression-test claims.

## Execution

1. Commit this plan and open its PR before implementing fixes.
2. Run bounded, read-only LongCat 2.5 review jobs on an immutable detached
   baseline checkout. Start with S5, S6 and S9, then work through the web,
   native and operations slices. Record timeouts and incomplete coverage.
3. For each important candidate selected for a fix, read the code and tests,
   establish the intended contract, and obtain an independent attempt to
   refute the claimed failure. Neither a model verdict nor a failing test
   with an incorrect expectation decides correctness. Record disagreements
   and their evidence; defer unresolved policy questions.
4. Reproduce the confirmed failure in a regression test and observe it fail
   before changing production code. Use a focused branch from then-current
   `origin/main`. If the baseline moved, recheck the finding before editing.
5. Observe the regression pass, run surrounding tests and relevant lint,
   and check affected boundaries. Database tests require an isolated
   Postgres 18 instance with pgvector; skips are not passes. Use platform
   suites for native changes and web checks for web changes. Run the full
   Go race suite for server or shared-contract fixes.
6. Push each fix through CI and actual review feedback. Apply the stopping
   rules in `AGENTS.md`, merge eligible PRs without asking again, and record
   exact verification gaps. Do not manufacture pushes for another review.
7. Publish the report dispositions and coverage before closing the batch.
   Remove only owned worktrees, branches and temporary test services after
   saving wanted work. Leave the shared checkout and other agents' work alone.

## Finding states

- **Candidate:** a review claim, not yet established as a defect.
- **Reproduced:** intended behavior and a repeatable failing case are established.
- **Fixed:** the linked fix PR merged, with its regression and checks recorded.
- **Refuted:** contrary evidence is recorded.
- **Deferred:** the remaining uncertainty, policy decision or validation gap is recorded.

Judge severity by demonstrated impact, not the helper's label. Important
findings affect correctness, security, data loss, builds or materially
degraded behavior or performance. Record minor suggestions without turning
this continuation into cosmetic cleanup or speculative hardening.

## Verification and coordination

- Every report states its baseline, reviewed paths, tests run, limitations
  and concrete reproduction or missing evidence. A missing report leaves
  coverage unverified.
- Do not silently reuse old reports as current coverage or count unrelated
  agents' work as this continuation's implementation.
- Use dedicated worktrees under `/private/tmp`; build/database scratch and
  review logs may live on the external SSD. Bound helper concurrency and
  avoid simultaneous native builds or broad Go test runs.
- Retain the repository-owned local-image build policy, binary health probe,
  tag-only versioning, SHA-pinned actions and least-privilege CI permissions.
- Before finishing, distinguish local checks from CI, name each PR and its
  completed review rounds, and disclose incomplete slices and deferred findings.

# Review and improvement plan, 2026-11-10

Status: first improvement batch implemented and submitted for merge.
Baseline commit `f8adbdb` (merge of PR #143). The linked PRs track CI,
review and merge gates; local implementation alone does not complete a task.

This plan covers one pass over the whole app: the Go server, the web
client, the Android and Mac clients, deployment, CI and the docs. It
reviews what is there, verifies what it finds, fixes the confirmed
problems in small pull requests and retires the stale root backlog.
[method.md](method.md) describes how the review runs.
[findings.md](findings.md) records what it found and what happened to
each finding. [backlog.md](backlog.md) reconciles all 134 historical items,
with the [original analysis](original-analysis.md) preserved unchanged.

The initial review fan-out was interrupted by the previous assistant's
weekly limit. Eight of nineteen review slices returned usable reports;
all eight backlog sections completed. Missing slices are a coverage gap,
not a clean review. The current batch prioritizes reproducible safety and
reliability fixes from the recovered reports.

## Where the app stood at the baseline

- Two tagged releases, v0.1.0 and v0.2.0. Fifty-four pull requests merged
  since 2026-11-06, most of them client work: Android reader tools, undo,
  OPML, feed health; Mac per-window state, icons; web routing, context
  menus, icons and thumbnails, starred import.
- CI is green on `main` at the baseline. The gate is `make check` plus the
  Mac and Android workflows. The automated GLM review runs on
  `pull_request_target`, which GitHub is phasing out for public repos.
- Seven Dependabot pull requests are open (#133 to #139). Five pass. #138
  (`golang.org/x/net` 0.62.0) failed on a transient module proxy error.
  #139 (`node:26-alpine`) fails because the image build runs
  `corepack enable` and Node 26 no longer ships corepack.
- `ANALYSIS.md` at the repository root lists 134 review findings from
  2026-11-06. Many were implemented since, so the file misleads anyone
  who reads it as a backlog.
- Size: about 74k lines of Go, 28k of TypeScript, 18k of Kotlin, 17k of
  Swift. Test files: 157 Go, 43 web, 48 Kotlin, 28 Swift.
- No TODO or FIXME markers in the code.

## Goals

1. Find and fix defects that matter: security and authorisation gaps,
   data loss, wrong sync results, crashes, broken builds, degraded
   performance, and docs that contradict the code.
2. Leave the Dependabot queue empty or explained.
3. Replace the stale root backlog with a reconciled list: implemented,
   still open, or declined with a reason.
4. Keep every change small, tested and merged through its own pull
   request, as `AGENTS.md` requires.

Out of scope: product decisions (new features, redesigns), and the
research documents. UX findings are fixed only when the fix is small and
uncontroversial; the rest are recorded for you to decide.

## Phases

| # | Phase | Output | Done when |
| --- | --- | --- | --- |
| 0 | Scout | This section | Done 2026-11-10 |
| 1 | Plan | This directory, merged | PR merged |
| 2 | Review fan-out | `findings.md` with findings and coverage gaps | Reports recovered, selected important findings independently checked, missing slices disclosed |
| 3 | Housekeeping | Dependabot queue cleared | Green PRs merged, #138 rerun, #139 fixed or closed with a reason |
| 4 | Fix slices | One PR per finding or coherent group | Each PR merged with CI green on its head |
| 5 | Backlog reconciliation | Root index, preserved original and `backlog.md` | PR merged |
| 6 | Close | Status table below, final report | All PRs merged or their blockers named |

Phases 2 and 3 run in parallel. Phase 4 starts as soon as the first
findings are verified; it does not wait for every slice.

## Ordering of fixes

1. Security, authorisation, data loss, wrong sync results.
2. Crashes, broken builds, wrong behaviour users will hit.
3. Performance that degrades with ordinary data sizes.
4. Docs that contradict the code or the deployment contract.
5. Small UX fixes with no product decision attached.

Everything else is deferred and listed with a reason.

## Constraints

- Work happens in dedicated worktrees under `/private/tmp`, never in the
  shared Paseo worktree, which other agents switch branches in.
- Other agents own the article frame channel and Today muted filter.
  The frame fix #146 has merged; its follow-up #157 and the Today work
  #153 remain separate. This batch avoids those implementation areas.
- Review and bounded verification jobs run directly on LongCat 2.5 through
  Pi. Each job has one small task and a timeout. Failed or missing reports
  are disclosed rather than treated as approval.
- Every fix follows the bug-fix rule in `AGENTS.md`: a regression test
  that fails first, then the fix.
- Review helpers used a detached checkout at the baseline that nobody
  edited. It was removed after the reports were recovered.

## Risks

- A push sometimes starts no CI runs; close and reopen the PR.
- The web timing test in `main-review.test.tsx` flakes under load; rerun
  before investigating.
- The GLM reviewer may hit the `pull_request_target` brownout; wait for
  the window, then retrigger.
- System disk space is shared with other jobs. Large build scratch files
  live on the data volume. The isolated test database moved to an external
  SSD after slow database cloning caused test-harness timeouts. Owned
  worktrees and test services are removed when their work ends.

## Status

Execution checkpoint after local verification. PR links are the source of
truth for the latest CI, review and merge state.

| Item | State | PR |
| --- | --- | --- |
| Plan committed | written before fixes, merged | [#147](https://github.com/L-K-M/Earnest/pull/147) |
| Review fan-out | 8/19 reports recovered; gaps recorded | see findings.md |
| Dependabot: merge green PRs #133 to #137 | merged | #133, #134, #135, #136, #137 |
| Dependabot: rerun #138 | rerun passed, merged | [#138](https://github.com/L-K-M/Earnest/pull/138) |
| Docker pnpm install | merged; local Node 24/26 web-stage builds passed | [#150](https://github.com/L-K-M/Earnest/pull/150) |
| Dependabot: Node 26 | refreshed with current main without force-pushing | [#139](https://github.com/L-K-M/Earnest/pull/139) |
| Retention overflow | regression verified, implementation submitted | [#155](https://github.com/L-K-M/Earnest/pull/155) |
| Redirect-triggered shutdown | Postgres regressions verified, implementation submitted | [#156](https://github.com/L-K-M/Earnest/pull/156) |
| Uncertain LLM billing | completion, embedding and Postgres regressions verified | [#159](https://github.com/L-K-M/Earnest/pull/159) |
| Duplicate redirected subscriptions | reuse, precedence and credential regressions verified | [#160](https://github.com/L-K-M/Earnest/pull/160) |
| Concurrent login throttle | mixed-endpoint concurrency regression verified; Go race suite passed | [#161](https://github.com/L-K-M/Earnest/pull/161) |
| Portable backup pruning | regression verified on macOS and Alpine | [#162](https://github.com/L-K-M/Earnest/pull/162) |
| Backlog reconciliation | all 134 headings assessed, original preserved | [#158](https://github.com/L-K-M/Earnest/pull/158) |

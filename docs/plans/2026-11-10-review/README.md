# Review and improvement plan, 2026-11-10

Status: in progress. Baseline commit `f8adbdb` (merge of PR #143).

This plan covers one pass over the whole app: the Go server, the web
client, the Android and Mac clients, deployment, CI and the docs. It
reviews what is there, verifies what it finds, fixes the confirmed
problems in small pull requests and retires the stale root backlog.
[method.md](method.md) describes how the review runs.
[findings.md](findings.md) records what it found and what happened to
each finding.

## Where the app stands

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
| 2 | Review fan-out | `findings.md` with verified findings | Every slice in `method.md` reported, every important finding verified or refuted |
| 3 | Housekeeping | Dependabot queue cleared | Green PRs merged, #138 rerun, #139 fixed or closed with a reason |
| 4 | Fix slices | One PR per finding or coherent group | Each PR merged with CI green on its head |
| 5 | Backlog reconciliation | `ANALYSIS.md` retired, open items in `findings.md` | PR merged |
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
- Two web branches are in flight by other agents:
  `fix/web-frame-channel` and `fix/web-today-muted-sync-flicker`. Fixes
  in this plan avoid the article frame channel and the Today muted
  filter, or wait for those to merge.
- Review and bounded implementation jobs run on LongCat 2.5 through Pi,
  with Devin `swe-2-max` as the fallback. Each job gets one small slice;
  long jobs time out at the relay.
- Every fix follows the bug-fix rule in `AGENTS.md`: a regression test
  that fails first, then the fix.
- Review helpers read the working tree live, so the tree they read is a
  detached checkout at the baseline that nobody edits.

## Risks

- A push sometimes starts no CI runs; close and reopen the PR.
- The web timing test in `main-review.test.tsx` flakes under load; rerun
  before investigating.
- The GLM reviewer may hit the `pull_request_target` brownout; wait for
  the window, then retrigger.
- The machine's data volume is at 97%. Worktrees are removed when their
  work merges.

## Status

Updated as work lands. PR numbers link the record.

| Item | State | PR |
| --- | --- | --- |
| Plan committed | open | |
| Review fan-out | pending | |
| Dependabot: merge green PRs #133 to #137 | pending | |
| Dependabot: rerun #138 | pending | |
| Dependabot: fix #139 corepack on Node 26 | pending | |
| Fix slices | pending | see findings.md |
| Backlog reconciliation | pending | |

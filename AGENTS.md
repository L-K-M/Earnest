# AGENTS.md — Earnest

Guidance for AI coding agents working in this repository.

## What Earnest is

Earnest is a self-hosted **RSS reader** for web, Mac and Android whose API
treats agents as distinct, governed actors: own identity, read position,
annotations, budget and revert log. Deployed on a home NAS via
Docker Compose; tailnet-first, not public.

The plan lives in [`docs/research/`](docs/research/README.md): decisions,
sequence and the threat model. The product is one Go binary (`earnest`)
deployed next to Postgres + pgvector, serving the API, the embedded web
client (`web/`), Google Reader compatibility, MCP and the CLI. Native
clients live in `clients/mac` and `clients/android`.

## Commands

| What | Command |
|---|---|
| Build the binary | `make build` or `scripts/build.sh` → `dist/earnest` |
| Tests + vet + gofmt | `make test`, `make lint` |
| Build the image | `make docker-build` or `scripts/build.sh --docker` → `earnest:local` |
| Validate the stack | `make compose-check` |
| Everything CI runs | `make check` |
| Run the stack | `docker compose up -d --build` → `localhost:8080/healthz` |
| Update a deployment | `./update.sh [branch]` |
| Tag a release | `scripts/release.sh X.Y.Z [--push]` (see `CICD.md`) |

## Layout and stack

- Go 1.27, one binary at `cmd/earnest`. `CGO_ENABLED=0`, `log/slog` only.
  Packages live under `internal/`; the map and layering rules are in
  `docs/architecture.md`.
- `Dockerfile`: `golang:1.27-alpine` → `alpine:3.24`, version/revision stamped
  by `-ldflags` at build time.
- `compose.yaml`: `earnest` (`image:` + `build:` + `pull_policy: build`) and
  `postgres` (`pgvector/pgvector:pg18`).

## Deployment contract — do not break

- **Repo-owned images are built locally, never pulled.** Keep `image:` +
  `build:` + `pull_policy: build` on the `earnest` service; `update.sh` never
  points at a registry (anonymous pulls fail on private/self-hosted hosts).
- **`update.sh` splits `pull` → `build --pull` → `up -d --remove-orphans`.**
  `up --build` alone reuses cached base layers forever; `--pull` is the only
  thing that refreshes alpine/golang.
- **The binary is its own liveness probe** (`earnest healthcheck` hits
  `127.0.0.1:$EARNEST_HTTP_ADDR/healthz`) — the runtime image ships no curl.
- **The version is the git tag** (`RELEASE_KIND=tag-only`): nothing committed
  declares a version, and the binary's version is ldflags-stamped. Do not add
  a version file.
- **Compose tunables are `${VAR:-default}`** documented inline; every one is
  covered by `.env.example`. Hard-required secrets use `${VAR:?message}`.

## Conventions

- The research docs are defaults to overturn, not commitments
  (`docs/research/README.md`) — but don't rewrite them in place; record new
  decisions alongside, as in `docs/research/decisions.md`, which overrides
  the research.
- The licence is the Unlicense. Don't copy, port or vendor code from
  third-party projects; read them for reference only. Code from L-K-M's own
  repositories may be reused. Libraries are fine as dependencies.
- CI is SHA-pinned actions + least-privilege permissions; `make check` mirrors
  the CI gate exactly.

## Working practices

- Follow explicit task instructions over the default workflow below.
- Writing the code is not finishing the task. A task is finished when
  its changes are merged to main through a PR that passed CI and review,
  or when the user explicitly accepts a different end state.
- Start every task on current code. Fetch first, then cut the task
  branch from origin/main — never from a stale local branch or an old
  checkout. To continue existing work, rebase or merge the latest
  origin/main into it before editing. Never overwrite existing work to
  update.
- Resolve ambiguity before making consequential changes. State low-risk
  assumptions; ask when scope, safety, or expected behavior is unclear.
- Keep changes focused. Do not modify unrelated code, formatting, or comments.
- Prefer surgical edits over whole-file rewrites when the result is equivalent.
- Stage only intended files. Inspect the diff before committing.

## Communication

- Be concise, factual, and direct. Preserve necessary context and uncertainty.
- Avoid praise, motivational filler, emojis, and em dashes in new prose.
- Address the reader directly in user-facing copy.
- Report what was verified and what remains unverified. Never imply that an
  unavailable check passed.

## Code design

- Prefer early returns and shallow nesting. Separate logical blocks with
  blank lines.
- Use descriptive constants or enums for meaningful or repeated values.
  Use existing standard definitions for protocol/specification constants.
  Keep obvious, one-off values inline.
- Use enums for behavioral modes that would otherwise require ambiguous
  boolean arguments.
- Default members to private. Widen visibility only for required consumers,
  and review the change as an API design decision.
- Follow the repository's declared dependency boundaries. UI and controllers
  must use application services rather than directly accessing databases,
  subprocesses, sockets, or other low-level mechanisms.
- Encapsulate low-level mechanics behind domain-oriented interfaces.
- Reuse genuinely shared logic. Avoid speculative abstractions and layers
  that only forward calls.
- Prefer pure functions for business rules and immutable data where practical.
  Isolate side effects; document non-obvious state ownership or synchronization.
- Explain non-obvious intent, constraints, and tradeoffs in comments.
  Do not narrate obvious code. Add examples or diagrams when they clarify it.

## Validation and errors

- Validate untrusted input at entry points. Where practical, represent valid
  states in types and enforce persistent invariants in database schemas.
- Represent absence and failure explicitly.
- Use assertions for internal programming invariants, not external-input
  validation or required runtime error handling.
- Prefer explicit, actionable errors over silent failure or undocumented
  fallback. Document intentional recovery behavior.
- Never report a skipped or failed operation as successful.

## Bug fixes

1. Identify the root cause and define an observable success criterion.
2. Add a regression test and observe the relevant failure before fixing it.
3. Implement the fix and observe the test passing.
4. Check surrounding behavior for regressions and architectural consistency.

If an automated regression test is impractical, document the reproduction
and verification procedure. State any inability to reproduce the failure.

## Verification

- Run relevant tests and lint after changes.
- Choose coverage by affected behavior and risk, not patch size.
- Use integration or end-to-end tests for critical workflows and boundaries;
  test isolated business rules at the lowest effective level.
- Run broader suites for cross-cutting or high-risk changes, and the full
  required release checks before releasing.
- Validate the requested command, options, platform, and configuration.
  Unrelated green CI is not proof that the reported problem is fixed.
- Recheck after the final edit. Distinguish local checks from CI results.

## Commit messages

- Use a capitalized, imperative subject without a final period.
- Target 50 characters; never exceed 72.
- Separate the subject and body with one blank line.
- Wrap body text at 72 characters.
- Explain what changed and why. Leave implementation mechanics to the code.

## Implementation and review

Unless explicitly instructed otherwise:

1. Work on a focused branch cut from the latest origin/main and open a PR
   against main before reporting the task as done.
2. Inspect CI results and completed review feedback for the latest commit.
   A successful reviewer job does not mean the review found no problems.
3. Address important findings or explain why they do not apply. Handle minor
   findings according to the stopping rules below.
4. Evaluate each fix in the surrounding project, add regression coverage,
   and rerun affected checks before pushing.
5. Repeat until a stopping criterion is met.
6. Merge without asking again once the stopping criterion is met, required
   checks pass on the latest commit, and no unresolved blockers or required
   human review requests remain.

### Reviewer context limits

The automated PR reviewer does not see the user's original prompt or
conversation. It may suggest changes that go against or beyond what the
user asked for. Do not implement such suggestions. Note each conflict and
report it to the user at the end of the thread.

### Automated review stopping rules

Judge findings by verified impact, not the reviewer's severity label.
Important findings concern correctness, security, data loss, broken builds,
or materially degraded behavior/performance.

Track completed review rounds and consecutive rounds without important
findings. Reruns of the same revision and integration failures do not count.

- No applicable actionable feedback: finish immediately.
- First minor-only round: optionally fix worthwhile, low-risk findings.
  Do not manufacture another push merely to obtain another review.
- Two consecutive rounds without important findings: stop responding to
  automated nitpicks, even if actionable minor suggestions remain.
  Defer worthwhile leftovers rather than continuing the cycle.
- A confirmed important finding resets the minor-only streak. Address it
  and verify the fix before continuing.

After ten completed rounds, enter stabilization:

- Stop optional cleanup, refactoring, and nitpick fixes.
- One completed review without confirmed important findings is sufficient
  to finish, even if minor suggestions remain.
- Continue only for confirmed important defects. If resolving them stalls,
  report the blockers rather than continuing indefinitely.

These limits end optional automated-feedback work. They do not waive
confirmed blockers, unresolved human review requests, or required checks.

### Reviewer integration failures

After two consecutive reviewer-integration failures, stop and report the
review gap. Do not treat failures as approval. An explicit user instruction
may waive review; report that waiver rather than claiming review passed.

## Ending a task

- A task ends with its changes merged to main — not with code written,
  and not with a PR merely opened. An open PR is work in progress:
  monitor CI on the latest commit, address review findings per the
  stopping rules, and merge once the criteria are met.
- Never finish with uncommitted changes or unpushed commits in the
  worktree. Commit, push, and open or update the PR first.
- If a step is impossible (missing push access, CI failure, reviewer
  outage), report the exact blocker instead. Never present unreviewed or
  unmerged work as finished.
- Before finishing, confirm: the requested behavior is implemented
  without unrelated changes; relevant checks pass on the latest code;
  important review findings are addressed or rejected with reasons;
  deferred suggestions, remaining risks, and validation gaps are
  disclosed.
- The final response states where the work stands: branch, PR, CI
  status, review rounds completed, and whether it is merged.

<!-- shared-rules:end -->

# Review method

The review runs as bounded, read-only slices over a detached checkout at
the baseline commit. Each slice reports findings in one structure. Every
finding rated important or higher selected for a fix is then handed to an
independent reviewer told to refute it. A reproduced defect, not a lack of
refutation, is the acceptance criterion. Coverage gaps are recorded in
[findings.md](findings.md).

## Slices

Each slice names its files and lenses. A slice is one helper job. The
lenses are: security and authorisation (S), correctness (C), data loss
and sync consistency (D), resource use and performance (P), accuracy of
docs against code (A), user-facing behaviour (U).

### Server

| Slice | Files | Lenses |
| --- | --- | --- |
| S1 | `internal/api`, `cmd/earnest/serve.go` | S, C, A |
| S2 | `internal/service`: principals, tokens, sessions, devices, scopes, CSRF | S, C |
| S3 | `internal/service`: entries, state, sync, changes, positions | C, D |
| S4 | `internal/service`: subscriptions, fetch, labels, kept import, media | C, D, P |
| S5 | `internal/service`: agents, annotations, briefs, notifications, activity, revert, webhooks, triage | S, C |
| S6 | `internal/store`, migrations, `internal/syncmodel` | C, D, P |
| S7 | `internal/outbound`, `internal/fetcher`, `internal/feed` | S, C, P |
| S8 | `internal/sanitize`, `internal/extract`, `internal/media`, `internal/imageproxy`, `internal/web` | S, C |
| S9 | `internal/mcp`, `internal/cli`, `internal/compat/greader` | S, C, A |
| S10 | `internal/llm`, `internal/pipeline`, `internal/push`, `internal/config`, `internal/metrics`, `internal/clientip` | S, C, P |

### Web client

| Slice | Files | Lenses |
| --- | --- | --- |
| W1 | `web/src/sync`, `web/src/offline`, `web/src/syncBridge.ts` | C, D, P |
| W2 | `web/src/App.tsx`, `web/src/router.tsx`, `web/src/routes.ts`, `web/src/River.tsx`, `web/src/ArticleReader.tsx`, `web/src/ArticleFrame.tsx`, `web/src/Sidebar.tsx`, `web/src/EntryMedia.tsx`, `web/src/markRest.ts`, `web/src/menus.ts`, `web/src/ContextMenu.tsx` | C, U, P |
| W3 | `web/src/Settings.tsx`, `web/src/SettingsPage.tsx`, `web/src/FeedSettings.tsx`, `web/src/Onboarding.tsx`, `web/src/Login.tsx`, `web/src/rules/`, `web/src/search/`, `web/src/labels/`, `web/src/agents/`, `web/src/palette/`, `web/src/pipeline/`, `web/src/account/`, `web/src/request.ts`, `web/src/style.css` | C, U, S |

### Native clients

| Slice | Files | Lenses |
| --- | --- | --- |
| A1 | `clients/android/core` | C, D, S |
| A2 | `clients/android/app` | C, U, P |
| M1 | `clients/mac/EarnestKit` | C, D, S |
| M2 | `clients/mac/App` | C, U, P |

### Operations and docs

| Slice | Files | Lenses |
| --- | --- | --- |
| O1 | `Dockerfile`, `compose.yaml`, `update.sh`, `.env.example`, `scripts/`, `.github/workflows`, `CICD.md`, `docs/releases.md`, `docs/operations.md` | S, C, A |
| O2 | `docs/*.md` against the code they describe: configuration against `internal/config`, agents against `internal/mcp` and `internal/service`, cli against `internal/cli`, greader, sync, schema against migrations, web, pipeline, llm-providers | A |

### Backlog reconciliation

`ANALYSIS.md` has eight sections. One job per section checks each
heading against the baseline code and reports: implemented (where),
still open, or no longer applicable.

| Slice | Section |
| --- | --- |
| B1 | usability |
| B2 | performance |
| B3 | visual issues |
| B4 | aesthetics and theming |
| B5 | missing features |
| B6 | bugs |
| B7 | general issues |
| B8 | novel ideas |

## Finding record

Every finding carries: slice, title, severity (blocking, important,
minor, nit), kind (security, correctness, data-loss, performance, docs,
ux), file and line, the evidence (what the code does), why it matters
(the concrete failure), a suggested fix, and the reviewer's confidence.

Severity follows `AGENTS.md`: important findings concern correctness,
security, data loss, broken builds or materially degraded behaviour.
Everything else is minor or a nit.

## Verification

- Each important or blocking finding selected for implementation goes to
  a second helper with its text and the instruction to refute it from the
  code and tests. Unresolved claims remain unverified, not confirmed.
- Before a fix starts, the author reads the cited code and reproduces the
  failure in a test. A claim contradicted by evidence is refuted. A claim
  that cannot be reproduced remains deferred with the validation gap.
- Minor findings are not verified individually. They are fixed only when
  a fix slice already touches the file, or deferred. Backlog reconciliation
  is a code-reading assessment, not independent regression verification.

## Fix slices

One branch from `origin/main` per finding or coherent group. Each PR:
regression test that fails first, the fix, `make lint test` plus the
affected platform checks, push, review rounds per the stopping rules in
`AGENTS.md`, merge. A LongCat watcher babysits each PR so the main
session wakes once per PR.

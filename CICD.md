# CI/CD — Earnest

## Workflows (`.github/workflows/`)

| Workflow | Trigger | Jobs |
|---|---|---|
| `ci.yml` | push to `main`, PRs, reusable call | Go checks, database tests, script tests, web checks and Compose validation |
| `images.yml` | push to `main`, PRs, reusable call | Builds the Dockerfile for `linux/amd64` and `linux/arm64` without publishing |
| `mac.yml` | relevant PRs, reusable call | Portable package tests and macOS app build/tests |
| `android.yml` | relevant PRs, reusable call | Core/app tests, debug build, lint and signed-release fixture validation |
| `release.yml` | `v*` tags, manual validation | Calls every platform gate; only a tag push prepares a draft release |
| `zai-code-review.yml` | PRs | automated GLM review: deep tier `glm-5.3`, fast tier MiMo `mimo-v2.6-pro`, StepFun `step-5-preview` last-resort fallback |

All workflows use least-privilege `permissions`, `timeout-minutes` on every
job, and `concurrency` that cancels superseded PR runs but never an
in-progress `main` run. Third-party actions are pinned to commit SHAs.

## Secrets

| Secret | Used by |
|---|---|
| `ZAI_API_KEY` | `zai-code-review.yml` — Z.ai coding-plan key for the review tiers |
| `MIMO_API_KEY` | `zai-code-review.yml` — fast-tier provider (mimo-v2.6-pro) |
| `STEPFUN_API_KEY` | `zai-code-review.yml` — last-resort fallback provider (step-5-preview) |

Server builds and deployments need no registry credentials: the image is
built locally on the deployment host. Native signing credentials stay in the
release operator's Keychain and secret manager, not in this table or Git.
Android CI signs only a disposable fixture, never a distribution package.

## Deployment and release

- A deployment is a clone of this repo plus a `.env` (see `.env.example`).
  `./update.sh` syncs the branch, pulls external images, rebuilds
  `earnest:local` with fresh base layers (`build --pull`) and recreates the
  stack with `--remove-orphans`.
- Releases are source tags. `scripts/release.sh X.Y.Z --push` (a stub over
  the shared `lkm-release` engine, `RELEASE_KIND=tag-only`) creates
  `vX.Y.Z`. The tag workflow validates server, web, images and both native
  clients at that revision before creating a draft release. The binary's
  `version`/`revision` come from `-ldflags` at image build time. Native release
  versions are stamped from the source tag, not a committed version file.
- Build signed clients with `scripts/package-client.sh`, attach their receipts
  and checksums, and record device/deployment acceptance before publishing the
  draft. See [three-platform releases](docs/releases.md). A source tag or a
  debug/unsigned CI build alone is not a finished release.

## Local gate

`make check` mirrors the server/web CI gate: Go formatting/vet/race tests,
backup and packaging-orchestration tests, web typecheck/lint/tests/build/schema
checks, Compose validation and the image build. Platform commands are documented
in [Mac](docs/mac.md) and [Android](docs/android.md). Packaging orchestration uses
fake tools; it does not prove real signing, notarization or device acceptance.

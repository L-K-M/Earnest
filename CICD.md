# CI/CD — Earnest

## Workflows (`.github/workflows/`)

| Workflow | Trigger | Jobs |
|---|---|---|
| `ci.yml` | push to `main`, PRs | `go` (gofmt, `go vet`, `go test -race`, `go build` on Go 1.27) · `compose` (`docker compose config --quiet`) |
| `images.yml` | push to `main`, PRs, `v*` tags | `image` — builds the Dockerfile for `linux/amd64` + `linux/arm64` without publishing (the tag-time publish gate) |
| `release.yml` | `v*` tags | `release` — mints the GitHub release with generated notes |
| `zai-code-review.yml` | PRs | automated GLM review: deep tier `glm-5.3`, fast tier StepFun `step-5-preview`, MiMo outage fallback |

All workflows use least-privilege `permissions`, `timeout-minutes` on every
job, and `concurrency` that cancels superseded PR runs but never an
in-progress `main` run. Third-party actions are pinned to commit SHAs.

## Secrets

| Secret | Used by |
|---|---|
| `ZAI_API_KEY` | `zai-code-review.yml` — Z.ai coding-plan key for the review tiers |
| `STEPFUN_API_KEY` | `zai-code-review.yml` — fast-tier provider (step-5-preview) |
| `MIMO_API_KEY` | `zai-code-review.yml` — outage fallback provider |

No build or deploy secrets exist: the image is built locally on the
deployment host, and no registry credentials are needed anywhere.

## Deployment and release

- A deployment is a clone of this repo plus a `.env` (see `.env.example`).
  `./update.sh` syncs the branch, pulls external images, rebuilds
  `earnest:local` with fresh base layers (`build --pull`) and recreates the
  stack with `--remove-orphans`.
- Releases are source tags. `scripts/release.sh X.Y.Z --push` (a stub over
  the shared `lkm-release` engine, `RELEASE_KIND=tag-only`) creates
  `vX.Y.Z`; `release.yml` mints the GitHub release and `images.yml` verifies
  the image builds on that tag. The binary's `version`/`revision` come from
  `-ldflags` at image build time — nothing committed declares a version.

## Local gate

`make check` mirrors CI: `lint` (gofmt + `go vet`), `test` (`go test -race`),
`compose-check` (`docker compose config --quiet`), `docker-build`.

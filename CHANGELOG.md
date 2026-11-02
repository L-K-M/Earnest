# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Decisions on licence, code reuse, the agent model and the round 9 sync
  and security gaps, in `docs/research/decisions.md`.
- Preliminary research: competitor survey, feature set, UX, tech stack
  (Go + Postgres/pgvector, provisional), API shape, LLM threat model and a
  staged build sequence — see `docs/research/` (#1, merged from
  BigBoyDevBox/Earnest).
- Deployment scaffold: `earnest` Go binary (healthcheck + build version only),
  multi-stage `Dockerfile`, `compose.yaml` (Postgres 18 + pgvector),
  `update.sh`, `Makefile`, `scripts/build.sh`, `scripts/release.sh`
  (tag-only), CI, image verification and automated PR review.

### Changed

- Research, round 9 review: step 0 now requires self-hosted ntfy and an
  allowlist enforced outside the agent; two factual errors fixed; sync
  and security gaps recorded as open in `api.md` and `llm.md` (#2).

# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Server foundation: the executable sync model with 35 shared test
  vectors (`docs/sync.md`), the full Postgres schema and migrations,
  users, sessions, app and agent tokens and the device flow, the
  outbound client with SSRF controls, feed parsing and OPML, the HTML
  sanitiser and full-text extraction, the fetcher, the service core for
  feeds, entries, state and sync, notification transports, metrics and
  backups. `docs/architecture.md` describes the layout.
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

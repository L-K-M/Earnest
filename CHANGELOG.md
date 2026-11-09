# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Context menus in the web client. Right-click, long-press or press
  Shift+F10 on Today, a folder or a feed for "Mark all as read", a
  refresh and its settings, or on an article for its reading actions and
  "Mark read up to here". Shift with a right click keeps the browser's
  menu.
- The Earnest logo in the web client's sidebar goes to Today.
- Import starred articles into Kept from another reader's Google Reader
  JSON export (FreshRSS, Inoreader, Google Takeout), with its labels, in
  Manage feeds, with `earnest entries import` or `POST /v1/kept:import`.
  Imported articles stay out of the river, never override articles or
  labels you removed, and are adopted by the feed's next fetch instead of
  duplicated. Upload limits are documented as 413 responses.
- The native API (`/v1`, OpenAPI, SSE hints), the Google Reader subset
  for NetNewsWire and Capy Reader, and `earnest serve` wiring it all.
- The agent surface: tokens bound to an audience, annotations and
  briefs, notifications with budgets and at-most-once delivery, the
  activity log with revert, triage, webhooks, the MCP server,
  `/llms.txt`, an agent skill and the `earnest` CLI. See
  `docs/agents.md`.
- Search, saved searches, mutes and deterministic rules with a dry run
  (`docs/rules.md`).
- The web client: a river that ends, unread mode, cadences, editions,
  keyboard triage, reader view in a sandboxed origin, offline reading,
  Web Push, and screens for agents, rules, search and settings.
- The optional LLM pipeline, off until configured: embeddings, story
  clustering, hybrid search, a classifier, watch rules, summaries with
  verified passages, editions, compiled rules and ask my feeds, all
  within hard monthly budgets (`docs/pipeline.md`).
- Mac and Android clients that run the shared sync vectors.
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

### Fixed

- The web reader scrolls an article in one place. The article frame now
  takes the article's height instead of the pane's, so Space, Page Down
  and the arrow keys move through the whole article, also after a click
  inside it.
- A feed that lists two items under one identity, such as a WordPress
  comments feed, no longer stops `earnest serve`: comments on one post
  keep distinct identities, a repeated item is stored once, and data the
  server refuses is recorded as that feed's failure.

### Changed

- The web client gives every view a URL: lists, open articles, searches,
  rules, notifications, feed management and settings survive refresh,
  Back and Forward, and can be bookmarked. Rows are links, `j`/`k` open
  the next article while reading, deliberate actions offer an undo
  (`z`), and narrow screens get a drawer and a single-pane reader.
  Fingerprinted web assets are served precompressed (Brotli or gzip).
- The Mac client now requires macOS 26 and follows Mac conventions:
  Settings in its own window, toolbar search with saved searches in the
  sidebar, a full menu bar, undoable reading actions, context menus,
  OPML import and export, find and zoom in the reader, and a clearer
  sign-in. See `docs/mac.md`.
- Research, round 9 review: step 0 now requires self-hosted ntfy and an
  allowlist enforced outside the agent; two factual errors fixed; sync
  and security gaps recorded as open in `api.md` and `llm.md` (#2).

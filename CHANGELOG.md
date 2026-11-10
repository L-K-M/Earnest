# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- The Android reader finds text in an article with match counts and
  steps, zooms its text from 80% to 200% with a device-local setting,
  and says so when it shows a cached copy of an article.
- The Android Feeds screen imports and exports OPML subscriptions. The
  import reports each outline's outcome and the export writes the
  account's subscriptions to a picked file.
- The Android Feeds screen groups subscriptions by folder and shows a
  failing feed's fetch error with the time of its last attempt.
- Android row and reader actions undo: Later, Done, Keep, Unkeep and Mark
  unread name themselves in an undo snackbar that writes back the
  registers they changed and leaves alone one changed since, and a
  cadence change offers the same: its undo leaves alone one a later
  change replaced. Mark unread returns a read article to unread
  and clears Done with it; scrolling never reverses it, only opening the
  article reads it again. The reader header gained Copy link and Share
  for the entry URL.
- Context menus in the web client. Right-click, long-press or press
  Shift+F10 on Today, a folder or a feed for "Mark all as read", a
  refresh and its settings, or on an article for its reading actions and
  "Mark read up to here". Shift with a right click keeps the browser's
  menu.
- The Earnest logo in the web client's sidebar goes to Today.
- The Android app shows feed icons and article thumbnails in article rows
  and feed icons in Feeds, loaded with the account's token and kept per
  account.
- The Mac app shows feed icons and article thumbnails in article rows and
  feed icons in Feeds, loaded with the account's token and kept per
  account. Article titles in the list now wrap to two lines as their row
  height assumes.
- The web article list shows each feed's icon and a thumbnail of the
  article's image. Earnest downloads and shrinks them when feeds and
  articles arrive and serves its own copies, so lists never contact
  publishers. Entries carry `thumbnail_url` and subscriptions `icon_url`,
  served by `GET /v1/entries/{id}/thumbnail` and
  `GET /v1/subscriptions/{id}/icon`.
- The web sidebar's feed list shows feed icons too. A thumbnail is
  fetched again when a publisher swaps an article's image without
  changing its text. Storing an icon or thumbnail now raises the
  subscription's or entry's revision with `subscription.updated` or
  `entry.updated`, and bootstrap carries `icon_url` and
  `thumbnail_url`, so sync clients pick them up.
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

- A feed's malformed icon or thumbnail metadata no longer drops the
  entries it arrives with. The Android media loader shares one HTTP
  client across a list's image loads, refuses oversized bodies before
  caching them, and no longer recomposes every image on each state
  change.
- The Android app remembers its section and open Settings screen across
  rotation and process death, like the open article already did, so the
  reader no longer reopens on Today after a configuration change.
- The web reader scrolls an article in one place. The article frame now
  takes the article's height instead of the pane's, so Space, Page Down
  and the arrow keys move through the whole article, also after a click
  inside it.
- Titles that a feed escaped twice, such as The Verge's
  "Pomera&#8217;s", now show the character instead of the reference.
  Earnest's identity for items without a GUID or link is unchanged, so
  no article is stored twice.
- A feed that lists two items under one identity, such as a WordPress
  comments feed, no longer stops `earnest serve`: comments on one post
  keep distinct identities, a repeated item is stored once, and data the
  server refuses is recorded as that feed's failure.

### Changed

- The web, Android and Mac clients take their colours from the app icon:
  each place has its own tone (orange Today, blue Edition, amber Later,
  plum Kept, green Done), unread dots are orange, selection is a warm
  cream, feed marks use the icon's colours, and the sign-in screens carry
  a ribbon of them. Android no longer shows Material's stock lavender.
  Article text stays neutral and e-ink stays black and white. See
  `media-sources/README.md`.
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

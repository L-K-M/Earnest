# Web reader

Set `EARNEST_BASE_URL` to the app origin and `EARNEST_CONTENT_ORIGIN` to the
separate origin the browser uses for article frames. The latter must include
its externally visible port when applicable. Route both origins to the same
Earnest process, with the app going to `EARNEST_HTTP_ADDR` (default `:8080`)
and content to `EARNEST_CONTENT_ADDR` (default `:8081`). Compose publishes
`EARNEST_PORT` and `EARNEST_CONTENT_PORT` (defaults 8080 and 8081). Configure
TLS at the reverse proxy when using HTTPS.

For development, start Earnest and run `pnpm install` and `pnpm dev` in
`web/`. Vite proxies API and web content URL requests to the Go server. Set
`EARNEST_BASE_URL` to the Vite origin (normally `http://localhost:5173`) so
unsafe requests pass the server origin check, and set `EARNEST_CONTENT_ORIGIN`
to the browser-accessible content origin. For a
production bundle run `make web-build` before `make build`; the Docker build
also builds and embeds the client. A plain Go build uses the checked-in
placeholder until the web bundle has been copied into `internal/web/dist`.

## Organize feeds

Drag a feed onto a sidebar folder to move it there. Drag a folder onto another
folder to nest it, keeping its feeds and subfolders together. Drop on the Feeds
heading or the top-level drop area to move an item out of its folder. Collapsed
folders accept drops and expand after a successful move. Invalid destinations,
including a folder's own descendants, do not accept drops. Failed saves leave
the original placement intact and show an error in the sidebar.

Folder views, unread counts and folder actions continue to apply to the feeds
directly in that folder. Nesting organizes the sidebar; it does not change feed
membership. Deleting a folder keeps its subfolders at the top level and leaves
its direct feeds unfiled. Folder nesting is preserved in sync and OPML exports.

## Reader workflow

1. Sign in. The browser shares the session's CSRF token between tabs and restores
   a valid session when you return.
2. On first run, import OPML or start empty, then choose an ending river or unread
   counts. The import report lists individual failures. Earnest checks feed health
   for firehoses and offers one action to put them in This week. Deferred fetches
   can finish later; the Feeds screen also shows their volume and cadence.
3. Open Today for Now and Today feeds, Edition for This week grouped by feed, or
   Later, Kept, Done, a feed, folder or label. Every view has a URL (see
   [Addresses](#addresses)), so refresh, Back and Forward and bookmarks keep
   your place. Lists use ingest sequence, with
   explicit earlier/later pagination and an independent resume position per view.
   A list starts from the position saved when it opened and keeps that start
   while it is open, so reading the position again (a tab refocus, the offline
   worker's first install) never blanks the list or moves it.
   Edition groups by subscription ID, then sequence, for deterministic order.
   Each row shows the feed's icon, or its letter mark when the site has none,
   and a square thumbnail when the article has an image. Earnest downloads
   and shrinks both when the feed and the article arrive and serves its own
   copies, so showing a list never contacts a publisher.
4. Open an article to read its publisher title, source, date and original link,
   followed by the existing sandboxed content iframe, sized to the article so
   the pane scrolls it in one place. Space, Page Down and the arrow keys move
   through the whole article, and after a click inside it the frame passes
   those keys and the single-key shortcuts back to the reader. The pane takes the article's theme.
   Settings control palette, font and a 62–72 character measure; the reader's
   Typography popover saves a per-feed override keyed by feed ID.
5. Use Done (`e`), Later (`s`), Keep (`f`) or unread (`m`). Unread clears Done.
   Each of these applies at once and offers Undo (`z`) in a short notice that
   restores the registers it changed. `j` and `k` select, and while an article
   is open they open the next or previous one in place, so Back still returns
   to the list. Arrow keys, Home and End move through the list whether or not
   single-key shortcuts are on. `o` or Enter opens, Escape or `u` closes, `v`
   opens the original, `g` then `t`, `e`, `l`, `k` or `d` goes to Today,
   Edition, Later, Kept or Done (`f`, `s`, `a`, `n` and `r` reach the other
   screens), `/` searches, `?` shows help, and Ctrl+K or Command+K opens the
   command palette. Single-key commands ignore text fields, editable regions
   and dialogs, and respect the account off switch. Rows are links, so an
   article opens in a new tab with the usual modifier click.
   In Today, a feed or a folder, the palette's "Mark the rest of ... read"
   marks everything up to the newest loaded article after a confirmation;
   articles that arrive later stay unread. There is no toolbar button for it.
   Right-click, long-press or press Shift+F10 (or the Menu key) for context
   menus. Today, a folder or a feed in the sidebar offers "Mark all as read",
   which marks through the newest article this browser knows of after a
   confirmation, and a feed adds Refresh feed and Feed settings. An article
   in a list offers its reading actions, Open in new tab, "Mark read up to
   here" (in Today, a feed or a folder) and Copy link to the original. Hold
   Shift while right-clicking for the browser's own menu. The Earnest logo
   goes to Today.
6. Read the written session end. Its numbers describe local reading choices;
   sync status separately reports pending actions and rejected/expired intent.
   Refresh articles explicitly to rebuild the current list. Marking an article
   leaves its row in place, avoiding a moving target during a reading session.
7. In Manage feeds, subscribe by URL, set credentials and custom headers, choose
   cadence/full-text behavior, manage folders, unsubscribe, refresh a feed, import
   or export OPML, import starred articles from another reader's Google Reader
   JSON export into Kept, and mute keywords/authors. Feed health explains failure
   classes and shows the service's last attempt/success and next scheduled fetch.

Importing starred articles keeps the articles Earnest already has and stores the
rest from the export, adding the export's labels to each article. It never rewrites an article, leaves out articles you
removed from Kept, never brings back a label you deleted or removed from an
article, and keeps imported articles out of Today; an unread one the
feed still carries arrives with the feed's next fetch. Articles from feeds you
don't follow are skipped unless you choose to subscribe to those feeds.

Search separates publisher matches from labelled agent notes and supports
full-text search without models. When embeddings are enabled, Hybrid search and
More like this show semantic results. Saved views can be opened, renamed or
deleted from the search panel without changing their query or filters.

In Reading settings, **Show only unread** filters Today, Edition, feeds,
folders and labels independently of unread-count display. An article is unread
when it is unseen and not Done. Articles admitted to a list stay there as you
read them, move between articles, close the reader, load another page, or sync
changes from another device. Reopening the parent list or successfully choosing
Refresh articles reapplies the filter. Background refetches and tab refocus do
not end the visit. Later, Kept, Done and search remain unfiltered.

Only unread-count mode requests and displays unread totals. Per-feed counts come
from one grouped server query (`/v1/entries:counts`), so totals stay exact
without paging unread entries through the entry list. The list's separate count
says explicitly that it covers loaded articles. Today excludes This week on the client because the
existing river API includes all three cadences.

## Addresses

| Path | Screen |
|---|---|
| `/`, `/today`, `/edition`, `/later`, `/kept`, `/done` | A reading list |
| `/feeds/:id`, `/folders/:id`, `/labels/:id` | A subscription, folder or label (IDs as in `/v1`) |
| `<list>/articles/:entry` | The list with that article open |
| `/search?q=&mode=hybrid&feed=&folder=&limit=`, `/search/saved/:id` | Search and saved searches |
| `/ask`, `/rules`, `/rules/new`, `/rules/:id` | Ask my feeds and the rule editor |
| `/activity?agent=&action=&since=`, `/notifications/:id` | Activity filters and one notification |
| `/feeds`, `/settings`, `/settings#push`, `/device?user_code=` | Feed management, settings sections, device approval |

The server answers every other path with the app, which shows a plain
not-found page for addresses it does not know. The offline worker serves the
cached app for these paths when the network is down, and an article link opened
offline falls back to the cached list. Signing out returns to `/`, so the next
account does not start on the previous account's article. The rule editor and
reading settings ask before leaving with unsaved changes; the question drafts
for Ask my feeds and the subscribe form are kept for the session.

Below 1100 px wide the sidebar becomes a drawer behind a top bar; below 760 px
the list and the reader share one pane. Rarely used screens (feed management,
settings, rules, onboarding) load on demand, and fingerprinted assets are
served precompressed when the browser accepts Brotli or gzip.

## Native labels

In Settings, create, rename or delete native label definitions. Deleting a
label removes its memberships, not the articles themselves. The sidebar's label
views and the rule editor's label choices refresh after a successful change.

## Settings contract

`GET /v1/settings` returns the validated reader document and its last change
revision. `PUT /v1/settings` replaces the reader fields, requires `op_id`, keeps
unrelated `users.settings` keys, and emits one transactional `settings.updated`.
Only human device/session actors can access settings. PUT is deduplicated per
principal. Returned `rev` is read-only and is omitted from mutation bodies.

Defaults are river, two Today days (matching the existing river service),
Show only unread disabled, single-key shortcuts enabled, system palette,
66 characters and serif. `show_only_unread` is an account-wide boolean; a PUT
that omits it preserves the saved choice so older clients cannot clear it. Palettes
are system/light/dark/black/eink; fonts are serif/sans/dyslexic. Today decay accepts
1–365 days. `feed_overrides` maps feed IDs to optional theme, measure and font.
An empty map clears the overrides. The settings service requires no migration.

## LLM service settings

Settings has a server-wide LLM services section at `/settings#llm`. It edits one
draft of the provider, task and model configuration; Save replaces the whole
document with `PUT /v1/settings/llm`, and Reload discards the draft and fetches
the current one. The section states that the configuration is shared by every
account and that keys stay on the server, and it asks before leaving with
unsaved changes.

Providers add, edit and remove with a kind, base URL, monthly limit, timeout and
token field. The API key field is write-only and masked: it starts blank, a
blank value keeps the saved key, a typed value replaces it, and a separate clear
control removes it. Changing the base URL or kind of a keyed provider needs a
replacement key or an explicit clear, so a saved secret is never forwarded to a
different destination. Local servers note that private addresses need
`EARNEST_OUTBOUND_ALLOW_PRIVATE` allowlisting.

All seven tasks (embed, classify, blurb, summary, edition, compile_rule, ask)
are always listed. A task is off until it has both a provider and a model;
clearing the provider clears the model. Anthropic has no embeddings API, so the
embed task rejects it. The advanced model pricing editor adds and removes named
models with their input, output and embedding rates per million tokens, plus
embedding dimensions and the truncatable flag. Prices are decimal strings, never
floats, and existing entries survive edits.

The revision comes from the GET and goes back on every save with a fresh
`op_id`; a retry of an identical failed request reuses the `op_id`. A 409 keeps
the draft, says a reload is required and never overwrites what changed. Typed
keys live in component state only and are cleared after a save, a reload, or a
sign out (which remounts the session).

## Sync and browser storage

`src/sync/client.ts` is the pure protocol client; `runtime.ts` drives bootstrap,
changes, SSE hints and flushes. Bootstrap captures its cursor before paging and
merges higher object/register revisions. Change pages validate contiguous
revisions before any mutation. Cache, cursor and outbox share one IndexedDB
snapshot transaction. A 410 replaces the server cache while retaining pending
intent; a generation change rebases reversible intent to revision zero and
preserves its original clock. Acknowledgements carry the request generation.

Each register coalesces independently, preserving its oldest observed base.
Submitted payloads remain frozen for deduplicated retry, while newer intent gets
a separate operation ID. Marking Done unread queues a separate `/clear-done`
shelf operation that survives subsequent seen coalescing. Range operations keep
their scope and fixed boundary, in order with ordinary writes. Expired and gone
operations are reported rather than silently dropped. State shown to the reader
is server state plus the pending overlay.

Tabs for one account use the same storage key, a Web Lock around each persisted
transition, BroadcastChannel invalidation, and a single elected polling leader.
SSE only triggers a changes read; it never supplies authoritative state. The web
UI requires IndexedDB and Web Locks for durable actions, with an actionable error
when either is unavailable. Use HTTPS or localhost because Web Locks require a
secure context. Article bodies remain on-demand; this task adds a durable action
queue, not a service-worker article cache.

The sync model and wire codec use bigint for exact 64-bit IDs and revisions.
The existing reader-facing generated types still use JavaScript numbers; that
layer rejects integers outside the safe range before addressing an article.

Run `pnpm typecheck`, `pnpm lint`, `pnpm test` and `pnpm build` from `web/`.
The vector suite reads the repository JSON fixtures unchanged, uses a small
in-memory server for server steps, and checks the manifest and step language so
new fixtures/operations cannot disappear from the drift gate. Local tests cover
stale request-generation acknowledgements and immutable retries as well.

The isolated article frame keeps its own keyboard focus. App shortcuts resume
when focus returns to the reader toolbar or article list; the frame does not run
a keyboard-forwarding script or gain script permissions.

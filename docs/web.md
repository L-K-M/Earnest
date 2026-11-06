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

## Reader workflow

1. Sign in. The browser shares the session's CSRF token between tabs and restores
   a valid session when you return.
2. On first run, import OPML or start empty, then choose an ending river or unread
   counts. The import report lists individual failures. Earnest checks feed health
   for firehoses and offers one action to put them in This week. Deferred fetches
   can finish later; the Feeds screen also shows their volume and cadence.
3. Open Today for Now and Today feeds, Edition for This week grouped by feed, or
   Later, Kept, Done, a feed, folder or label. Lists use ingest sequence, with
   explicit earlier/later pagination and an independent resume position per view.
   Edition groups by subscription ID, then sequence, for deterministic order.
4. Open an article to read its publisher title, source, date and original link,
   followed by the existing sandboxed content iframe. Settings control palette,
   font and a 62–72 character measure; feed overrides use feed IDs.
5. Use Done (`e`), Later (`s`), Keep (`f`) or unread (`m`). Unread clears Done.
   `j` and `k` select, `o` or Enter opens, `v` opens the original, `?` shows help,
   and Ctrl+K or Command+K opens view navigation. Single-key commands ignore text
   fields, editable regions and dialogs, and respect the account off switch.
6. Read the written session end. Its numbers describe local reading choices;
   sync status separately reports pending actions and rejected/expired intent.
   Refresh articles explicitly to rebuild the current list. Marking an article
   leaves its row in place, avoiding a moving target during a reading session.
7. In Manage feeds, subscribe by URL, set credentials and custom headers, choose
   cadence/full-text behavior, manage folders, unsubscribe, refresh a feed, import
   or export OPML, and mute keywords/authors. Feed health explains failure classes
   and shows the service's last attempt/success and next scheduled fetch.

Only unread-count mode requests and displays unread totals. Per-feed counts come
from one grouped server query (`/v1/unread-counts`), so totals stay exact
without paging unread entries through the entry list. The list's separate count
says explicitly that it covers loaded articles. Today excludes This week on the
client because the existing river API includes all three cadences.

## Settings contract

`GET /v1/settings` returns the validated reader document and its last change
revision. `PUT /v1/settings` replaces the reader fields, requires `op_id`, keeps
unrelated `users.settings` keys, and emits one transactional `settings.updated`.
Only human device/session actors can access settings. PUT is deduplicated per
principal. Returned `rev` is read-only and is omitted from mutation bodies.

Defaults are river, two Today days (matching the existing river service),
single-key shortcuts enabled, system palette, 66 characters and serif. Palettes
are system/light/dark/black/eink; fonts are serif/sans/dyslexic. Today decay accepts
1–365 days. `feed_overrides` maps feed IDs to optional theme, measure and font.
An empty map clears the overrides. The settings service requires no migration.

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
new fixtures/operations cannot disappear from the drift gate. This checkout has
vectors 01–35; the task's referenced 36/37 are absent. Local tests cover stale
request-generation acknowledgements and immutable retries as well.

## Integration boundary for this task

The assigned paths exclude `internal/api/api.go` and `internal/web/content.go`.
`web/settings-registration.patch` contains the required one-line route mount for
review by the router owner. Until that patch is applied, the production router
returns 404 for settings; the settings handler tests mount those routes directly.
The checked-in OpenAPI/types preview is generated with that proposed mount via
a Go source overlay. After applying it, ordinary `go run ./cmd/earnest openapi`
and `pnpm generate` reproduce the artifacts without an overlay.

The existing content server understands serif/sans but not the stored dyslexic
choice; that choice currently renders as its serif fallback. Adding the
allowlisted face/spacing requires the content-server owner or an approved scope
extension. `web/content-typography.patch` contains the small allowlisted font
and spacing change for review. The web application cannot style the isolated
iframe's document.

The module-level exported types/functions and created-file manifest are listed
in [web/API.md](../web/API.md). Runtime integration methods are documented in
[the sync README](../web/src/sync/README.md).

The isolated article frame keeps its own keyboard focus. App shortcuts resume
when focus returns to the reader toolbar or article list; the frame does not run
a keyboard-forwarding script or gain script permissions.

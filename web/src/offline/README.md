Offline reading uses the module service worker `/offline-worker.js`, scoped to
`/`. Vite emits it at the root so it needs no Service-Worker-Allowed header.
It preloads the production shell and caches successful same-origin reading
requests. Each visited entry page warms stored `/v1/entries/{id}/content` data,
never publisher URLs. Reading content is bounded to 200 entries; other reading
page and position responses to 100; individual responses to 2 MiB. Session and
navigation metadata have a separate bounded group, so page eviction cannot
remove the cached identity. Oldest saved copies leave first.
The offline reader renders the cached plain text. It cannot reuse the separate
content origin's expiring capabilities and does not render publisher HTML in the
app origin. Images and rich formatting require connectivity.

Reading actions continue through ReaderSync's existing IndexedDB outbox. The
shell kicks sync on `online`; the runtime also retries with backoff. API writes
and change feeds never fall back to a cached success. Login, logout, expired
sessions and identity changes clear private response caches. Browser storage can
still be evicted by the browser, so an offline copy is not a backup.

Push registration scheme: keep push in a separate registration with explicit
scope `/push/` (for example, register `/push-worker.js` with `{scope:'/push/'}`).
Subscribe using that returned registration, never `serviceWorker.ready`, which
refers to the root offline registration. Neither registration replaces the other.
There is currently no push worker in this checkout. If a deployment already has
a root push registration, offline registration refuses to overwrite it. Migrate
that registration to `/push/`, or combine its handlers into the root worker
before enabling offline support. Notification clicks may open the app at `/`.

For a browser check: build and serve the production assets over HTTPS or
localhost; sign in; wait for the root worker to control the page; visit Today and
a feed; wait for content requests to finish; disconnect and reload. The visited
river and its text copies should open. Mark Later or Done, reload offline, then
reconnect and verify the existing outbox drains once. Sign out online and verify
the `earnest-reading-v1` cache has gone. Development Vite does not emit the worker.

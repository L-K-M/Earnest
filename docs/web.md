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

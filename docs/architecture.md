# Architecture

How Earnest is built. The plan and its reasons are in [research/](research/README.md); [decisions.md](research/decisions.md) overrides the research. Where this file and the research differ, this file describes what the code does.

## Shape

```
 Web (React, embedded) ─┐
 Mac, Android ──────────┤ /v1 REST + SSE ─┐
 Agents (REST, CLI) ────┤                 │
 Agents (MCP) ──────────┼─ /mcp ──────────┤
 Capy, NetNewsWire ─────┴─ /api/greader ──┤
                                          ▼
                     internal/service  (every rule, scope and budget)
                      │        │          │            │
               internal/store  │   internal/outbound   internal/push
                (Postgres)     │    (all egress)        (ntfy, ...)
                               ▼
            internal/fetcher ─► internal/feed ─► internal/sanitize
              (schedule, ingest)  (parse, identity)  (HTML subset)
```

Each layer talks only to the layer below it. HTTP handlers, the Google Reader adapter, MCP and the CLI call `internal/service`; none touches `internal/store` or `internal/outbound` directly, and none implements its own rules. Scopes, budgets and the conflict policy live in the service layer, so a new surface can't bypass them.

## Packages

| Package | Owns |
| --- | --- |
| `cmd/earnest` | Binary entry point and subcommands: `serve`, `healthcheck`, `migrate`, `user`, `token`, plus the API client CLI |
| `internal/config` | Environment configuration, validated at start |
| `internal/syncmodel` | Pure state model: registers, conflict policy, clocks, range records, bootstrap merge, generations. No I/O. The service calls it, so the server and the model can't disagree |
| `internal/store` | Postgres access, embedded migrations, counters and locking. Only `internal/service` imports it |
| `internal/service` | Domain operations, authorisation by principal and scope, change log, activity log |
| `internal/outbound` | The one HTTP client for egress: SSRF checks at dial time, redirects, per-host limits, credential scoping |
| `internal/feed` | RSS, Atom and JSON Feed parsing into Earnest's types, entry identity, URL cleaning, autodiscovery, OPML |
| `internal/sanitize` | The stored HTML subset, versioned |
| `internal/extract` | Full-text extraction from fetched pages |
| `internal/fetcher` | Scheduling, polite fetching, ingest, feed health |
| `internal/api` | `/v1` handlers, OpenAPI, SSE |
| `internal/compat/greader` | Google Reader subset |
| `internal/mcp` | MCP server, stateless Streamable HTTP |
| `internal/push` | Notification transports behind one interface |
| `internal/llm` | Provider layer and the internal pipeline |
| `internal/web` | Serves the embedded web client and the sandboxed content origin |
| `web/` | React client |
| `clients/mac`, `clients/android` | Native clients |
| `testdata/vectors` | Language-neutral sync test vectors, run by the server and every client |

## Conventions

- **IDs** are 64-bit integers, serialised as JSON numbers. Entry IDs map to Google Reader item IDs without a lookup table.
- **Ordering** uses `entries.seq`, assigned at ingest. Publisher dates are display only.
- **Time** is UTC `timestamptz` in the database and RFC 3339 in JSON.
- **Errors** over HTTP are `application/problem+json` (RFC 9457).
- **Mutations** take a client-generated `op_id`, deduplicated per principal.
- **Agent text** is plain text everywhere.
- **Logs** use `log/slog`. Tokens, passwords and feed credentials never reach a log.
- **Tests** that need Postgres read `EARNEST_TEST_DATABASE_URL` and skip without it. CI provides one.

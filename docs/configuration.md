# Configuration

Copy `.env.example` to `.env` for Compose. Compose reads it for interpolation and passes its entries to the Earnest container; entries under `environment` in Compose take precedence. The binary also accepts these settings directly in its environment. Empty values normally select the default. Protect `.env`: it can contain passwords and API keys. Restart the service after changes.

`<NAME>` is a provider name made of letters, digits and single dashes, case insensitive. `<TASK>` is `EMBED`, `CLASSIFY`, `BLURB`, `SUMMARY`, `EDITION`, `COMPILE_RULE` or `ASK`. See [LLM providers](llm-providers.md) for model fields and limitations.

## Core

| Name | Default | What it does | When you need it |
| --- | --- | --- | --- |
| `EARNEST_DB_PASSWORD` | `earnest` in Compose | Postgres password for the stack and backup service. | Set a random password before deployment; the fallback is for development. Changing it later does not change an initialized Postgres password. |
| `EARNEST_SECRET_KEY` | Required | Raw key material of at least 32 bytes for signing and encryption. | Always; keep it with backups. |
| `EARNEST_DATABASE_URL` | Required by binary; Compose builds a DSN to its Postgres | Postgres connection string. | Direct binary use or an external database. Compose backups still target its own Postgres. |
| `EARNEST_LOG_LEVEL` | `info` | slog threshold, such as `debug`, `info`, `warn`, or `error`. | Change log verbosity. |
| `EARNEST_DB_VOLUME` | `pgdata` | Compose database volume or host path. | Store database files at a specific host path. |
| `EARNEST_BACKUP_VOLUME` | `backups` | Compose backup volume or host path. | Store dumps at a specific host path. |
| `EARNEST_BACKUP_KEEP` | `14` | Number of successful daily dumps to retain. | Change backup retention; positive integer. |
| `EARNEST_URL` | `http://127.0.0.1:8080` | CLI server URL; overrides CLI config. | Run the CLI against another server. |
| `EARNEST_TOKEN` | Unset | CLI app or agent token; overrides CLI config. | Authenticate CLI without device login. |

## Network and origins

| Name | Default | What it does | When you need it |
| --- | --- | --- | --- |
| `EARNEST_HTTP_ADDR` | `:8080` | Binary's main listener; Compose fixes it at `:8080`. | Change the direct binary listener. |
| `EARNEST_CONTENT_ADDR` | `:8081` | Binary's content listener; Compose fixes it at `:8081`. | Change the direct binary listener. |
| `EARNEST_PORT` | `8080` | Compose host port for the main listener. | Change the published main port. |
| `EARNEST_CONTENT_PORT` | `8081` | Compose host port for content. | Change the published content port. |
| `EARNEST_DB_BIND` | `127.0.0.1` | Compose host bind address for Postgres. | Change database exposure deliberately. |
| `EARNEST_DB_PORT` | `5432` | Compose host port for Postgres. | Resolve a host port conflict. |
| `EARNEST_BASE_URL` | `http://localhost:8080` | Canonical app origin, with no path. | Set the browser-facing HTTPS origin. |
| `EARNEST_CONTENT_ORIGIN` | `http://localhost:8081` | Separate origin for sandboxed content. | Set the browser-facing content HTTPS origin; it must differ from the app origin. |
| `EARNEST_OUTBOUND_ALLOW_PRIVATE` | Empty | Comma-separated exact `host:port` private destinations allowed for outbound requests. | Reach a private model server, ntfy or other approved destination. |

## Fetching and retention

| Name | Default | What it does | When you need it |
| --- | --- | --- | --- |
| `EARNEST_FETCH_MIN_INTERVAL` | `15m` | Minimum feed fetch interval. | Tune scheduling; positive Go duration, no greater than maximum. |
| `EARNEST_FETCH_MAX_INTERVAL` | `24h` | Maximum feed fetch interval. | Tune scheduling; positive Go duration. |
| `EARNEST_RETENTION_DAYS` | `90` | Entry and change-log retention in days. | Change retention; positive integer. |

## Push

ntfy server, topic and token are per-user settings, not environment variables. Web Push and UnifiedPush share VAPID keys. Provide both explicit keys or a key file, never both. With neither, Earnest generates a file in its user config directory; persist it across container replacements. APNs requires all three key settings.

| Name | Default | What it does | When you need it |
| --- | --- | --- | --- |
| `EARNEST_PUSH_VAPID_PRIVATE_KEY` | Unset | PEM PKCS#8 P-256 private key. | Supply your own VAPID pair. |
| `EARNEST_PUSH_VAPID_PUBLIC_KEY` | Unset | Base64url public point. | Supply your own VAPID pair. |
| `EARNEST_PUSH_VAPID_SUBJECT` | Empty | Contact URI for push providers. | Supply a `mailto:` or `https:` contact. |
| `EARNEST_PUSH_VAPID_KEY_FILE` | `earnest/vapid.json` under OS user config directory | Path for generated or existing VAPID pair. | Persist keys on a mounted directory. |
| `EARNEST_APNS_PRIVATE_KEY` | Unset | PEM PKCS#8 signing key contents. | Enable APNs with the key and team IDs. |
| `EARNEST_APNS_KEY_ID` | Unset | APNs signing key ID. | Enable APNs. |
| `EARNEST_APNS_TEAM_ID` | Unset | Apple team ID. | Enable APNs. |
| `EARNEST_APNS_SANDBOX` | `false` | Select APNs sandbox. | Test APNs with sandbox credentials. |

## LLM providers and tasks

All tasks are disabled until both task settings are supplied. A provider requires its kind and base URL; its key can be empty for local servers. A zero or unset limit means no cap. Configure model prices before enabling an unknown model.

| Name | Default | What it does | When you need it |
| --- | --- | --- | --- |
| `EARNEST_LLM_PROVIDER_<NAME>_KIND` | Unset | Provider dialect: `openai`, `anthropic` or `gemini`. | Configure a provider. |
| `EARNEST_LLM_PROVIDER_<NAME>_BASE_URL` | Unset | Provider API root. | Configure a provider. |
| `EARNEST_LLM_PROVIDER_<NAME>_API_KEY` | Empty | Provider credential. | Provider requires authentication. |
| `EARNEST_LLM_PROVIDER_<NAME>_LIMIT_USD` | `0` (unlimited) | Monthly provider spend threshold in USD. | Bound spending; concurrent calls can overshoot. |
| `EARNEST_LLM_PROVIDER_<NAME>_TIMEOUT` | `60s` | Request timeout as a Go duration. | Override the provider timeout. |
| `EARNEST_LLM_PROVIDER_<NAME>_TOKEN_FIELD` | `max_completion_tokens` | OpenAI completion token limit field. | Set `max_tokens` for compatible servers requiring it. |
| `EARNEST_LLM_TASK_<TASK>_PROVIDER` | Unset | Provider selected for the task. | Enable a task with its model. |
| `EARNEST_LLM_TASK_<TASK>_MODEL` | Unset | Model selected for the task. | Enable a task with its provider. |
| `EARNEST_LLM_MODELS` | Built-in model table | JSON overlay of model prices and embedding geometry. | Add a model or update its pricing. |

## Tests

| Name | Default | What it does | When you need it |
| --- | --- | --- | --- |
| `EARNEST_TEST_DATABASE_URL` | Unset | Postgres DSN for integration tests. | Set it to run database tests; otherwise they skip. |

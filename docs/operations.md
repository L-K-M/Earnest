# Operations

## NAS deployment and HTTPS

Follow the [quick start](../README.md), keep `.env` private, and retain both secrets off the NAS with your backups. Compose publishes app and content ports on all host interfaces by default. Restrict access to those ports with your NAS firewall, including on LAN interfaces; Tailscale Serve alone does not hide the published ports. Postgres binds to loopback by default.

Install Tailscale on the NAS, connect it to your tailnet, enable HTTPS certificates and use its MagicDNS name. For example, with the default Compose ports:

```sh
tailscale serve --bg --https=443 8080
tailscale serve --bg --https=8443 8081
tailscale serve status
```

Set `EARNEST_BASE_URL=https://<nas>.<tailnet>.ts.net` and `EARNEST_CONTENT_ORIGIN=https://<nas>.<tailnet>.ts.net:8443` in `.env`, substituting the actual name shown by Tailscale, then run `docker compose up -d`. Both URLs must be reachable from your browser. A different port makes a different browser origin: article HTML and images run on the content origin so untrusted publisher content is isolated from the app. Do not proxy both listeners onto the same origin. See [Tailscale Serve](https://tailscale.com/docs/reference/tailscale-cli/serve) and [web](web.md).

Login rate limits key on the client address. Under Compose, connections from `tailscale serve` reach the container from the Docker network gateway rather than loopback, so every browser would share one rate-limit bucket and a few failed logins would lock everyone out. Set `EARNEST_TRUSTED_PROXIES` to that gateway, which `docker network inspect earnest_default --format '{{(index .IPAM.Config 0).Gateway}}'` prints (adjust the network name if your Compose project is not called `earnest`). Earnest then reads the client from the `X-Forwarded-For` header the proxy appends; connections from any other address keep their own socket address and their forwarding headers are ignored.

Password logins, including Google Reader ClientLogin, share a limit of five
failed or currently running checks per username and per client. Failure
counters expire 15 minutes after the first recorded failure. Successful
logins clear account failures, not the client's password-spray counter;
completed successful checks do not consume the allowance. In-flight checks
keep their slots until completion, even across a window boundary. Counters
are process-local and reset on restart.

Web Push and UnifiedPush need stable VAPID keys. Compose does not mount the generated key file persistently by default, so supply a persistent mounted key file through a Compose override, or provide the explicit key pair in `.env`; back it up securely. See [configuration](configuration.md).

## Users and clients

Create a user on the NAS and enter a password at the prompt:

```sh
docker compose exec earnest earnest user create <name>
```

Sign in to the web app and import OPML in onboarding. To bring your starred articles along, export them from your old reader as Google Reader JSON (FreshRSS offers this for favourites under Import / export) and import the file in Manage feeds, or run `earnest entries import starred.json`. Imported articles land in Kept. To connect a Google Reader client, generate its separate compatibility password:

```sh
docker compose exec earnest earnest compat-password --user <name>
```

Store the printed password securely. In NetNewsWire or Capy Reader use the HTTPS app URL with `/api/greader`, your username and that password. See [Google Reader](greader.md).

Create a scoped agent token for MCP, recording the secret shown once:

```sh
docker compose exec earnest earnest token create --user <name> --kind agent --name "Reader agent" --scope entries:read,annotations:write --audience mcp --budget 3
```

Configure your MCP client with the HTTPS app URL plus `/mcp` and the token as a bearer credential. For the CLI use an `api` audience token instead, or run `earnest login` and approve the device flow in the web app. Set `EARNEST_URL` to your HTTPS app origin on the CLI host. Use `earnest whoami` to check identity; token commands, scopes, revocation and client examples are in [agents](agents.md) and [CLI](cli.md). Agent tokens can read all subscriptions, including private feeds.

## Backups and restore

Compose's backup service dumps its Postgres database on startup and every 24 hours, retaining 14 successful custom-format dumps by default. `EARNEST_BACKUP_KEEP` and `EARNEST_BACKUP_VOLUME` change retention and location. If `EARNEST_DATABASE_URL` targets an external database, arrange separate backups and restore for it: these scripts target Compose Postgres. Copy dumps off host and protect them along with the original `EARNEST_SECRET_KEY` and any VAPID keys.

Test recovery periodically on a separate disposable stack and database, never by overwriting production. Stop all other applications and notification workers using the target database, copy in the original secret and a dump, then from that stack's checkout run:

```sh
scripts/restore.sh /path/to/earnest-backup.dump
docker compose up -d earnest
```

The script stops Earnest, restores with `pg_restore` and rotates the generation before the app can restart. If restore or rotation fails, leave the app stopped and investigate. Verify users and entries, and that clients bootstrap again under the new generation. In the web app's Notifications screen, select **Release current notifications** after checking the restore. Delivery is withheld until that action; pending notifications restored from the old generation remain withheld permanently. Reissue any wanted message as a new operation. See [sync](sync.md#restore-and-notification-delivery). This procedure must be tested in your own environment before relying on the backups.

## Updates, metrics and logs

Run `./update.sh` in the deployment checkout, optionally followed by a branch name. It syncs the branch, pulls external images, rebuilds the local image with fresh base layers and recreates the stack. See [CICD.md](../CICD.md). Check `docker compose ps` and `docker compose logs --tail=100 earnest` after an update; `docker compose logs backup` shows backup failures. The main listener exposes Prometheus metrics at `/metrics` and liveness at `/healthz`; restrict access with the rest of the service.

## Outbound access and models

Outbound requests to private addresses are denied unless the exact destination is listed in `EARNEST_OUTBOUND_ALLOW_PRIVATE`, for example `ollama:11434,ntfy:80`. Use names and ports reachable from the Earnest container; do not include schemes or paths. Configure ntfy's server, topic and optional token in your user push settings. Only allow destinations you trust. Public endpoints still pass the outbound address and redirect checks.

The media worker also fetches through the outbound client. For each subscribed feed it reads the site's home page and icon about once a month, or weekly while the site offers none. For each new article with an image, it downloads up to three candidate images. It stores a 64-pixel icon per feed and a 192-pixel square thumbnail per article in Postgres, a few kilobytes each. Backups include them. Retention deletes an article's thumbnail with its body; a feed's icon stays with the feed. After an upgrade it also covers articles from the previous seven days.

The model pipeline is disabled by default. To enable it:

1. Choose a provider and model, and understand what feed content will be sent. Credentialed feeds require explicit human opt-in even for local providers.
2. In `.env`, set `EARNEST_LLM_PROVIDER_<NAME>_KIND` and `_BASE_URL`, plus `_API_KEY` if needed. Add a private server to the outbound allowlist. Set a positive `_LIMIT_USD`; zero means unlimited.
3. Check the model's price and embedding dimensions in the built-in table or add an `EARNEST_LLM_MODELS` JSON overlay. Configure `EARNEST_LLM_TASK_EMBED_PROVIDER` and `_MODEL`, or the corresponding `CLASSIFY` pair, to start the worker. Add other tasks only as needed.
4. Recreate with `docker compose up -d`, check logs and `GET /v1/pipeline`, and monitor spending. The monthly threshold can be exceeded by concurrent in-flight calls; Gemini embedding usage is not metered. Watch rules have separate daily notification and confirmation budgets.

Full-text search works without models. See [pipeline](pipeline.md) and [LLM providers](llm-providers.md) for routes, prices, task names and privacy controls.

## Feed health

Use the web feed list or `GET /v1/feeds/{id}/health` to inspect health and the last error. The stored classes guide the next check:

| Class | Check |
| --- | --- |
| `dns` | DNS resolution from the container and the feed hostname. |
| `network` | Routing, connectivity and the remote server. |
| `tls` | Certificate validity, hostname and system clock. |
| `timeout` | Remote latency and connectivity; retry after the server recovers. |
| `policy` | URL and private-address rules; explicitly allow only trusted private host and port pairs. |
| `size` | Feed or response exceeds a size limit. |
| `gone` | The feed returned a gone or missing response; check its URL. |
| `rate_limit` | The server rate-limited requests; allow backoff to run. |
| `challenge` | The server refused access, for example with a challenge or authorization failure. |
| `http` | Other HTTP or redirect failure; inspect the source server. |
| `parse` | Invalid or unsupported feed or response encoding; inspect the feed. |

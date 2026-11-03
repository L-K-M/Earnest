# Agents

Earnest treats an agent as a distinct, governed actor. Each agent gets its
own principal, its own read position, its own annotations, a daily
notification budget, and a revert log. This document covers how an agent
token is created, what it may do, and why the limits are shaped this way.

## Creating a token

Tokens are created on the host with the `earnest` binary. The command talks
to the database directly and requires `EARNEST_DATABASE_URL` and
`EARNEST_SECRET_KEY`:

```sh
earnest user create alice
earnest token create --user alice --kind agent --name "Morning Agent" \
  --scope entries:read,annotations:write,notify:send \
  --audience api --budget 3 --expires 90d
```

- `--kind` is `agent` (default) or `app`. An agent token also creates the
  agent's principal, named by `--name`, so the name is required.
- `--scope` is a comma-separated subset of the agent scopes listed below.
  An agent token can never hold a human-only scope; the request is refused.
- `--audience` is `api` (default) for REST callers or `mcp` for the MCP
  surface.
- `--budget` is the daily notification budget for this token (default 3).
- `--expires` accepts a Go duration such as `720h` or a day count such as
  `30d`. Omit it for no expiry.

The command prints the token secret, prefixed `earnest_agent_`, exactly
once. The service stores only its hash, so a lost secret cannot be
recovered; revoke the token and create a new one:

```sh
earnest token list --user alice
earnest token revoke <id>
```

## What an agent may do

Agent tokens may hold only these scopes:

| Scope | Allows |
|---|---|
| `entries:read` | Read entries |
| `annotations:write` | Add and edit the agent's own annotations |
| `position:own` | Move the agent's own read position |
| `state:write-human` | Change read/done state on your behalf |
| `notify:send` | Send notifications, within budget |
| `notify:urgent` | Bypass quiet hours; still within budget, not granted by default |

Everything else is human-only: `feeds:write`, `webhooks:write`,
`devices:write`, `state:write`, `tokens:write` and `admin`. The check runs
in the service layer, so REST, MCP and CLI callers get the same answer.
Device registration additionally requires a human principal, not merely
the scope: a push endpoint is a destination the server posts to, so it
stays yours only, like a webhook URL.

## Notification budget

Each `notify:send` call is accepted atomically and debits the token's
daily budget. The day boundary is midnight in the timezone configured in
the user's push settings, so the count resets where you sleep, not where
the server is. When the budget is spent the call is refused with a rate
limit error that reports `used_today` and `limit_today`; held and muted
notifications still count, because the agent already spent the attempt.
A `dedupe_key` re-sent within 24 hours replays the earlier notification
instead of spending again.

## Quiet hours and urgent

Push settings carry optional quiet hours as `HH:MM` start and end in an
IANA timezone. Overnight windows work (`22:00` to `07:00`), and the
calculation follows the configured zone including DST. A notification
accepted inside the window is held and delivered when the window ends;
held is not lost, and a held notification can be cancelled. An agent with
`notify:urgent` bypasses quiet hours only, not the budget. Grant it
sparingly.

## Devices and push settings

Humans register push destinations and manage push settings; agents can
do neither. Supported registrations are Web Push and UnifiedPush
subscriptions (endpoint plus the RFC 8291 `p256dh` and auth keys). ntfy is
configured once per user in push settings (server, topic, optional token)
rather than as a per-device registration.

`show_text` is a per-destination opt-in for carrying the agent's own
words in the push payload. The transport decides whether the flag takes
effect: Web Push and UnifiedPush honor it because payloads are encrypted
end to end; ntfy honors it only against a self-hosted server, never the
public ntfy.sh service; APNs never carries agent text. Custom ntfy server
URLs are treated as servers you operate, so configure a custom server only
when you operate it.

## Threat model

Feed content is untrusted input, and the agent reads it on your behalf.
The controls above exist so a hostile feed, or a hostile agent, can do
little with the outbound paths it touches:

- **Agent text stays off the wire by default.** Push payloads are
  server-generated ("Morning Agent: 1 alert") with an ID. The agent's
  note travels only to destinations that opted in and that can protect
  it, per the transport rules above. The app fetches the rest over the
  tailnet on open.
- **No caller-supplied URLs.** Notification bodies never include links,
  and the ntfy transport rejects a body that contains one. A notification
  cannot smuggle a fetch.
- **Destinations are validated.** Device endpoints must be HTTPS URLs
  without credentials or fragments, and an IP literal must be a public
  address. Validation happens at registration; nothing is fetched to
  decide. A DNS name is checked again at dial time by the outbound
  client, which applies the same address rules, a redirect policy and
  host spacing on every send.
- **Secrets are sealed and never returned.** Device keys and the ntfy
  token are stored encrypted with the service AEAD, each under its own
  authenticated purpose so an envelope copied between columns fails to
  open. Read APIs return no key material at all.
- **Budgets bound the blast radius.** The worst a hostile agent can do
  with `notify:send` is spend its daily budget and choose when to send
  within it.

## Reading, annotations and triage

An agent reads every subscription, including private feeds and stored full
text. There is no feed allowlist. Reading an entry returns stored content
and never fetches a URL or triggers extraction.

Agents can write notes, labels, scores and briefs over one or more entries.
The newest brief per agent appears at the top of Today. Summary and blurb
annotations belong to the internal pipeline. Annotation values are plain
text with control characters removed and bounded lengths. Render them as
text, without interpreting HTML or Markdown.

Agent labels have their own namespace and cannot feed rules. Search can
find agent notes and names their author. Notes from another principal are
marked untrusted when returned to an agent. Shared change events mark
annotation text untrusted for every reader. Internal model calls, including
Ask my feeds, must never consume agent annotations.

With state:write-human, an agent can mark your entries seen or Done.
Earnest derives the source from the authenticated actor. Agent writes
cannot masquerade as your deliberate actions. Changes to entries you had
not seen appear in Today and at session end.

Triage returns a random 128-bit revert handle that expires after 24 hours.
The handle is bound to its principal and is not a credential: every use
still checks authentication and scope. Revert succeeds only if the affected
register revisions have not changed. Your later edit blocks a stale revert;
an edit to an unrelated register does not. You can revert any agent action,
and an agent can revert only its own.

Agents cannot create proposals, subscribe or unsubscribe, refresh a feed,
register devices, configure push destinations, manage webhooks, manage
tokens or administer Earnest.

## Delivery and restore

Notification categories are alert, digest and brief. Dedupe keys belong to
the sender's principal, so one agent cannot suppress another's message.
An op_id is stable across retries and is deduplicated per principal.
The server builds the Why block from the sender and budget, separately
from the agent's words.

A muted agent's notifications are recorded and counted but remain held.
You can cancel a held notification. Delivery cannot be undone.

Notification delivery is at most once. The worker commits an attempt before
sending. A definite failure is shown as failed. A lost response is uncertain
and is never retried. A crash between recording the attempt and sending can
lose a notification; accepting that loss prevents duplicate sends.
At startup, recover interrupted attempts before starting delivery workers.

Every notification request includes the database generation. Keep that
generation when retrying. After restoring a backup, rotate the generation
with delivery workers stopped, then explicitly release the current
generation. Restored pending notifications from an older generation stay
withheld forever. Sending their message again requires a new operation in
the current generation.

Push text is a preview of at most 512 characters when opted in. The full
note stays in Earnest. Push metadata contains internal IDs and no URL,
icon, attachment or action URL. ntfy also omits opted-in text containing
a URL.

## MCP

Agents can reach the same service layer over MCP instead of REST. The
transport is stateless Streamable HTTP at `/mcp`, authenticated with a
bearer agent token created with `--audience mcp`. A token minted for the
`api` audience is refused here, and vice versa, so REST and MCP agents
hold separate tokens. OAuth applies only if the server is ever exposed
publicly; on the tailnet the bearer token is the whole setup.

The tool set stays small rather than mirroring every REST endpoint. Each
tool calls the service layer, so scopes, budgets, dedupe and provenance
behave exactly as over REST:

| Tool | Scope | Does |
|---|---|---|
| `get_briefing` | `entries:read` | Entries new since the agent's position in view `briefing`, grouped by cadence. Titles and IDs by default; `include_summaries` adds stored summaries and `limit` bounds the page. Returns `generation`, `through_seq` and `has_more` |
| `list_entries` | `entries:read` | Filtered, compact listing with a bounded page |
| `read_entry` | `entries:read` | One entry's stored content, marked untrusted; never fetches |
| `search` | `entries:read` | Currently returns an unavailable tool error; service-layer entry search is not implemented |
| `list_feeds` | `entries:read` | Subscriptions with cadence and health |
| `advance_position` | `position:own` | Move the agent's own position in a view |
| `annotate` | `annotations:write` | A note, label, score or brief over one or more `entry_ids` |
| `notify` | `notify:send` | Send within budget; takes `generation` as well as `operation_id` |
| `triage` | `state:write-human` | Mark your entries seen or Done; takes `generation`, returns a revert handle |
| `revert` | `state:write-human` | Undo the principal's own triage while the handle is valid |

MCP retries arrive as fresh requests with a new JSON-RPC ID, so every
mutating tool takes a stable `operation_id`, deduplicated per principal
like `op_id` elsewhere. `notify` and `triage` additionally take the
`generation` returned by `get_briefing`, binding the request to a
database generation the same way the REST endpoints do; a generation
left behind by a restore is refused.

Reads return stored content only and mark it untrusted, and nothing on
the surface can fetch a URL, refresh or subscribe. The expected run
(briefing, selective full reads, one `brief` annotation, notifications
for real alerts only, then `advance_position`) is written up for the
agent itself in [`skills/earnest/SKILL.md`](../skills/earnest/SKILL.md),
an [agent skills](https://agentskills.io) file shipped in this
repository. Client configuration for Claude Code, Codex and a generic
Streamable HTTP client lives there too.

### Client configuration

Create a token with `--audience mcp` and the scopes your workflow needs.
Set `EARNEST_MCP_TOKEN` in the client environment to its secret.

Claude Code ([HTTP configuration](https://code.claude.com/docs/en/mcp)):

```sh
claude mcp add --transport http earnest https://<earnest-host>/mcp \
  --header "Authorization: Bearer ${EARNEST_MCP_TOKEN}"
```

Codex `~/.codex/config.toml`
([MCP configuration](https://learn.chatgpt.com/docs/extend/mcp)):

```toml
[mcp_servers.earnest]
url = "https://<earnest-host>/mcp"
bearer_token_env_var = "EARNEST_MCP_TOKEN"
```

A generic Streamable HTTP client can use this configuration shape; check
its documentation for environment-variable substitution:

```json
{
  "mcpServers": {
    "earnest": {
      "type": "http",
      "url": "https://<earnest-host>/mcp",
      "headers": {"Authorization": "Bearer <mcp-agent-token>"}
    }
  }
}
```

Use a client supporting revision `2026-07-28`: `server/discover` replaces
the session handshake, and requests carry protocol metadata plus
`Mcp-Method` and, for tool calls, `Mcp-Name`. The SDK supplies these.
List and resource-read results use private cache scope with zero TTL;
tool-call results have no cache-scope field in SDK v1.8.0.

### External-agent safety

Give scheduled agents Earnest as their only tool. Agents can read every
subscription, including private feeds, so a hostile article may try to
steer a session with email, shell or browser tools into leaking private
data. Untrusted markers help the agent recognise data but do not prevent
prompt injection. Earnest cannot constrain its client's other tools.

Earnest's tools read stored content and queue notifications for destinations
you configured. They cannot subscribe, discover, refresh, extract on demand,
register destinations or trigger a fetch of an agent-chosen URL. Tool
descriptions are fixed strings. Scopes, notification budgets, operation replay
and revert ownership remain service-layer checks. Push text is generated by
the server unless you explicitly enable agent text for a protected destination.

## Webhooks

Only you can register a webhook destination. Supported events are
feed.failing, edition.ready and entry.matched. The receiver gets IDs, never
article content, and fetches the content itself with its own credentials.

Earnest signs requests using Standard Webhooks. Save the whsec_ secret
shown once at creation and verify webhook-id, webhook-timestamp and
webhook-signature on the receiver. The secret is encrypted at rest.

Requests use the shared outbound client and never follow redirects.
Only definite failures are retried, with exponential backoff. Unknown
outcomes are not retried. Five consecutive definite failures disable the
destination. Re-enable it after fixing the receiver.

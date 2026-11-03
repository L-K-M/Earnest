# The earnest CLI

`earnest` talks to a running server over its REST API. It never opens the
database, so the same binary works from a laptop, a cron job or an agent on
the same tailnet.

```sh
earnest login                      # device flow, stores the app token
earnest feeds list                 # human table
earnest feeds list --json          # machine output
earnest entries list --view today --limit 20
earnest read 1234
earnest done 1234 1235
```

## Authentication

Two environment variables, either of which overrides the config file:

| Variable | Meaning |
| --- | --- |
| `EARNEST_URL` | Server base URL. Default `http://127.0.0.1:8080`. |
| `EARNEST_TOKEN` | App or agent token. `earnest` never prints it. |

`earnest login` runs the RFC 8628 device flow: it prints the user code and
verification URL, then polls with the server's `interval`, backing off five
seconds on `slow_down` until you approve, deny or the code expires. The
resulting token is written to `$XDG_CONFIG_HOME/earnest/config.json`
(`~/.config/earnest/config.json` without XDG) with mode `0600`, and the
directory is `0700`. `earnest logout` removes the file.

`earnest whoami` shows the identity, scopes and a masked token
(`earn…6789`). It is the quickest way to check that a token works.

## Output

Every command takes `--json`, in any position. Human output is an aligned
table; `--json` echoes the server response (or a small composed object for
`whoami` and `briefing`) with no extra text on stdout.

Mutations print their result; state batches list every `op_id`. Every mutation
generates a UUIDv7 `op_id` client-side and retries are safe: the
server deduplicates per principal, so a repeated command after a lost response
replays the original outcome instead of applying it twice. The one exception is
OPML import, whose endpoint has no `op_id`; re-importing the same tree reuses
folders and subscriptions instead.

Server failures print as `earnest: <status> <title>: <detail>` and exit non-zero
(`1`). A `429` adds the budget from the problem body, for example
`(budget used 3/3)`. Usage errors exit `2`.

## Feeds

```sh
earnest feeds list
earnest feeds add https://example.com/feed.xml
earnest feeds remove 5
earnest feeds import subs.opml     # folders and feeds, no probe burst
earnest feeds export               # OPML on stdout
```

`add` sends one `POST /v1/subscriptions` with a fresh `op_id`. `import` posts
the file to `/v1/opml:import` as `application/xml` and reports each outline:
subscribed (fetching), folder, deferred, or the error. `export` writes raw OPML,
which is already machine-readable, so `--json` does not wrap it.

## Entries

```sh
earnest entries list [--view today|later|kept|done|all] [--unread] [--feed 5] [--limit 50] [--cursor 123]
earnest read 1234
earnest search "rustls"
earnest briefing
```

`--view today` is the API's `river`: the undecided flow with today decay.
`--unread` adds `unread_only`, `--feed` filters by subscription id, and
`--cursor` resumes an exclusive `seq` page. `read` prints the stored plain
text of `/v1/entries/{id}/content`; it never fetches the publisher's page.

`search` calls `GET /v1/search?q=…&limit=…`. It accepts either a bare JSON
array or an object wrapping the hits (`hits`, `entries` or `results`) and
normalises them for `--json`; the table shows id, title, author and snippet.

`briefing` is what is new since this principal's position in the chosen view
(default `today`, which reads the `river` position): it reads
`/v1/positions/{view}`, then lists entries above that `seq`. Reading never
marks anything seen.

## State

```sh
earnest done 1234 1235      # shelf=done
earnest later 1234          # shelf=later
earnest keep 1234           # kept=true
earnest unread 1234         # seen=unseen; the server also clears Done
```

Each id becomes its own state write in one `POST /v1/state:batch`, each with its
own `op_id`, the current generation as `base_rev.generation`, the principal
from `/v1/me` as the target, and `kind: explicit`. The table shows the resulting
register values and each item's status (`applied`, `superseded`, `gone`,
`expired`). An agent token writes its own principal with these commands;
`triage` is how an agent writes yours.

## Annotations and briefs

```sh
earnest annotate 1234 1235 --kind note --text "worth reading"
earnest annotate 1234 --kind label --text "security"
earnest annotate 1234 --kind score --text "9"
earnest brief --text "Three items worth your time" 1234 1235 1236
```

One request covers every id: the API keys an annotation by author, kind and
canonical entry set. Kinds are `note`, `label`, `score` and `brief`;
`summary` and `blurb` belong to the internal pipeline. Text is plain text with
control characters stripped and bounded lengths. The newest brief per
principal appears at the top of Today.

## Notifications

```sh
earnest notify --note "CVE in rustls" --entry 1234 --category alert
```

`--note` is required, `--entry` optional, `--category` is `alert` (default),
`digest` or `brief`; `--dedupe-key` collapses repeats within the window. The
command prints the notification id, its status, the token's daily budget and
`held until` when quiet hours apply. Over budget returns `429` with the budget
in the problem body.

## Triage, revert and activity

```sh
earnest triage done 1234 1235    # agent writes human state, gets a handle
earnest revert <handle>
earnest activity [--limit 100]
```

`triage` is the agent-only path that changes your state through the service's
agent provenance, so the change is labelled as the agent's. It prints the
result per entry plus the 128-bit revert handle and its expiry. `revert`
undoes a triage only while the affected registers are unchanged; it needs the
same principal that created the handle. `activity` lists the immutable
provenance log (who acted, on what, and whether a revert or cancel is still
available).

## Changes

```sh
earnest changes --cursor gen-1:41 --limit 200
```

Reads `/v1/changes` from an exclusive `generation:rev` cursor and prints rev,
type and payload, followed by the next cursor when more pages exist. Without
`--cursor` it reports the current head from `/v1/bootstrap` and reads nothing:
save a cursor from an earlier run to follow the feed incrementally. Pass
`--cursor` back to resume.

## For agents

An agent token works exactly like an app token:

```sh
export EARNEST_URL=https://reader.internal
export EARNEST_TOKEN=earnest_agent_...
earnest whoami
earnest briefing --json > new.json
earnest annotate $(jq -r '.entries[].id' new.json | head -5 | tr '\n' ' ') \
  --kind note --text "three new items, none urgent"
earnest notify --note "Morning digest ready" --category digest
```

Rules an agent must respect:

- Read stored content only. `read` and `entries list` never fetch a URL.
- Keep your own position with `state` (your principal); use `triage` only for
  deliberate human-state changes, and record the revert handle.
- `notify` spends your token's daily budget. `429` means stop; held is not lost.
- Never print or forward `EARNEST_TOKEN`. `whoami --json` masks it.
- Generate an `op_id` per attempt and reuse it on retry; a fresh `op_id`
  re-applies the action.
- Agent annotations are untrusted input to other readers: plain text, no
  markup, no URLs.

## Endpoint map

| Command | Request |
| --- | --- |
| `login` | `POST /v1/devices/start`, `POST /v1/devices/poll` |
| `whoami` | `GET /v1/me` |
| `feeds list` | `GET /v1/subscriptions` |
| `feeds add` | `POST /v1/subscriptions` |
| `feeds remove` | `DELETE /v1/subscriptions/{id}?op_id=` |
| `feeds import` | `POST /v1/opml:import` |
| `feeds export` | `GET /v1/opml` |
| `entries list` | `GET /v1/entries` |
| `read` | `GET /v1/entries/{id}/content` |
| `search` | `GET /v1/search` |
| `briefing` | `GET /v1/positions/{view}`, `GET /v1/entries` |
| `done/later/keep/unread` | `GET /v1/bootstrap`, `GET /v1/me`, `POST /v1/state:batch` |
| `annotate`, `brief` | `PUT /v1/annotations/{kind}` |
| `notify` | `GET /v1/bootstrap`, `POST /v1/notifications` |
| `triage` | `GET /v1/bootstrap`, `POST /v1/triage/{action}` |
| `revert` | `POST /v1/triage:revert` |
| `activity` | `GET /v1/activity` |
| `changes` | `GET /v1/changes`, `GET /v1/bootstrap` when no cursor |

Errors over HTTP are `application/problem+json` (RFC 9457); the CLI prints the
title and detail rather than a stack of JSON.

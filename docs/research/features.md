# Feature set

Tiers are steps in the sequence in [README.md](README.md). **Later** means useful but deferred. **Never** means a deliberate refusal. Size is relative (S, M, L), not a time estimate.

**Decided since.** [decisions.md](decisions.md#agents) removes proposals and the feed allowlist.

## What would make Earnest worth building

In a field this crowded, reading features won't do it: Current, Reeder and NetNewsWire are excellent and cheap. If these don't earn their keep, use NewsBlur or Miniflux.

1. **Governed agents.** Own token, feed allowlist, budget and position. A labelled annotation layer. An activity log that says honestly what can be reverted, cancelled or not. Open in the market, unproven in demand.
2. **Inspectable automation.** Plain-language rules compile to visible filters with a dry run. Verified passages. Partly open: others have rules, none found compiles them this way.
3. **Your own reader, on your own server, with native clients.** Not a market gap, since NewsBlur exists. It's the reason you'd enjoy using it.

Cadences, decay and end states have shipped in Current, so they don't differentiate. Whether they help is a separate, untested question. Build them, offer the unread model beside them, and don't lean on them.

## Matrix

### Ingest

| Feature | Tier | Size | Why |
| --- | --- | --- | --- |
| RSS, Atom, JSON Feed | 2 | S | Table stakes |
| Polite fetcher: conditional GET, per-host limits, adaptive interval, Retry-After | 2 | L | The notorious long tail; see [tech-stack.md](tech-stack.md) |
| Outbound client with SSRF controls | 2 | M | Every fetch goes through it |
| Authenticated feeds: per-feed HTTP auth and headers, encrypted | 2 | S | Private and paid feeds fail without it. Private to their owner |
| OPML import and export | 3 | S | No lock-in |
| Full-text extraction | 2 | S | The engine is easy |
| Per-feed extraction status and override | 5 | M | Top self-hoster complaint, and a permanent support surface |
| YouTube channels, Reddit via their feeds | 2 | S | Already RSS; discovery only |
| Feed autodiscovery from any URL | 3 | S | RSS Spy shows the heuristics. Never callable by agents |
| Podcasts: enclosures shown with a link | 5 | S | Playback can wait |
| Mastodon and Bluesky | Later | M | Profiles already have RSS |
| Newsletters by email | Later | M | Needs inbound mail or IMAP; bridge with Kill the Newsletter |
| WebSub | Later | M | Needs a public callback; polling suffices |
| Generated feeds for sites without one | Later | L | Point at RSSHub or RSS-Bridge |

### Read and triage

| Feature | Tier | Size | Why |
| --- | --- | --- | --- |
| River with resume position | 5 | M | Core |
| Unread-count mode | 5 | S | Many readers want it; state already supports it |
| Cadences: Now, Today, This week | 5 | M | Makes many feeds livable |
| Deterministic edition: This week as a list | 5 | S | Without it, This week has no surface until the LLM pipeline |
| Done, Later, Keep | 5 | S | Separates the river from the shelf |
| Reader view: typography, measure, themes, per-feed overrides | 5 | M | What you feel every day |
| Stored HTML subset, versioned | 2 | S | Three renderers depend on it |
| Image proxy | 5 | M | Privacy, mixed content. Destination checks from the outbound client |
| Keyboard map and command palette | 7 | M | Mac and web parity |
| Session end state | 7 | S | Plain statement of what happened |
| Time box | Later | S | No evidence anyone wants it |
| Swipe actions | Android | S | |
| Offline reading and action queue | Phone client | L | Outbox, conflicts and recovery. Web and Mac can start with less |
| Text-to-speech via the OS | Later | S | Cheap, not core |

### Organise and search

| Feature | Tier | Size | Why |
| --- | --- | --- | --- |
| Folders | 3 | S | Maps onto Google Reader tags |
| Entry labels | 4 | S | Capy Reader writes them through `edit-tag` |
| Exact dedupe across feeds | 2 | S | Free, high value |
| Mute by keyword or author | 5 | S | Cheapest noise reduction |
| Full-text search | 7 | M | Baseline |
| Saved searches as views | 7 | S | Also the unit for rules |
| Deterministic rules: tag, cadence, done, notify | 7 | M | Useful alone; the target for compiled rules |
| Clustering of the same story | 10 | M | Biggest volume win. Reserve the `cluster` state scope in the model |
| Semantic search, more like this | 10 | S | Cheap once embeddings exist |

### Notifications

| Feature | Tier | Size | Why |
| --- | --- | --- | --- |
| ntfy, through a single transport interface | 6 | S | Fast proof on your own phone. Self-hosted, default deny |
| Budgets, quiet hours, dedupe, generated provenance | 6 | M | Non-negotiable for agents |
| Digest by default; realtime only for rules you wrote | 7 | S | Calm |
| APNs | Mac client | M | Developer ID allows it |
| UnifiedPush | Android client | S | Encrypted; no relay needed |
| Web Push | Later | M | ntfy covers you until there are other users |
| FCM flavour | Later | S | If Play distribution happens |

### API and agents

| Feature | Tier | Size | Why |
| --- | --- | --- | --- |
| **Executable state model and sync test vectors** | 1, then a CI gate on every client | M | Main risk in the plan. Settles the protocol before schema and clients depend on it |
| Native REST with OpenAPI, RFC 9457 errors | 3 | M | Everything rests on it |
| Change feed with bootstrap, tombstones, generation, recovery | 3 | L | Harder than it looks; see [api.md](api.md) |
| Google Reader subset, by real client calls | 4 | M | New code, not adaptation |
| Tokens with kinds, audiences, scopes, feed allowlist | 6 | S | Agent identity |
| Annotations with provenance, including multi-entry briefs | 6 | S | Agent output as data |
| Activity log, revert where possible | 6 | M | Trust |
| MCP over Streamable HTTP, about 8 tools | 6 | M | |
| CLI, `llms.txt`, agent skill | 6 | S | Cheap discoverability |
| Webhooks you create | 7 | S | Wakes agents |
| Proposals (agent asks, you approve) | Later | S | When an agent needs to suggest feeds |
| OAuth 2.1 and a public listener | Later | M | Only for some cloud-hosted LLM clients |
| Library export: entries, annotations, saved items | Later | S | Pocket's lesson. OPML covers only subscriptions |
| Fever API | Never | S | MD5 credential; clients that need it mostly speak Google Reader |

### Earnest's own LLM features

Off by default. "No AI" removes the surfaces.

| Feature | Tier | Size | Why |
| --- | --- | --- | --- |
| Provider layer: OpenAI-compatible, Anthropic | 10 | S | Bring your own key |
| Golden set and evaluation | 10 | M | Nothing has been evaluated yet |
| Embeddings | 10 | S | Measure on your NAS |
| Ingest classifier with closed vocabulary | 10 | M | Compiled rules depend on it |
| Lazy blurb and summary | 10 | M | Expected, not differentiating |
| LLM edition | 10 | M | The daily artifact |
| Compiled rules with dry run | 11 | M | Signature |
| Watch rules with semantic match | 11 | M | Notify only, with budgets |
| Ask my feeds, with verified passages | 11 | L | Useful only after clustering and search |
| "What's new since" over a cluster | Later | M | High value; depends on 10 |
| Translation | Later | S | When a real feed needs it |
| Audio edition | Later | M | Enjoyable, unproven |
| On-device models | Later | M | Device-limited and inconsistent |

### Operations

| Feature | Tier | Size | Why |
| --- | --- | --- | --- |
| Docker Compose, `update.sh`, fleet conventions | 2 | S | House style |
| Feed health: last success, error class, redirects, why it fails | 3 | S | Self-hosters lose feeds silently |
| Backups: nightly dump, tested restore | 2 | S | Your reading history is the asset |
| Logs, `/metrics` | 2 | S | Debugging at a distance |
| Supply chain: `govulncheck`, pinned actions and digests | 2 | S | Cheap, and the prior art does it |
| Multi-user | Later | M | Schema ready from day one; no UI until needed |
| Retention policy | 7 | S | Keep read and saved; age out unread firehoses by choice |

## Karakeep

Karakeep is your read-later store, so Later could mean Karakeep. Default: **Later stays in Earnest**, because it needs to sync, with one "Send to Karakeep" action over [its REST API](https://github.com/karakeep-app/karakeep/blob/main/packages/open-api/index.ts). That action is yours or a deterministic rule's, never an LLM's alone.

## Non-goals

Social features. "For You" feeds. Plugin marketplace. ActivityPub or AT Protocol server roles. PDF and EPUB library. Teams and shared folders. Windows or Linux native apps (the web client covers them). Billing, until a hosted tier is a decision.

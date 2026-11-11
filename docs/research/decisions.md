# Decisions

Taken by you on 2026-11-02, after the ninth review. Where they differ from the research, they win. The research stays as written, so it may still describe what these replace.

## Licence and code reuse

- **Unlicense**, the file the repository already carries. This answers [question 3](README.md#questions-only-you-can-answer) and replaces the Apache-2.0 default.
- **No code from third-party projects.** Nothing is copied, ported or vendored. Miniflux is reference only, so the parsers, encoding handling, `urlcleaner`, scraper rules and tests that [tech-stack.md](tech-stack.md#server-language-go-provisionally) proposes copying are written fresh. Code from your own repositories, such as Kararead's `ReaderHtmlBuilder`, may be copied and relicensed. Libraries such as gofeed, River and the MCP Go SDK stay ordinary dependencies.

## Agents

An agent reads everything you subscribe to and reports back. For example, it reads what's new each morning and sends you a summary.

- **All feeds, no allowlist.** An agent token reads every subscription and its articles, full text and private feeds included. Tokens lose the feed allowlist, and nothing filters by one: not the MCP tools, the app API or clusters. This closes the open gap on clusters across the allowlist. It also widens what an article that injects an agent can see.
- **What it may do.** Keep its own position, so it knows what's new. Post a brief, shown at the top of Today. Push to your phone through your ntfy, within a budget. Add notes to articles. Mark articles read or Done for you, so the `triage` tool joins the starting set.
- **No proposals.** The `proposal` resource, `proposals:write`, `/v1/proposals` and `propose_subscription` are removed. An agent can name a feed in a note, and you subscribe yourself. This closes the proposals exit.
- **Its writes rank as an agent's.** The server sets `source` from the token and the kind of operation, and ignores the caller's value, so an agent can't pass as your deliberate action. Agent changes to items you haven't seen are listed in Today and at session end, not only in the activity log.
- **The app API too.** Agent tokens may use the REST API as well as MCP, reading everything and writing only what their scopes allow. `/v1/changes` returns them the full feed. Devices are yours only, like webhooks, which closes the other exit. Agents still never hold `feeds:write`, `webhooks:write`, token management or `admin`.
- **Agent notes in search, not in AI.** Search finds agent notes, labelled as the agent's. Ask my feeds never passes them to a model.

That decides all six [open gaps](llm.md#open-gaps). The rule that nothing an agent can reach fetches a URL it influenced is unchanged.

## Sync

These settle the [round 9 list](api.md#change-feed). Step 1's model tests them like everything else.

- **A batch commits as one transaction** with its one compact `state.updated`. Each item still gets its own result.
- **Every bootstrapped object carries the `rev` of its last change**, and state its `state_rev`, so bootstrap can keep the higher one.
- **Labels are fully native**: endpoints to add and remove them, change events and tombstones, like read state.
- **Positions sync live** through a `position.updated` event, still last writer wins.
- **Clusters are written per article.** A cluster-scoped mark resolves to its member entries and writes them as a batch, not a range record. In vector 18, a member that joins later stays unread until read-time suppression arrives at step 10.
- **Marking a Done item unread clears Done**, by a server rule on every surface, as vector 16 already requires through Google Reader.
- **Lock order:** the `seq` counter first, then per-user counters in user ID order.
- **`mark-all-as-read` without `ts`** means receipt time: through the highest `seq` at receipt.

## Reading on the web and the Mac

Taken by you on 2026-11-07, while triaging the review PRs.

- **Bulk mark-read is a command, not a button.** "Mark the rest read" lives in the web command palette for Today, a feed or a folder. It marks through the newest entry loaded in that view, after a confirmation, so items that arrive later stay unread. The [anti-pattern](ux.md#anti-patterns) against "Mark all read" as a primary toolbar action stands.
- **Tab moves focus.** The web reader's summary opens and closes only from its button. This drops the [ux.md](ux.md) binding "Tab hides the summary", so you can always tab through the page.
- **Mac windows are independent.** Each window keeps its own selection and reader over the shared sync engine, so you can read two articles side by side. The app stays multi-window instead of becoming single-window.

- **Context menus also offer bulk mark-read** (2026-11-09, at your request). Today, a folder or a feed in the sidebar has "Mark all as read", and an article row has "Mark read up to here". A sidebar list may not be open, so "Mark all as read" marks through the newest article the browser knows of (synced, or loaded in any list) rather than the newest loaded one; the confirmation says so, and later arrivals stay unread. It is still not a toolbar button.
- **The article frame runs one script of Earnest's own** (2026-11-09, at your request). A frame sized to the pane gave the reader two scrollbars, and Space could not scroll the article. The content page now carries a short script, admitted by its hash in the page's CSP, that reports the article's height and forwards scrolling keys and single-key shortcuts to the app origin only. The frame's sandbox adds `allow-scripts` but never `allow-same-origin`, so the page keeps an opaque origin, and the sanitiser and CSP still stop every script the article itself carries. This relaxes the [tech-stack](tech-stack.md) rule that the frame allows only popups.

- **Web lists start with the newest articles** (2026-11-11, at your request). Opening a list loads the highest ingest sequences first, then pages toward older entries. Saved reading positions still sync but never filter or delay the list. Edition retains its feed grouping, with newest entries first within each feed. Read/unread state is unchanged by opening a list.

## Fetching

Taken on 2026-11-09, at your request, while fixing a feed that stopped the server.

- **One feed's bad data never stops the server.** When the service refuses what a feed served, the fetcher records it as that feed's parse failure, visible in feed health, and keeps fetching the others. Database and other system errors still stop the fetcher, as before.
- **A repeated item is stored once per fetch.** When a feed lists the same identity twice, the first occurrence wins.
- **GUID fragments are part of the identity.** Comment feeds such as WordPress's give each comment its post's URL plus `#comment-N`, so dropping the fragment merged every comment on a post into one entry. Link identities still drop fragments, because there they only scroll the page. An entry stored under the earlier rule is adopted by its first fetch under the new identity, so nothing is stored twice.

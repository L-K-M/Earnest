# Earnest analysis

Unimplemented findings from the longcat-1, longcat-2, mimo-1 and
mistral-large-2 reviews, ordered by severity within each focus area.
Every blocking and important finding from these reviews has an open pull
request against main, except the items noted inline whose files are
claimed by concurrent open PRs.

## usability

### River rows show no timestamp

usability — nice-to-have

`web/src/River.tsx:122` renders source and state but no time, so you cannot
tell how fresh an item is without opening it (the Android app shows a relative
time).

Suggested fix: add a relative time for `first_seen_at`/`published_at` in the
meta line, matching the Android `relativeTime()` style.

### Subscription rows are dense when managing many feeds

usability — nice-to-have

`web/src/FeedSettings.tsx:206-224` stacks four selects and a title form in
every feed row, so managing many feeds means a lot of scrolling and the health
alert sits above the actions.

Suggested fix: collapse cadence/folder/extract into one inline row and move
health into an icon/tooltip.

### Feed health timestamps are absolute and timezone-dependent

usability — nice-to-have

`web/src/FeedSettings.tsx:245` renders feed health times with
`toLocaleString()` in the browser's local timezone. Users in different zones
see different absolute times for the same feed, and no relative age is shown,
so "Tried …" is hard to interpret.

Suggested fix: render a relative time ("2 h ago") plus the absolute time in
the user's zone, or send a preformatted relative string from the server.

### The notifications release button is always visible

usability — nice-to-have

`web/src/agents/Surfaces.tsx:104` shows "Release current notifications" even
when no restore has happened and nothing is withheld. A user cannot tell
whether pressing it does anything.

Suggested fix: show it only when a withheld notification exists, or label it
with the current generation state.

### Onboarding blocks Continue while measuring feed volume

usability — nice-to-have

`web/src/Onboarding.tsx:11` polls feed health for up to 20 seconds and
disables Continue while checking. A user with many feeds waits with no sense
of progress.

Suggested fix: show a live count of feeds still being measured and let the
user continue immediately; update the report when the poll finishes.

### Saving settings gives no success feedback

usability — nice-to-have

`web/src/Settings.tsx:21` clears errors on save but never confirms success.
Users reasonably wonder whether the save worked.

Suggested fix: show a brief "Saved" status, as the Feeds screen already does.

### Typography changes do not refresh an open article

usability — nice-to-have

`web/src/ArticleReader.tsx:26` fetches the content URL once per mount. A
typography change requires closing and reopening the article before the
iframe reflects it.

Suggested fix: re-render the iframe when the typography values change by
keying it on them.

### Sidebar feeds ignore their folders

usability — nice-to-have

`web/src/main.tsx` (the nav "Feeds" details) lists feeds flat even when they
are in folders; folders appear as a separate list of names only. Users with
many feeds cannot find a feed where they organised it.

Suggested fix: nest feeds under folder headings in the sidebar; about half a
day.

### The rules editor's multi-selects are clumsy

usability — nice-to-have

`web/src/rules/PredicateEditor.tsx` chooses feeds, folders and labels with
native `<select multiple>`, which needs ctrl-click and hides selection state in
a small box; the rules editor is the app's most complex form.

Suggested fix: checkbox lists or token inputs; about a day.

### Destructive actions use browser confirm dialogs

usability — nice-to-have

`web/src/FeedSettings.tsx` confirms deletes and unsubscribes with
`window.confirm`, which clashes with the styled `<dialog>` used elsewhere and
cannot explain consequences (for example that entries are kept).

Suggested fix: use the app's dialog component with a consequence sentence;
about half a day.

### An expired session surfaces as scattered request errors

usability — nice-to-have

`web/src/request.ts` has no 401 handling: when a session expires mid-use every
panel shows a generic "Request failed (401)" and sync keeps failing, with no
path back to sign-in short of finding the Sign out button.

Suggested fix: on 401 clear the session and return to the login screen with an
explanation; about half a day.

### The Android Activity screen is blank on empty or error

usability — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:601`
(`ActivityScreen`) renders only its groups, so a failed or empty load shows a
blank screen with no empty state, error text or retry.

Suggested fix: empty/error list items with a retry action, mirroring the entry
list's end item; about an hour.

### Android unsubscribe fires without confirmation

usability — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:589` runs
Unsubscribe immediately with no confirmation (the Mac asks first). The same row
once showed no cadence state either; the current cadence indicator is covered
by open PR #68.

Suggested fix: a confirmation dialog as on the Mac; about an hour.

### Android activity rows can name nothing and cannot cancel

usability — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/AgentChanges.kt:67` shows
"Entry #null" when an entry id is absent and "Cannot revert this change" for
`nextAction == "cancel"`, leaving notification-cancel actions unreachable (the
Mac can cancel). Open PR #69 makes the action descriptions readable; the
missing entry name and the cancel action remain.

Suggested fix: fall back to target metadata instead of `#null` and add a
Cancel button calling the notification-cancel endpoint; about half a day.

### Android reuses river copy for every view

usability — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:432` says
"Nothing new. A rare and beautiful thing." for an empty Later list and "That is
everything since Tuesday" for non-river views, misstating what those lists are.

Suggested fix: per-view end copy, matching the Mac's per-section wording;
under an hour.

### River passive-read behaviour is invisible and irreversible

usability — nice-to-have

`web/src/River.tsx:56,77` marks an article seen after 60% visibility for
600 ms and advances the resume position with no visible affordance. A user who
flings through the list silently loses those articles from Today, and the only
way back is Done, Kept or search. The session-end copy counts these as choices.

Suggested fix: add a brief "Reading marks articles read" hint near the river
header, or offer an undo snackbar after passive marks, matching the Android
swipe undo; about half a day.

### The Activity screen filters are raw protocol fields

usability — nice-to-have

`web/src/agents/Surfaces.tsx` (ActivityScreen) exposes "Principal ID" (a
number), "Action" (a free-text protocol value like `state.updated`) and
"Since" (a date). A user cannot discover valid values without reading
`docs/agents.md`.

Suggested fix: populate the principal select from `/v1/agents` and the action
select from the documented vocabulary; about half a day.

### Onboarding can reappear after the user already has feeds

usability — nice-to-have

`web/src/main.tsx:177` shows onboarding whenever `onboardingStarted` is true,
even after an OPML import has created subscriptions. A user who imports, opens
a sidebar view and returns to the river is greeted with "Welcome to Earnest".

Suggested fix: drop the `onboardingStarted` term, or use it only to suppress
onboarding after an import completed in this session; under an hour.

## performance

### The passive-seen observer is rebuilt on every scroll frame

performance — important

`web/src/River.tsx:41,50,81` keys the mark-as-seen effect on the joined
visible-ID string, so it tears down and recreates the IntersectionObserver and
the 600 ms timer on every scroll frame that changes the visible set. The timer
reset means passive "seen" writes lag behind fast scrolling (items can briefly
reappear as unread), and the churn itself drops frames on a long list. The
same tick also does an O(n) `entriesRef.current.find(...)` per pending row
(`River.tsx:64`), i.e. O(n*m) with hundreds of loaded entries.

Suggested fix: create the observer and timer once per view, observe/unobserve
rows as they mount, keep the timer outside the observer's lifecycle, and look
pending entries up in a `Map`. Must keep the existing "marks only intersecting
rendered rows" test green. Note: `web/src/River.tsx` is touched by many
concurrent open PRs, so land this after those merge.

### The river re-checks freshness on every action

performance — nice-to-have

`web/src/River.tsx:84` rebuilds the `actions` object and the command list on
every render, and the river's keydown effect depends on them. The listener is
re-bound on every state change. Open PR #76 fixes the palette's equivalent;
the river's `actions`/`onOpen` identity remains.

Suggested fix: memoise the actions object and read it from a ref in the
keydown effect, matching the palette fix.

### The Activity and Notifications screens load entire histories

performance — nice-to-have

`web/src/agents/Surfaces.tsx` (`useActivities`, the `Notifications` list) pages
the whole history in 1000-item loops into memory on every invalidation, and
`ActivityRows` builds its groups map with repeated array spreads (O(n²)).
Open PR #33 batches the per-row entry lookups but not the pagination loops.

Suggested fix: page incrementally or virtualise, and group in one pass; a few
hours.

### Every session request rewrites the sessions row

performance — nice-to-have

`internal/service/session.go:61` issues an `UPDATE` on the session row for
every authenticated request and each SSE heartbeat, serialising concurrent
requests from one session on a row lock and churning dead tuples for the
session's 30-day life.

Suggested fix: refresh `expires_at` only when the remaining idle lifetime is
below a threshold, mirroring the throttled token `last_used_at` write; a few
hours.

### The Mac table redraws on every state change

performance — nice-to-have

`clients/mac/App/Sources/EntryTable.swift:56` runs `table.reloadData()` plus
re-selection on every SwiftUI state change (article load, sync, pending ops,
selection), a full list redraw each time.

Suggested fix: diff items and apply row-level updates; about a day.

### The Android article cache grows without bound

performance — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/AppViewModel.kt:315` writes
every opened article's full HTML to the `contents` Room table and never evicts
(`Daos.kt:26 delete` is dead code); the Mac caps its cache at 200 articles.

Suggested fix: bounded LRU eviction on save; a few hours.

### Sync bootstrap reloads and rewrites the whole IndexedDB snapshot per page

performance — nice-to-have

`web/src/sync/runtime.ts:612` calls `mutate()` for every page of every
bootstrap resource. `mutate` reloads a full structured clone from IndexedDB
and writes the whole snapshot back, so cost is O(pages × snapshot size). A
10,000-entry cache with 1000-entry pages does ~10 full clones and writes, and
the UI is notified after each page.

Suggested fix: add a bootstrap-specific apply path that mutates the in-memory
client and persists once at the end (or every N pages), keeping the atomic
cursor contract; about a day. Note: `web/src/sync/runtime.ts` is touched by
concurrent open PR #35.

### SyncClient.state scans the whole outbox for every visible row

performance — nice-to-have

`web/src/sync/client.ts:444` iterates every pending outbox item for each call,
and `web/src/River.tsx:97` calls it once per visible row on every render. With
100 visible rows and a 1000-item offline outbox that is 100,000 iterations per
render, which shows up as jank while flinging or draining the outbox.

Suggested fix: index pending writes by `(principal, entry, register)` and apply
only the newest relevant entry; about half a day. Note: `client.ts` is touched
by concurrent open PR #35.

### GetByIDs loads article content once per entry

performance — nice-to-have

`internal/service/entries.go:222` issues one `LoadContent` query per entry.
The rules dry run uses it in 200-id batches, so a dry run over thousands of
matches costs one content query per match. Search opens one entry, so the
common path is fine.

Suggested fix: load content for the whole ID list in one `ANY($1)` query, or
add an `include_content` flag and leave it off for dry runs; about half a day.
Note: `entries.go` is touched by concurrent open PRs #20 and #31.

### ClusterEntries can scan the same 200-row window repeatedly

performance — nice-to-have

`internal/service/cluster_entries.go:10` re-queries 200 rows at a time until
it accumulates `limit` unsuppressed entries. A river that is mostly duplicates
scans the whole feed to fill one page.

Suggested fix: fetch cluster suppression for a larger window once, or paginate
with a keyset over unsuppressed entries; about half a day.

### ArticleReader refetches the signed content URL on every open

performance — nice-to-have

`web/src/ArticleReader.tsx:26` sets `gcTime: 0` on the content query, so the
signed URL is discarded when the reader unmounts and refetched on every open.
The signed URL is short-lived by design, but a small cache with an expiry check
avoids the extra request.

Suggested fix: keep a positive `gcTime` and validate the cached URL's
`expires_at` before reuse, or cache the signed URL per entry; about half a day.

### Android openEntry always fetches raw content even when the signed URL works

performance — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/AppViewModel.kt:245` calls
`api.content(entryId)` unconditionally to populate the offline cache, then
calls `api.contentUrl(entryId)` for the rendered URL. Every article open costs
two requests, even online with a healthy connection.

Suggested fix: fetch the signed URL first and reuse the cached HTML when it
exists; only fall back to the raw content when the signed URL fails or no
cache exists; about half an hour.

## visual issues

### The reader column can be squeezed between 700 px and 1050 px

visual issues — nice-to-have

`web/src/style.css:12,30-31` only switches `.workspace.with-reader` to a single
column below 700 px, so at ~900 px the article iframe and list fight for width.

Suggested fix: lower the single-column breakpoint for `.with-reader` (or use
`auto-fit`) so the reader goes full-width earlier; verify at several widths.

### Sidebar `<details>` are hard-coded open

visual issues — nice-to-have

`web/src/main.tsx:181` renders the Feeds/Folders `<details>` with `open`, so a
user with many folders scrolls a long sidebar and cannot collapse sections; the
state is not remembered.

Suggested fix: default to collapsed (or persist open state in localStorage).

### The mobile sidebar can take a third of the screen

visual issues — nice-to-have

`web/src/style.css:54` turns the sidebar into a 30 dvh scroll box with every
feed rendered as a button at max-width 700 px. With many feeds the nav takes a
third of the small screen before the reader is visible.

Suggested fix: collapse the feed list behind a details/summary by default on
small screens and let the reader take the full height.

### Switching views scrolls the workspace to the heading

visual issues — nice-to-have

`web/src/River.tsx` focuses the `<h2>` on every view change, which scrolls
the workspace to the top even though the list itself is remounted.

Suggested fix: focus without scrolling (`preventScroll: true`) and only on
keyboard-driven view changes.

### The article iframe has a hard 75 dvh height

visual issues — nice-to-have

`web/src/style.css:34` fixes the reader iframe at 75 dvh. On a tall monitor
that leaves a large empty area under the article; on a short one it dominates.

Suggested fix: let the article body fill the available height with an
internal scroll, or offer a compact/comfortable density choice. The frame also
scrolls inside the already scrollable reader, giving double scrollbars; growing
the iframe to its content height (a postMessage from the content origin) would
fix both.

### River rows print raw register values

visual issues — nice-to-have

`web/src/River.tsx` (the entry meta line) prints raw values ("later", "done")
beside properly capitalised UI labels ("Later", "Done"), and mixes "seen" with
"Unread".

Suggested fix: map values to display labels; under an hour.

### Palette key hints are all the same

visual issues — nice-to-have

`web/src/main.tsx` (commands) gives every palette command the same
"Ctrl+K, Enter" hint and the sidebar buttons carry long inline hints ("Command
palette (Ctrl+K / ⌘K)"), so the shortcut table reads as noise while the real
single-key hints (e, s, f, m) live only in the help dialog.

Suggested fix: show each command's real key (or none) and shorten the button
labels; about an hour.

### The Android widget ignores river state and theme

visual issues — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/EarnestWidget.kt:99` shows
the five highest-seq entries regardless of river membership or read state (done
items can headline it) and hardcodes light colours, ignoring the launcher
theme.

Suggested fix: filter with the same river/state logic as the app and use
GlanceTheme colours; about half a day.

### Dark theme leaves the document canvas cream

visual issues — nice-to-have

`web/src/style.css:1` sets `background:#f8f6ef` on `:root`, and the
`@media(prefers-color-scheme:dark)` block overrides only the custom properties,
never `background`. The `.layout` element paints `var(--paper)`, so the
viewport looks correct, but the propagated canvas behind it stays cream.
Rubber-band scrolling and the overscroll glow show a light frame around a dark
app.

Suggested fix: set `background:var(--paper)` on `:root`/`html` (or set
`background` explicitly in the dark media query and each `[data-theme]` block);
under an hour. Note: `web/src/style.css` is touched by concurrent open PR #19.

## aesthetics and theming

### The eink theme is high-contrast black on white

aesthetics and theming — nice-to-have

`web/src/style.css` sets `--paper:#fff`, `--ink:#000` and `--line:#000` for
the `eink` theme. Real e-ink panels are grey-ish, and pure black on pure white
is the highest contrast but not the most comfortable for long reading; the
content origin uses the same `#fff`/`#000` (`internal/web/content.go`). The
name promises more than it delivers.

Suggested fix: use a light grey paper (e.g. `#e8e8e8`) with near-black ink and
soften `--line` to a dark gray (e.g. `#444`), or rename the theme to "high
contrast"; under an hour.

### The sign-in screen is a bare form

aesthetics and theming — nice-to-have

`web/src/style.css` (`.login`) is a bare centered form with no brand mark or
card, while the rest of the app has a considered paper palette.

Suggested fix: wrap the login in a bordered card using the existing
`--paper/--line` tokens.

### The palette has no accent colour

aesthetics and theming — nice-to-have

`web/src/style.css` defines a single warm paper and one ink colour per theme.
Selection, links and focus use the browser default outline, so the app feels
flatter than the reading experience it provides.

Suggested fix: add one restrained accent per theme and use it for
`aria-current`, focus rings and the selected row. Hover, the current page and
pressed buttons all use the same `--selected` background today, so the selected
article row is indistinguishable from a hovered one while navigating with the
keyboard.

### The dyslexia-friendly choice has no preview

aesthetics and theming — nice-to-have

`web/src/Settings.tsx:37` changes the face and spacing for the dyslexia
option but shows no preview. A user cannot tell what they are choosing before
saving.

Suggested fix: show a short live preview paragraph in the selected theme,
measure and font, and extend the preview to the palette choices (light, dark,
black, eink), which are named with no visual cue.

### Typography settings mix app-wide and article-only scopes

aesthetics and theming — nice-to-have

`web/src/Settings.tsx` puts Theme (which restyles the whole app) with Measure
and Font (article body only) under one "Typography" label, so readers discover
the font setting expecting the interface to change too, or vice versa.

Suggested fix: split or annotate the two scopes ("App theme" vs "Article
type"); about an hour.

## missing features

### River mode shows no per-feed unread signal

missing features — nice-to-have

`web/src/main.tsx:181` renders unread counts only in `unread_counts` mode; in
river mode the sidebar has no at-a-glance indication of which feeds have new
items.

Suggested fix: show a small unread dot/count per feed in river mode too (needs
the bounded count endpoint from PR #32).

### Unread counts cover feeds but not folders or labels

missing features — nice-to-have

`web/src/main.tsx:181` shows counts only for feeds in unread-counts mode.
Folders and labels have no count, so a user cannot see which folder is backing
up without opening it.

Suggested fix: extend `readUnreadCounts` to return per-folder and per-label
totals from the same bounded scan.

### Manage feeds has no filter

missing features — nice-to-have

`web/src/FeedSettings.tsx:234` renders every subscription in a long scroll
with no filter once there are more than a few dozen.

Suggested fix: add a client-side filter box over title and URL.

### The reader has no font-size control

missing features — nice-to-have

`web/src/Settings.tsx` controls measure and font but not font size. Users who
need larger text have no in-app control.

Suggested fix: add a font-size stepper to the typography fieldset and the
content URL.

### The reader has no focus mode, text-to-speech or share action

missing features — nice-to-have

`web/src/ArticleReader.tsx` has a small toolbar row but no focus mode, no
text-to-speech and no share action. These are common reader expectations.

Suggested fix: pick one (focus mode that hides the sidebar is the cheapest)
and ship it behind a setting.

### Native clients cannot search, manage rules or change settings

missing features — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:76` and
`clients/mac/App/Sources/Models.swift:5` cover reading only: no search (or
saved searches), rules, notifications, settings or Ask, and positions are read
at bootstrap but never written, so cross-device continue-reading works only
from the web.

Suggested fix: stage native Search and Notifications first, a settings surface
for reading mode and typography, and `PUT /v1/positions` on scroll; weeks,
staged per feature.

### Verified citations cannot highlight in the article frame

missing features — nice-to-have

`web/src/pipeline/Pipeline.tsx` (Summary) says "Highlighting is unavailable in
the article frame", so users must hunt a cited passage by hand.

Suggested fix: postMessage the passage to the content origin and scroll and
highlight it there; about two days.

### Search snippets do not mark their matches

missing features — nice-to-have

`web/src/search/Search.tsx` (`Matches`) never highlights query terms in
snippets, so scanning results is harder than it needs to be.

Suggested fix: highlight query terms server-side (offsets) or in a client-side
match pass; about half a day.

## bugs

### Article iframes let popups escape the sandbox onto the app origin

bugs — important

`web/src/main.tsx:198` renders the article iframe with
`sandbox="allow-popups allow-popups-to-escape-sandbox"`. A link in untrusted
article HTML can therefore open a popup that is not sandboxed on the same
origin as the app. Today the `/content/` CSP (`default-src 'none'`) and JSON
problem responses make this hard to exploit, but it removes a whole defense
layer for any future same-origin route that reflects content. Affects every
web user reading an article.

Suggested fix: drop `allow-popups-to-escape-sandbox` and keep `allow-popups`
(external links still open in a sandboxed popup; they just cannot reach the
parent origin). Note: `web/src/main.tsx` is touched by several concurrent open
PRs, so land this after those merge.

### Google Reader `s=` include filters are AND-ed instead of unioned

bugs — nice-to-have

`internal/compat/greader/streams.go:157` sets `pass=false` when any `s=` value
does not match, so multiple include-stream parameters require every stream to
match. The Google Reader protocol treats repeated `s=` as a union; a client
asking for `s=read&s=starred` gets the intersection. Affects third-party Reader
clients that combine streams.

Suggested fix: treat `s` as OR (pass if any matches), keep `xt` as AND-NOT.
Note: `streams.go` has concurrent open PRs (#47, #20).

### A literal `<!DOCTYPE` in article text rejects a feed

bugs — nice-to-have

`internal/feed/charset.go:91` rejects any document containing the literal
bytes `<!DOCTYPE` or `<!ENTITY`, even inside CDATA, comments or escaped
article text. A legitimate feed whose article body contains that string is
refused as a DTD attack.

Suggested fix: only reject when the declaration appears in the prolog before
the root element, or strip CDATA and comments before the check.

### Passive "seen" can mark articles the user only scrolled past

bugs — nice-to-have

`web/src/River.tsx:53` triggers passive seen at 60% visibility for 600 ms. A
reader who scrolls a long list quickly, or who leaves the window open while
doing something else, gets entries marked seen that they never read; the
saved position then moves past them.

Suggested fix: require the entry to stay intersecting across two consecutive
timer ticks, or tie passive-seen to an explicit dwell time and never advance
the saved position past unread entries.

### Single-key actions act on the first article when nothing is selected

bugs — nice-to-have

`web/src/River.tsx:84` makes `j`/`k`/`e`/`s`/`f`/`m` act on `entries[0]` when
no entry is selected. A user who presses `e` while the list is still loading,
or after opening a view with nothing focused, silently marks the first
article Done.

Suggested fix: require a selection for single-key actions, or make the first
action only move the selection.

### Clearing the Today decay field saves NaN

bugs — nice-to-have

`web/src/Settings.tsx:28` takes `today_decay_days` from `valueAsNumber`, so
clearing the field saves `NaN`, which serialises to `null` and fails
server-side with a confusing error.

Suggested fix: guard on `Number.isFinite` and keep the input on a valid value;
under an hour.

### The Keychain account key breaks on URL normalisation

bugs — nice-to-have

`clients/mac/App/Sources/KeychainTokens.swift:9` keys the Keychain account on
`server.absoluteString`, so `https://host` and `https://host/` are different
accounts and the saved token "disappears" when the URL normalises.

Suggested fix: key on a normalised scheme+host+port origin; about an hour.

### A failed Android notification tap does nothing

bugs — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:139` swallows
`notification()` failures (`runCatching{}.getOrNull()`), so tapping a push
notification while offline silently does nothing: no toast, no retry, no
fallback.

Suggested fix: show a snackbar or fall back to the activity list; about an
hour.

### The summary panel captures the Tab key

bugs — nice-to-have

`web/src/pipeline/Pipeline.tsx` (Summary's key handler) intercepts Tab outside
form fields while the summary is visible to hide it. The behavior is
deliberate and tested ("allows forward Tab navigation except when hiding a
visible summary") but undiscoverable — it is not in the help dialog — and
briefly diverts Tab from focus navigation.

Suggested fix: move the close shortcut to a non-navigation key (or plain Esc),
or at least list the behavior in the help dialog; about an hour plus a test.

### The river resume position is the last rendered row, not the furthest read

bugs — nice-to-have

`web/src/River.tsx:77` saves `rendered.at(-1)` as the resume position every
600 ms. `pending` is a Set populated in IntersectionObserver callback order,
which is not sequence order, so the saved seq can move backwards or stop short
of what the user actually read. The next visit resumes at the wrong place.

Suggested fix: save `Math.max(...rendered.map(e => e.seq))`, or keep a
monotonic `furthestSeq` ref; under an hour.

### Android marks rows seen the instant they render

bugs — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:451`
(`MarkSeenOnRender`) marks every visible LazyColumn row with no dwell time and
no visibility threshold. A fast fling marks everything it touches, and the
first screenful is marked before the user has read a word. The web waits for
60% visibility and a 600 ms interval (`web/src/River.tsx:56`).
`docs/android.md` states "Rows mark themselves seen as they render", so this
is intended, but it is much more aggressive than the other clients.

Suggested fix: require a minimum visible duration (e.g. 400–600 ms) or a
visibility fraction before marking, matching the web; about half a day.

### The session-end "seen" count includes passive reads

bugs — nice-to-have

`web/src/main.tsx:119` increments `summary.seen` for every seen write,
including the passive marks River sends when rows scroll into view. The copy
says "Your choices this session: N seen", but most of those were not choices.

Suggested fix: only increment `seen` when `kind === 'explicit'`, or word the
copy as "read this session"; under an hour.

## general issues

### `lastVisit` is only written on pagehide

general issues — nice-to-have

`web/src/main.tsx:56-58` writes `earnest.lastVisit` only on `pagehide`, so a
tab that is killed or crashes keeps a stale timestamp and the next session's
"While you were out" agent-change window is older than reality.

Suggested fix: also write `lastVisit` periodically or on `visibilitychange`.

### The outbound host limiter map grows without bound

general issues — nice-to-have

`internal/outbound/outbound.go:443` (`acquire`) inserts one `hostLimiter` per
destination host and never evicts: every feed, webhook target, push endpoint
and image origin ever contacted accumulates for the process lifetime. Open PR
#24 reuses connections per destination but does not bound the map.

Suggested fix: add a size bound or idle-time eviction to the per-host limiter
map; about an hour.

### docs/web.md describes a task boundary that no longer exists

general issues — nice-to-have

`docs/web.md:105` (also :91, :102, :115) says the production router returns
404 for settings until `web/settings-registration.patch` is applied, that the
content server renders the dyslexic choice as a serif fallback, and that
vectors 36/37 are absent. All three are false: `api.go:109` mounts
`settingsRoutes`, `internal/web/content.go` implements the dyslexic face, and
`testdata/vectors/` contains 36–39. A new contributor following the doc will
look for patches that do not exist.

Suggested fix: delete the stale section, or replace it with a short note that
the settings route, dyslexic face and vectors 36–39 have landed; about half an
hour. Note: `docs/web.md` is touched by concurrent open PR #27.

### The CHANGELOG has no entries for the post-v0.1.0 work

general issues — nice-to-have

`CHANGELOG.md` still has a single Unreleased block that reads as the initial
feature dump. The fixes since v0.1.0 are not recorded, so the changelog is not
a useful release input.

Suggested fix: add Fixed/Changed entries for the post-v0.1.0 commits, or
squash the Unreleased section at the next release; about half an hour.

### Dead CSS rules for the sync status and alerts

general issues — nice-to-have

`web/src/style.css:17,18` targets `.workspace > .sync-status` and
`.workspace > [role=alert]`, but the sync status and alerts are nested inside
`.sync-messages` or `.river`. The rules never match; the real styling comes
from `.sync-messages`. Harmless, but misleading.

Suggested fix: delete the dead selectors or retarget them at `.sync-messages`
children; under an hour. Note: `web/src/style.css` is touched by concurrent
open PR #19.

## novel ideas

### The session end could carry a reading streak

novel ideas — nice-to-have

`web/src/River.tsx:127` already has a session summary card. Extending it with
a "days in a row you opened Earnest" streak (from `earnest.lastVisit` history)
would give a gentle, non-gamified nudge alongside the existing choices summary.

Suggested fix: track daily visits in localStorage and render the streak in the
session-end card.

### The session end could offer a reset and a real reading count

novel ideas — nice-to-have

`web/src/River.tsx:127` already has a small session summary. It could carry a
"Start again from the top" action that resets the view position, and a count
of articles actually opened this session (not just marked seen).

Suggested fix: add a compact session summary with a reset action.

### Agent briefs deserve their own surface

novel ideas — nice-to-have

`web/src/agents/Surfaces.tsx` renders agent briefs as details/summary rows in
the Today river. Grouping today's notes by principal, with the entries they
refer to, would make the agent relationship legible.

Suggested fix: promote briefs to their own panel, reusing `TodayAgents` data.

### The command palette could expose reading-state commands

novel ideas — nice-to-have

`web/src/palette/Palette.tsx` is fast and the fuzzy matcher is good. Adding
"Mark everything above as read", "Jump to oldest unread" and "Open a random
unread article" would make it more useful without new chrome.

Suggested fix: extend the command list and the underlying reader actions.

### A focus mode would fit the calm aesthetic

novel ideas — nice-to-have

`web/src/reader.ts` builds the content URL and the shell already supports
hiding the sidebar. A focus mode that dims the backdrop and centers the
article would be cheap.

Suggested fix: a toggle in the reader header that adds a class to the layout
and hides the nav.

### Reading time on river rows

novel ideas — nice-to-have

Derive a "~4 min" estimate from the stored text length and show it beside the
source name in `web/src/River.tsx`; it helps triage without adding counts.

Suggested fix: one content-length lookup at ingest plus a small formatter;
about half a day.

### One from your Kept shelf at the river's end

novel ideas — nice-to-have

When the session-end card appears (`web/src/River.tsx`), offer "One from your
Kept shelf" — a random kept article not opened in a while — as a small reward
for finishing.

Suggested fix: a client-side pick from the Kept view behind one button; about
half a day.

### A page-turn flourish for opening an article

novel ideas — nice-to-have

A short (200ms) translate/opacity transition from the list row to the reader
header when an article opens, disabled under `prefers-reduced-motion`, which
the app already honours globally.

Suggested fix: CSS transition on the reader open in `web/src/ArticleReader.tsx`
and `web/src/style.css`; about half a day.

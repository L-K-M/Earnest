# Earnest analysis

Unimplemented findings from the longcat-1, longcat-2, mimo-1, mimo-2,
mistral-large-2, mistral-1 and Argon reviews, ordered by severity within each
focus area. Every blocking and important finding from these reviews has an
open pull request against main, except the items noted inline whose files are
claimed by concurrent open PRs. Findings implemented in open PRs (including
mistral-1 PRs #70, #74, #77, #79, #80, #81, mimo-2 PRs #36, #51, #78, #82,
#83, #84, #85, #86, #87, #88, #89, #90, #91, Argon PRs #93, #94, #95, #96,
#97, #98, #99, #100 and concurrent PRs such as #19, #20, #22, #26, #32, #33,
#41, #42, #45, #49, #72, #73, #81, #85) are removed here and live in their
PR descriptions.

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
`docs/agents.md`. The screen already loads `agents.data` (principal id, name
and muted state) but forces a raw numeric id, and there is no inline
Mute/Unmute action outside an individual notification's detail.

Suggested fix: populate the principal select from the loaded agents list and
the action select from the documented vocabulary, and add inline Mute/Unmute
buttons; about half a day.

### Onboarding can reappear after the user already has feeds

usability — nice-to-have

`web/src/main.tsx:177` shows onboarding whenever `onboardingStarted` is true,
even after an OPML import has created subscriptions. A user who imports, opens
a sidebar view and returns to the river is greeted with "Welcome to Earnest".

Suggested fix: drop the `onboardingStarted` term, or use it only to suppress
onboarding after an import completed in this session; under an hour.

### The sign-in form has no pending state

usability — nice-to-have

`web/src/main.tsx:176` leaves the Sign in button enabled while the login
request runs, so a double click sends two login attempts that both count
toward the throttle window.

Suggested fix: disable the button (and show "Signing in…") until the request
settles; a few minutes.

### Android subscribe and device login report raw errors and cannot cancel

usability — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/AppViewModel.kt:266-273`
posts the URL field verbatim and surfaces raw API failures ("Earnest request
failed (400)"), while `LoginScreen.kt:43-71` polls `while(true)` honouring
`slow_down` but never `expires_in`, with no Cancel once polling starts and
the server field disabled: killing the app is the only escape. Suggested
fix: a client-side URL check like the Mac's, friendly errors, an expiry
deadline and a Cancel button; hours.

### A missing UnifiedPush distributor fails silently

usability — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/PushService.kt:90-96`
ignores `tryUseCurrentOrDefaultDistributor` failure and drops endpoints
lacking `pubKeySet`: the user gets no push and no explanation. Suggested
fix: a settings/status line ("push unavailable — install a UnifiedPush
distributor"); hours.

### A Mac deep link while signed out leaves a dead selection

usability — nice-to-have

`clients/mac/App/Sources/AppModel.swift:778-784` sets `selected` from
`openDeepLink` but `open()` returns early when logged out, and the `didSet`
never re-fires after sign-in, so the detail pane stays blank. Suggested fix:
re-open the pending selection once the session is established; hours.

### Dropped reading operations are easy to miss

usability — nice-to-have

`web/src/syncBridge.ts` (`onDropped`) lists rejected op IDs in one transient
banner; there is no way to inspect or retry dropped actions afterwards even
though the session end claims "Sync status shows any actions still waiting".
Suggested fix: keep dropped operations visible in the session end or a
status popover; half a day.

### The Mac palette has no keyboard handling

usability — nice-to-have

`clients/mac/App/Sources/EarnestApp.swift` (`PaletteView`) opens without
focusing the filter field, ignores Return (so the top match cannot be run)
and has no Cancel/Escape action to dismiss the sheet.

Suggested fix: add `@FocusState` to focus the field on appear, `.onSubmit` to
run the first enabled match, and a Cancel toolbar item with `.cancelAction`;
about an hour.

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
 The same 600 ms tick also PUTs the resume position with a fresh
`op_id` while rows are pending (`web/src/River.tsx:77`), so scrolling
grows the change log with position updates.
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

### Android stores the whole agent-change history in SharedPreferences

performance — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/SessionCache.kt:63-77`
(`refreshAgentChanges`) pages through every agent change since the last visit
(1000 per request, unbounded page count) and stores the full list as one JSON
string in SharedPreferences, which is parsed synchronously on read and grows
without bound.

Suggested fix: keep only the latest page window plus the cursor, and page the
Activity screen on demand; about an hour.

### pipelinePending still sweeps the entries table per pass

performance — nice-to-have

PR #84 bounds the idle pipeline scan rate (at most one sweep per 30s), but
each sweep is still a `NOT EXISTS`-filtered scan over `entries` with
per-entry consent/cache lookups
(`internal/store/embed_pipeline.go:35-43,136-142`). The durable fix from the
mimo-2 finding remains: a pending-work table written at ingest or a
processed-seq watermark (the "durable work queue" the code comment
describes), and batching the per-entry lookups with `id = ANY($1)`;
one to two days.

### The Mac Activity screen fetches the entire history

performance — nice-to-have

`clients/mac/EarnestKit/Sources/EarnestKit/API.swift:243-259` pages through
all activity (1000/page, unbounded) every time the Activity section
refreshes. Suggested fix: window or incremental fetch (`after_id`/`since`
like Android); hours.

### Android builds a fresh OkHttpClient per API instance

performance — nice-to-have

`clients/android/core/src/main/kotlin/ch/lkmc/earnest/core/EarnestApi.kt:42-53`
initialises `OkHttpClient()` per construction, and every `api()` call builds
a new client: separate connection pools and a TCP+TLS handshake per request
(login polling, each refresh). Suggested fix: share one lazily built client
in the companion; hours.

### Android recomputes river membership and re-parses raw JSON on the UI thread

performance — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:157-177,235`
and `River.kt:54-55` recompute `lists`/`subscriptionTitles`/`entryTitles`
per state change, re-parsing each subscription/mute `raw` JSON per check and
building O(n) maps inline during composition. Suggested fix: parse `raw`
once at merge time and derive lists off the main thread; hours to a day.

### The JSON Feed parser buffers every item before decoding

performance — nice-to-have

`internal/feed/jsonfeed.go:31-67` accumulates the whole `items` array as
`json.RawMessage` (a second full copy of a bounded 20 MiB input) before
decoding. Suggested fix: decode each raw item inside the token loop; hours.

### Gemini Embed sends one request per input text

performance — nice-to-have

`internal/llm/gemini.go:130` loops `callJSON(embedContent)` per text although
`:batchEmbedContents` exists; dormant today (single-text callers) but the
batch-shaped contract invites linear-latency callers. Suggested fix: use the
batch endpoint; hours.

### semantic_queries grows without bound per unique search

performance — nice-to-have

`internal/service/semantic.go:30-44` caches every distinct hybrid query and
Ask question (text + 1024-dim vector) forever; ad-hoc search text is
unbounded. Suggested fix: TTL/LRU eviction, or cache only watch-rule
descriptions; hours.

### Ask loads full source text per fused candidate

performance — nice-to-have

`internal/service/ask.go:82-95` issues one `SummaryContent` query per fused
candidate (up to ~400 sequential joins) on an interactive endpoint.
Suggested fix: batch with `e.id = ANY($1)`; hours.

### FeedsScreen parses subscription JSON on every recomposition

performance — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt`
(`FeedsScreen`) calls `Json.parseToJsonElement(subscription.raw)` while
composing each row, so every recomposition re-parses the same JSON.

Suggested fix: parse once with `remember(subscription.raw)` (or hold a parsed
model in the sync store) and read the fields from that; under an hour.

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

### Unread river rows are not visually distinguished

visual issues — nice-to-have

`web/src/River.tsx:113-121` marks unread only with the word "Unread" in the
small meta line, so scanning a long river for what is new is slow.

Suggested fix: bold the title or add a leading dot for unread rows; 30
minutes.

### The sidebar is a fixed 12 rem and cannot collapse

visual issues — nice-to-have

`web/src/style.css:12` gives the nav a fixed 12 rem column on desktop with no
way to collapse it, which wastes space on narrow windows.

Suggested fix: collapsible nav with the state remembered; about half a day.

### Mac entry rows clip at 44pt and the briefs column pops

visual issues — nice-to-have

`clients/mac/App/Sources/EntryTable.swift:42,74` fixes rows at 44pt while
the two-line title plus subtitle needs ~48pt (long titles clip), and
`SectionViews.swift:30-32` sizes the briefs ScrollView from a measured
height that starts at 0, collapsing and expanding on first render.
Suggested fix: automatic row heights (~50pt) and clip the briefs height
change; hours.

### Untitled Android rows render blank and the swipe snaps back without animation

visual issues — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:497` has no
"(untitled)" fallback (web and Mac do), and `EarnestApp.kt:544-553` snaps
`offset` back to 0 with no animation, the Later/Done zones differing only by
a word. Suggested fix: fallback title, animated snap-back, distinct icons or
colours per action; hours.

### Android system bars ignore the theme

visual issues — nice-to-have

`clients/android/app/src/main/AndroidManifest.xml:9` sets
`Theme.Material.Light` and nothing styles the status bar icons, so dark and
true-black themes sit under a light status bar with poor contrast.
Suggested fix: `enableEdgeToEdge()` with theme-aware system bar styling;
hours.

### The web sidebar shows no active state for dynamic views

visual issues — nice-to-have

`web/src/main.tsx` (nav) gives `aria-current` only to the five standard
views; feed, folder, label and saved-search buttons never show which view is
open, so the sidebar loses its place. Suggested fix: extend the
`aria-current` styling to the dynamic nav buttons; under an hour.

### Offline Android articles render without a viewport or image limits

visual issues — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/Reader.kt` serves the
offline `cachedHtml` as a raw fragment, so the WebView has no
`<meta name="viewport">`, no readable margins and no
`img { max-width: 100% }`. Wide images and pre blocks overflow horizontally.

Suggested fix: wrap `cachedHtml` in a minimal HTML5 document with a viewport
meta tag and responsive body/img/pre styles; about an hour.

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

### The Mac reader pins styles with 'unsafe-inline' while the web pins a hash

aesthetics and theming — nice-to-have

`clients/mac/App/Sources/Reader.swift:29` serves the article wrapper with CSP
`style-src 'unsafe-inline'`, while the web content origin pins the wrapper
style by SHA-256 (`internal/web/content.go:229`). Both are safe because the
sanitizer strips all style, but the Mac page accepts any inline style.

Suggested fix: hash the wrapper style in the Mac scheme handler like the web
content origin; about 30 minutes.

### index.html ships no icon, theme colour or manifest

aesthetics and theming — nice-to-have

`web/index.html` has no favicon (404s and a default tab icon), no
`theme-color` and no web app manifest: mobile browser chrome stays white
over dark themes and "add to home screen" gets the default icon. Suggested
fix: an icon set, a per-theme `theme-color`, and a minimal manifest; hours.

### The Android notification icon is generic

aesthetics and theming — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/Notifications.kt:63` uses
the generic `stat_notify_chat` icon rather than an app icon. (Open PR #100
replaced the Material 3 palette in `Theme.kt`.)

Suggested fix: use an app-icon notification icon; under an hour.

### Watch rules render raw ISO timestamps

aesthetics and theming — nice-to-have

`web/src/rules/Rules.tsx` renders `watch.last_match_at` as a raw RFC 3339
string (for example `2026-03-15T14:22:09Z`) while standard rules format
`last_match_at` with `toLocaleString()`.

Suggested fix: format `watch.last_match_at` with
`new Date(watch.last_match_at).toLocaleString()` when present; under 15
minutes.

## missing features

### River mode shows no per-feed unread signal

missing features — nice-to-have

`web/src/main.tsx:181` renders unread counts only in `unread_counts` mode; in
river mode the sidebar has no at-a-glance indication of which feeds have new
items.

Suggested fix: show a small unread dot/count per feed in river mode too (needs
the bounded count endpoint; open PRs #32 and #82 provide one).

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

### The web UI cannot assign labels to articles

missing features — nice-to-have

`web/src/ArticleReader.tsx:46` offers Done/Later/Keep/Unread only. The API
supports `PUT/DELETE /v1/labels/{id}/entries/{entry_id}`, and label views exist
in the sidebar, but no web control adds or removes a label, so labels can only
come from rules or agents and manual triage into labels is impossible on web.

Suggested fix: add a label picker to the reader actions; about half a day.

### Only the latest edition is reachable

missing features — nice-to-have

`web/src/pipeline/Pipeline.tsx:66-80` (`Edition`) fetches only
`/v1/editions/latest`, so past weekly editions cannot be re-read.

Suggested fix: list recent editions behind the edition view; 1-2 hours.

### No bulk triage anywhere

missing features — nice-to-have

The sync model has first-class ranges (`Through`) and the Google Reader
surface exposes mark-as-read, but no first-party UI does: no "mark
everything in this view seen/Done" or "mark through here" on web, Mac or
Android, so heavy inboxes have no catch-up escape hatch. Suggested fix: a
view-scoped action queueing a seen range at the current boundary; about a
day per surface.

### Entry enclosures are parsed but never shown

missing features — nice-to-have

`internal/feed/rss.go:110` and `internal/service/fetch.go:386` parse and
store podcast/media enclosures, but `service.Entry` has no enclosures field
and no client can show one: podcast episodes arrive as bare text entries.
Suggested fix: surface enclosures in the entry schema and render them as
links in the reader; about a day.

### Android Edition, feed health and mark-unread parity gaps

missing features — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:162-177`
sorts the Edition flat where the Mac groups by feed;
`EarnestApp.kt:563-594` (`FeedsScreen`) never surfaces
`health.consecutive_failures`/`error_message`; and there is no mark-unread
action. (Open PRs #68/#69 cover the reader header and activity wording.)
Suggested fix: group Edition per subscription, show feed health, and add an
Unread action; about a day.

### No way to manage native labels from the web UI

missing features — nice-to-have

The backend and OpenAPI expose full CRUD for `/v1/labels` and
`/v1/labels/{id}/entries/{entry_id}`, and `web/src/main.tsx` renders Labels in
the sidebar and the rules editor, but there is no UI to create, rename or
delete a label or to apply/remove one on an article.

Suggested fix: add a Labels section in settings and a label picker on the
article reader; about half a day.

### Saved searches cannot be renamed from the search panel

missing features — nice-to-have

`web/src/search/Search.tsx` can create and open saved searches and the
backend supports `PATCH /v1/saved-searches/{id}`, but the panel offers no
inline rename control, so a misnamed view can only be recreated. (Open PRs
#81 and #72 add delete and filter controls.)

Suggested fix: add a Rename action when viewing a saved search; about 1.5
hours.

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

### Mac windows mirror one shared reading state

bugs — important

`clients/mac/App/Sources/EarnestApp.swift:7-14` keeps one `@StateObject`
AppModel for the whole `WindowGroup`, so every window shares `selected`,
`html` and `readingEntry`: File > New Window produces an exact mirror and
two articles can never be read side by side. `DeepLinkTests` asserts that
New Window keeps multi-window support, so the single-window policy the
mimo-2 review first proposed would overturn tested behavior; the correct
fix is per-window selection/reader state over the shared engine (commands
need the key window's state), about one to two days. Open PR #91 instead
takes the single-window route (a `Window` scene) and updates `DeepLinkTests`
accordingly, flagged there as a product decision; if that PR is declined,
this per-window path is the remaining fix.

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

### Single-key actions fall back to the first article

bugs — nice-to-have

`web/src/River.tsx:84` makes `j`/`k`/`e`/`s`/`f`/`m` act on `entries[0]` when
no entry is selected, and also when the active article was opened from Search,
Ask, Briefs, Notifications or More Like This and is not in the loaded river
page (`findIndex` returns -1). A user who presses `e` while the list is still
loading silently marks the first article Done. `seen.current` is also never
cleared when an entry is marked unread or the view changes, so a returning
reader can see stale seen state.

Suggested fix: require a selection for single-key actions (keep `j`/`k`
navigation on the loaded entries) and clear `seen.current` on view change and
on unseen actions; about 2 hours.

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

### A feed without a title opens an empty river heading

bugs — nice-to-have

`web/src/main.tsx:181` navigates with `title: sub.title`, which is empty when
the feed has no title override, so the river `<h2>` renders blank. The command
palette (`main.tsx:133`) already falls back to `sub.title || sub.url`.

Suggested fix: use the same fallback in the sidebar button; 5 minutes.

### A cancelled Mac sync keeps running and overlaps the next one

bugs — nice-to-have

`clients/mac/App/Sources/SyncCoordinator.swift:19-25` clears `active` in a
`defer` when the awaiting call is cancelled, but the unstructured inner task
continues; the next `synchronize()` starts a second `synchronizeOnce`
concurrently, causing duplicate batch sends (the server dedupes by op_id)
and racy cache writes. Suggested fix: keep `active` bound to the task's real
completion, or make cancellation stop the work; hours.

### One corrupt cached row aborts the whole Mac offline restore

bugs — nice-to-have

`clients/mac/App/Sources/AppModel.swift:223-248` (`restoreCachedPresentation`)
throws through the whole map, so one undecodable cached entry aborts the
identity's offline launch instead of skipping the row. Suggested fix:
`compactMap` with per-row `try?` and a log; hours.

### Android re-queues seen writes for rows already seen

bugs — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:451-469`
(`MarkSeenOnRender`) queues a passive seen write for every rendered row with
no state check (the Mac guards `state.seen != "seen"`), so every Kept/Done/
Later row re-queues a write each session: outbox noise and redundant server
writes. Open PR #68 adds dwell-based marking but not this guard. Suggested
fix: skip when the effective state is already seen; hours.

### Android silently drops expired outbox actions

bugs — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/AppViewModel.kt:51,171-172`
populates `UiState.expired` but never renders it: locally expired reading
actions disappear without a word (the Mac reports "N queued action(s)
expired"). Suggested fix: surface the count in `cacheNotice` or a snackbar;
hours.

### The Android reader renders a blank page for non-HTTPS origins and a dead WebView after renderer death

bugs — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/Reader.kt:79-81,120-141`:
`allowed()` hard-requires `https`, so an `http` signed URL gets an empty 200
intercept with no error and no fallback; and `onRenderProcessGone` returns
true without recreating the WebView, leaving it dead. Suggested fix: accept
the signed URL's scheme and recreate the WebView after renderer death;
hours.

### The web reader offers an original link for entries without a URL

bugs — nice-to-have

`web/src/River.tsx` (keyboard `v`) and `web/src/ArticleReader.tsx:41` render
`window.open(entry.url)` and an "Open original" anchor even when the entry
URL is empty (`COALESCE(e.url,'')` in `internal/store/entry_queries.go:35`),
opening a blank tab or a dead link. Suggested fix: hide the link and skip
the shortcut when `entry.url` is empty; under an hour.

### A transient edition batch failure consumes a retry attempt

bugs — nice-to-have

`internal/service/edition_jobs.go:277-289` persists `BatchTried = true`
before checking the error, so a transport-level submit failure (timeout,
DNS) consumes one of three attempts; three unlucky submits fail an edition
the deadline fallback could have produced. Suggested fix: consume an attempt
only on accepted submissions, treating transport errors like
`ErrUnsupported`; hours.

### Deleted feeds and folders stay in the manage-feeds lists

bugs — nice-to-have

`web/src/FeedSettings.tsx` renders raw Dexie rows without filtering
`deleted`, so a subscription removed on another device (or a folder deleted
after a sync tombstone arrives) stays visible in the subscribe form, the
folder list and every subscription row until the next manual reload.

Suggested fix: filter `!sub.deleted` and `!folder.deleted` consistently across
`SubscribeForm`, `FolderSection`, `SubscriptionSection` and
`SubscriptionRow`; under an hour.

### Fetching one entry omits its cached blurb

bugs — nice-to-have

`internal/service/entries.go` (`Service.Entry`, `GET /v1/entries/{id}`) does
not call `attachBlurbs`, while `Service.Entries` and `Service.GetByIDs` do. A
client that opens a single entry sees `blurb: null` even when a Summaries
pipeline blurb exists for the entry's current content hash.

Suggested fix: call `attachBlurbs` in `Service.Entry` before returning; under
an hour.

### The reader keeps the previous article's typography draft and error

bugs — nice-to-have

`web/src/ArticleReader.tsx` initialises `overrideDraft` and `error` once, so
navigating between articles while the reader stays open keeps the previous
feed's typography draft and a stale action error banner. Changing theme,
measure or font also refetches `/web/content-url/{id}` even though those
parameters are applied client-side.

Suggested fix: reset `overrideDraft` and clear `error` when `entry.id` or the
settings change, and key the content-url query on `['content', entry.id]`;
under an hour.

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

### docs/llm-providers.md understates the budget guarantee

general issues — nice-to-have

`docs/llm-providers.md:117-123` says concurrent in-flight calls can
overshoot the monthly budget, but `internal/llm/budget.go` and
`internal/store/llm_spend.go` implement atomic 48-hour reservations that
count in-flight estimates. Suggested fix: describe reserve/settle semantics;
hours.

### The Android legacy-cache banner is undismissable

general issues — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/AppViewModel.kt:102` pins
the "older cache is retained" notice to every list forever once `earnest.db`
exists, with no acknowledgement path. Suggested fix: a dismiss action backed
by preferences; hours.

### Android openEntry swallows 401 like any other error

general issues — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/AppViewModel.kt:313-333`
maps all `api.content`/`api.contentUrl` failures to cache fallback with
`runCatching{}.getOrNull()`, so a revoked token silently serves stale content
instead of signing out (only `refresh()` checks `isUnauthorized`). Suggested
fix: rethrow `ApiException.isUnauthorized`; hours.

### Mac "While you were out" is fixed to a 24-hour window

general issues — nice-to-have

`clients/mac/EarnestKit/Sources/EarnestKit/API.swift:260` requests agent
changes since now-86400, so agent changes older than a day never reach Today
(Android persists the last-seen timestamp). Suggested fix: persist a
per-identity last-seen timestamp like Android's `KEY_CHANGES_SINCE`; hours.

### The image proxy buffers whole images and omits Referrer-Policy

general issues — nice-to-have

`internal/imageproxy/imageproxy.go` (`ServeHTTP`) reads the upstream body with
`io.ReadAll`, buffering up to 10 MB per image even though MIME and AVIF
sniffing only need the first 512 bytes. `/img/` responses also omit the
`Referrer-Policy: no-referrer` header that `/content/` and `/v1/` send.

Suggested fix: read at most 512 bytes for `http.DetectContentType` and
`isAVIF`, stream the rest with `io.Copy`, and set
`Referrer-Policy: no-referrer`; about 1.5 hours.

### Manual refresh and OPML import wait up to a poll interval

general issues — nice-to-have

`internal/service/events.go` (`RefreshSubscription`) and
`internal/service/opml.go` (`ImportOPML`) set `feeds.next_fetch_at` but never
call `Fetcher.Kick()`, so "Refresh now" and a deferred OPML import wait up to
`PollInterval` (60 s) before the scheduler wakes. `runRetention` in
`cmd/earnest/serve.go` also runs outside the worker `WaitGroup`, so
`db.Close()` can race an active retention sweep on shutdown.

Suggested fix: expose `Kick()` on the feed prober and invoke it after both
mutations commit, and launch `runRetention` via `workers.Go`; about 2 hours.

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

### The caught-up moment could name today's feeds

novel ideas — nice-to-have

The session-end card (`web/src/River.tsx:126`) shows counts only. Naming the
feeds that contributed the items read this session would make the "that is
everything" moment feel earned and point at what to subscribe to more or less.

Suggested fix: group the session's seen entries by subscription title in the
end card; 1-2 hours.

### Hint at swipe gestures on first Android run

novel ideas — nice-to-have

`clients/android/.../EarnestApp.kt` wires horizontal swipe gestures to
Done/Later but nothing tells a new user they exist.

Suggested fix: a one-time hint (or a short gesture cheat-sheet in the
overflow menu); about 30 minutes.

### Prefetch reading content for the top of Today during sync

novel ideas — nice-to-have

Both native clients cache only already-opened articles (the web service
worker prefetches visited pages). Fetching the first N river entries'
content after a pull would make offline opens instant for the most likely
next reads; days.

### Right-swipe Keep and a long-press menu on Android

novel ideas — nice-to-have

The gesture lane after #68's Later/Done swipes has room for Keep, and a
long-press context menu is a natural home for mark-unread and per-feed
actions; hours.

### An optional reading time box

novel ideas — nice-to-have

`docs/research/ux.md` sketched a dismissible "Twenty minutes" box that ends
at time or at the end of the river, whichever comes first. Deliberately
deferred in the research, but it is the one idea there that nothing else in
the market does; about a day.

### Per-article "Load images" on Android

novel ideas — nice-to-have

`clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApi.kt:174` pins
`contentUrl` to `mode=click`; a Mac-style "Load Images" control would give
parity where images are wanted; hours.

### "Catch me up since..." horizon slider and a newspaper colophon

novel ideas — nice-to-have

When a reader returns after a busy week, a lightweight "horizon" control on
the river header could fold older Today items into a printable weekly Edition
digest with an editorial colophon (reading time, top authors, quietest feeds),
turning backlog guilt into a calm finite ritual.

Suggested fix: add a colophon summary card and a "Move older than 24h to
Edition" batch action in `web/src/River.tsx`; about a day.

### Agent dry-run postcard and provenance diff in the reader margin

novel ideas — nice-to-have

When an agent has annotated, scored or triaged an article, a subtle margin
"postcard" in the reader could show the agent's note and score with a
one-click "Agree / Revert and teach rule" action, connecting Earnest's
governed-agent model directly to the reading experience.

Suggested fix: query `/v1/annotations?entry_id={id}` in
`web/src/ArticleReader.tsx` and render an expandable margin note with one-click
revert/rule prefill; about a day.

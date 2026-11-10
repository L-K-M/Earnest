# Reconciled backlog

Assessment baseline: `f8adbdb`, 2026-11-10. All 134 headings from the
[original analysis](original-analysis.md) were checked by LongCat 2.5 helpers.
The assessments are: 41 implemented, 92 still open and
1 no longer applicable.

These are code-reading assessments, not 134 independently reproduced bugs.
An open item may be a product idea, a partial implementation or an unverified
claim. It is not automatically approved for implementation. Important defects
selected for this batch and their regression evidence live in
[findings.md](findings.md). New features and redesigns remain deferred.

Paths and line numbers below refer to the frozen baseline. Concurrent PRs
and fixes from this review can supersede them; the findings record links
those changes. The historical prose is preserved rather than silently
rewritten.

## Usability

### B1-1: River rows show no timestamp

Assessment: **implemented**.

Evidence: web/src/River.tsx:348 (web/src/format.ts:8).

The meta line now renders <time dateTime={date} title={longDate(date)}>{shortAge(date)}</time> from published_at||first_seen_at; shortAge gives 'now'/'N min'/'N h'/a weekday, matching the Android relative style.

### B1-2: Subscription rows are dense when managing many feeds

Assessment: **implemented**.

Evidence: web/src/FeedSettings.tsx:400,392-399,429.

SubscriptionRow now keeps the title form and cadence/folder/extract selects inside a <details class="feed-edit"> that is closed unless the feed is failing; health is a muted summary line (healthSummary) rather than an alert stacked above the actions.

### B1-3: Feed health timestamps are absolute and timezone-dependent

Assessment: **open**.

Evidence: web/src/FeedSettings.tsx:368-370.

when() still formats last_attempt_at/last_success_at/next_fetch_at with date.toLocaleString() in the browser zone; no relative age is shown for feed health (only River rows use shortAge).

### B1-4: The notifications release button is always visible

Assessment: **open**.

Evidence: web/src/agents/Surfaces.tsx:143.

Notifications renders the 'Release current notifications' button unconditionally; Surfaces.test.tsx:47-52 clicks it with no withheld-generation state, and nothing labels it with the current generation.

### B1-5: Onboarding blocks Continue while measuring feed volume

Assessment: **open**.

Evidence: web/src/Onboarding.tsx:127,121.

Continue is still disabled={busy || checking} while measure() polls (HEALTH_POLLS=8 x 2500 ms); the checking message has no live count of feeds still being measured, and the report only shows the pending count after checking stops.

### B1-6: Saving settings gives no success feedback

Assessment: **implemented**.

Evidence: web/src/Settings.tsx:55,32,46.

save() sets a saved flag and the form renders <span role="status">Saved.</span> when the draft is not dirty; any change clears it.

### B1-7: Typography changes do not refresh an open article

Assessment: **implemented**.

Evidence: web/src/ArticleReader.tsx:34,105.

The content query key includes typography.theme/measure/font and renderBody calls readerURL(url, typography, dark), so a typography change refetches the content URL and re-renders the frame with the new typography.

### B1-8: Sidebar feeds ignore their folders

Assessment: **implemented**.

Evidence: web/src/Sidebar.tsx:73,104-116.

groupByFolder feeds are rendered as folder groups with a disclosure and a nested nav-list of feed links; unfiled feeds follow the groups.

### B1-9: The rules editor's multi-selects are clumsy

Assessment: **open**.

Evidence: web/src/rules/PredicateEditor.tsx:35,37.

feed_in/folder_in and classifier_in still use native <select multiple>; there is no checkbox list or token input.

### B1-10: Destructive actions use browser confirm dialogs

Assessment: **open**.

Evidence: web/src/FeedSettings.tsx:352,421,473.

Folder delete, unsubscribe and clear-credentials still call window.confirm; the app's confirmAction in App.tsx:455 is also window.confirm, not the styled dialog.

### B1-11: An expired session surfaces as scattered request errors

Assessment: **open**.

Evidence: web/src/request.ts:18-24.

fetchResponse only throws RequestError(status) with no 401 handling; App.tsx handles 401 only in login (513) and logout (523-530), and the service worker's 401 purge (offline/worker.ts:70,129) does not return to the login screen.

### B1-12: The Android Activity screen is blank on empty or error

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:1275-1305; AppViewModel.kt:1173-1176.

ActivityScreen renders only the grouped rows; model.activity swallows failures with getOrNull(), so an empty or failed load is a blank list with no empty state, error text or retry.

### B1-13: Android unsubscribe fires without confirmation

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:1256,414.

FeedRow's Unsubscribe TextButton calls onUnsubscribe directly (model.unsubscribe at AppViewModel.kt:1093) with no confirmation dialog; the cadence chips are now present.

### B1-14: Android activity rows can name nothing and cannot cancel

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/AgentChanges.kt:125-128; EarnestApp.kt:1299.

The 'Entry #null' fallback is gone (agentChangeText falls back to 'an entry'/'N articles'; ActivityDescriptionTest.kt:57), but only next_action == 'revert' gets a button: 'cancel' still prints 'Cannot revert this change' and no Android code calls the notification-cancel endpoint.

### B1-15: Android reuses river copy for every view

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:881-883.

EntryListScreen's end item still uses 'Nothing new. A rare and beautiful thing.' for every empty view and 'That is everything since <day>' for every non-empty view.

### B1-16: River passive-read behaviour is invisible and irreversible

Assessment: **open**.

Evidence: web/src/River.tsx:198; web/src/App.tsx:327.

Passive seen writes still happen with no toast or undo (notify is only for kind==='explicit'), and the river header has no 'Reading marks articles read' hint; the session-end copy still counts passive marks as choices.

### B1-17: The Activity screen filters are raw protocol fields

Assessment: **open**.

Evidence: web/src/agents/Surfaces.tsx:129.

The Agent select is now populated from agents.data, but Action is still a free-text <input> (not a documented-vocabulary select) and the Agents list only prints Muted/Unmuted with no inline Mute/Unmute button.

### B1-18: Onboarding can reappear after the user already has feeds

Assessment: **open**.

Evidence: web/src/App.tsx:551,560.

firstRun still ORs onboardingStarted, which is set by any change/click on the onboarding screen; after an OPML import the user can leave and return to the river and be shown step 1 'Welcome to Earnest' again.

### B1-19: The sign-in form has no pending state

Assessment: **implemented**.

Evidence: web/src/Login.tsx:9,24,28.

Login keeps a pending state, sets aria-busy on the form, disables the button and labels it 'Signing in…' until the request settles.

### B1-20: Android subscribe and device login report raw errors and cannot cancel

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/AppViewModel.kt:1084-1091; LoginScreen.kt:63-87,144.

subscribe still posts the URL verbatim and reports failureText('Could not subscribe', it); LoginScreen polls while(true), honours slow_down but never expires_in, and offers no Cancel once authorization is set (the server field is disabled).

### B1-21: A missing UnifiedPush distributor fails silently

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/PushService.kt:90-96,38.

register() only calls UnifiedPush.register when tryUseCurrentOrDefaultDistributor reports success, and onNewEndpoint still drops endpoints with no pubKeySet; no settings/status line explains that push is unavailable.

### B1-22: A Mac deep link while signed out leaves a dead selection

Assessment: **implemented**.

Evidence: clients/mac/App/Sources/WindowModel.swift:776-784,398-404; AppModel.swift:187-190.

openDeepLink stores pendingDeepLink when the engine is not ready, rebuildPresentation re-opens it once logged in, and connect() defers it across a reconnect; MacBehaviorTests.swift:93-103 covers the deferred link.

### B1-23: Dropped reading operations are easy to miss

Assessment: **open**.

Evidence: web/src/syncBridge.ts:39-41; web/src/App.tsx:610.

onDropped still only sets sync.error, shown in the single top banner with a Retry sync button; dropped operations are not kept in the session end or a status popover, and Retry sync does not replay the dropped op (acknowledge removes it from the outbox).

### B1-24: The Mac palette has no keyboard handling

Assessment: **implemented**.

Evidence: clients/mac/App/Sources/Palette.swift:11,29,62-70.

PaletteView focuses the filter on appear (@FocusState), runs the highlighted enabled match on Return (.onSubmit and .onKeyPress(.return)), and dismisses on Escape; there is no toolbar Cancel item.

## Performance

### B2-1: The passive-seen observer is rebuilt on every scroll frame

Assessment: **implemented**.

Evidence: web/src/River.tsx:190-221; web/src/River.test.tsx:43-52.

River.tsx no longer creates an IntersectionObserver; the 600 ms interval effect is keyed on [key] and runs once per view, and the test now asserts rows are not marked merely for rendering. The suggested Map lookup is still absent (entriesRef.current.find at River.tsx:192), but the scroll-frame rebuild defect is gone.

### B2-2: The river re-checks freshness on every action

Assessment: **open**.

Evidence: web/src/App.tsx:321; web/src/River.tsx:288.

App.tsx still builds the actions object as a fresh literal on every render, and River's keydown effect still lists actions in its dependency array, so the listener re-binds on each render. The palette's ref pattern (web/src/palette/Palette.tsx:49-50) is not applied here.

### B2-3: The Activity and Notifications screens load entire histories

Assessment: **open**.

Evidence: web/src/agents/Surfaces.tsx:16-26,87,137.

useActivities and the Notifications list still loop while (more), accumulating every 1000-item page in memory, and ActivityRows still groups with an array spread per row (O(n^2)).

### B2-4: Every session request rewrites the sessions row

Assessment: **open**.

Evidence: internal/service/session.go:61.

ResolveSession still issues an unconditional UPDATE of expires_at on every call (web requests at internal/api/api.go:164 and SSE heartbeats at internal/api/sync.go:129). No remaining-lifetime threshold; compare the throttled token write at internal/service/token.go:249.

### B2-5: The Mac table redraws on every state change

Assessment: **implemented**.

Evidence: clients/mac/App/Sources/EntryTable.swift:100-107.

updateNSView now calls table.reloadData() only when coordinator.items != newItems; selection-only updates just re-select the row, so article loads, syncs and pending-op changes no longer redraw the whole list.

### B2-6: The Android article cache grows without bound

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/AppViewModel.kt:1152; clients/android/app/src/main/java/ch/lkmc/earnest/db/Daos.kt:33.

openEntry still saves every opened article's HTML to the contents table with no eviction, and ContentDao.delete has no callers anywhere in the app.

### B2-7: Sync bootstrap reloads and rewrites the whole IndexedDB snapshot per page

Assessment: **implemented**.

Evidence: web/src/sync/runtime.ts:172-175,637-646.

bootstrapFlushPages (8) buffers pages and flushBuffer applies one mutate per batch, so the reload/apply/persist cycle now runs every 8 pages instead of per page.

### B2-8: SyncClient.state scans the whole outbox for every visible row

Assessment: **open**.

Evidence: web/src/sync/client.ts:453; web/src/River.tsx:334.

SyncClient.state still loops over this.pending for every call, and River calls actions.state(entry) once per visible row on every render (plus the unread filter at River.tsx:304).

### B2-9: ArticleReader refetches the signed content URL on every open

Assessment: **open**.

Evidence: web/src/ArticleReader.tsx:34.

The content query still sets gcTime:0, so the signed URL is discarded on unmount and refetched on every open; no expiry-checked cache exists.

### B2-10: Android openEntry always fetches raw content even when the signed URL works

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/AppViewModel.kt:1150,1163.

openEntry still calls api.content(entryId) unconditionally before api.contentUrl(entryId), so every open costs two requests even when cached HTML exists.

### B2-11: Android stores the whole agent-change history in SharedPreferences

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/SessionCache.kt:96-98,101-120.

refreshAgentChanges still pages through every change since the cursor (1000 per page, unbounded) and stores the full list as one JSON string; agentChanges() parses it synchronously on read.

### B2-12: pipelinePending still sweeps the entries table per pass

Assessment: **open**.

Evidence: internal/store/embed_pipeline.go:35-43.

pipelinePending still runs a NOT EXISTS-filtered scan over entries with a LATERAL contents lookup per entry; there is no pending-work table or processed-seq watermark.

### B2-13: The Mac Activity screen fetches the entire history

Assessment: **open**.

Evidence: clients/mac/EarnestKit/Sources/EarnestKit/API.swift:456-472; clients/mac/App/Sources/AppModel.swift:630.

EarnestAPI.activity() still pages through all activity (1000 per page, unbounded while loop) and AppModel.refreshActivity calls it on every Activity refresh.

### B2-14: Android builds a fresh OkHttpClient per API instance

Assessment: **open**.

Evidence: clients/android/core/src/main/kotlin/ch/lkmc/earnest/core/EarnestApi.kt:43; clients/android/app/src/main/java/ch/lkmc/earnest/AppViewModel.kt:136-138.

EarnestApi still defaults to a new OkHttpClient() per construction, and the apiFactory creates a new EarnestApi (and thus a new connection pool) on every call.

### B2-15: Android recomputes river membership and re-parses raw JSON on the UI thread

Assessment: **open**.

Evidence: clients/android/core/src/main/kotlin/ch/lkmc/earnest/core/River.kt:54-55; clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:431,443,1248.

River.fields() still parses each subscription/mute raw JSON on every contains() check, and EarnestApp still builds entryTitles inline per composition and calls River.cadence(subscription) in FeedRow. Membership lists are now remembered and derived on Dispatchers.IO (EarnestApp.kt:281, AppViewModel.kt:219), but the parse-once-at-merge fix is not in place.

### B2-16: The JSON Feed parser buffers every item before decoding

Assessment: **open**.

Evidence: internal/feed/jsonfeed.go:31,67,115.

parseJSONFeed still accumulates every item into itemRaws []json.RawMessage and only decodes them in a second pass after the walk.

### B2-17: Gemini Embed sends one request per input text

Assessment: **open**.

Evidence: internal/llm/gemini.go:135-144.

Embed still loops over req.Texts issuing one :embedContent call per text; the batch endpoint is unused.

### B2-18: semantic_queries grows without bound per unique search

Assessment: **open**.

Evidence: internal/service/semantic.go:30-44; internal/store/embed_pipeline.go:111-121.

semanticVector still caches every distinct description forever via SemanticQueryPut, and no TTL/LRU eviction or cleanup exists.

### B2-19: Ask loads full source text per fused candidate

Assessment: **open**.

Evidence: internal/service/ask.go:82-95.

Ask still issues one SummaryContent query per fused candidate in a loop; askSourceLimit (8) bounds the sources kept but not the number of sequential queries.

### B2-20: FeedsScreen parses subscription JSON on every recomposition

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:1212-1248 (FeedRow, called from FeedsScreen:1160).

FeedRow still calls subscription.iconUrl(), subscription.raw.url(), subscription.health() and River.cadence(subscription) during composition with no remember(subscription.raw).

## Visual issues

### B3-1: The reader column can be squeezed between 700 px and 1050 px

Assessment: **open**.

Evidence: web/src/style.css:234,413-438; web/src/App.tsx:131-132,562-569.

The cited `.workspace.with-reader` rule is gone, but `.panes` is still a two-column grid above 759 px (style.css:234,424-438), so between 759 and 1050 px the reader stays beside the list (about 328 px at 760 px) rather than going full width. The 1099 px drawer only hides the sidebar; it does not give the reader the full width earlier.

### B3-2: Sidebar `<details>` are hard-coded open

Assessment: **implemented**.

Evidence: web/src/Sidebar.tsx:62-71,106-113.

Folder sections are no longer hard-coded `<details open>`; a disclosure button toggles them (aria-expanded) and the open set is persisted in localStorage under `earnest.sidebar.collapsed`, which is the alternative the item allowed.

### B3-3: The mobile sidebar can take a third of the screen

Assessment: **implemented**.

Evidence: web/src/style.css:413-423,424-438; web/src/App.tsx:131,595-603; docs/web.md:116.

Below 1099 px the sidebar is a `dialog.drawer` behind the top bar instead of an always-visible 30 dvh box, and below 759 px the list and reader share one pane, so the reader gets the full height. docs/web.md:116 records this layout.

### B3-4: Switching views scrolls the workspace to the heading

Assessment: **implemented**.

Evidence: web/src/River.tsx:154-158.

The new view's `<h2>` is still focused on a view change, but now with `focus({preventScroll: true})`, so the workspace no longer scrolls to the heading. The focus still happens on pointer-driven changes, but the scrolling defect is gone.

### B3-5: The article iframe has a hard 75 dvh height

Assessment: **implemented**.

Evidence: web/src/ArticleFrame.tsx:47-77; web/src/style.css:321; docs/research/decisions.md (The article frame runs one script of Earnest's own).

The frame is sized to the height its own page reports over postMessage (tested by ArticleFrame.test.tsx 'sizes itself to the height its own page reports'), and style.css:321 fills the pane with `height: 100cqb` until then, removing the hard 75 dvh and the double scrollbar.

### B3-6: River rows print raw register values

Assessment: **implemented**.

Evidence: web/src/River.tsx:35-39,346-348; web/src/River.test.tsx:168-174.

`shelfLabel` maps the register values to 'Done'/'Later', the meta line appends 'Kept' and a screen-reader-only 'Unread', and River.test.tsx:168-174 asserts the title-case labels.

### B3-7: Palette key hints are all the same

Assessment: **implemented**.

Evidence: web/src/App.tsx:475,491-501; web/src/Sidebar.tsx:93; web/src/App.tsx:617.

Commands now carry their real keys (`g t`, `e`, `s`, `f`, `m`, `z`, `/`, `g r`, …) or none (the mark-rest command's default hint is overridden to `key: undefined` at App.tsx:492). The sidebar palette button is icon-only with sr-only 'Command palette', and the long-labelled trigger is not rendered (`triggers={false}`).

### B3-8: The Android widget ignores river state and theme

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/EarnestWidget.kt:47-79,101-106.

`WidgetData.headlines` still takes the five highest-seq non-deleted snapshot entries with no river or read-state filter, so Done items can still headline it, and `Headlines` still hardcodes `Color(0xFFFFFFFF)` background and `Color(0xFF101418)` text instead of GlanceTheme colours. docs/android.md:176 describes the widget as 'newest five headlines', but no decision in docs/research/decisions.md replaces the filtering or theming.

### B3-9: Unread river rows are not visually distinguished

Assessment: **implemented**.

Evidence: web/src/style.css:263-264; web/src/River.tsx:339,346.

Unread rows now get a bold ink title (`.entry[data-unread] .entry-title`) plus a leading `.unread-dot`, rendered by River.tsx for rows whose seen state is not 'seen' and shelf is not 'done'.

### B3-10: The sidebar is a fixed 12 rem and cannot collapse

Assessment: **open**.

Evidence: web/src/style.css:79,177,413-423.

The nav is still a fixed column on desktop (`--sidebar: 15.5rem`, style.css:79,177) with no collapse control; only below 1099 px does it become a drawer. The suggested collapsible nav with remembered state was not added.

### B3-11: Mac entry rows clip at 44pt and the briefs column pops

Assessment: **open**.

Evidence: clients/mac/App/Sources/EntryTable.swift:185-192,450-458; clients/mac/App/Sources/SectionViews.swift:12,40-43.

Entry rows are no longer fixed at 44 pt: `heightOfRow` measures each row through `EntryCell.height(for:width:)`, so long titles no longer clip. The briefs column still sizes its ScrollView from a measured `contentHeight` that starts at 0, so it still collapses and expands on first render.

### B3-12: Untitled Android rows render blank and the swipe snaps back without animation

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:950,1018-1024.

`EntryRow` still renders `Text(entry.title)` with no '(untitled)' fallback (web and Mac both have one), and `SwipeActionRow` still sets `offset = 0f` on release with no animation. Only the distinct Later/Done reveal colours (amber vs green) are new.

### B3-13: Android system bars ignore the theme

Assessment: **implemented**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/Theme.kt:245-252,257-263,274-277; clients/android/app/src/main/AndroidManifest.xml:9; clients/android/app/src/test/java/ch/lkmc/earnest/ThemeTest.kt:66-72,100-101.

The activity now calls `enableEdgeToEdge` with `SystemBarStyle.auto` driven by `barIcons(scheme)` in a SideEffect, so the status and navigation bar icons follow the effective theme; ThemeTest covers light, dark, black, e-ink and system.

### B3-14: The web sidebar shows no active state for dynamic views

Assessment: **implemented**.

Evidence: web/src/Sidebar.tsx:64,78,85,111,121,125; web/src/style.css:205.

`current(key)` feeds `aria-current=page` for feed, folder, label and saved-search links, and `routeSection` (routes.ts:187-192) produces the matching keys; style.css:205 styles `.nav-link[aria-current=page]`.

### B3-15: Offline Android articles render without a viewport or image limits

Assessment: **implemented**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/Reader.kt:206-215; clients/android/app/src/test/java/ch/lkmc/earnest/ReaderTest.kt:177-200.

`offlineReaderHtml` wraps `cachedHtml` in a full HTML5 document with `<meta name="viewport" content="width=device-width,initial-scale=1">`, and `readerCss` adds `img{max-width:100%;height:auto}` and `pre{overflow:auto}`; ReaderTest exercises the fallback document.

## Aesthetics and theming

### B4-1: The eink theme is high-contrast black on white

Assessment: **not applicable**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/Theme.kt:208; clients/mac/EarnestKit/Sources/EarnestKit/ReaderSettings.swift:10; CHANGELOG.md:119.

The palette is still #fff/#000 (web/src/style.css:85, internal/web/content.go:305), but it is a deliberate design: the Mac and Android code call e-ink the high-contrast variant, Theme.kt says 'E-ink is deliberately monochrome and high contrast', and the CHANGELOG records 'e-ink stays black and white'. The item was never a defect.

### B4-2: The sign-in screen is a bare form

Assessment: **implemented**.

Evidence: web/src/Login.tsx:23; web/src/style.css:170.

Login renders the Brand lockup, and .login form is now a bordered card using var(--surface)/var(--line) with radius and shadow.

### B4-3: The palette has no accent colour

Assessment: **implemented**.

Evidence: web/src/style.css:22; web/src/style.css:104; web/src/style.css:260.

Every theme defines --brand and six tones; links, ::focus-visible, ::selection and aria-current use it, while hover (--hover), selected (--selected) and pressed (--brand-soft) are distinct.

### B4-4: The dyslexia-friendly choice has no preview

Assessment: **open**.

Evidence: web/src/Settings.tsx:64.

The dyslexia option is still a plain select with no preview paragraph; web/src has no settings preview/specimen, and ArticleReader applies typography only after saving.

### B4-5: Typography settings mix app-wide and article-only scopes

Assessment: **open**.

Evidence: web/src/Settings.tsx:61.

The Typography fieldset still groups Theme (app-wide, set on <html> in App.tsx:400) with Measure and Font (article-only) under one legend, with no scope annotation.

### B4-6: The Mac reader pins styles with 'unsafe-inline' while the web pins a hash

Assessment: **open**.

Evidence: clients/mac/App/Sources/Reader.swift:77; internal/web/content.go:327.

The Mac scheme handler still serves style-src 'unsafe-inline'; the web content origin pins its CSS by SHA-256, and no hash was added on the Mac side.

### B4-7: index.html ships no icon, theme colour or manifest

Assessment: **implemented**.

Evidence: web/index.html:1; web/public/manifest.json:1; web/src/App.tsx:402.

index.html now links favicon-32.png, icon-192.png, apple-touch-icon.png and manifest.json, declares light/dark theme-color metas, and App.tsx updates them per theme.

### B4-8: The Android notification icon is generic

Assessment: **implemented**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/Notifications.kt:73; clients/android/app/src/test/java/ch/lkmc/earnest/NotificationsTest.kt:67.

Notifications.show uses R.drawable.ic_notification, generated from the app glyph (media-sources/glyph.svg), and usesTheAppGlyphAsTheStatusBarIcon pins it.

### B4-9: Watch rules render raw ISO timestamps

Assessment: **open**.

Evidence: web/src/rules/Rules.tsx:145.

The watch list still renders `Last match: {watch.last_match_at ?? 'Never'}` as the raw RFC 3339 string; only the ordinary rules list at line 46 uses toLocaleString().

## Missing features

### B5-1: River mode shows no per-feed unread signal

Assessment: **open**.

Evidence: web/src/App.tsx:203,557; web/src/Sidebar.tsx:72; docs/web.md:90.

The counts query is enabled only when reading_mode === 'unread_counts' and the sidebar is passed counts only in that mode, so river mode still shows no per-feed unread signal; docs/web.md still says only unread-count mode requests and displays totals.

### B5-2: Unread counts cover feeds but not folders or labels

Assessment: **open**.

Evidence: web/src/counts.ts:3,12; internal/service/entries.go:141-147; web/src/Sidebar.tsx:107,113.

readUnreadCounts and service.UnreadCounts still return only by_subscription/later/kept; the sidebar now sums a folder total client-side (Sidebar.tsx:107) but label rows (Sidebar.tsx:113) still carry no count, and the suggested server-side extension was not made.

### B5-3: Manage feeds has no filter

Assessment: **open**.

Evidence: web/src/FeedSettings.tsx:357-365.

SubscriptionSection maps every subscription (grouped only by folder) with no filter state or input; grepping FeedSettings.tsx for 'filter' matches only the mutes list, and the 'Make feed management scannable' work added no filter box.

### B5-4: The reader has no font-size control

Assessment: **open**.

Evidence: web/src/Settings.tsx:60-66; web/src/reader.ts:45-51; web/src/api.generated.ts:1771-1783.

The Typography fieldset still offers only theme, measure and font, and readerURL sets only those; the ReaderSettings schema has no font-size field. Android has a device-local text zoom, but the cited web surface has none.

### B5-5: The reader has no focus mode, text-to-speech or share action

Assessment: **implemented**.

Evidence: web/src/ArticleReader.tsx:39,51-58,93.

The item's fix was to pick one of the three and ship it; a Share action using navigator.share now ships in the web reader toolbar (and in Android's reader header, ReaderTest.kt:257). Focus mode and text-to-speech are still absent.

### B5-6: Native clients cannot search, manage rules or change settings

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/SearchScreen.kt; clients/mac/App/Sources/SearchView.swift; clients/android/.../SettingsScreen.kt; clients/mac/App/Sources/SettingsView.swift; clients/android/core/.../EarnestApi.kt:352.

Native search (with saved views) and reader-settings screens now exist on both clients, but there is still no rules or Ask UI, Mac has no notifications surface, and positions are only read: EarnestApi.kt:352 defines putPosition but no app code calls it.

### B5-7: Verified citations cannot highlight in the article frame

Assessment: **open**.

Evidence: web/src/pipeline/Pipeline.tsx:37; web/src/ArticleFrame.tsx:34-38; internal/web/content.go:160-171.

Summary still renders 'Highlighting is unavailable in the article frame. Passage: …', and the frame protocol carries only height and key messages; frameScript has no highlight or scroll-to-text path.

### B5-8: Search snippets do not mark their matches

Assessment: **open**.

Evidence: web/src/search/Search.tsx:34; internal/service/search.go:99.

Matches renders match.snippet as plain text and the server builds it with StartSel=""/StopSel="", so no query term is marked client- or server-side.

### B5-9: The web UI cannot assign labels to articles

Assessment: **open**.

Evidence: web/src/ArticleReader.tsx:85-95; web/src/menus.ts:68-79.

The reader toolbar and article context menu offer only Done/Later/Keep/Mark unread/Open original/Copy link/Share/Typography; /v1/labels/{id}/entries/{entry_id} is never called from web/src, so labels still come only from rules, agents or import.

### B5-10: Only the latest edition is reachable

Assessment: **open**.

Evidence: web/src/pipeline/Pipeline.tsx:69; api/openapi.json paths /v1/editions, /v1/editions/latest.

Edition still fetches only /v1/editions/latest; the OpenAPI exposes no edition list route (only POST /v1/editions and GET /v1/editions/latest), so past weekly editions cannot be opened even though service.editionByID can read one by id.

### B5-11: No bulk triage anywhere

Assessment: **open**.

Evidence: web/src/markRest.ts:17; web/src/menus.ts:41,87; docs/research/decisions.md:40,44.

The web now queues seen ranges via the palette command and the sidebar/article context menus, but clients/mac and clients/android still have no mark-all-as-read or mark-through action, so the item's 'web, Mac or Android' scope is only partly met.

### B5-12: Entry enclosures are parsed but never shown

Assessment: **open**.

Evidence: internal/service/entries.go:82-100; internal/syncmodel/types.go:149-157; api/openapi.json Entry schema.

service.Entry, the REST Entry schema and syncmodel.Entry all lack an enclosures field; enclosures are parsed and stored (internal/service/fetch.go:534, internal/store/sync_queries.go:194) but no web, Mac or Android client decodes or renders them.

### B5-13: Android Edition, feed health and mark-unread parity gaps

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:291,1223,544; clients/android/.../AppViewModel.kt:908.

Feed health is now surfaced ('Fetch failing: …', EarnestApp.kt:1223) and Mark unread exists in the list and reader (EarnestApp.kt:544, AppViewModel.kt:908), but Edition is still a flat seq-sorted list (EarnestApp.kt:291) with no per-feed grouping.

### B5-14: No way to manage native labels from the web UI

Assessment: **implemented**.

Evidence: web/src/labels/LabelSettings.tsx:46-47,75-87; web/src/SettingsPage.tsx; docs/web.md:121-125.

Settings → Native labels now creates, renames (PATCH /v1/labels/{id}) and deletes label definitions, refreshing the sidebar and rule choices. Applying/removing a label on an article is still missing, which is the separate article-assignment item.

### B5-15: Saved searches cannot be renamed from the search panel

Assessment: **implemented**.

Evidence: web/src/search/Search.tsx:115-130; web/src/search/Search.test.tsx:81.

The saved-search panel now has a Rename form that PATCHes /v1/saved-searches/{id} and invalidates the sidebar cache; covered by 'renames the saved search explicitly and invalidates the sidebar cache' and the failure/blank-name cases.

## Bugs

### B6-1: Article iframes let popups escape the sandbox onto the app origin

Assessment: **open**.

Evidence: web/src/ArticleFrame.tsx:77 (asserted by web/src/ArticleFrame.test.tsx:52).

The iframe moved from main.tsx to ArticleFrame.tsx but still renders sandbox="allow-scripts allow-popups allow-popups-to-escape-sandbox". decisions.md only records adding allow-scripts and never allow-same-origin; allow-popups-to-escape-sandbox is unchanged and test-asserted.

### B6-2: Mac windows mirror one shared reading state

Assessment: **implemented**.

Evidence: clients/mac/App/Sources/MainWindow.swift:17; clients/mac/App/Tests/WindowTests.swift:40 testWindowsHoldDifferentSectionsSelectionsAndReaders.

WindowRootView now creates one WindowModel per window, and the test asserts two windows hold different selected/readingEntry/html. decisions.md also records 'Mac windows are independent'.

### B6-3: Google Reader `s=` include filters are AND-ed instead of unioned

Assessment: **open**.

Evidence: internal/compat/greader/streams.go:187-191; internal/service/entries.go:593-596.

Repeated s= values still build a conjunctive require list, both in the in-memory recheck (pass=false on any non-match) and as AND-ed SQL conditions, so s=read&s=starred is an intersection.

### B6-4: A literal `<!DOCTYPE` in article text rejects a feed

Assessment: **open**.

Evidence: internal/feed/charset.go:91-95.

rejectDTD still does a blanket case-insensitive containsFold for <!DOCTYPE and <!ENTITY over the whole document; no prolog check or CDATA/comment stripping was added. Tests only cover real prolog DTD attacks.

### B6-5: Passive "seen" can mark articles the user only scrolled past

Assessment: **open**.

Evidence: web/src/River.tsx:223-231 (markScrolledPast) and 191-218 (600 ms interval); web/src/River.test.tsx:54.

The mechanism was redesigned (virtualized list; a row is marked once it was observed intersecting and then leaves through the top edge), but there is still no dwell time or two-tick requirement, and the resume position still advances to the max seq of scrolled-past rows. River.test.tsx:54 asserts the new behaviour.

### B6-6: Single-key actions fall back to the first article

Assessment: **open**.

Evidence: web/src/River.tsx:268-269.

The keyboard handler still does entries[Math.max(0, index)] after findIndex, so with no selection (index -1) or a selection outside the loaded page, e/s/f/m act on entries[0]. The stale seen-set half is gone (River is keyed per view and pending is cleared on unread), but the fallback remains.

### B6-7: Clearing the Today decay field saves NaN

Assessment: **open**.

Evidence: web/src/Settings.tsx:52.

today_decay_days is still read straight from e.target.valueAsNumber with no Number.isFinite guard; clearing yields NaN, which JSON-serialises to null and the server rejects as invalid (internal/service/settings.go:202).

### B6-8: The Keychain account key breaks on URL normalisation

Assessment: **implemented**.

Evidence: clients/mac/App/Sources/AppModel.swift:181-182,199; clients/mac/App/Sources/Models.swift:288-303; clients/mac/App/Tests/MacBehaviorTests.swift:293.

The address is passed through ServerAddress.normalize (strips trailing slashes, lowercases the scheme) before KeychainTokens(server:) is created, so https://host and https://host/ share one account; MacBehaviorTests asserts normalize(" earnest.example.com/ ") == "https://earnest.example.com".

### B6-9: A failed Android notification tap does nothing

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/AppViewModel.kt:1179-1183; clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:245-251.

notificationEntry still swallows failures with runCatching{...}.getOrNull(); on null the LaunchedEffect does nothing, so an offline tap still gives no toast, retry or fallback.

### B6-10: The summary panel captures the Tab key

Assessment: **implemented**.

Evidence: web/src/pipeline/Pipeline.tsx:33-34; docs/research/decisions.md ('Tab moves focus').

Summary has no key handler at all and toggles only from its button; decisions.md records that the Tab-hides-summary binding was dropped so Tab always moves focus.

### B6-11: The river resume position is the last rendered row, not the furthest read

Assessment: **implemented**.

Evidence: web/src/River.tsx:205-210; web/src/River.test.tsx:120 'never regresses the resume position when an earlier row becomes due later'.

The position is now a monotonic max over the seq of rows that became due (targetSeq/resumeSeq refs), never the last rendered row, and the test asserts an earlier row cannot drag it backwards.

### B6-12: Android marks rows seen the instant they render

Assessment: **implemented**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/PassiveSeen.kt:20-27; clients/android/app/src/test/java/ch/lkmc/earnest/PassiveSeenTest.kt.

MarkSeenOnRender was replaced by PassiveSeenTracker: a row must be composed on screen and then leave through the top edge before it is reported, and only once. PassiveSeenTest covers first-composition, bottom-exit and never-composed cases.

### B6-13: The session-end "seen" count includes passive reads

Assessment: **open**.

Evidence: web/src/App.tsx:296-297,326; web/src/River.tsx:198.

count() increments summary.seen for every seen write regardless of kind, and River still calls act(...,'passive') for scrolled-past rows; the session-end copy still reads 'Your choices this session: N seen'.

### B6-14: A feed without a title opens an empty river heading

Assessment: **implemented**.

Evidence: web/src/App.tsx:216; web/src/Sidebar.tsx:85.

View resolution now uses sub.title || sub.url (and 'Feed' when the subscription is gone), and the sidebar link uses the same fallback, so a title-less feed no longer renders a blank h2.

### B6-15: A cancelled Mac sync keeps running and overlaps the next one

Assessment: **open**.

Evidence: clients/mac/EarnestKit/Sources/EarnestKit/SyncCoordinator.swift:19-26.

synchronize() still clears active in a defer when the awaiting call returns/throws, while the unstructured inner Task{ synchronizeOnce() } is never cancelled; the next synchronize() therefore starts a second pass concurrently.

### B6-16: One corrupt cached row aborts the whole Mac offline restore

Assessment: **open**.

Evidence: clients/mac/App/Sources/AppModel.swift:326 (currentEntries: try cached.map { try cachedEntry(...) }).

The entries map still throws through as a whole, so one undecodable cached entry fails restoreCachedPresentation; the caller's catch then clears the entire cache (cold presentation) instead of skipping the row. Feed/brief resource rows already use failable compactMap, but the cited whole-map throw remains.

### B6-17: Android re-queues seen writes for rows already seen

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/AppViewModel.kt:868-872; clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:460.

markSeen/markScrolledPast queue a passive seen write with no state.seen != 'seen' guard (markScrolledPast only checks markedUnread). PassiveSeenTracker limits each row to one report per session, but an already-seen Kept/Done/Later row still re-queues a redundant write.

### B6-18: Android silently drops expired outbox actions

Assessment: **implemented**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/AppViewModel.kt:1393-1402; clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:381-383; clients/android/app/src/test/java/ch/lkmc/earnest/AppViewModelTest.kt:122 retentionRecoveryRejectionsReachTheUi.

droppedOperationMessage now folds the locally expired count into state.error ('N expired before they could sync'), which the scaffold renders; the test asserts the message reaches the UI.

### B6-19: The Android reader renders a blank page for non-HTTPS origins and a dead WebView after renderer death

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/Reader.kt:247-249,292-294.

allowed() still hard-requires uri.scheme == "https", so an http signed URL is intercepted with an empty response and no fallback; onRenderProcessGone still just returns true without recreating the WebView.

### B6-20: The web reader offers an original link for entries without a URL

Assessment: **open**.

Evidence: web/src/ArticleReader.tsx:91; web/src/River.tsx:281.

The 'Open original' anchor and the v shortcut still use entry.url unconditionally (also the palette at App.tsx:484), and internal/store/entry_queries.go:38 still COALESCEs url to '', so an empty URL still opens a blank tab or dead link.

### B6-21: A transient edition batch failure consumes a retry attempt

Assessment: **open**.

Evidence: internal/service/edition_jobs.go:277-288.

BatchTried is still set immediately after Submit and before the error check, and a transport error (not ErrUnsupported) still returns err, consuming a retry attempt and abandoning the batch path.

### B6-22: Deleted feeds and folders stay in the manage-feeds lists

Assessment: **implemented**.

Evidence: web/src/feeds.ts:161-166 (groupByFolder); web/src/FeedSettings.tsx:358; internal/service/subscriptions.go:172 and internal/service/folders.go:26.

SubscriptionSection groups through groupByFolder, which filters !sub.deleted and !folder.deleted, and the /v1/subscriptions and /v1/folders lists already exclude deleted_at rows, so removed feeds/folders no longer linger until a reload.

### B6-23: Fetching one entry omits its cached blurb

Assessment: **open**.

Evidence: internal/service/entries.go:208-236 (Service.Entry; contrast :195 and :310).

Service.Entry still calls attachThumbnails but never attachBlurbs, while Entries and getByIDs do, so GET /v1/entries/{id} returns blurb null even when a Summaries blurb exists for the current content hash.

### B6-24: The reader keeps the previous article's typography draft and error

Assessment: **implemented**.

Evidence: web/src/App.tsx:570 (Reader key={openEntry.id}).

The Reader is remounted whenever the open entry changes, so overrideDraft and the error banner reset between articles. The secondary refetch-on-typography point remains (ArticleReader.tsx:30 still keys the content-url query on theme/measure/font).

## General issues

### B7-1: `lastVisit` is only written on pagehide

Assessment: **open**.

Evidence: web/src/App.tsx:138.

The save still runs only from a pagehide listener (moved from main.tsx to App.tsx); grep finds no visibilitychange or periodic write, so a killed tab still leaves a stale timestamp.

### B7-2: The outbound host limiter map grows without bound

Assessment: **open**.

Evidence: internal/outbound/outbound.go:451-457.

acquire still inserts one hostLimiter per host into c.hosts and nothing ever deletes an entry; no eviction, size bound or idle cleanup exists anywhere in the package.

### B7-3: docs/web.md describes a task boundary that no longer exists

Assessment: **implemented**.

Evidence: docs/web.md:107,137.

The stale section is gone: /settings is listed as a screen, fonts include dyslexic, and no text claims vectors 36/37 are absent. internal/api/api.go:114 mounts settingsRoutes, internal/web/content.go:295 implements the dyslexic face, and testdata/vectors/ contains 36-39.

### B7-4: The CHANGELOG has no entries for the post-v0.1.0 work

Assessment: **implemented**.

Evidence: CHANGELOG.md:94.

The Unreleased block now has Fixed (line 94) and Changed (line 112) sections recording post-v0.1.0 work; the Fixed section was added after the analysis date (commit 6df6f89, 2026-11-09).

### B7-5: Dead CSS rules for the sync status and alerts

Assessment: **implemented**.

Evidence: web/src/style.css:178.

Neither `.workspace > .sync-status` nor `.workspace > [role=alert]` appears in style.css any more; `.workspace` remains at line 178 and alerts are styled through .problem/.banner at line 138.

### B7-6: docs/llm-providers.md understates the budget guarantee

Assessment: **open**.

Evidence: docs/llm-providers.md:129.

The paragraph still says the check and record are separate operations and concurrent in-flight calls can overshoot, while internal/llm/budget.go:139-149 and internal/store/llm_spend.go:49 implement atomic 48-hour reserve/settle that counts in-flight estimates.

### B7-7: The Android legacy-cache banner is undismissable

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:380.

cacheNotice is still set from legacyCacheRetained at AppViewModel.kt:239 and rendered as plain Text with no dismiss action; no dismissCacheNotice or preference-backed acknowledgement exists.

### B7-8: Android openEntry swallows 401 like any other error

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/AppViewModel.kt:1150.

openEntry still wraps api.content (line 1150) and api.contentUrl (line 1163) in runCatching{}.getOrNull(), so a revoked token silently serves stale content instead of signing out.

### B7-9: Mac "While you were out" is fixed to a 24-hour window

Assessment: **open**.

Evidence: clients/mac/EarnestKit/Sources/EarnestKit/API.swift:473.

agentChanges still defaults to since now-86400 and AppModel.swift:605 calls it with no argument; the Mac client persists no per-identity last-seen timestamp.

### B7-10: The image proxy buffers whole images and omits Referrer-Policy

Assessment: **open**.

Evidence: internal/imageproxy/imageproxy.go:346.

ServeHTTP still io.ReadAlls the upstream body, and serve (line 391) sets Cache-Control/ETag/Content-Type but no Referrer-Policy, unlike /content/ and /v1/.

### B7-11: Manual refresh and OPML import wait up to a poll interval

Assessment: **open**.

Evidence: internal/service/events.go:143; cmd/earnest/serve.go:169.

RefreshSubscription still only sets feeds.next_fetch_at and nothing calls Fetcher.Kick() (only fetcher.go defines it); runRetention is still launched with a bare `go` at serve.go:169, outside the workers WaitGroup that precedes db.Close().

## Novel ideas

### B8-1: The session end could carry a reading streak

Assessment: **open**.

Evidence: web/src/River.tsx:356.

The session-end card still renders only the choices summary and SessionChanges; there is no streak. web/src/App.tsx:125,138 stores a single earnest.lastVisit timestamp (used as the agent-changes `since`), and grepping for streak/daily-visit tracking finds nothing.

### B8-2: The session end could offer a reset and a real reading count

Assessment: **open**.

Evidence: web/src/River.tsx:358.

The card reports seen/Done/Later/Keep action counts but no count of articles actually opened, and there is no 'Start again from the top' reset; grepping for start again/reset view/from the top finds no such action.

### B8-3: Agent briefs deserve their own surface

Assessment: **open**.

Evidence: web/src/agents/Surfaces.tsx:100.

TodayAgents still renders each brief as a <details> row in the Today river (Brief at :107) with its entries nested inside; briefs have not been promoted to their own panel.

### B8-4: The command palette could expose reading-state commands

Assessment: **open**.

Evidence: web/src/markRest.ts:7.

'Mark the rest read' exists as a palette command (web/src/App.tsx:492; decision at docs/research/decisions.md:40), but 'Jump to oldest unread' and 'Open a random unread article' are absent from the command list and no equivalent behaviour exists.

### B8-5: A focus mode would fit the calm aesthetic

Assessment: **open**.

Evidence: web/src/ArticleReader.tsx:83.

The reader toolbar has Back/Done/Later/Keep/Mark unread/Open original/Copy/Share/Typography only, and grepping for focus-mode/dim finds no toggle or backdrop CSS.

### B8-6: Reading time on river rows

Assessment: **open**.

Evidence: web/src/River.tsx:348.

The entry meta line shows source, shelf, kept, cluster, unread state and time only; there is no reading-time estimate, no content-length field in the API schema, and web/src/format.ts has no such formatter.

### B8-7: One from your Kept shelf at the river's end

Assessment: **open**.

Evidence: web/src/River.tsx:356.

The session-end card offers no 'One from your Kept shelf' pick; grepping for random/pick finds nothing in the web client.

### B8-8: A page-turn flourish for opening an article

Assessment: **implemented**.

Evidence: web/src/style.css:459.

.panes.reading .reader-pane runs a .2s slide-in animation (translate 2.5rem, opacity .6; keyframes at :463) inside the prefers-reduced-motion: no-preference block, and web/src/App.tsx:562 adds .reading when an article opens. It is scoped to max-width: 759px, so desktop opens get no flourish.

### B8-9: The caught-up moment could name today's feeds

Assessment: **open**.

Evidence: web/src/River.tsx:358.

The end card shows action counts and SessionChanges (agent changes); it does not group the session's seen entries by subscription title.

### B8-10: Hint at swipe gestures on first Android run

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:850.

SwipeActionRow wires horizontal drags to Later/Done only, and no first-run hint or gesture cheat-sheet exists in the app sources (grepping hint/gesture/first run finds nothing).

### B8-11: Prefetch reading content for the top of Today during sync

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/SyncWorker.kt:22.

Android's sync worker only pulls/flushes and refreshes agent changes; content is fetched solely in openEntry (AppViewModel.kt:1146), and the Mac client does not prefetch either. The web worker warms ten entries of a visited river page (web/src/offline/policy.ts:16) but not during sync.

### B8-12: Right-swipe Keep and a long-press menu on Android

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:975.

SwipeActionRow handles only Later/Done (SwipeActionTest.kt names those two); Keep is a text button under the row (:865-872) and no long-press/combinedClickable menu exists.

### B8-13: An optional reading time box

Assessment: **open**.

Evidence: docs/research/ux.md:50.

The 'Twenty minutes' box remains only a research sketch; no time-box/timer UI exists in web or either native client, and docs/research/decisions.md records no deferral.

### B8-14: Per-article "Load images" on Android

Assessment: **open**.

Evidence: clients/android/app/src/main/java/ch/lkmc/earnest/EarnestApp.kt:706.

Android's ReaderHeader offers Open original/Copy/Share/Find/text size/Mark unread but no Load Images (the Mac has one at clients/mac/App/Sources/Reader.swift:293). The cited EarnestApi.kt moved to core/EarnestApi.kt:203 and now only agent-selected entries use mode=click (AppViewModel.kt:1163), but no in-reader load-images control exists.

### B8-15: "Catch me up since..." horizon slider and a newspaper colophon

Assessment: **open**.

Evidence: web/src/pipeline/Pipeline.tsx:64.

The Edition exists but there is no horizon slider on the river header, no 'Move older than 24h to Edition' batch action, and no colophon (reading time, top authors, quietest feeds) in the Edition schema or UI.

### B8-16: Agent dry-run postcard and provenance diff in the reader margin

Assessment: **open**.

Evidence: web/src/ArticleReader.tsx:82.

The reader renders Summary and MoreLikeThis but never queries /v1/annotations or renders a margin postcard with Agree/Revert-and-teach; the endpoint appears only in the generated API types (web/src/api.generated.ts:87).


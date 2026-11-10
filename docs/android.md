# Android client

## Build and install

Install JDK 21 and the Android SDK, including platform `android-37.0`, build
tools `36.1.0` and platform tools. Set `ANDROID_HOME` to the SDK directory.
From the repository root:

```sh
cd clients/android
./gradlew :core:test :app:testDebugUnitTest :app:assembleDebug lint
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

The debug APK is signed with the local debug key. The phone needs Android 10
(API 29) or later. USB debugging must be enabled for `adb install`; otherwise
transfer the APK to the phone and install it there.

## Release packages

From an exact source tag, `scripts/package-client.sh android <tag>` builds and
verifies an APK using your persistent release key and a tag-derived version.
Missing signing inputs fail rather than using the debug key. See
[three-platform releases](releases.md) for credentials, receipts and acceptance.
The disposable APK built by `scripts/android-release-test.sh` is a fixture,
not a distribution package. Before replacing a debug installation signed by a
different key, sync every pending action; uninstalling deletes its local outbox.

## Connect

The phone must reach your Earnest server, for example over your private
tailnet. Configure HTTPS on your reverse proxy and use the externally
reachable app origin. The article content origin is a separate origin; see
[Web reader](web.md) for its server configuration.

Open the app, enter the HTTPS origin and start device sign in. The app shows
an eight-character code and polls the server at the interval the server
returned, honouring `slow_down` by waiting longer. Enter the code at the
server's `/device` page in a browser where you are signed in, then approve.
The app verifies the token with `/v1/me` before attaching any cached state,
then stores the token encrypted with the Android Keystore. Snapshot, outbox,
content, briefs and session preferences are isolated by HTTPS server origin,
user ID and human principal ID. Returning to that account restores its own
pending actions. Signing in again clears the displayed state before attaching
the verified account.

Notification permission is requested only after a successful sign-in, when
push registration starts, or from the top-bar Notifications action. The
connect screen never asks for it. Queued actions are sent a few seconds after
the last one; offline, the durable queue waits for the periodic sync.

The old unscoped `earnest.db` and `session` preferences are retained on upgrade,
but never attached to a new login or replayed into another account. Their
ownership cannot be established safely. Preserve those files if pending
actions need manual recovery; the app reports a retained legacy cache. No Room
schema changes or destructive migrations are used.

### Testing against a local server

Release builds trust only system certificate authorities. Debug builds also
trust certificates you install yourself (`res/xml/network_security_config.xml`),
so you can test against a local server behind a reverse proxy with a
self-signed certificate. In the emulator the host is `10.0.2.2`: issue the
proxy's certificate for that address, set `EARNEST_BASE_URL` and
`EARNEST_CONTENT_ORIGIN` to the proxy's `https://10.0.2.2:<port>` origins, and
install your CA certificate in the emulator under Settings > Security >
Encryption & credentials > Install a certificate > CA certificate. Cleartext
HTTP stays disabled in every build.

## Reading offline

The app caches the sync snapshot (entries, subscriptions and state) in Room
and caches article content when you open it. Lists work without a connection;
the reader falls back to the cached content when the signed content URL cannot
be fetched. Swipe a row left a short distance for Later, or a long distance
for Done; the label appears during the gesture and an undo action is offered
in a snackbar that names the action and auto-dismisses. The swipe background
is hidden until the row moves. Rows also offer Keep, Unkeep and, once read,
Mark unread. Mark unread returns the article to unread and clears Done with
it, as the server does; its undo restores both registers. Undo writes back
the registers an action changed and leaves alone one another writer has
changed since. A row marks itself seen once it has been on
screen and then scrolls out through the top edge, or when you open it; rows
visible on first composition, or never composed, stay unseen. Scrolling never
marks a row read again after you marked it unread; opening the article does. Today keeps
its session membership stable: passive seen updates do not remove rows or
expose more rows to automatic marking. Later and Done remove rows
immediately; refresh starts a new membership from the current effective
state. Cached subscription cadences are restored before a network request,
so Today and Edition also work on an offline cold launch.

List rows show the feed's icon next to the source line and the article's
thumbnail at the end of the row, and Feeds rows show the icon before the
feed name. Earnest stored those copies when the feed and the article
arrived, so showing them never contacts a publisher. The app keeps its own
copies per account in its cache directory, plus a small set in memory.

Today and Edition share the server's river membership rule. Subscription import
boundaries, subscription muting, case-insensitive author and title-keyword mutes,
seen/shelf state and first-seen decay all apply. Now never decays, Today uses the
reader setting (two days by default), and Edition uses seven days. Today excludes
week-cadence feeds, which appear in Edition. Both lists retain their session
membership through passive seen updates until refresh, while Later and Done
remove rows immediately.

Online sync fetches `/v1/mutes` and `/v1/settings` because bootstrap omits them,
then replays mute and settings changes by revision. These inputs persist inside
the identity-scoped snapshot for cold offline launches. Connect online once
after upgrading to populate previously uncached inputs.

Today shows the newest brief from each agent at the top with the agent's name.
It also lists agent changes to unseen articles under "While you were out" and
again at session end for changes made during the session. Each change names
the agent, article and action, and offers Revert only when the server permits
it. Failure to refresh this list is reported. The last successful list remains
available offline. Background sync caches the same notices; an interrupted or
failed refresh leaves a visible warning on the next offline launch.

The reader shows the feed title and the published date (falling back to
first seen) under the article title, with Open original, Copy link and Share
actions for the entry URL; a read article's header also offers Mark unread.
Open original opens the URL in the browser, Copy link places it on the
clipboard and Share opens the system share sheet. It loads the signed
content URL in a WebView with JavaScript, file access and content access
disabled, and allows only
that origin; other links open in the browser. Articles you open from your lists
load images through the image proxy. Articles an agent surfaced (a
notification, a deep link or an agent-change notice) request `mode=click`, so
automatic image loading cannot reveal an agent's article selection. On a main-frame network or
HTTP failure, the reader loads the cached HTML once through the local asset
origin. Subresource failures do not replace the article, and local fallback
content cannot load network resources. Agent text is shown as plain text,
never as HTML.

Notifications and deep links resolve missing metadata through `/v1/entries/{id}`;
selection follows the current snapshot as cold-launch loading and sync complete.
Incremental entry events are hydrated before their page and cursor are committed.
A metadata 404 is retained as a tombstone; other failures leave the cursor for
retry.

## Search and saved views

Use the top-bar Search action to search stored articles and separately labelled
agent notes. Full-text search works without models. When embeddings are enabled,
you can choose Hybrid, which can make an embedding call. Feed, folder and page-size
filters are available. More search results and Save as new keep the submitted
query and filters even while you edit the next query.

Save a query as a named view, open it from Saved searches, rename it or delete
its definition without removing articles. Mutations report errors rather than
claiming offline success. Search is online-only; its transient results and drafts
are cleared when you change accounts or sign out, and invalidated after a server
restore. A failed AI availability check still leaves full-text search available.
Opening an agent-note result uses click-to-load images like other agent-surfaced
articles. Notes and snippets remain plain text.

## Feeds and activity

Feeds subscribes by URL, changes a subscription's cadence (Now, Today, This
week) and unsubscribes. The current cadence is selected in the row. A saved
cadence change offers an undo snackbar that restores the previous cadence.
Feeds groups subscriptions by folder and shows a failing feed's fetch error
with the time of its last attempt.
Activity groups agent changes by principal, shows a readable description with the
article title where there is one, and offers Revert where the server reports
that the action can still be undone.

## Themes and the widget

The top bar offers System, Light, Dark, True black and E-ink; the choice is
remembered. The home-screen widget shows the newest five headlines and no
unread count, and opens the entry when tapped.

## Notifications and UnifiedPush

Notification channels are per category: new content, digests and briefs are
`IMPORTANCE_LOW`; only Now alerts use the higher alert channel. A notification
shows only the title and body the server generated and opens the stored entry
inside the app. Agent text is never requested or displayed.

The app registers through UnifiedPush, so it needs a distributor such as the
ntfy Android app. Run an ntfy server you control, configure the distributor to
use it, and select it in Earnest. On registration the app sends the endpoint
and its `p256dh` and auth keys to `/v1/devices`. The server requires an HTTPS
endpoint it can reach; private IP literals are rejected. Notification
permission must be granted on Android 13+; the app asks after sign-in, when
push registration starts, or from the top-bar action, and the top bar shows
that action only while permission is missing.

## Verification

`:core:test` runs the shared sync vectors and the OpenAPI contract test;
`:app:testDebugUnitTest` covers snapshot replacement failure, concurrent
foreground/background writes, account isolation, offline cadence restoration,
Today session membership, passive seen on scroll, missing notification metadata,
agent-change retrieval, WebView error fallback, deep-link parsing, brief labels,
widget headline selection, theme persistence, notification channels and server
notification text, notification permission timing, cadence selection, swipe
thresholds and action messages, undo restoration and its changed-since
guard, mark unread clearing Done and surviving passive marking, share
and clipboard intents, activity descriptions, reader date and original
URL, and outbox flush debounce.
Core repository tests exercise real wire payloads, batch boundaries, in-flight
retries, entry hydration and sibling subscription projections. Room regressions
use Robolectric's native SQLite mode on Apple silicon because legacy SQLite
has no compatible binary. Other hosts retain legacy mode because native SQLite
stalled on the shared CI host. There is no emulator in CI, so the device-flow
approval, UnifiedPush delivery, widget rendering and WebView reading have not
been verified on a device.

## Reader preferences and exact counts

Open Settings from the top-bar overflow. The screen edits a local draft over
the account's synced reader preferences. Wait for a synced baseline or load it
before editing; assumed defaults cannot be saved. Save publishes explicitly. Load
fetches the server's preferences with GET only, without a PUT, and preserves an
edited draft. Reload explicitly discards the draft and loads the newest known
settings. If a newer account revision is known, your draft is kept; reload
explicitly before saving instead of overwriting it. An ambiguous save failure freezes the exact body
and operation ID, so Retry resends them unchanged; editing the draft or losing
the account or server scope ends the frozen request. Errors are reported
honestly. Private drafts and transient results clear on account change,
sign-out or restore.

Settings cover the reading mode, Today decay days (1-365), single-key shortcuts
(shared with web and Mac), article theme, font and measure (62-72), and
per-feed typography overrides keyed by feed ID with inherit and reset. The
device-local chrome theme stays separate from these synced article appearance
settings.

Exact unread totals are requested and shown only in unread-count mode,
labelled with the last-synced time. Fetch failures retain the last successful
totals. When none are cached, counts are unavailable, not zero. Cached reading typography respects the safe enums and the existing
image and origin restrictions.

Settings can also bring starred articles from another reader into Kept. Pick a
Google Reader JSON export, for example from FreshRSS or Inoreader, and Earnest
keeps the articles you starred there. Articles already in Earnest, and articles
you removed from Kept, are left alone. Articles whose feed you do not follow
are skipped unless you ask Earnest to subscribe to those feeds first, and the
report names what was added, what was already there, what was left out and
what could not be imported.

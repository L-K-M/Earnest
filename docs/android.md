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

The old unscoped `earnest.db` and `session` preferences are retained on upgrade,
but never attached to a new login or replayed into another account. Their
ownership cannot be established safely. Preserve those files if pending
actions need manual recovery; the app reports a retained legacy cache. No Room
schema changes or destructive migrations are used.

## Reading offline

The app caches the sync snapshot (entries, subscriptions and state) in Room
and caches article content when you open it. Lists work without a connection;
the reader falls back to the cached content when the signed content URL cannot
be fetched. Swipe a row left a short distance for Later, or a long distance
for Done; the label appears during the gesture and an undo action is offered
in a snackbar. A row marks itself seen once it has been on screen and then
scrolls out through the top edge, or when you open it; rows visible on first
composition, or never composed, stay unseen. Today keeps its session
membership stable: passive seen updates do not remove rows or expose more
rows to automatic marking. Later and Done remove rows immediately; refresh
starts a new membership from the current effective state. Cached
subscription cadences are restored before a network request, so Today and
Edition also work on an offline cold launch.

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

The reader loads the signed content URL in a WebView with JavaScript, file
access and content access disabled, and allows only that origin; other links
open in the browser. All signed content URLs request `mode=click`, including
notification-selected articles, so automatic image loading cannot reveal an
agent's article selection. On a main-frame network or HTTP failure, the reader
loads the cached HTML once through the local asset origin. Subresource failures
do not replace the article, and local fallback content cannot load network
resources. Agent text is shown as plain text, never as HTML.

Notifications and deep links resolve missing metadata through `/v1/entries/{id}`;
selection follows the current snapshot as cold-launch loading and sync complete.
Incremental entry events are hydrated before their page and cursor are committed.
A metadata 404 is retained as a tombstone; other failures leave the cursor for
retry.

## Feeds and activity

Feeds subscribes by URL, changes a subscription's cadence (Now, Today, This
week) and unsubscribes. Activity groups agent changes by principal and offers
Revert where the server reports that the action can still be undone.

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
permission must be granted on Android 13+.

## Verification

`:core:test` runs the shared sync vectors and the OpenAPI contract test;
`:app:testDebugUnitTest` covers snapshot replacement failure, concurrent
foreground/background writes, account isolation, offline cadence restoration,
Today session membership, passive seen on scroll, missing notification metadata,
agent-change retrieval, WebView error fallback, deep-link parsing, brief labels,
widget headline selection, theme persistence, notification channels and server
notification text.
Core repository tests exercise real wire payloads, batch boundaries, in-flight
retries, entry hydration and sibling subscription projections. Room regressions
use Robolectric's native SQLite mode on Apple silicon because legacy SQLite
has no compatible binary. Other hosts retain legacy mode because native SQLite
stalled on the shared CI host. There is no emulator in CI, so the device-flow
approval, UnifiedPush delivery, widget rendering and WebView reading have not
been verified on a device.

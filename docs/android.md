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
The app stores the issued token in the Android Keystore and signs in.

## Reading offline

The app caches the sync snapshot (entries, subscriptions and state) in Room
and caches article content when you open it. Lists work without a connection;
the reader falls back to the cached content when the signed content URL cannot
be fetched. Swipe a row left a short distance for Later, or a long distance
for Done; the label appears during the gesture and an undo action is offered
in a snackbar. Rows mark themselves seen as they render. Today shows the
newest brief from each agent at the top with the agent's name, and ends with
the end-of-river statement.

The reader loads the signed content URL in a WebView with JavaScript, file
access and content access disabled, and allows only that origin; other links
open in the browser. Agent text is shown as plain text, never as HTML.

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
`:app:testDebugUnitTest` covers deep-link parsing, brief labels, widget
headline selection, theme persistence, notification channels and the
notification's server text. There is no emulator in CI, so the device-flow
approval, UnifiedPush delivery, widget rendering and WebView reading have not
been verified on a device.

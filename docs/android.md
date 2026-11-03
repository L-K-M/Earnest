# Android client

## Build and install

Install JDK 21 and the Android SDK, including platform `android-37.0`, build
tools `36.1.0`, and platform tools. Set `ANDROID_HOME` to the SDK directory.
From the repository root:

```sh
cd clients/android
./gradlew :core:test :app:testDebugUnitTest :app:assembleDebug lint
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

The debug APK is for development and is signed with the local debug key. The
phone needs Android 10 (API 29) or later. USB debugging must be enabled for
`adb install`; alternatively transfer the APK to the phone and install it there.

## Connect

The app is incomplete. Device authorization has not yet been verified on a device,
and the reading and offline sync workflows are not ready for use.

The phone must be able to reach your Earnest server, for example over your
private tailnet. Configure HTTPS on your reverse proxy and use the externally
reachable app origin. The article content origin is a separate origin; see
[Web reader](web.md) for its server configuration. Sign in using the app's
device authorization flow and approve the displayed code in the browser.

## UnifiedPush and your own ntfy

UnifiedPush registration is not implemented yet. The following describes the
intended setup once support is added.

Run an ntfy server you control and install an Android UnifiedPush distributor
that supports it, such as the ntfy Android app. Configure the distributor to
use your server and select it for Earnest. The distributor supplies an endpoint
for device registration; notification permission and a working network path
are also needed. UnifiedPush subscriptions use encrypted Web Push payloads;
the server requires an HTTPS endpoint and rejects private IP literals. DNS
names are checked again when sending, so the server must be able to reach the
endpoint under its outbound address policy. See [Agents](agents.md#devices-and-push-settings)
for destination and notification privacy details. Direct ntfy settings on the
server are separate from a UnifiedPush device registration.

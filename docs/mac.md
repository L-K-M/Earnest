# Mac client

The portable Swift package is in `clients/mac/EarnestKit`. With Swift 6
installed, build and test it on Linux or macOS:

```sh
cd clients/mac/EarnestKit
swift build
swift test
```

The native app requires macOS 15 or later, Xcode and XcodeGen. Generate the
project and build and test the unsigned app:

```sh
cd clients/mac/App
xcodegen generate
xcodebuild build test -project Earnest.xcodeproj -scheme Earnest \
  -destination 'platform=macOS' CODE_SIGNING_ALLOWED=NO
```

The Mac workflow runs the package tests on Linux and builds and tests the app
on a macOS runner. The macOS build resolves its Swift package dependencies
when Xcode builds the generated project.

## Server connection

The app connects to the server's `/v1` API. Device login uses a short-lived
code approved on the server and stores the resulting token in the macOS
Keychain. The sync engine and GRDB store are not yet connected to the app. Changes made
offline are not synchronized; see [sync.md](sync.md) for the intended protocol.

The native reader fetches article content from the authenticated API and
serves it to WebKit through a custom scheme; it does not use the web reader's separate content origin. See [web.md](web.md) for that origin's listener
and reverse proxy settings. Article HTML is untrusted: the reader disables
JavaScript and restricts navigation and remote content. Agent briefs and notes are not yet shown. Deep links identify entries by internal ID and do not
supply a URL to fetch.

## Distribution later

The CI build is unsigned and is not a distribution artifact. Distribution
will require an Apple Developer ID Application certificate and a team with
Developer ID access. Configure the team and signing identity in Xcode, archive
with the hardened runtime enabled, sign the exported app, submit it to
Apple's notary service with `notarytool`, and staple the resulting ticket.
Verify the signature and stapled ticket before distribution. Credentials for
signing and notarization must remain outside the repository.

Sparkle updates are planned for later; no update feed, signing keys or
release automation are configured here.

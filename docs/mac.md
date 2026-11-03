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

The native reader serves article content to WebKit through a custom scheme.
When signed content URLs are available it fetches them from the separate
content origin; otherwise it fetches the authenticated API content. See
[web.md](web.md) for the origin's listener and reverse proxy settings.
Article HTML is untrusted: the reader disables
JavaScript and restricts navigation and remote content. Agent briefs and notes are not yet shown. Deep links identify entries by internal ID and do not
supply a URL to fetch.

## Server changes needed

`GET /web/content-url/{id}` requires a browser session, so a native app's
bearer token cannot use it. Add `GET /v1/entries/{id}/content-url`, accepting
the same bearer authentication and entry authorization as the content API.
Return JSON `{"url":"https://content.example/article?...", "content_origin":"https://content.example"}`,
where `url` is a short-lived signed URL on the separately configured content
origin and `content_origin` is its scheme, host and optional port. The URL
must not require the API bearer token. The app checks that the URL belongs to
that origin, rejects redirects, fetches it without the token, and renders it
through its restricted custom scheme. Until the endpoint exists, it falls back
to `GET /v1/entries/{id}/content` and renders the sanitized HTML with the same
JavaScript-disabled WebKit and restrictive CSP.

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

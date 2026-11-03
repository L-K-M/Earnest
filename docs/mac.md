# Mac client

The portable Swift package is in `clients/mac/EarnestKit`. With Swift 6.2 or later
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

## API generation

The package uses Apple's `swift-openapi-generator` 1.14.0 build plugin with
`swift-openapi-runtime` 1.13.0 and `swift-http-types` 1.7.0, pinned exactly in
Package.swift and resolved in Package.resolved. Swift 6.2 or later is required;
Linux validation uses Swift 6.3.3. The target's `openapi.json` is a relative
symlink to the repository's committed `api/openapi.json`, not a copied schema.
The configuration filters generation to the operations the repository uses.
Generated Swift files live under `.build` and are not checked in. See the
[generator documentation](https://github.com/apple/swift-openapi-generator).

`EarnestAPI` wraps the generated `Client` and keeps generated names internal.
The existing Codable models remain stable repository-facing adapters, rather
than an alternative network client. The schema test constructs examples from
every corresponding OpenAPI schema and verifies round trips, including both
register value alternatives. The generated-runtime identity test checks that
untyped change payloads preserve `Int64.max`. The only hand-written REST call
is the forthcoming content-url operation, which is absent from the committed
OpenAPI document. SSE uses a streaming URLSession delegate because it carries
hints rather than JSON responses.

## Server connection

The app enables Apple's
[local-network ATS exception](https://developer.apple.com/documentation/bundleresources/information-property-list/nsapptransportsecurity/nsallowslocalnetworking)
for unqualified hostnames and local servers. Use HTTPS for a fully qualified
server hostname; the app does not disable ATS globally.

Device login uses `/v1/devices/start` and `/v1/devices/poll`, obeys the poll
interval and expiry, handles pending/slow-down responses, and stores the bearer
token in the macOS Keychain. The database cache is partitioned by configured
server and authenticated principal. GRDB transactions persist a complete sync
snapshot and its cursor together. Tokens never enter that cache.

The coordinator bootstraps resources by keyset pagination, pulls contiguous
`/v1/changes` pages, sends register batches and first-class ranges in outbox
order, and merges acknowledgements by independent register revisions. A 410
restarts bootstrap and replays preserved intent once per synchronization
attempt. Generation changes rebase reversible intent to revision zero while
keeping the original action time. Expired and gone operations are reported.
`/v1/events` is only a hint: connect and every hint cause a durable change pull.
The event stream reconnects using the persisted generation/revision.

Annotations currently have no `/v1/bootstrap/annotations` resource; the client
uses keyset-paged `/v1/annotations?include_deleted=true` and then replays changes
from the cursor captured before bootstrap. Label definitions and memberships
use the server's distinct bootstrap resources, including its capitalized label
JSON field names. Objects and tombstones merge by higher revision; states merge
by register revision independently of `state_rev`.

Article HTML is untrusted. The app first requests a signed content URL, fetches
it without bearer credentials or cookies from a browser session, rejects
redirects, and renders the response through `earnest-content://article/`.
If the endpoint is unavailable or its response is invalid, it falls back to
`GET /v1/entries/{id}/content`. Both paths use the same nonpersistent WKWebView
with JavaScript disabled and a strict CSP blocking network subresources,
forms and base URL overrides. Navigation is restricted to the custom scheme;
user-activated HTTP(S) links open in the default browser. See [web.md](web.md)
for the separate content listener and reverse proxy configuration.

Agent briefs, notes and notification notes render with plain SwiftUI `Text`,
without HTML or Markdown interpretation. Deep links accept only positive
internal 64-bit entry IDs in `earnest://entry/<id>`.

## Server changes needed

`GET /web/content-url/{id}` requires a browser session. Native apps authenticate
with bearer tokens and cannot use that endpoint. The required native operation
is `GET /v1/entries/{id}/content-url`, with the same bearer authentication and
entry authorization as `GET /v1/entries/{id}/content`, returning:

```json
{"url":"https://content.example/article?...","expires_at":"2026-11-03T12:05:00Z"}
```

`url` must be a short-lived signed HTTP(S) URL on the configured content origin
that needs no bearer token or browser session. `expires_at` must be an RFC 3339
UTC timestamp. No `content_origin` field is required: the authenticated server
selects the origin in `url`, and the app rejects credentials, fragments, expired
URLs and redirects before rendering through its restricted custom scheme.
This endpoint is being implemented in parallel. No server or API file was
changed by this client task. Until it appears in the committed OpenAPI document,
the repository implements this one request explicitly and tests its exact shape
and absence of Authorization on the signed URL request.

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

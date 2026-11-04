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
untyped change payloads preserve `Int64.max`. Signed content URLs and notification
cancellation use operations in the committed OpenAPI document and the generator
filter. SSE uses a streaming URLSession delegate because it carries hints rather
than JSON responses.

## Server connection

The app enables Apple's
[local-network ATS exception](https://developer.apple.com/documentation/bundleresources/information-property-list/nsapptransportsecurity/nsallowslocalnetworking)
for unqualified hostnames and local servers. Use HTTPS for a fully qualified
server hostname; the app does not disable ATS globally.

Device login uses `/v1/devices/start` and `/v1/devices/poll`, obeys the poll
interval and expiry, handles pending/slow-down responses, and stores the bearer
token in the macOS Keychain. After a successful `/v1/me`, the app remembers the
verified server origin, user ID and principal ID in preferences, without the
token. Cache partitions include all three values. Origins normalize hostname
case, the default HTTP/HTTPS port and a trailing slash; use an HTTP or HTTPS
origin without a path, credentials, query or fragment. GRDB transactions persist
a complete sync snapshot and its cursor together. Tokens never enter that cache.

On cold launch, a connection failure (offline, refused connection, DNS failure,
lost connection or timeout) reopens the last verified identity's cache. Cached
article rows, subscription titles and cadences, and Today briefs appear without
a successful network request. Reading and triage continue against the durable
outbox. The app retries identity verification every ten seconds until it can
start sync, and verifies `/v1/me` before sending queued actions on subsequent
sync attempts. Event hints trigger sync, with a two-minute poll as a fallback.
A changed user or principal clears the old presentation and opens only the
newly verified identity's partition. A 401 signs you out. Signing out
or starting a fresh device login forgets the offline identity for that server;
a new credential must establish its own identity online before using a cache.

Connection failures appear in the non-modal connection note as “Offline.
Showing what was last synced.” Successful sync clears that note. Actionable
failures use an alert with a human-readable message. TLS trust failures and
HTTP errors do not authorize offline identity recovery.

The earlier server/principal-only cache is retained on disk but is not reused:
it has no verified user ID. After upgrading, connect and sync once while online
to populate the new identity partition before relying on offline launch.

Online Today and Edition use the server's river endpoint. Offline they evaluate
that rule over cached sync entries and subscription objects. Entries must be
unseen, on no shelf, in an active unmuted subscription and inside its initial
import boundary. Author mutes compare whole names case-insensitively. Keyword
mutes match the title case-insensitively. Now entries never decay, Today entries
decay after the configured number of days (two by default), and Edition entries
decay after seven days, all measured from first-seen time with inclusive cutoffs.
Today excludes week-cadence feeds, which appear in Edition.

Bootstrap omits mutes and reader settings. Each online synchronization fetches
`/v1/mutes` and `/v1/settings` before replaying changes. The identity's sync
snapshot retains those resources and applies `mute.updated`, `mute.deleted` and
`settings.updated` by revision. Connect online once after upgrading to fill any
previously uncached inputs. Agent names from token metadata and activity are
cached per identity and sync generation, and restored with offline briefs.
Rows without a publisher date fall back to their first-seen date.

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
with JavaScript disabled and a strict CSP blocking subresources except explicitly
authorized images from the signed URL's content origin. Forms and base URL
overrides are blocked. Navigation is restricted to the custom scheme;
user-activated HTTP(S) links open in the default browser. See [web.md](web.md)
for the separate content listener and reverse proxy configuration.

Agent briefs, notes and notification notes render with plain SwiftUI `Text`,
without HTML or Markdown interpretation. Deep links accept only positive
internal 64-bit entry IDs in `earnest://entry/<id>`. External URLs route to an
existing reader window, creating one only when none exists. File > New Window
still creates additional windows.

## Article content

The reader asks `GET /v1/entries/{id}/content-url` with the app's bearer token
and receives `{"url": ..., "expires_at": ...}`: a short-lived signed URL on the
server's content origin that needs no token or session. The app rejects URLs
with credentials or fragments, expired URLs and redirects, then renders through
its restricted custom scheme with JavaScript off. If the content origin can't be
reached, it falls back to the sanitised HTML from
`GET /v1/entries/{id}/content`, with all images blocked.

List selections in Today, Edition, Later, Kept and Done, including keyboard
navigation, request `mode=proxied`. Their CSP permits `img-src` only for the
signed URL's exact content origin, including its port. External deep links
request `mode=click` and block all image subresources. The server replaces images
with “Load image” links; activating one opens the signed proxy link in the default
browser. The reader's **Load Images** button explicitly re-fetches the article
with `mode=proxied`. Opening another deep link blocks images again, even when it
names the same article. Fallback content never authorizes original image URLs.

The reader saves the HTML it displays in the identity's SQLite cache. It retains
the 200 most recently opened articles, including reads of saved copies. If both
content requests fail, a saved copy from the current sync generation is shown
with an “Offline copy. Images are blocked.” note. Offline copies never authorize
image subresources, even if the saved HTML came from a signed proxied response.
A 401 still signs you out instead of displaying a saved copy. Cache copies and
briefs from an older server generation are not reused after a restore.

A SwiftUI header above the HTML shows the publisher's title as a heading,
feed title, author and published date (or first-seen date), followed by **Open
Original**, which opens the article URL in the default browser. The WebKit page
follows system light and dark appearance, with a serif default, a 68-character
measure and uncropped images constrained to that measure.

Activity offers Revert for reversible changes and Cancel for held notifications.
Cancellation sends the notification target ID and a fresh `op_id`; delivered
notifications cannot be recalled. Revert's accessibility label names the action
and article. Agent text remains plain SwiftUI text.

## Regression checks

The hosted app tests snapshot a WKWebView with dark appearance and assert its
background pixel, check short and long brief layout, inspect Activity button
accessibility labels, deliver native URL events to the running app, and exercise
content image policies and fallback loading. Offline tests cover identity
persistence and validation, connection-only recovery, changed accounts, 401
sign-out, durable rows/briefs/outbox restoration, cached river inputs and brief names,
missing publisher dates, cached reader HTML, generation
checks, article eviction and server/user/principal isolation. The test scheme
supplies an empty server preference for that launch so tests do not use your saved connection or Keychain credential.

For scene routing, launch the app, run `open earnest://entry/3` twice and confirm
that the same window receives both URLs. Use File > New Window to create another
window, then close every window and open the URL again to confirm one is created.
At the 880 × 520 minimum window size, check Feeds: titles wrap above the cadence
picker and the unsubscribe icon, whose help and accessibility label name the
feed. Briefs grow with their text up to 180 points, then scroll.

For an end-to-end offline check, sign in online, sync, open an article and note
its brief and feed title. Stop the server, quit and relaunch the app. Confirm
cached rows and briefs appear with a connection note and no alert. Open the
article and confirm the offline-copy note appears and images stay blocked.
Queue Keep or Later, quit and relaunch again, then restart the server. Confirm
the queued state syncs and the connection note clears. Repeat with a revoked
token to check sign-out, and sign in as another account to check that the prior
account's rows, briefs, HTML and queued actions never appear.

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

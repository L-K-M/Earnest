# Three-platform releases

A source tag fixes the version. It is not evidence that all three platforms
are ready. A finished release needs passing checks for that exact revision,
installable signed clients and recorded deployment/device acceptance results.
No committed file declares the next release version.

## Source and CI gate

Before tagging, you can exercise the complete reusable gate with
`gh workflow run release.yml --ref <candidate-branch>`. A manual run validates
all platforms but never creates or publishes a release. This avoids tagging
unfinished code merely to test the workflow.

1. Merge the intended changes through reviewed PRs with passing CI. Choose the
   release version deliberately; do not tag unfinished work just to test packaging.
2. Use `scripts/release.sh X.Y.Z --push` from the clean, current release checkout.
   This creates the source tag; it does not install or publish either native app.
3. The tag workflow calls the server/web, image, Mac and Android workflows for
   the same revision. Only after they pass does it create a **draft** GitHub
   release. A failed platform gate must not produce a published release.
4. Check out that exact tag in a clean checkout with complete Git history.
   Packaging refuses a missing/mismatched tag, shallow history or tracked edits.
   It exports the committed tree into a private temporary directory before
   building, so local `.env` files, ignored files and untracked sources do not
   enter the build.

The server image is still built locally on the deployment host. No registry
credentials or anonymous image pulls are introduced. See [operations](operations.md).

## Signing prerequisites

Keep credentials outside the checkout and back them up securely. Never put
private keys or passwords in release notes, command-line arguments, Git, chat
or CI logs. The scripts do not generate production signing identities.

### Mac

Use macOS, Xcode, XcodeGen and an unlocked signing Keychain containing a
**Developer ID Application** certificate and its private key. An Apple
Development or ad-hoc identity is not a distribution identity.

Set these non-secret selectors in your release shell:

```sh
export EARNEST_MAC_SIGN_IDENTITY='Developer ID Application: Your Name (TEAMID1234)'
export EARNEST_MAC_TEAM_ID='TEAMID1234'
export EARNEST_MAC_NOTARY_PROFILE='earnest-notary'
```

Store the notary credentials in that Keychain profile with Apple's
`xcrun notarytool store-credentials`, using its interactive prompts or your
credential manager rather than putting a password in the command line.
The script uses the profile by name; it never reads or prints its password.
A prerequisite check does not prove the profile can authenticate or that Apple
will accept a submission.

### Android

Install JDK 21 and the SDK/build tools listed in [Android](android.md). Set
`JAVA_HOME` and `ANDROID_HOME`. Supply a persistent production keystore outside
the repository and make these variables available through your secret manager:

- `EARNEST_ANDROID_KEYSTORE`: readable keystore path.
- `EARNEST_ANDROID_STORE_PASSWORD`: keystore password.
- `EARNEST_ANDROID_KEY_ALIAS`: signing key alias.
- `EARNEST_ANDROID_KEY_PASSWORD`: signing key password.

Keep the same signing key for updates. Losing it prevents an in-place Android
upgrade. Do not use the disposable key created by `android-release-test.sh`.
The ordinary debug build keeps working without release credentials; artifact-
producing release tasks fail rather than falling back to the debug key.

## Build and verify the clients

For the checked-out source tag, run:

```sh
scripts/package-client.sh mac vX.Y.Z --check
scripts/package-client.sh android vX.Y.Z --check
scripts/package-client.sh mac vX.Y.Z
scripts/package-client.sh android vX.Y.Z
```

`--check` checks local prerequisites only. It does not build, sign, notarize,
install, contact a device or claim acceptance. Replace `vX.Y.Z` with the real tag.
An optional output-directory argument goes before `--check` when used.

The default output is `dist/clients/<tag>/<platform>/`. Each directory contains
its installable package, `release-<platform>.json` and `SHA256SUMS-<platform>`.
The receipt records the tag, full source revision, source-history build number,
package hash and signer identity. Output directories are exclusive: the script
will not overwrite an earlier package. Choose a new output directory to rebuild.
Build numbers come from the complete source history, not a version file.

- **Mac:** archive arm64 and x86_64 slices, stamp the tag's numeric version and
  build number, verify Developer ID/team/hardened-runtime signing, submit for
  notarization, require an `Accepted` result, then staple the ticket. Re-extract
  the final ZIP and verify its signature, ticket and Gatekeeper acceptance.
  A successful archive command alone is not success.
- **Android:** run core/app tests and lint, assemble the release APK with explicit
  tag/build properties, verify its signature against the selected keystore
  alias, and verify application ID and version fields. Passwords are passed by
  environment, not Gradle arguments. An unsigned APK is never a fallback.

Pre-release tags retain their suffix in the Android version and receipt; Apple's
marketing version is the numeric X.Y.Z portion. The full tag and revision remain
in the receipt for both platforms.

## Acceptance gate

Use the packages you will actually distribute, not only a debug build. Record
the tag, revision, package hashes, device/OS versions, environment, checks and
results in the release draft or a credential-free operations record.

At minimum:

- Start and update the isolated NAS stack, create a test user, sign in on the
  web and approve both native device logins. Check both HTTPS origins and
  restrict published ports as described in [operations](operations.md).
- Import representative feeds, read articles and images, and exercise the
  release's advertised search, settings, automation and AI workflows. Disabled
  optional providers must not break ordinary reading or full-text search.
- Round-trip seen, unread, Done, Later and Kept across all three clients. Check
  offline launch, an opened article's saved copy, durable queued changes after
  restart, reconnection, revoked credentials and account isolation.
- On Android, exercise actual WebView reading, widget rendering, notification
  permission, UnifiedPush delivery and notification/deep-link navigation. Unit
  tests and fixture signing are not device validation.
- Test backup and restore on a separate disposable stack. Verify generation
  recovery and notification withholding/release without overwriting production.
- Install the Mac ZIP on a clean account or another Mac and confirm Gatekeeper,
  login, reading and relaunch. Verify both supported architectures or explicitly
  disclose an architecture validation gap.

Document intentional platform limitations and unresolved defects. Do not mark a
missing feature implemented or a skipped operation successful to close a gate.

## Install and publish

The Mac ZIP contains `Earnest.app`; move it to Applications. Private releases
currently use manual updates, not a Sparkle update feed.

Install the signed APK with `adb install -r <apk>` or transfer it to the phone.
A production key cannot upgrade an APK installed with a different debug key.
Before removing a debug installation, reconnect and confirm every queued reading
action has synced. Uninstalling deletes that installation's private cache and
outbox; do not uninstall while it contains unsent actions.

Only after all gates pass, upload both platform directories' package, receipt
and checksum files to the existing draft. Set `tag` to the exact tag you packaged, and use your chosen output
directory. For the default layout:

```sh
tag=vX.Y.Z
out="dist/clients/$tag"
gh release upload "$tag" "$out"/mac/* "$out"/android/*
```

Confirm the upload succeeded, then inspect the attachments, acceptance record
and tag checks. Only after that review, publish it separately:

```sh
gh release edit "$tag" --draft=false
```

Do not publish the fixture APK or turn an incomplete draft into a finished
release. Code-signing, notarization, device acceptance and NAS recovery remain
separate checks; none can stand in for another.

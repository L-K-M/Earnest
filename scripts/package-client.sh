#!/usr/bin/env bash
# Build an installable client from an exact source tag. Credentials stay in the
# environment or macOS Keychain; neither keys nor passwords enter the artifact.
set -euo pipefail
umask 077

fail() { printf 'error: %s\n' "$*" >&2; exit 1; }
usage() {
  printf 'Usage: %s (mac|android) vX.Y.Z[-pre] [output-directory] [--check]\n' "$0" >&2
  exit 2
}
need() { command -v "$1" >/dev/null 2>&1 || fail "Required tool not found: $1"; }
required() { [[ -n "${!1:-}" ]] || fail "Set $1 outside the repository before packaging"; }

[[ $# -ge 2 && $# -le 4 ]] || usage
platform=$1
tag=$2
shift 2
case "$platform" in mac|android) ;; *) usage ;; esac
[[ "$tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$ ]] || fail 'Expected a source tag such as v1.2.3 or v1.2.3-rc.1'
check=false
output=''
for argument in "$@"; do
  case "$argument" in
    --check) [[ "$check" == false ]] || usage; check=true ;;
    --*) usage ;;
    *) [[ -z "$output" ]] || usage; output=$argument ;;
  esac
done

need git
need python3
root=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
revision=$(git -C "$root" rev-parse --verify HEAD)
tag_revision=$(git -C "$root" rev-parse --verify "refs/tags/$tag^{commit}" 2>/dev/null) || fail "Source tag $tag does not exist locally"
[[ "$revision" == "$tag_revision" ]] || fail "$tag must name the checked-out commit"
[[ "$(git -C "$root" rev-parse --is-shallow-repository)" == false ]] || fail 'Fetch complete history before packaging (git fetch --unshallow)'
git -C "$root" diff --quiet && git -C "$root" diff --cached --quiet || fail 'Commit or preserve tracked changes before packaging a source tag'
build=$(git -C "$root" rev-list --count "$revision")
[[ "$build" =~ ^[1-9][0-9]*$ && "$build" -le 2100000000 ]] || fail 'The source-history build number is outside the Android versionCode range'
version=${tag#v}
marketing=${version%%-*}
output=$(python3 -c 'import os,sys; print(os.path.abspath(sys.argv[1]))' "${output:-$root/dist/clients/$tag}")

if [[ "$platform" == mac ]]; then
  [[ "$(uname -s)" == Darwin ]] || fail 'Mac packaging requires macOS and Xcode'
  for tool in xcodegen xcodebuild xcrun security codesign lipo ditto spctl plutil; do need "$tool"; done
  required EARNEST_MAC_SIGN_IDENTITY
  required EARNEST_MAC_TEAM_ID
  required EARNEST_MAC_NOTARY_PROFILE
  case "$EARNEST_MAC_SIGN_IDENTITY" in 'Developer ID Application: '*) ;; *) fail 'Mac distribution requires a Developer ID Application identity, not ad-hoc or Apple Development signing' ;; esac
  [[ "$EARNEST_MAC_TEAM_ID" =~ ^[A-Z0-9]{10}$ ]] || fail 'EARNEST_MAC_TEAM_ID must be your ten-character Apple team ID'
  identities=$(security find-identity -v -p codesigning)
  printf '%s\n' "$identities" | grep -F -- "\"$EARNEST_MAC_SIGN_IDENTITY\"" >/dev/null || fail 'The requested Developer ID identity is not available in the Keychain'
  xcrun --find notarytool >/dev/null
else
  required EARNEST_ANDROID_KEYSTORE
  required EARNEST_ANDROID_STORE_PASSWORD
  required EARNEST_ANDROID_KEY_ALIAS
  required EARNEST_ANDROID_KEY_PASSWORD
  [[ -r "$EARNEST_ANDROID_KEYSTORE" && -f "$EARNEST_ANDROID_KEYSTORE" ]] || fail 'The Android keystore is not a readable file'
  EARNEST_ANDROID_KEYSTORE=$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$EARNEST_ANDROID_KEYSTORE")
  export EARNEST_ANDROID_KEYSTORE
  python3 - "$root" "$EARNEST_ANDROID_KEYSTORE" <<'PY' || fail 'Keep the production keystore outside the repository'
import os, sys
root, key = map(os.path.realpath, sys.argv[1:])
sys.exit(1 if os.path.commonpath([root, key]) == root else 0)
PY
  sdk=${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}
  [[ -n "$sdk" ]] || fail 'Set ANDROID_HOME to an Android SDK installation'
  apksigner="$sdk/build-tools/36.1.0/apksigner"
  aapt="$sdk/build-tools/36.1.0/aapt2"
  [[ -x "$apksigner" && -x "$aapt" ]] || fail 'Install Android SDK build-tools 36.1.0'
  java=${JAVA_HOME:+$JAVA_HOME/bin/}java
  keytool=${JAVA_HOME:+$JAVA_HOME/bin/}keytool
  java_version=$("$java" -version 2>&1) || fail 'Install JDK 21 and set JAVA_HOME'
  [[ "$java_version" == *'version "21.'* || "$java_version" == *'version "21"'* ]] || fail 'Client release builds require the validated JDK 21 toolchain'
  command -v "$keytool" >/dev/null 2>&1 || fail 'The JDK keytool is unavailable'
fi

if [[ "$check" == true ]]; then
  printf 'Prerequisites found for %s at %s (%s). No build, signing, notarization or device test was performed.\n' "$platform" "$tag" "$revision"
  exit 0
fi

scratch=$(mktemp -d)
published=''
complete=false
cleanup() {
  rm -rf -- "$scratch"
  if [[ "$complete" == false && -n "$published" ]]; then rm -rf -- "$published"; fi
}
trap cleanup EXIT
mkdir "$scratch/source"
# Build only committed files from the tag. Local .env files, ignored sources,
# untracked files and generated project state cannot enter this source tree.
git -C "$root" archive --format=tar "$revision" | tar -xf - -C "$scratch/source"
source_root="$scratch/source"
notary_id=''
signer=''

if [[ "$platform" == mac ]]; then
  (
    cd "$source_root/clients/mac/App"
    xcodegen generate
    xcodebuild archive -project Earnest.xcodeproj -scheme Earnest -configuration Release \
      -destination 'generic/platform=macOS' -archivePath "$scratch/Earnest.xcarchive" \
      -derivedDataPath "$scratch/DerivedData" -skipPackagePluginValidation -jobs 2 \
      ARCHS='arm64 x86_64' ONLY_ACTIVE_ARCH=NO MARKETING_VERSION="$marketing" \
      CURRENT_PROJECT_VERSION="$build" DEVELOPMENT_TEAM="$EARNEST_MAC_TEAM_ID" \
      CODE_SIGN_STYLE=Manual CODE_SIGN_IDENTITY="$EARNEST_MAC_SIGN_IDENTITY" \
      CODE_SIGNING_ALLOWED=YES CODE_SIGNING_REQUIRED=YES ENABLE_HARDENED_RUNTIME=YES
  )
  app="$scratch/Earnest.xcarchive/Products/Applications/Earnest.app"
  [[ -d "$app" ]] || fail 'Xcode did not produce Earnest.app'
  plist="$app/Contents/Info.plist"
  [[ "$(plutil -extract CFBundleIdentifier raw -o - "$plist")" == ch.lkmc.Earnest ]] || fail 'Unexpected Mac bundle identifier'
  [[ "$(plutil -extract CFBundleShortVersionString raw -o - "$plist")" == "$marketing" ]] || fail 'Mac marketing version does not match the source tag'
  [[ "$(plutil -extract CFBundleVersion raw -o - "$plist")" == "$build" ]] || fail 'Mac build number does not match the source history'
  architectures=" $(lipo -archs "$app/Contents/MacOS/Earnest") "
  [[ "$architectures" == *' arm64 '* && "$architectures" == *' x86_64 '* ]] || fail 'The Mac app must contain both arm64 and x86_64 slices'
  codesign --verify --deep --strict --verbose=2 "$app"
  signature=$(codesign --display --verbose=4 "$app" 2>&1)
  printf '%s\n' "$signature" | grep -Fx -- "TeamIdentifier=$EARNEST_MAC_TEAM_ID" >/dev/null || fail 'The Mac app is signed by a different team'
  [[ "$signature" == *'Authority=Developer ID Application:'* && "$signature" == *'(runtime)'* ]] || fail 'The Mac app is not Developer ID signed with the hardened runtime'
  ditto -c -k --sequesterRsrc --keepParent "$app" "$scratch/submission.zip"
  xcrun notarytool submit "$scratch/submission.zip" --keychain-profile "$EARNEST_MAC_NOTARY_PROFILE" --wait --output-format json > "$scratch/notary.json"
  status=$(plutil -extract status raw -o - "$scratch/notary.json") || fail 'Notary service returned no readable status'
  notary_id=$(plutil -extract id raw -o - "$scratch/notary.json") || fail 'Notary service returned no submission ID'
  [[ "$status" == Accepted ]] || fail "Notarization returned $status for submission $notary_id; inspect its notarytool log"
  xcrun stapler staple "$app"
  xcrun stapler validate "$app"
  artifact="Earnest-$tag-mac-universal.zip"
  ditto -c -k --sequesterRsrc --keepParent "$app" "$scratch/$artifact"
  mkdir "$scratch/unpacked"
  ditto -x -k "$scratch/$artifact" "$scratch/unpacked"
  # Verify the app from the actual distributed archive, not just the build tree.
  codesign --verify --deep --strict --verbose=2 "$scratch/unpacked/Earnest.app"
  xcrun stapler validate "$scratch/unpacked/Earnest.app"
  spctl --assess --type execute --verbose=2 "$scratch/unpacked/Earnest.app"
  signer=$EARNEST_MAC_TEAM_ID
else
  (
    cd "$source_root/clients/android"
    ./gradlew --no-daemon --no-configuration-cache :core:test :app:testDebugUnitTest lint :app:assembleRelease \
      "-PearnestVersion=$version" "-PearnestVersionCode=$build"
  )
  apk="$source_root/clients/android/app/build/outputs/apk/release/app-release.apk"
  [[ -f "$apk" ]] || fail 'Gradle did not produce a signed release APK'
  "$apksigner" verify --verbose "$apk"
  "$apksigner" verify --print-certs "$apk" > "$scratch/apk-certificates.txt"
  "$keytool" -exportcert -keystore "$EARNEST_ANDROID_KEYSTORE" -alias "$EARNEST_ANDROID_KEY_ALIAS" \
    -storepass:env EARNEST_ANDROID_STORE_PASSWORD -file "$scratch/signer.der"
  signer=$(python3 - "$scratch/signer.der" "$scratch/apk-certificates.txt" <<'PY'
import hashlib, pathlib, re, sys
expected = hashlib.sha256(pathlib.Path(sys.argv[1]).read_bytes()).hexdigest()
actual = re.findall(r'^Signer #\d+ certificate SHA-256 digest: ([a-fA-F0-9:]+)$', pathlib.Path(sys.argv[2]).read_text(), re.M)
if [value.replace(':', '').lower() for value in actual] != [expected]:
    sys.exit('APK signing certificate does not match the selected keystore alias')
print(expected)
PY
  )
  "$aapt" dump badging "$apk" > "$scratch/apk-metadata.txt"
  python3 - "$scratch/apk-metadata.txt" "$version" "$build" <<'PY'
import pathlib, re, sys
line = pathlib.Path(sys.argv[1]).read_text().splitlines()[0]
values = dict(re.findall(r"(\w+)='([^']*)'", line))
if values.get('name') != 'ch.lkmc.earnest' or values.get('versionName') != sys.argv[2] or values.get('versionCode') != sys.argv[3]:
    sys.exit('APK identity or version does not match the tagged source')
PY
  artifact="Earnest-$tag-android.apk"
  cp "$apk" "$scratch/$artifact"
fi

mkdir -p -- "$output"
destination="$output/$platform"
# An exclusive directory prevents silently replacing a previously packaged tag.
# Do not arm cleanup until mkdir succeeds: an interrupted invocation must never
# remove an existing output directory owned by an earlier release.
mkdir -- "$destination" || fail 'The platform output directory already exists; choose a new output directory'
published=$destination
cp "$scratch/$artifact" "$published/$artifact"
python3 - "$published" "$artifact" "$platform" "$tag" "$revision" "$version" "$build" "$signer" "$notary_id" <<'PY'
import hashlib, json, pathlib, sys
out, name, platform, tag, revision, version, build, signer, notary = sys.argv[1:]
folder = pathlib.Path(out)
digest = hashlib.sha256((folder / name).read_bytes()).hexdigest()
receipt = dict(platform=platform, tag=tag, revision=revision, version=version,
               build=int(build), artifact=name, sha256=digest)
receipt['signer'] = {'team_id': signer} if platform == 'mac' else {'certificate_sha256': signer}
if platform == 'mac':
    receipt['notarization'] = {'status': 'Accepted', 'submission_id': notary}
(folder / f'release-{platform}.json').write_text(json.dumps(receipt, indent=2) + '\n')
(folder / f'SHA256SUMS-{platform}').write_text(f'{digest}  {name}\n')
PY
complete=true
printf 'Verified %s package: %s/%s\nReceipt: %s/release-%s.json\n' "$platform" "$published" "$artifact" "$published" "$platform"

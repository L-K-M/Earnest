#!/usr/bin/env bash
# Android release configuration regression smoke test.
#
# Fixture only: creates a throwaway keystore under mktemp (outside the
# checkout), exercises release validation failures, then builds a signed
# fixture release APK. The APK produced here is a test fixture signed with a
# throwaway key and is NOT FOR DISTRIBUTION. No production credentials are
# used, logged, or persisted.
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ANDROID_DIR="$SCRIPT_DIR/../clients/android"

: "${JAVA_HOME:?JAVA_HOME must point to a JDK}"
: "${ANDROID_HOME:?ANDROID_HOME must point to the Android SDK}"
export JAVA_HOME ANDROID_HOME

BUILD_TOOLS_DIR="$ANDROID_HOME/build-tools"
[[ -d "$BUILD_TOOLS_DIR" ]] || { echo "error: missing $BUILD_TOOLS_DIR" >&2; exit 1; }

# Match the pinned toolchain rather than silently testing another SDK version.
BUILD_TOOLS_VERSION="36.1.0"
AAPT2="$BUILD_TOOLS_DIR/$BUILD_TOOLS_VERSION/aapt2"
APKSIGNER="$BUILD_TOOLS_DIR/$BUILD_TOOLS_VERSION/apksigner"
[[ -x "$AAPT2" && -x "$APKSIGNER" ]] || {
  echo "error: missing aapt2/apksigner in $BUILD_TOOLS_DIR/$BUILD_TOOLS_VERSION" >&2
  exit 1
}
KEYTOOL="$JAVA_HOME/bin/keytool"
[[ -x "$KEYTOOL" ]] || { echo "error: missing $KEYTOOL" >&2; exit 1; }

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/earnest-android-release-test.XXXXXX")"
cleanup() { rm -rf -- "$WORK_DIR"; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# Relocate every fixture output, not only the key. Testing must not overwrite a
# real release APK or leave a throwaway-key APK at the ordinary release path.
export EARNEST_RELEASE_FIXTURE_BUILD_ROOT="$WORK_DIR/build"
INIT_SCRIPT="$WORK_DIR/output-directories.gradle"
cat > "$INIT_SCRIPT" <<'GROOVY'
gradle.beforeProject { project ->
    project.layout.buildDirectory.set(new File(System.getenv('EARNEST_RELEASE_FIXTURE_BUILD_ROOT'), project.path.replace(':', '_')))
}
GROOVY
RELEASE_APK="$EARNEST_RELEASE_FIXTURE_BUILD_ROOT/_app/outputs/apk/release/app-release.apk"

# Fixture credentials only. This password is not a production secret and is
# never printed; keytool reads it from the environment.
FIXTURE_STORE_PASSWORD="fixture-test-password"
export FIXTURE_STORE_PASSWORD
FIXTURE_ALIAS="earnest-release-fixture"
FIXTURE_VERSION_NAME="9.9.9"
FIXTURE_VERSION_CODE="900000009"

run_gradle() {
  local log="$1"
  shift
  ( cd "$ANDROID_DIR" && ./gradlew --no-daemon --no-configuration-cache --init-script "$INIT_SCRIPT" "$@" ) >"$log" 2>&1
}

run_gradle_no_secrets() {
  local log="$1"
  shift
  ( cd "$ANDROID_DIR" && env -u EARNEST_ANDROID_KEYSTORE -u EARNEST_ANDROID_STORE_PASSWORD \
      -u EARNEST_ANDROID_KEY_ALIAS -u EARNEST_ANDROID_KEY_PASSWORD \
      ./gradlew --no-daemon --no-configuration-cache --init-script "$INIT_SCRIPT" "$@" ) >"$log" 2>&1
}

expect_failure() {
  local label="$1" pattern="$2" runner="$3"
  shift 3
  local log="$WORK_DIR/$label.log"
  local status=0
  "$runner" "$log" "$@" || status=$?
  if [[ $status -eq 0 ]]; then
    echo "FAIL: $label: expected Gradle to fail, but it succeeded" >&2
    tail -n 40 "$log" >&2
    exit 1
  fi
  if ! grep -q -- "$pattern" "$log"; then
    echo "FAIL: $label: failure output does not match /$pattern/" >&2
    tail -n 40 "$log" >&2
    exit 1
  fi
  echo "PASS: $label (failed as expected; matched /$pattern/)"
}

expect_success() {
  local label="$1" runner="$2"
  shift 2
  local log="$WORK_DIR/$label.log"
  if ! "$runner" "$log" "$@"; then
    echo "FAIL: $label: expected Gradle to succeed" >&2
    tail -n 60 "$log" >&2
    exit 1
  fi
  echo "PASS: $label"
}

# Ephemeral fixture keystore under mktemp; removed by the cleanup trap.
FIXTURE_KEYSTORE="$WORK_DIR/fixture-release.keystore"
"$KEYTOOL" -genkeypair \
  -keystore "$FIXTURE_KEYSTORE" \
  -storepass:env FIXTURE_STORE_PASSWORD \
  -keypass:env FIXTURE_STORE_PASSWORD \
  -alias "$FIXTURE_ALIAS" \
  -keyalg RSA -keysize 2048 -validity 7 \
  -dname "CN=Earnest Release Fixture, OU=Test Fixture, O=Earnest, C=CH" \
  >/dev/null

normalize_sha256() { printf '%s' "$1" | tr -d ': ' | tr 'A-F' 'a-f'; }

FIXTURE_CERT_SHA256="$("$KEYTOOL" -list -v -keystore "$FIXTURE_KEYSTORE" \
  -storepass:env FIXTURE_STORE_PASSWORD -alias "$FIXTURE_ALIAS" \
  | awk '/SHA256:/{print $2}' | head -n 1)"
FIXTURE_CERT_SHA256="$(normalize_sha256 "$FIXTURE_CERT_SHA256")"
[[ -n "$FIXTURE_CERT_SHA256" ]] || {
  echo "error: could not read fixture certificate fingerprint" >&2
  exit 1
}

echo "== 1. Missing release inputs (no secrets, no explicit version) =="
expect_failure "missing-inputs" "Release inputs are invalid" run_gradle_no_secrets \
  :app:assembleRelease

echo "== 2. Explicit malformed debug versions must not silently become placeholders =="
expect_failure "noninteger-version-code" "must be an integer" run_gradle_no_secrets \
  :app:assembleDebug -PearnestVersionCode="not-an-integer"

echo "== 2a. No-secret debug assemble and lint (including release analysis) =="
expect_success "debug-lint-no-secrets" run_gradle_no_secrets \
  :app:assembleDebug :app:lint :app:lintVitalRelease

export EARNEST_ANDROID_KEYSTORE="$FIXTURE_KEYSTORE"
export EARNEST_ANDROID_STORE_PASSWORD="$FIXTURE_STORE_PASSWORD"
export EARNEST_ANDROID_KEY_ALIAS="$FIXTURE_ALIAS"
export EARNEST_ANDROID_KEY_PASSWORD="$FIXTURE_STORE_PASSWORD"

echo "== 3. Invalid version name =="
expect_failure "invalid-version" "must be numeric X.Y.Z" run_gradle \
  :app:assembleRelease -PearnestVersion="not-a-version" -PearnestVersionCode="$FIXTURE_VERSION_CODE"

echo "== 4. Invalid version code =="
expect_failure "invalid-version-code" "must be between 1 and 2100000000" run_gradle \
  :app:assembleRelease -PearnestVersion="$FIXTURE_VERSION_NAME" -PearnestVersionCode="2100000001"

echo "== 5. Missing keystore file =="
EARNEST_ANDROID_KEYSTORE="$WORK_DIR/does-not-exist.keystore"
export EARNEST_ANDROID_KEYSTORE
expect_failure "missing-keystore-file" "does not point to an existing key file" run_gradle \
  :app:assembleRelease -PearnestVersion="$FIXTURE_VERSION_NAME" -PearnestVersionCode="$FIXTURE_VERSION_CODE"
EARNEST_ANDROID_KEYSTORE="$FIXTURE_KEYSTORE"
export EARNEST_ANDROID_KEYSTORE

echo "== 6. Signed fixture release build =="
expect_success "signed-fixture" run_gradle \
  :app:assembleRelease -PearnestVersion="$FIXTURE_VERSION_NAME" -PearnestVersionCode="$FIXTURE_VERSION_CODE"

if [[ ! -f "$RELEASE_APK" ]]; then
  echo "FAIL: expected signed APK at $RELEASE_APK" >&2
  exit 1
fi

echo "== 7. APK signature and version verification =="
VERIFY_LOG="$WORK_DIR/apksigner-verify.log"
if ! "$APKSIGNER" verify --print-certs --verbose "$RELEASE_APK" >"$VERIFY_LOG" 2>&1; then
  echo "FAIL: apksigner verify failed" >&2
  cat "$VERIFY_LOG" >&2
  exit 1
fi
APK_CERT_SHA256="$(awk '/certificate SHA-256 digest:/{print $NF}' "$VERIFY_LOG" | head -n 1)"
APK_CERT_SHA256="$(normalize_sha256 "$APK_CERT_SHA256")"
if [[ "$APK_CERT_SHA256" != "$FIXTURE_CERT_SHA256" ]]; then
  echo "FAIL: APK signer certificate does not match the fixture keystore" >&2
  echo "  fixture: $FIXTURE_CERT_SHA256" >&2
  echo "  apk:     $APK_CERT_SHA256" >&2
  exit 1
fi
echo "PASS: APK signature verified with apksigner (fixture certificate)"

BADGING="$("$AAPT2" dump badging "$RELEASE_APK")"
ACTUAL_VERSION_NAME="$(printf '%s\n' "$BADGING" | sed -n "s/^package: .*versionName='\([^']*\)'.*/\1/p" | head -n 1)"
ACTUAL_VERSION_CODE="$(printf '%s\n' "$BADGING" | sed -n "s/^package: .*versionCode='\([^']*\)'.*/\1/p" | head -n 1)"
if [[ "$ACTUAL_VERSION_NAME" != "$FIXTURE_VERSION_NAME" || "$ACTUAL_VERSION_CODE" != "$FIXTURE_VERSION_CODE" ]]; then
  echo "FAIL: APK version mismatch (got name='$ACTUAL_VERSION_NAME' code='$ACTUAL_VERSION_CODE';" >&2
  echo "      want name='$FIXTURE_VERSION_NAME' code='$FIXTURE_VERSION_CODE')" >&2
  exit 1
fi
echo "PASS: APK version fields verified with aapt2 (name=$ACTUAL_VERSION_NAME, code=$ACTUAL_VERSION_CODE)"

echo
echo "NOT FOR DISTRIBUTION: the verified APK used a throwaway fixture key."
echo "The fixture APK, keystore and logs under $WORK_DIR are removed on exit."

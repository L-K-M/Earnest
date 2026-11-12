#!/usr/bin/env bash
#
# Build every Earnest target this host can build, and print one clear summary.
# The single local entry point; CI builds the same pieces in .github/workflows/.
#
#   web     — web client (pnpm) → web/dist, copied into internal/web/dist so
#             the binary and the image embed it (the committed copy there is a
#             placeholder). Builds before `binary` on a default run.
#   binary  — Go server binary → dist/earnest (host arch, version-stamped).
#             It embeds whatever internal/web/dist already holds — list `web`
#             first, or rely on the default target order, for a fresh client.
#   docker  — deployment image → earnest:local, what compose.yaml and
#             update.sh consume. The Dockerfile builds the web client itself.
#   mac     — native macOS app (XcodeGen + xcodebuild) → dist/Earnest.app.
#             macOS hosts only.
#   android — Android debug APK (gradlew :app:assembleDebug)
#             → dist/earnest-debug.apk. The only signed variant — no release
#             signing is configured. Add --install to install via adb.
#
# Usage:
#   scripts/build.sh                 # every target this host can build
#   scripts/build.sh mac binary      # just these (naming a target turns an
#                                    # infeasible target into an error, not a skip)
#   scripts/build.sh --debug mac     # Debug Xcode configuration (the other
#                                    # targets have no debug/release split)
#   scripts/build.sh --install       # build both native apps; install the Mac
#                                    # app to /Applications/Earnest.app and the
#                                    # Android APK to a connected device
#   scripts/build.sh android --install # build and install only the Android app
#                                    # (no connected device: skip installation)
#   scripts/build.sh --clean         # remove dist/
#
# VERSION and REVISION stamp the binary and the image; they default to
# `git describe` / the short HEAD.
#
# Exit status is non-zero if any requested target fails to build or install.
# Targets that can't build on this host (missing toolchain, wrong OS) are reported
# as skipped, and only fail the run when you named them explicitly.
set -uo pipefail

# Absolute path to this script — usage() must find it after the cd below.
SELF="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT" || exit 1

GO="${GO:-go}"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
REVISION="${REVISION:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
IMAGE=earnest:local

PROFILE="release"
INSTALL=false
declare -a REQUESTED=()
EXPLICIT=0

usage() {
  # Print the leading comment block (the file header), minus the shebang.
  awk 'NR==1 && /^#!/ {next} /^#/ {sub(/^# ?/,""); print; next} {exit}' "$SELF"
  exit "${1:-0}"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    -h|--help) usage 0 ;;
    --clean) rm -rf dist; echo "==> removed dist/"; exit 0 ;;
    --debug) PROFILE="debug"; shift ;;
    --install) INSTALL=true; shift ;;
    --docker) REQUESTED+=(binary docker); EXPLICIT=1; shift ;;  # legacy flag: binary + image
    web|binary|docker|mac|android) REQUESTED+=("$1"); EXPLICIT=1; shift ;;
    all) REQUESTED=(web binary docker mac android); EXPLICIT=1; shift ;;
    *) echo "unknown argument: $1" >&2; usage 1 ;;
  esac
done

case "$(uname -s)" in
  Darwin) HOST="macos" ;;
  Linux) HOST="linux" ;;
  MINGW*|MSYS*|CYGWIN*) HOST="windows" ;;
  *) HOST="unknown" ;;
esac

# Without explicit targets, --install builds the native clients this host can
# build. Explicit targets keep their usual feasibility checks and selection.
if $INSTALL && [[ ${#REQUESTED[@]} -eq 0 ]]; then
  REQUESTED=(mac android)
fi

# Default to all targets; feasibility is decided per-target below.
if [[ ${#REQUESTED[@]} -eq 0 ]]; then
  REQUESTED=(web binary docker mac android)
fi

echo "Earnest build"
echo "  host:    $HOST ($(uname -s))"
echo "  profile: $PROFILE"
echo "  targets: ${REQUESTED[*]}"
echo "  version: $VERSION ($REVISION)"
echo

have() { command -v "$1" >/dev/null 2>&1; }

declare -a RESULTS=()
record() { RESULTS+=("$1"); }   # "binary: built -> …" | "mac: skipped (…)" | "android: FAILED"

# Collect a finished artifact into dist/ (file or directory). The staged copy
# is what the summary points at — one folder with everything this run built.
DIST="$ROOT/dist"
STAGED=0
STAGED_APP=0
stage() {
  local src="$1" name="${2:-$(basename "$1")}"
  mkdir -p "$DIST"
  rm -rf "${DIST:?}/$name"
  cp -R "$src" "$DIST/$name" || return 1
  STAGED=$((STAGED + 1))
  echo "-- staged dist/$name"
}

# Was a given target explicitly named on the command line?
was_named() {
  [[ $EXPLICIT -eq 1 ]] || return 1
  local t
  for t in "${REQUESTED[@]}"; do [[ "$t" == "$1" ]] && return 0; done
  return 1
}

# Skip a target: a soft skip on a default run, a hard error when named.
skip_or_fail() {
  local target="$1" reason="$2"
  if was_named "$target"; then
    echo "!! $target: cannot build on this host — $reason" >&2
    record "$target: FAILED ($reason)"
    return 1
  fi
  echo ".. $target: skipped — $reason"
  record "$target: skipped ($reason)"
  return 0
}

# Copy completely before replacing embedded assets. Keep the old directory
# until publication succeeds so a copy or rename failure leaves it usable.
stage_web() {
  local tmp
  tmp="$(mktemp -d internal/web/.dist-stage.XXXXXX)" || return 1
  if ! mkdir "$tmp/next" || ! cp -R web/dist/. "$tmp/next/"; then
    rm -rf "$tmp"
    return 1
  fi
  if [[ -e internal/web/dist ]] && ! mv internal/web/dist "$tmp/previous"; then
    rm -rf "$tmp"
    return 1
  fi
  if ! mv "$tmp/next" internal/web/dist; then
    if [[ -e "$tmp/previous" ]] && ! mv "$tmp/previous" internal/web/dist; then
      echo "!! web: could not restore previous assets; recover them from $tmp/previous" >&2
      return 1
    fi
    rm -rf "$tmp"
    return 1
  fi
  rm -rf "$tmp"
}

# ---------------------------------------------------------------------------
build_web() {
  echo "== web (web client, embedded by binary and docker) =="
  if ! have pnpm; then skip_or_fail web "pnpm not found"; return; fi
  echo "-- pnpm install + build"
  if ( cd web && pnpm install --frozen-lockfile && pnpm build ); then
    if [[ ! -d web/dist ]]; then
      echo "!! web: pnpm build produced no web/dist" >&2
      record "web: FAILED (no dist output)"; return 1
    fi
    if stage_web; then
      record "web: built -> internal/web/dist"
    else
      echo "!! web: failed to stage client into internal/web/dist" >&2
      record "web: FAILED (stage copy)"; return 1
    fi
  else
    echo "!! web: pnpm build failed" >&2
    record "web: FAILED (pnpm build)"; return 1
  fi
}

# ---------------------------------------------------------------------------
build_binary() {
  echo "== binary (Go server) =="
  if ! have "$GO"; then skip_or_fail binary "Go toolchain ($GO) not found"; return; fi
  mkdir -p "$DIST" || { echo "!! binary: cannot create dist/" >&2; record "binary: FAILED (mkdir dist)"; return 1; }
  echo "-- go build → dist/earnest (version=$VERSION revision=$REVISION)"
  if "$GO" build -trimpath \
       -ldflags "-s -w -X main.version=$VERSION -X main.revision=$REVISION" \
       -o "$DIST/earnest" ./cmd/earnest; then
    STAGED=$((STAGED + 1))
    record "binary: built -> dist/earnest"
  else
    echo "!! binary: go build failed" >&2
    record "binary: FAILED (go build)"; return 1
  fi
}

# ---------------------------------------------------------------------------
build_docker() {
  echo "== docker (deployment image) =="
  if ! have docker; then skip_or_fail docker "docker not found"; return; fi
  if ! docker info >/dev/null 2>&1; then
    skip_or_fail docker "docker daemon not reachable"; return
  fi
  echo "-- docker build → $IMAGE"
  if docker build --build-arg VERSION="$VERSION" --build-arg REVISION="$REVISION" \
       -t "$IMAGE" .; then
    record "docker: built -> $IMAGE"
  else
    echo "!! docker: image build failed" >&2
    record "docker: FAILED (docker build)"; return 1
  fi
}

# ---------------------------------------------------------------------------
build_mac() {
  echo "== mac (native macOS app) =="
  if [[ "$HOST" != "macos" ]]; then
    skip_or_fail mac "requires an Xcode/macOS host"; return
  fi
  if ! have xcodegen; then
    skip_or_fail mac "xcodegen not found (install: brew install xcodegen)"; return
  fi
  if ! have xcodebuild; then
    skip_or_fail mac "xcodebuild not found (install Xcode)"; return
  fi

  echo "-- xcodegen generate"
  if ! ( cd clients/mac/App && xcodegen generate ); then
    record "mac: FAILED (xcodegen)"; return 1
  fi

  local config="Release"
  [[ "$PROFILE" == "debug" ]] && config="Debug"
  # Pin DerivedData under dist/ (gitignored) so the product path is deterministic
  # instead of a hashed ~/Library/Developer/Xcode/DerivedData/... path.
  local dd="$DIST/macos/DerivedData" app=""
  echo "-- xcodebuild ($config)"
  if ( cd clients/mac/App && xcodebuild build \
         -project Earnest.xcodeproj -scheme Earnest -configuration "$config" \
         -destination 'platform=macOS' -derivedDataPath "$dd" \
         -skipPackagePluginValidation ); then
    app="$dd/Build/Products/$config/Earnest.app"
    if [[ -d "$app" ]] && stage "$app" "Earnest.app"; then
      record "mac: built ($config) -> dist/Earnest.app"
      STAGED_APP=1
    else
      echo "!! mac: product not found to stage" >&2
      record "mac: FAILED ($config — product not found to stage)"; return 1
    fi
  else
    echo "!! mac: xcodebuild failed" >&2
    record "mac: FAILED (xcodebuild)"; return 1
  fi

  if $INSTALL; then
    if [[ -z "$app" ]]; then
      echo "!! mac: nothing to install (product not found)" >&2
      record "mac: install FAILED (product not found)"; return 1
    fi
    echo "-- installing /Applications/Earnest.app"
    # Copy to a sibling path first so a failed ditto can't destroy the
    # previous install; the final rename is atomic on the same volume.
    local dst="/Applications/Earnest.app" tmp="/Applications/.Earnest.app.new.$$"
    rm -rf "$tmp"
    # ditto preserves the signature, resource forks, and permissions.
    if ditto "$app" "$tmp" && rm -rf "$dst" && mv "$tmp" "$dst"; then
      INSTALLED="$dst"
      record "mac: installed -> /Applications/Earnest.app"
      # Replacing a bundle in place leaves LaunchServices with the OLD
      # registration for the path; it then refuses to open the app with error
      # -10810 even though the bundle runs fine. Force a re-registration.
      LSREG="/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
      if [[ -x "$LSREG" ]]; then
        "$LSREG" -f "$INSTALLED" >/dev/null 2>&1 || true
      fi
      if ! codesign --verify --deep --strict "$INSTALLED" 2>/dev/null; then
        echo "!! mac: $INSTALLED fails codesign verification — it may not launch." >&2
        record "mac: WARNING — installed app fails codesign verify"
      fi
    else
      rm -rf "$tmp"
      record "mac: install FAILED (ditto)"; return 1
    fi
  fi
}

# ---------------------------------------------------------------------------
# An installed Android SDK is what separates "can build the APK" from a long
# doomed Gradle run; probe the usual locations.
detect_android_sdk() {
  local c
  for c in "${ANDROID_SDK_ROOT:-}" "${ANDROID_HOME:-}" \
           "$HOME/Library/Android/sdk" "$HOME/Android/Sdk"; do
    [[ -n "$c" && -d "$c/platforms" ]] && { echo "$c"; return 0; }
  done
  return 1
}

install_android() {
  local sdk="$1" adb="adb" devices serial state extra selected="" ready=0 unavailable=0
  if [[ -x "$sdk/platform-tools/adb" ]]; then
    adb="$sdk/platform-tools/adb"
  elif ! have adb; then
    echo "!! android: adb not found; install Android SDK platform tools" >&2
    record "android: install FAILED (adb not found)"; return 1
  fi

  if ! devices="$("$adb" devices)"; then
    echo "!! android: could not list connected devices" >&2
    record "android: install FAILED (adb devices)"; return 1
  fi
  while read -r serial state extra; do
    [[ -z "$serial" || "$serial" == "List" || "$serial" == \* ]] && continue
    [[ -n "${ANDROID_SERIAL:-}" && "$serial" != "$ANDROID_SERIAL" ]] && continue
    if [[ "$state" == "device" ]]; then
      selected="$serial"
      ready=$((ready + 1))
    else
      unavailable=1
    fi
  done <<< "$devices"

  if [[ $ready -eq 0 ]]; then
    if [[ -n "${ANDROID_SERIAL:-}" || $unavailable -eq 1 ]]; then
      echo "!! android: no ready device${ANDROID_SERIAL:+ matching ANDROID_SERIAL=$ANDROID_SERIAL}; enable and authorize USB debugging, or reconnect the device" >&2
      record "android: install FAILED (device not ready)"; return 1
    fi
    echo ".. android: installation skipped (no connected device)"
    record "android: install skipped (no connected device)"; return 0
  fi
  if [[ $ready -gt 1 ]]; then
    echo "!! android: multiple devices connected; set ANDROID_SERIAL to choose one (see adb devices)" >&2
    record "android: install FAILED (multiple devices)"; return 1
  fi

  echo "-- installing dist/earnest-debug.apk to $selected"
  if "$adb" -s "$selected" install -r "$DIST/earnest-debug.apk"; then
    record "android: installed -> $selected"
  else
    record "android: install FAILED (adb install)"; return 1
  fi
}

build_android() {
  echo "== android (debug APK) =="
  if [[ "$HOST" != "linux" && "$HOST" != "macos" ]]; then
    skip_or_fail android "needs a Linux or macOS host"; return
  fi
  if [[ ! -x clients/android/gradlew ]]; then
    skip_or_fail android "clients/android/gradlew missing or not executable"; return
  fi
  if ! have java && [[ -z "${JAVA_HOME:-}" ]]; then
    skip_or_fail android "JDK not found (install JDK 21, or set JAVA_HOME)"; return
  fi
  local sdk
  if ! sdk="$(detect_android_sdk)"; then
    skip_or_fail android "no Android SDK found (set ANDROID_SDK_ROOT, or install one via Android Studio)"; return
  fi
  # Gradle locates the SDK via local.properties or ANDROID_HOME; supply the env.
  export ANDROID_HOME="$sdk"
  echo "-- Android SDK: $sdk"
  echo "-- gradlew :app:assembleDebug"
  if ( cd clients/android && ./gradlew :app:assembleDebug ); then
    local apk="clients/android/app/build/outputs/apk/debug/app-debug.apk"
    if [[ -f "$apk" ]] && stage "$apk" "earnest-debug.apk"; then
      record "android: built -> dist/earnest-debug.apk"
    else
      echo "!! android: APK not found to stage" >&2
      record "android: FAILED (APK not found to stage)"; return 1
    fi
  else
    echo "!! android: gradle build failed" >&2
    record "android: FAILED (gradle)"; return 1
  fi

  if $INSTALL; then
    install_android "$sdk" || return 1
  fi
}

# ---------------------------------------------------------------------------
FAILED=0
INSTALLED=""
for target in "${REQUESTED[@]}"; do
  case "$target" in
    web)     build_web     || FAILED=1 ;;
    binary)  build_binary  || FAILED=1 ;;
    docker)  build_docker  || FAILED=1 ;;
    mac)     build_mac     || FAILED=1 ;;
    android) build_android || FAILED=1 ;;
  esac
  echo
done

echo "Summary"
for line in "${RESULTS[@]}"; do echo "  $line"; done

# Reveal the result: the installed copy when --install ran, else the dist/
# folder with everything this run produced (macOS only — elsewhere the paths
# in the summary are the deliverable).
if [[ -n "$INSTALLED" ]]; then
  echo "  installed: $INSTALLED"
  if [[ "$(uname -s)" == "Darwin" ]]; then
    open -R "$INSTALLED"          # Finder, with the app selected
  fi
elif [[ "$STAGED" -gt 0 ]]; then
  echo "  artifacts: $DIST"
  if [[ "$(uname -s)" == "Darwin" ]]; then
    # Highlight the app when one was staged — dist/ also holds the macos/
    # DerivedData intermediates, which are not deliverables.
    if [[ "$STAGED_APP" -eq 1 ]]; then open -R "$DIST/Earnest.app"; else open "$DIST"; fi
  fi
fi

exit "$FAILED"

#!/usr/bin/env bash
# Build the Earnest binary into dist/ — and, with --docker, the deploy image.
#
#   scripts/build.sh            # go build → dist/earnest (host arch)
#   scripts/build.sh --docker   # also docker build → earnest:local
#   scripts/build.sh --clean    # remove dist/
#
# The image is what compose.yaml and update.sh consume; the plain binary is
# for running outside Docker. No lkm-build kind covers a Go service, so this
# is a bespoke script in the same contract: one artifact staged into dist/.
set -euo pipefail

cd "$(dirname "$0")/.."

GO="${GO:-go}"
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
REVISION="${REVISION:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
DOCKER=0

for arg in "$@"; do
  case "$arg" in
    --docker) DOCKER=1 ;;
    --clean)  rm -rf dist; echo "==> removed dist/"; exit 0 ;;
    -h|--help)
      awk 'NR==1 && /^#!/ {next} /^#/ {sub(/^# ?/,""); print; next} {exit}' "$0"
      exit 0 ;;
    *) echo "unknown argument: $arg (try --help)" >&2; exit 2 ;;
  esac
done

echo "==> go build → dist/earnest (version=$VERSION revision=$REVISION)"
"$GO" build -trimpath \
  -ldflags "-s -w -X main.version=$VERSION -X main.revision=$REVISION" \
  -o dist/earnest ./cmd/earnest

if [[ "$DOCKER" -eq 1 ]]; then
  echo "==> docker build → earnest:local"
  docker build --build-arg VERSION="$VERSION" --build-arg REVISION="$REVISION" \
    -t earnest:local .
fi

echo "==> done: dist/earnest"

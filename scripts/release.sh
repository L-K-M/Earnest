#!/usr/bin/env bash
# Tags a source revision as "v<version>"; --push also pushes branch and tag.
# CI (images.yml) verifies the image builds for both architectures on every
# tag; deployments build locally from the chosen source revision via
# ./update.sh, so nothing publishes to a registry. No committed file declares
# a release version, so the version argument is required.
#
#   scripts/release.sh 0.1.0          # check tree is clean + tag is new, tag v0.1.0
#   scripts/release.sh 0.1.0 --push   # …also push HEAD + tag (CI checks builds)
#
# Usage: scripts/release.sh X.Y.Z[-pre] [--push]
# Shared engine: https://github.com/L-K-M/release-tool (this stub only sets config).
set -euo pipefail

export RELEASE_APP_NAME="Earnest"
export RELEASE_KIND="tag-only"
export RELEASE_CI_NOTE="CI (images.yml) verifies both image architectures on the tag; the release is the tagged source, deployed via ./update.sh which builds locally."
export RELEASE_INVOKED_AS="scripts/release.sh"

BIN="${LKM_RELEASE_BIN:-lkm-release}"
command -v "$BIN" >/dev/null 2>&1 || {
  echo "error: lkm-release not found — clone https://github.com/L-K-M/release-tool and run ./install.sh" >&2
  exit 1
}
exec "$BIN" "$@"

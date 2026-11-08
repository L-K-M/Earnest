#!/usr/bin/env bash
# Tags a source revision as "v<version>"; --push also pushes branch and tag.
# CI validates server, web, images and both native clients at the tag before
# preparing a release draft. Signed client packages and acceptance evidence
# are still required before publication; see docs/releases.md. Deployments
# build the server image locally, never through a registry. The tag supplies
# the release version, so the version argument is required.
#
#   scripts/release.sh 0.1.0          # check tree is clean + tag is new, tag v0.1.0
#   scripts/release.sh 0.1.0 --push   # …also push HEAD + tag (CI checks builds)
#
# Usage: scripts/release.sh X.Y.Z[-pre] [--push]
# Shared engine: https://github.com/L-K-M/release-tool (this stub only sets config).
set -euo pipefail

export RELEASE_APP_NAME="Earnest"
export RELEASE_KIND="tag-only"
export RELEASE_CI_NOTE="CI validates server, web, images and both native clients at the tag, then prepares a draft. Build signed clients with scripts/package-client.sh and record acceptance before publishing (docs/releases.md). Server images are built locally."
export RELEASE_INVOKED_AS="scripts/release.sh"

BIN="${LKM_RELEASE_BIN:-lkm-release}"
command -v "$BIN" >/dev/null 2>&1 || {
  echo "error: lkm-release not found — clone https://github.com/L-K-M/release-tool and run ./install.sh" >&2
  exit 1
}
exec "$BIN" "$@"

#!/usr/bin/env bash
#
# Update an Earnest deployment: sync source, refresh external images and rebuild
# the owned image locally before recreating the stack.
#
#   ./update.sh [branch]
#
# Syncs the current branch by default (pass a branch name to override). Requires:
#   - git with pull access to this repo (the GitHub CLI `gh` is used when
#     found — on PATH or next to this checkout in the parent directory —
#     with a plain `git pull --ff-only` fallback)
#   - Docker with the Compose plugin
#   - a `.env` next to this file with the secrets compose.yaml expects
#     (see .env.example)
set -euo pipefail

cd "$(dirname "$0")"

branch="${1:-$(git rev-parse --abbrev-ref HEAD)}"

if [[ ! -f .env ]]; then
    echo "WARNING: no .env found — compose interpolates EARNEST_DB_PASSWORD" >&2
    echo "         and friends from it (see .env.example)." >&2
fi

# gh is optional: it does the sync when found, with the git fallback below.
# Deployments without a system-wide install can drop the binary next to the
# checkout — `../gh`, `../bin/gh`, or an unpacked release `../gh*/bin/gh`.
GH=""
if command -v gh >/dev/null 2>&1; then
    GH="gh"
else
    for candidate in ../gh ../bin/gh ../gh*/bin/gh; do
        if [[ -f "$candidate" && -x "$candidate" ]]; then
            GH="$candidate"
            break
        fi
    done
fi

# The recreate below uses this checkout (compose.yaml, .env), so an explicitly
# named branch must be checked out — syncing it alone would redeploy the old one.
if [[ "$branch" != "$(git rev-parse --abbrev-ref HEAD)" ]]; then
    echo "==> Switching to '$branch'…"
    git checkout "$branch"
fi

echo "==> Syncing '$branch' from the remote…"
if [[ -n "$GH" ]]; then
    if ! "$GH" repo sync --branch "$branch"; then
        echo "    gh repo sync failed; falling back to: git pull --ff-only" >&2
        git pull --ff-only origin "$branch"
    fi
else
    git pull --ff-only origin "$branch"
fi

# pull_policy: build skips owned images; pull failures must stop this update.
echo "==> Pulling external images…"
docker compose pull

# --pull refreshes the base images (alpine, golang) too — without it a local
# build reuses cached bases forever and the update never actually refreshes
# the base layers.
echo "==> Building local images…"
docker compose build --pull

echo "==> Recreating containers…"
docker compose up -d --remove-orphans

# Superseded earnest:local layers stay as a rollback target; prune only if the
# deployment documents a different stance.

echo "==> Stack status:"
docker compose ps

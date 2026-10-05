#!/usr/bin/env bash
set -euo pipefail

# release.sh cuts a new release:
#   1. tags the current commit as v<version>,
#   2. pushes the commit and the tag to the origin remote.
#
# Pushing the tag triggers the GitHub Actions release workflow
# (.github/workflows/release.yml), which builds binaries and opens a release.
#
# Usage: scripts/release.sh <X.Y.Z>
#   e.g. scripts/release.sh 1.0.0

new_version="${1:-}"

# Require an explicit, valid semantic version.
if ! printf '%s' "$new_version" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "Usage: scripts/release.sh <X.Y.Z>" >&2
  echo "  e.g. scripts/release.sh 1.0.0" >&2
  exit 1
fi

tag="v$new_version"
if git rev-parse "$tag" >/dev/null 2>&1; then
  echo "! Tag $tag already exists" >&2
  exit 1
fi

# Require a clean tree so the tag points at committed code only.
if [ -n "$(git status --porcelain)" ]; then
  echo "! Working tree has uncommitted changes; commit or stash them first" >&2
  exit 1
fi

git tag -a "$tag" -m "Release $tag"
git push origin HEAD
git push origin "$tag"

echo
echo "✓ Released $tag"
echo "  Branch and tag pushed to origin; the release workflow is now running."

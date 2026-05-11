#!/usr/bin/env bash
# Compute the next semver tag for a Go sub-module.
# Usage: next-version.sh <prefix> <bump>

set -euo pipefail

prefix="${1:?prefix required (e.g. packages/internalauth-go)}"
bump="${2:?bump required (patch|minor|major)}"

case "$bump" in
  patch|minor|major) ;;
  *)
    echo "next-version.sh: unknown bump '$bump' (expected patch|minor|major)" >&2
    exit 1
    ;;
esac

latest=$(git tag --list "${prefix}/v*" --sort=-v:refname | head -n 1)
if [ -z "$latest" ]; then
  echo "v0.1.0"
  exit 0
fi

ver="${latest#"${prefix}/v"}"
IFS='.' read -r major minor patch <<<"$ver"

case "$bump" in
  patch) patch=$((patch + 1)) ;;
  minor) minor=$((minor + 1)); patch=0 ;;
  major) major=$((major + 1)); minor=0; patch=0 ;;
esac

echo "v${major}.${minor}.${patch}"

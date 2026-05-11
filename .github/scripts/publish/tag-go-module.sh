#!/usr/bin/env bash
# Build a Go sub-module and push its next semver tag.
# Usage: tag-go-module.sh <package_dir> <bump>

set -euo pipefail

package_dir="${1:?package_dir required (e.g. packages/internalauth-go)}"
bump="${2:?bump required (patch|minor|major)}"

repo_root=$(git rev-parse --show-toplevel)
script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

cd "${repo_root}/${package_dir}"
go build ./...

next=$("${script_dir}/next-version.sh" "${package_dir}" "${bump}")
tag="${package_dir}/${next}"

cd "${repo_root}"
git tag "${tag}"
git push origin "${tag}"

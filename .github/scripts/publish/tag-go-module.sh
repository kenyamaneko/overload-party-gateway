#!/usr/bin/env bash
# tag-go-module.sh: build a Go sub-module, compute next semver tag, push it.
# Usage: tag-go-module.sh <package_dir> <bump>
#   package_dir: path to the Go module dir, e.g. packages/internalauth-go
#   bump:        patch | minor | major (used only when prior tag exists)
# Tag format is "<package_dir>/vX.Y.Z" — Go module proxy requires this layout
# for sub-module versioning.

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

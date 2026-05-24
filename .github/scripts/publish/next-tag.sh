#!/usr/bin/env bash
# 指定 prefix のタグから最新を見つけ、bump レベルに応じて次バージョンを算出する。
# 入力: PREFIX (例: packages/api-gateway-npm/v) / BUMP (patch|minor|major)、出力: GITHUB_OUTPUT に "tag=..." を書き込み。
set -euo pipefail

: "${PREFIX:?PREFIX env required}"
: "${BUMP:?BUMP env required}"

latest=$(git tag --list "${PREFIX}*" --sort=-v:refname | head -n1 || true)
if [ -z "${latest}" ]; then
  next="${PREFIX}0.1.0"
else
  ver="${latest#${PREFIX}}"
  IFS='.' read -r major minor patch <<EOF
${ver}
EOF
  case "${BUMP}" in
    patch) patch=$((patch + 1)) ;;
    minor) minor=$((minor + 1)); patch=0 ;;
    major) major=$((major + 1)); minor=0; patch=0 ;;
    *) echo "unknown bump: ${BUMP}" >&2; exit 1 ;;
  esac
  next="${PREFIX}${major}.${minor}.${patch}"
fi
echo "tag=${next}" >> "${GITHUB_OUTPUT}"

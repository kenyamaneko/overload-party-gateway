#!/usr/bin/env bash
# 入力: TARGET (auto または publish 対象名)、出力: GITHUB_OUTPUT に "<output_name>=true|false" を書き込み。
set -euo pipefail

: "${TARGET:?TARGET env required}"

# (output_name:target_name:tag_prefix:watch_path)
PACKAGES=(
  "ws_constants_go:ws-constants:packages/ws-constants:packages/ws-constants/"
  "api_gateway_go:api-gateway:packages/api-gateway:packages/api-gateway/"
  "internalauth_go:internalauth-go:packages/internalauth-go:packages/internalauth-go/"
  "ws_constants_npm:ws-constants-npm:packages/ws-constants-npm:packages/ws-constants-npm/"
  "api_gateway_npm:api-gateway-npm:packages/api-gateway-npm:packages/api-gateway-npm/"
)

# 未知の対象は、どのパッケージにも一致せず何も publish せずに成功してしまうため弾く
if [ "$TARGET" != "auto" ]; then
  is_known_target=false
  for entry in "${PACKAGES[@]}"; do
    IFS=':' read -r _ target_name _ _ <<< "$entry"
    if [ "$TARGET" = "$target_name" ]; then
      is_known_target=true
      break
    fi
  done
  if [ "$is_known_target" = false ]; then
    echo "unknown publish target: ${TARGET}" >&2
    exit 1
  fi
fi

for entry in "${PACKAGES[@]}"; do
  IFS=':' read -r output_name target_name prefix path <<< "$entry"

  if [ "$TARGET" != "auto" ]; then
    [ "$TARGET" = "$target_name" ] && changed=true || changed=false
    echo "${output_name}=${changed}" >> "${GITHUB_OUTPUT}"
    continue
  fi

  last_tag=$(git tag --list "${prefix}/v*" --sort=-v:refname | head -n1 || true)
  # 未公開のパッケージは比較対象が無いので公開対象に含める
  if [ -z "${last_tag}" ]; then
    echo "${output_name}=true" >> "${GITHUB_OUTPUT}"
    continue
  fi
  if git diff --quiet "${last_tag}" HEAD -- "${path}"; then
    echo "${output_name}=false" >> "${GITHUB_OUTPUT}"
  else
    echo "${output_name}=true" >> "${GITHUB_OUTPUT}"
  fi
done

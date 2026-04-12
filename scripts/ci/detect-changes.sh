#!/usr/bin/env bash
# Detect package changes since last tag and compute next versions.
# Called from publish.yaml — outputs are written to $GITHUB_OUTPUT.
#
# Package list (21 publish units, ADR-015 Phase 3/4/5 naming):
#
#   Go modules (9):
#     - game-design-constants, game-logic-constants, ws-constants,
#       shop-constants, newsfeed-constants, card-types, api-client,
#       api-battle-rpc, devdata
#
#   C# csproj (4 — ADR-015 Phase 5 で分割済み):
#     - game-design-constants-dotnet (OverloadParty.GameDesignConstants)
#     - game-logic-constants-dotnet  (OverloadParty.GameLogicConstants)
#     - game-state-dotnet            (OverloadParty.GameState)
#     - api-battle-rpc-dotnet        (OverloadParty.ApiBattleRpc)
#     ws / shop / newsfeed 定数は battle 未使用なので C# 版は publish しない。
#
#   npm packages (8 — ADR-015 Phase 4 で分割済み):
#     - game-design-constants-npm, game-logic-constants-npm,
#       ws-constants-npm, shop-constants-npm, newsfeed-constants-npm
#       (Layer 1: 独立)
#     - card-types-npm, game-state-npm
#       (Layer 2: Layer 1 に依存)
#     - api-client-npm
#       (Layer 3: Layer 1/2 に依存)
#
# Each package has its own tag prefix "packages/{name}/v{version}".

set -euo pipefail

TARGET="${1:-auto}"
BUMP="${2:-patch}"

# (package_name, tag_prefix, watch_path, fallback_prefix)
# - tag_prefix  : 新規タグを作成するときのプレフィクス
# - watch_path  : git diff で変更検知する対象ディレクトリ
# - fallback_prefix : 新 tag_prefix 配下にタグが存在しないときに "最後の既知の
#   バージョン" として参照する旧プレフィクス (ADR-015 Phase 3/4 でパッケージを
#   分割した際の移行サポート用)。該当なしは "-"
PACKAGES=(
  "game-design-constants:packages/game-design-constants:packages/game-design-constants/:-"
  "game-logic-constants:packages/game-logic-constants:packages/game-logic-constants/:-"
  "ws-constants:packages/ws-constants:packages/ws-constants/:-"
  "shop-constants:packages/shop-constants:packages/shop-constants/:-"
  "newsfeed-constants:packages/newsfeed-constants:packages/newsfeed-constants/:-"
  "card-types:packages/card-types:packages/card-types/:-"
  "api-client:packages/api-client:packages/api-client/:-"
  "api-battle-rpc:packages/api-battle-rpc:packages/api-battle-rpc/:-"
  "devdata:packages/devdata:packages/devdata/:-"
  "game-design-constants-dotnet:packages/game-design-constants-dotnet:packages/game-design-constants-dotnet/:-"
  "game-logic-constants-dotnet:packages/game-logic-constants-dotnet:packages/game-logic-constants-dotnet/:-"
  "game-state-dotnet:packages/game-state-dotnet:packages/game-state-dotnet/:-"
  "api-battle-rpc-dotnet:packages/api-battle-rpc-dotnet:packages/api-battle-rpc-dotnet/:-"
  "game-design-constants-npm:packages/game-design-constants-npm:packages/game-design-constants-npm/:-"
  "game-logic-constants-npm:packages/game-logic-constants-npm:packages/game-logic-constants-npm/:-"
  "ws-constants-npm:packages/ws-constants-npm:packages/ws-constants-npm/:-"
  "shop-constants-npm:packages/shop-constants-npm:packages/shop-constants-npm/:-"
  "newsfeed-constants-npm:packages/newsfeed-constants-npm:packages/newsfeed-constants-npm/:-"
  "card-types-npm:packages/card-types-npm:packages/card-types-npm/:-"
  "game-state-npm:packages/game-state-npm:packages/game-state-npm/:-"
  "api-client-npm:packages/api-client-npm:packages/api-client-npm/:-"
)

compute_version() {
  local prefix="$1" bump="$2" fallback="$3"
  local last_tag
  last_tag=$(git tag -l "${prefix}/v*" | sort -V | tail -1)
  if [ -z "$last_tag" ] && [ "$fallback" != "-" ]; then
    # New prefix has no tags yet — fall back to the pre-migration prefix so
    # the version number continues monotonically (Phase 3/4 migration support).
    last_tag=$(git tag -l "${fallback}/v*" | sort -V | tail -1)
    if [ -n "$last_tag" ]; then
      # Rewrite prefix so the "{current}" extraction works uniformly below.
      last_tag="${prefix}/v${last_tag#"${fallback}/v"}"
    fi
  fi
  if [ -z "$last_tag" ]; then
    echo "0.1.0"
    return
  fi
  local current major minor patch
  current="${last_tag#"${prefix}/v"}"
  IFS='.' read -r major minor patch <<< "$current"
  case "$bump" in
    major) echo "$((major + 1)).0.0" ;;
    minor) echo "${major}.$((minor + 1)).0" ;;
    *)     echo "${major}.${minor}.$((patch + 1))" ;;
  esac
}

has_changes() {
  local prefix="$1" path="$2" fallback="$3"
  local last_tag
  last_tag=$(git tag -l "${prefix}/v*" | sort -V | tail -1)
  if [ -z "$last_tag" ] && [ "$fallback" != "-" ]; then
    last_tag=$(git tag -l "${fallback}/v*" | sort -V | tail -1)
  fi
  if [ -z "$last_tag" ]; then
    # No previous tag → treat as changed (first publish).
    return 0
  fi
  ! git diff --quiet "$last_tag" -- "$path"
}

# output-key は GitHub Actions で参照する matrix 化しやすい形式にする。
# 例: "changed=game-design-constants,api-client,game-design-constants-npm"
CHANGED=()

for entry in "${PACKAGES[@]}"; do
  IFS=':' read -r name prefix path fallback <<< "$entry"

  case "$TARGET" in
    auto)
      if has_changes "$prefix" "$path" "$fallback"; then
        CHANGED+=("$name")
      fi
      ;;
    "$name")
      CHANGED+=("$name")
      ;;
  esac
done

# Emit per-package outputs: "{name}=true" and "{name}-version={ver}".
# Use underscore in output keys (GitHub Actions does not allow hyphens in step
# output references: needs.detect.outputs.game_design_constants).
for entry in "${PACKAGES[@]}"; do
  IFS=':' read -r name prefix _ fallback <<< "$entry"
  key=$(echo "$name" | tr '-' '_')

  if [[ " ${CHANGED[*]} " == *" $name "* ]]; then
    echo "${key}=true" >> "$GITHUB_OUTPUT"
    ver=$(compute_version "$prefix" "$BUMP" "$fallback")
    echo "${key}_version=$ver" >> "$GITHUB_OUTPUT"
    echo "$name → v$ver"
  else
    echo "${key}=false" >> "$GITHUB_OUTPUT"
  fi
done

# Aggregate flags for easy conditional gating in publish.yaml.
ANY_CHANGED=false
if [ ${#CHANGED[@]} -gt 0 ]; then
  ANY_CHANGED=true
fi
echo "any_changed=$ANY_CHANGED" >> "$GITHUB_OUTPUT"
echo "changed_list=${CHANGED[*]:-}" >> "$GITHUB_OUTPUT"

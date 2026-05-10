#!/usr/bin/env bash
# asyncapi-diff.sh — base ブランチ (PR の merge 先) と PR head の data/asyncapi.yaml を比較し、
# 後方非互換な変更があれば exit 1 で CI を fail させる。
#
# base に data/asyncapi.yaml が存在しない場合は「初回導入 PR」として明示的にログを残してパスする。
# (silent skip ではなく一回限りの導入イベントとして可視化する。)
set -euo pipefail

BASE_REF="${1:?base ref is required (e.g. origin/main)}"
SPEC_PATH="data/asyncapi.yaml"
BASE_TMP="/tmp/base-asyncapi.yaml"

if ! git show "${BASE_REF}:${SPEC_PATH}" >"${BASE_TMP}" 2>/dev/null; then
  echo "::notice::${BASE_REF} に ${SPEC_PATH} が存在しない。AsyncAPI spec の初回導入 PR と判定し、breaking change チェックをスキップ (一回限り)。"
  exit 0
fi

asyncapi diff "${BASE_TMP}" "${SPEC_PATH}" --type breaking

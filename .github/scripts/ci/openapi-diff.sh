#!/usr/bin/env bash
# openapi-diff.sh — base ref と PR head の data/openapi.yaml を比較し breaking change を検出する。
# 第 1 引数: base 側 spec パス、第 2 引数: head 側 spec パス。
set -euo pipefail

BASE_SPEC="${1:-}"
HEAD_SPEC="${2:-}"

if [[ -z "${BASE_SPEC}" || -z "${HEAD_SPEC}" ]]; then
  echo "::error::usage: openapi-diff.sh <base-spec> <head-spec>"
  exit 2
fi

if [[ ! -f "${BASE_SPEC}" ]]; then
  echo "::notice::base side openapi.yaml が存在しないため初回導入とみなし spec-diff を skip"
  exit 0
fi

if ! oasdiff breaking "${BASE_SPEC}" "${HEAD_SPEC}" --fail-on ERR; then
  echo "::error::OpenAPI に breaking change が検出されました。互換性方針を確認してください。"
  exit 1
fi

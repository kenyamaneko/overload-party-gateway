#!/usr/bin/env bash
# check-codegen-drift.sh — codegen 後に差分や未追跡ファイルがないか検証する。
set -euo pipefail

if ! git diff --exit-code; then
  echo "::error::Generated files are out of sync. Run 'bash scripts/generate_types.sh' and commit."
  exit 1
fi
if [ -n "$(git ls-files --others --exclude-standard)" ]; then
  echo "::error::Codegen produced untracked files:"
  git ls-files --others --exclude-standard
  exit 1
fi

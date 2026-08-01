#!/usr/bin/env bash
set -euo pipefail

COMMON_REPO="git+https://github.com/kenyamaneko/overload-party-common.git@main"

pip install \
  "overload-party-codegen-tools @ ${COMMON_REPO}#subdirectory=packages/codegen-tools" \
  "overload-party-asyncapi-codegen-tools @ ${COMMON_REPO}#subdirectory=packages/asyncapi-codegen-tools"

scripts/generate_types.sh

#!/usr/bin/env bash
# generate_types.sh — data/{openapi,asyncapi}.yaml から packages/api-gateway の Go 型を、
# packages/ws-constants の Go 定数から packages/ws-constants-npm の TypeScript 定義を再生成する。
#
# REST 部分は oapi-codegen、WS 部分は overload-party-asyncapi-codegen-tools (common 由来) を使う。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

cd "$REPO_ROOT/packages/api-gateway"
oapi-codegen -config openapi-codegen.yaml ../../data/openapi.yaml

cd "$REPO_ROOT"
asyncapi-codegen \
  --input data/asyncapi.yaml \
  --output packages/api-gateway/asyncapi_gen.go \
  --package apigateway

# Post-process: WS で battle 由来の生 JSON を pass-through するフィールドを
# json.RawMessage に置き換える (asyncapi-codegen-tools は x-go-type 未対応)。
python3 scripts/postprocess_asyncapi_gen.py packages/api-gateway/asyncapi_gen.go

go run ./scripts/gen-ws-constants-npm \
  --input packages/ws-constants/constants.go \
  --output packages/ws-constants-npm/src/index.ts

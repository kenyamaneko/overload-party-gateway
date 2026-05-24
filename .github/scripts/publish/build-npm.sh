#!/usr/bin/env bash
# 算出済みタグから npm パッケージを build し package.json に version を反映する。
# 入力: TAG (env、prefix を含む完全タグ名) / PREFIX (env、TAG から version を取り出す接頭辞)、PWD は対象パッケージディレクトリ。
set -euo pipefail

: "${TAG:?TAG env required}"
: "${PREFIX:?PREFIX env required}"

version="${TAG#${PREFIX}}"
rm -f package-lock.json
npm install --no-package-lock
npm version "${version}" --no-git-tag-version
npm run build

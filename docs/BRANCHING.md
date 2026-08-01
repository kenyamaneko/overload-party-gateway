# Branching Strategy

ブランチ戦略そのものは
[repos.yaml](https://github.com/kenyamaneko/overload-party-common/blob/main/rules/repos.yaml) が
`flow: github-flow` と宣言しており、共通ルール (`keyandnotes-rules` の `rules/flow/github-flow.md`) に従う。

## ブランチ保護設定

GitHub Ruleset で `main` に以下を設定している。

- 直 push 禁止、PR マージのみ (linear history)
- force push 禁止、削除禁止
- 必須ステータスチェック: ci / lint, ci / test-unit, ci / test-integration, ci / image-scan, ci / codegen-sync
- required reviews: 1 (self-approve 不可)

チェック名の `ci` は `ci.yaml` の呼び出し側ジョブ名で、続く名前は common の `go-service-ci.yaml` のジョブ名。

## CI/CD パイプライン

| ワークフロー | トリガー | 役割 |
|---|---|---|
| [ci.yaml](../.github/workflows/ci.yaml) | PR: main | lint + テスト + 脆弱性スキャン + コード生成ドリフト検出。中身は common の `go-service-ci.yaml` に集約している |
| [test-catalog.yaml](../.github/workflows/test-catalog.yaml) | push: main | `ci.yaml` を呼び、そのテスト結果からテスト観点カタログを生成して GitHub Pages に公開 |
| [deploy.yaml](../.github/workflows/deploy.yaml) | push: main / タグ `v*` / workflow_dispatch | dev へのデプロイ、タグからの stg 昇格、prod への手動昇格 |
| [publish.yaml](../.github/workflows/publish.yaml) | push: main (`packages/**` 変更時) + workflow_dispatch | `packages/` 配下の Go モジュールのタグ付けと npm パッケージの Cloudsmith 公開 |

`feature/*` への直 push では CI が走らない。main 宛の PR を作ると実行され、追加 push のたびに再実行される。

## バージョニング

Semantic Versioning を採用する。桁の選び方は共通ルール (`keyandnotes-rules` の `rules/cicd.md`) に従う。

### `packages/` のタグ

gateway は Go の 3 モジュール (`api-gateway` / `ws-constants` / `internalauth-go`) と npm の 2 パッケージ、計 5 配布単位を持つ。

| パッケージ | タグ形式 / 公開先 |
|---|---|
| `packages/api-gateway` (Go) | `packages/api-gateway/vX.Y.Z` |
| `packages/ws-constants` (Go) | `packages/ws-constants/vX.Y.Z` |
| `packages/internalauth-go` (Go) | `packages/internalauth-go/vX.Y.Z` |
| `packages/api-gateway-npm` | Cloudsmith (npm) |
| `packages/ws-constants-npm` | Cloudsmith (npm) |

発行は [publish.yaml](../.github/workflows/publish.yaml) が担当し、`packages/**` に main で変更が入ると変更を検出したモジュールだけをタグ付け・公開する。桁を人が選ぶときは `workflow_dispatch` で `bump` を指定する。

各パッケージのバージョンはサービス本体のバージョンと独立で、REST 契約型や WS プロトコルに破壊的変更が入るときは対応するパッケージの桁を上げる。

手動でのタグ付けは禁止する ([CLAUDE.md](../CLAUDE.md) の禁止事項)。

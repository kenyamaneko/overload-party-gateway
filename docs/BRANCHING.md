# Branching Strategy

本リポジトリのブランチ戦略とリリース運用を定義する。

> **Note**: このドキュメントは将来 `overload-party-common` に移動する予定。他リポジトリの main ブランチの品質が安定した段階で、共通ルールとして参照される形にする。
>
> **現状のギャップ**: gateway は現在 `main` 単一ブランチ運用で、`develop` / `release/*` ブランチと `release-tag.yaml` ワークフローは未整備。本ドキュメントは account / shop と揃えた **目指すべき運用** を記述している。実態との差分は「現状のギャップ」節を参照。

## 概要

GitFlow をベースに、環境とブランチを対応付けた運用を採用する。gateway は WS 終端とゲームセッション中継を担当し、セッション断が対戦中プレイヤーの forfeit を直接引き起こすため、本番事故がユーザー体験に即時影響する。stg 環境での実機検証（WS 接続安定性、切断タイマー、match_made ハンドリング等）を挟む昇格モデルが必須となる。

## ブランチ一覧

| ブランチ | 環境 | 寿命 | 派生元 | マージ先 | 保護 |
|---|---|---|---|---|---|
| `main` | prod | 永続 | — | — | 最大 |
| `release/vX.Y.Z` | stg | 短命 | `develop` | `main` | あり |
| `develop` | dev | 永続 | `main` (初回のみ) | — | あり |
| `feature/xxx` | なし | 短命 | `develop` | `develop` | なし |
| `hotfix/xxx` | なし | 短命 | `main` | `main` + `develop` (+ `release` if exists) | なし |

## ブランチ運用ルール

### main

- **prod 環境のソース・オブ・トゥルース**。main の HEAD = prod で動作しているコード
- 直 push 禁止。PR 経由のマージのみ
- マージ元として許可するのは `release/*` と `hotfix/*` のみ
- `develop` や `feature/*` を直接 main にマージしない
- タグは CI が自動で打つ（手動タグ付け禁止）
- force push 禁止、履歴書き換え禁止

### develop

- **dev 環境のソース**。次リリースに向けた統合ブランチ
- 直 push 禁止。PR 経由のマージのみ
- マージ元として許可するのは `feature/*` と `hotfix/*` の back-merge
- CI green 必須。レビューは self-approve 可（速度優先）

### release/vX.Y.Z

- **stg 環境のソース**。リリース候補の検証ブランチ
- 短命。main にマージ後、削除する
- ブランチ名に候補バージョンを含める（例: `release/v1.2.0`）
- `develop` から切る。切った時点で feature の取り込みは停止する
- release 中に feature を追加で取り込みたい場合は、原則として次の release に回す
- バグ修正やリリース準備（CHANGELOG 更新等）のコミットは PR 経由で release に入れる
- release に入れた修正は、main マージ後に develop にも back-merge する（後述）

### feature/xxx

- 新機能・改善の作業ブランチ
- `develop` から切って `develop` にマージ
- 命名例: `feature/gateway/issue-42`, `feature/add-spectate-heartbeat`
- PR マージ時にブランチ削除

### hotfix/xxx

- **prod 緊急修正**の作業ブランチ
- `main` から切る（develop からではない — develop には未リリース変更が混ざっているため）
- main と develop の両方にマージする（back-merge 必須）
- release ブランチが存在する場合は、release にもマージする
- 命名例: `hotfix/fix-ws-reconnect`, `hotfix/pubsub-dedup-leak`

## リリースフロー

### 通常リリース

```
1. develop で feature を統合・dev 環境で検証
   └─ feature/xxx → develop (PR)

2. release ブランチを切る
   └─ git switch -c release/v1.2.0 develop
   └─ push → stg 環境に自動デプロイ

3. stg 環境で検証
   └─ WS 接続安定性、match_made → match_found 疎通、切断タイマー動作
   └─ 他サービス（account / card / battle / matchmaking）との通信確認
   └─ バグ発見時は PR 経由で release ブランチに修正を入れる

4. main にマージ
   └─ release/v1.2.0 → main (PR)
   └─ CI が自動でタグ v1.2.0 を打つ
   └─ main が prod 環境に自動デプロイ

5. develop に back-merge
   └─ release/v1.2.0 → develop (PR)
   └─ release 中に入れた修正を develop に戻す

6. release ブランチ削除
```

### hotfix リリース

```
1. hotfix ブランチを切る
   └─ git switch -c hotfix/fix-ws-reconnect main

2. 修正 → PR → main にマージ
   └─ hotfix/xxx → main (PR)
   └─ CI が自動でタグ v1.2.1 を打つ（patch bump）
   └─ prod 環境に自動デプロイ

3. develop に back-merge（必須）
   └─ hotfix/xxx → develop (PR)

4. release ブランチが存在する場合は release にも back-merge
   └─ hotfix/xxx → release/vX.Y.Z (PR)

5. hotfix ブランチ削除
```

### hotfix の back-merge 忘れ対策

hotfix を main にマージしたが develop に戻し忘れると、次のリリースでバグが再発する。

対策:

- PR テンプレートに back-merge チェックリストを入れる
- main に hotfix が入ったら、CI で develop への back-merge PR を自動生成する workflow を用意する（未作成）

## バージョニング

Semantic Versioning (SemVer) を採用する。

- **MAJOR**: 破壊的変更（REST API スキーマ破壊、WS プロトコル破壊、Pub/Sub イベントスキーマ破壊等、既存クライアントが動かなくなる変更）
- **MINOR**: 後方互換のある機能追加
- **PATCH**: バグ修正、ドキュメント修正、内部リファクタ

### サービス本体のタグ

サービス本体のタグは CI（`release-tag.yaml`、未整備）が main への PR マージ時に自動で打つ想定。

- release マージ時: ブランチ名からバージョンを取得（`release/v1.2.0` → `v1.2.0`）
- hotfix マージ時: 最新タグから patch を自動 bump（`v1.2.0` → `v1.2.1`）

**手動タグ禁止**。CLAUDE.md 禁止事項にも記載。

### `packages/` のタグ

gateway は Go + npm の 2 言語で 2 パッケージ（`api-gateway` / `ws-constants`）、計 4 配布単位を持つ。タグ形式と発行は以下:

| パッケージ | タグ形式 / 公開先 | 発行トリガー |
|---|---|---|
| `packages/api-gateway` (Go) | `packages/api-gateway/vX.Y.Z` | `publish.yaml` が main push で自動検出 + `workflow_dispatch` |
| `packages/ws-constants` (Go) | `packages/ws-constants/vX.Y.Z` | 同上 |
| `packages/api-gateway-npm` | GitHub Packages (npm) | 同上 |
| `packages/ws-constants-npm` | GitHub Packages (npm) | 同上 |

発行は [.github/workflows/publish.yaml](../.github/workflows/publish.yaml) が担当する。`packages/**` / `data/ws_constants.yaml` / `data/models.yaml` / `scripts/generate_types.py` のいずれかに main で変更が入った時に自動発火し、変更検出したモジュールのみタグ付け・公開する。bump 種別を人が判断したい場合は `workflow_dispatch` で `bump=patch|minor|major` を明示指定できる。

各パッケージのバージョンはサービス本体のバージョンと独立で、REST 契約型や WS プロトコルに破壊的変更が入る release では対応パッケージを major bump する。

## ブランチ保護設定

GitHub Rulesets で以下を設定する（account / shop と同等）。

### main

- 直 push 禁止
- PR マージのみ許可（linear history）
- force push 禁止、削除禁止
- 履歴書き換え禁止
- 必須ステータスチェック: CI / lint, CI / test, Validate codegen が green
- required reviews: 1（self-approve 不可）
- マージ元ブランチ制限: `release/*` と `hotfix/*` のみ

### release/*

- 直 push 禁止。PR 経由のマージのみ
- force push 禁止、削除は手動で可
- 必須ステータスチェック: CI / lint, CI / test が green

### develop

- 直 push 禁止
- PR マージのみ許可
- 必須ステータスチェック: CI / lint, CI / test が green
- required reviews: 不要（一人開発での速度優先）

## CI/CD パイプライン

| ワークフロー | トリガー | 役割 |
|---|---|---|
| [ci.yaml](../.github/workflows/ci.yaml) | PR: main (将来的に develop / release/** も) | lint + test + Docker build & push |
| [validate.yaml](../.github/workflows/validate.yaml) | PR: main | `generate_types.py` / `generate_schema_doc.py` / `generate_api_docs.py` の出力ドリフト検出 |
| [publish.yaml](../.github/workflows/publish.yaml) | push: main (packages/** 変更時) + workflow_dispatch | `packages/api-gateway`, `packages/ws-constants` の Go モジュールタグ付け + 対応 npm パッケージの GitHub Packages 公開 |
| `release-tag.yaml`（未整備） | PR closed (merged) to main | `release/*` または `hotfix/*` が main にマージされた時に SemVer タグを自動作成 |

CI と CD は将来的に分離する方針。現在は `ci.yaml` 内で build & push まで行っているが、account / shop に合わせて `deploy.yaml` への分離を計画している。

### feature / hotfix ブランチの CI

feature/* や hotfix/* ブランチへの直 push では CI が走らない。CI を実行するには、対象ブランチ（develop / main / release/**）宛の PR を作成する。PR 更新時（追加 push）にも CI が再実行される。

### main へのマージ源の制約

`release/*` / `hotfix/*` のみを main のマージ元として許容するため、`ci.yaml` に `check-source-branch` ジョブを追加する（未整備 — account / shop に合わせて追加予定）。

## 現状のギャップ

本ドキュメントは目指すべき運用を記述しているが、以下は未整備であり、移行作業として追って対応する。

- `develop` / `release/*` ブランチが未作成（現在 `main` 単一ブランチ運用）
- `release-tag.yaml` ワークフロー未作成（サービス本体の SemVer タグが自動化されていない）
- `deploy.yaml` 分離未実施（現状 `ci.yaml` 内で build & push）
- `check-source-branch` ジョブ未追加（main へのマージ元を release/hotfix に限定する機械チェック）
- develop への back-merge 自動 PR workflow 未作成

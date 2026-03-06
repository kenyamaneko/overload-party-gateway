# Overload Party Gateway

**Overload Party** のクライアント向け REST API ゲートウェイサーバー。

Overload Party は、AWS・Azure・GCP・OCI など実在するクラウドサービスを擬人化したキャラクターで戦う 1v1 リアルタイム対戦カードゲームです。本リポジトリはクライアントアプリとバトルサーバーの間に位置し、認証・プレイヤーデータ・デッキ管理・ショップ/課金処理、そして **WebSocket 経由での対戦通信・マッチメイキング** などを担うゲートウェイ API を提供します。

## 技術スタック

- **Go** (Gin)
- **PostgreSQL** (pgx)
- **Firebase Authentication**
- **Apple / Google IAP** レシート検証

## プロジェクト構成

```
cmd/
  main/       # 本番エントリポイント (PostgreSQL + Firebase Auth)
  local/      # ローカル開発用エントリポイント (インメモリ Mock)
internal/
  cache/      # カード定義キャッシュ
  config/     # 環境変数の読み込み
  constants/  # 定数 (コード生成)
  handler/    # REST ハンドラー
  middleware/ # CORS, 認証ミドルウェア
  model/      # データモデル
  platform/   # Apple/Google レシート検証
  repository/ # PostgreSQL / Mock リポジトリ
  service/    # ビジネスロジック
```

## セットアップ

### 必要なもの

- Go 1.25+
- PostgreSQL (本番モード)
- Firebase プロジェクト (本番モード)

### ローカル開発 (推奨)

DB・Firebase 不要。インメモリ Mock リポジトリで起動します。

```bash
make run-local
# http://localhost:9001/api/v1/
```

ローカルモードでは `X-Dev-UID` ヘッダーで認証をバイパスできます。

### 本番モード

```bash
# .env を作成
cp .env.example .env  # 環境変数を設定

# 起動
make run-gateway
```

### 環境変数

| 変数 | 必須 | デフォルト | 説明 |
|------|------|-----------|------|
| `PORT` | - | `9001` | サーバーポート |
| `ENV` | - | `dev` | 実行環境 (`dev` / `prod`) |
| `DATABASE_URL` | 本番 | - | PostgreSQL 接続文字列 |
| `ALLOWED_ORIGINS` | 本番 | - | CORS 許可オリジン (カンマ区切り) |
| `APPLE_KEY_ID` | - | - | App Store Connect API キー ID |
| `APPLE_ISSUER_ID` | - | - | App Store Connect Issuer ID |
| `APPLE_BUNDLE_ID` | - | - | アプリ Bundle ID |
| `APPLE_PRIVATE_KEY_PATH` | - | - | App Store Connect 秘密鍵パス |
| `APPLE_ENVIRONMENT` | - | `Sandbox` | `Production` / `Sandbox` |
| `GOOGLE_PACKAGE_NAME` | - | - | Google Play パッケージ名 |

## 開発コマンド

```bash
make help          # コマンド一覧
make run-local     # ローカルサーバー起動
make run-gateway   # 本番モードサーバー起動
make test          # テスト実行
make lint          # Lint 実行
make fmt           # コードフォーマット
make generate      # コード生成 (YAML → Go/JSON)
make build         # Docker イメージビルド
```

## 関連リポジトリ

| リポジトリ | 概要 |
|-----------|------|
| `overload-party-common` | ゲーム仕様・カード YAML 定義・コード生成スクリプト (Single Source of Truth) |
| `overload-party-client` | クライアント (React + Capacitor) |
| `overload-party-battle` | バトルサーバー (ゲームエンジン / REST API バックエンド) |

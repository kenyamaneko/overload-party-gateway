# overload-party-gateway

クライアントが唯一通信する薄い WS/REST ゲートウェイ。認証・ルーティング・WebSocket リレーを担い、ドメインロジックはすべて下流サービスに委譲する。

[テスト観点カタログ](https://kenyamaneko.github.io/overload-party-gateway/): テスト名から生成した、テスト済みの観点の一覧。

## サービス間連携

```
Client (React / Capacitor)
  ├─ WS  /ws                        ← ゲーム状態同期、マッチメイキング
  └─ REST /api/v1/*                  ← 認証、デッキ、カード、ショップ等
              │
              ▼
Gateway (このサービス, :9001)
  ├─ HTTP → account   (:9005)  プレイヤー / 認証 / 設定 / EXP
  ├─ HTTP → card      (:9003)  カードマスター / デッキ CRUD
  ├─ HTTP → matchmaking(:9004) enqueue / cancel
  ├─ HTTP → battle    (:9002)  ゲーム作成 / アクション / 状態取得
  ├─ HTTP → shop      (:9006)  商品 / 購入 / サブスクリプション
  ├─ HTTP → scenario  (:9007)  エピソード / スクリプト
  ├─ HTTP → news      (:9008)  ニュース記事
  ├─ HTTP → support   (:9009)  お知らせ
  ├─ PostgreSQL                 gateway.game_players (所有)
  └─ POST /internal/v1/pubsub/match-made  ← Cloud Pub/Sub push 配信
```

REST エンドポイント契約は [data/openapi.yaml](data/openapi.yaml) を参照。

## 環境変数

既定値のある変数は、未設定なら既定値を使い、空文字が設定されていれば起動時にエラーで止まる。値を投入する側が渡した空文字を既定値で覆い隠さないため。

**Deployment env (インフラ層):**

| 変数名 | デフォルト | 説明 |
|---|---|---|
| `PORT` | `9001` | リッスンポート |
| `ENV` | `dev` | 動作環境 (`dev` / `stg` / `prod`) |
| `DATABASE_CONN` | *(必須)* | PostgreSQL 接続文字列 (`gateway.game_players`) |
| `DATABASE_IAM_AUTH_ENABLED` | *(必須)* | Cloud SQL への接続を Cloud SQL Go Connector 経由の自動 IAM データベース認証で行うかどうか。`true` / `false` のいずれか必須で、フォールバックは無い |
| `CLOUDSQL_CONNECTION_NAME` | *(空)* | Cloud SQL インスタンスの接続名 (`project:region:instance`)。`DATABASE_IAM_AUTH_ENABLED=true` のときのみ必須 |
| `GOOGLE_CLOUD_PROJECT_ID` | *(必須)* | Google Cloud プロジェクト ID (Pub/Sub および Firestore game_config) |
| `INTERNAL_AUTH_PRIVATE_KEY` | *(必須)* | 内部認証 JWT (RS256) の署名鍵。PKCS#8 の PEM |

**ConfigMap (サービス URL):**

8 本すべてが必須で、未設定または空なら起動時に落ちる。欠けた宛先への転送は実行時まで誤りが見えないため、既定値へのフォールバックを持たない。

| 変数名 | デフォルト | 説明 |
|---|---|---|
| `BATTLE_SERVER_URL` | *(必須)* | Battle サービス URL |
| `CARD_SERVICE_URL` | *(必須)* | Card サービス URL |
| `MATCHMAKING_SERVICE_URL` | *(必須)* | Matchmaking サービス URL |
| `ACCOUNT_SERVICE_URL` | *(必須)* | Account サービス URL |
| `SHOP_SERVICE_URL` | *(必須)* | Shop サービス URL |
| `SCENARIO_SERVICE_URL` | *(必須)* | Scenario サービス URL |
| `NEWS_SERVICE_URL` | *(必須)* | News サービス URL |
| `SUPPORT_SERVICE_URL` | *(必須)* | Support サービス URL |

**ConfigMap (アプリ挙動):**

| 変数名 | デフォルト | 説明 |
|---|---|---|
| `ALLOWED_ORIGINS` | *(空)* | CORS 許可オリジン (カンマ区切り、prod 必須) |
| `MATCHMAKING_TIMEOUT_SEC` | `30` | プレイヤーごとのマッチメイク待ちタイムアウト秒 |
| `APP_MIN_VERSION` | `0.1.0` | 最低必要バージョン |
| `APP_LATEST_VERSION` | `0.1.0` | 最新バージョン |
| `APP_FORCE_UPDATE` | `false` | 強制アップデートフラグ。設定するなら `true` / `false` のいずれかで、他の値は起動時エラー |
| `APP_STORE_URL` | *(空)* | 強制アップデート時にクライアントが開くストア URL |

## ローカル実行

```
cp .env.example .env
make run-local
```

`.env` は Makefile が読み込んで export する。`.env.example` は下流サービスの宛先 8 本と `DATABASE_CONN` を上のサービス間連携図のポートで埋めてあり、値の書き換えなしで起動する。

`INTERNAL_AUTH_PRIVATE_KEY` だけは `.env` に置けない。PEM が複数行で Makefile の `include` に載らないため、`make run-local` が `.localdev/` に RSA 鍵を生成して環境変数として渡す。生成には `openssl` を使う。

`make run-local` が起動するのは `cmd/local` で、Firebase ID トークンの代わりに `dev-token-{uid}` を受け付ける。PostgreSQL と下流サービスへの接続は要求時まで遅延するため、それらが落ちていても起動して `/health` は 200 を返す。転送を伴う操作には宛先の起動が要る。

`make run-gateway` が起動するのは Cloud Run 向けの `cmd/main` で、Firebase Auth・Firestore・Pub/Sub push 検証を前提とするため Google の資格情報 (ADC) が別途要る。

## 公開パッケージ

| パッケージ | 言語 | 説明 |
|---|---|---|
| `packages/ws-constants/` | Go | WS メッセージ型定数 |
| `packages/ws-constants-npm/` | npm | 同上 (クライアント向け) |
| `packages/api-gateway/` | Go | REST / WS エンベロープ型 |
| `packages/api-gateway-npm/` | npm | 同上 (クライアント向け) |
| `packages/internalauth-go/` | Go | 内部認証 JWT の検証 (下流サービス向け) |

SSoT: `data/openapi.yaml` + `data/asyncapi.yaml`。`make generate-types` (`scripts/generate_types.sh`) で `packages/api-gateway` の `*_gen.go` と `packages/ws-constants-npm/src/index.ts` を再生成する。生成物は直接編集しない。

WS メッセージ種別だけは全件が `packages/ws-constants/constants.go` にある。`data/asyncapi.yaml` は payload を持つ種別しか載せず、battle 由来の生 JSON をそのまま積む種別 (`game_state` や `pong` など) を表現できないためである。TypeScript 版はこの Go 定数から生成し、両者の食い違いは CI の codegen-sync ジョブ (再生成して差分が出たら失敗) が検出する。

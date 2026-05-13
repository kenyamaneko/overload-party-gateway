# overload-party-gateway

クライアントが唯一通信する薄い WS/REST ゲートウェイ。認証・ルーティング・WebSocket リレーを担い、ドメインロジックはすべて下流 6 サービスに委譲する。

## サービス間連携

```
Client (React / Capacitor)
  ├─ WS  /ws                        ← ゲーム状態同期、マッチメイキング、観戦
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
  ├─ PostgreSQL                 gateway.game_players (所有) + newsfeed.news_articles (read-only)
  └─ Cloud Pub/Sub subscriber
        └─ matchmaking-events-gateway         ← match_made
```

エンドポイント一覧は [docs/API_REFERENCE.md](docs/API_REFERENCE.md) を参照。

## 環境変数

**Deployment env (インフラ層):**

| 変数名 | デフォルト | 説明 |
|---|---|---|
| `PORT` | `9001` | リッスンポート |
| `ENV` | `dev` | 動作環境 (`dev` / `stg` / `prod`) |
| `LOG_LEVEL` | `info` | ログレベル |
| `DATABASE_URL` | *(必須)* | PostgreSQL 接続文字列 (`gateway.game_players` + `newsfeed.news_articles`) |
| `GOOGLE_CLOUD_PROJECT_ID` | *(必須)* | Google Cloud プロジェクト ID (Pub/Sub および Firestore game_config) |

**ConfigMap (サービス URL):**

| 変数名 | デフォルト | 説明 |
|---|---|---|
| `BATTLE_SERVER_URL` | `http://localhost:9002` | Battle サービス URL |
| `CARD_SERVICE_URL` | `http://localhost:9003` | Card サービス URL |
| `MATCHMAKING_SERVICE_URL` | `http://localhost:9004` | Matchmaking サービス URL |
| `ACCOUNT_SERVICE_URL` | `http://localhost:9005` | Account サービス URL |
| `SHOP_SERVICE_URL` | `http://localhost:9006` | Shop サービス URL |
| `SCENARIO_SERVICE_URL` | `http://localhost:9007` | Scenario サービス URL |

**ConfigMap (Pub/Sub):**

| 変数名 | デフォルト | 説明 |
|---|---|---|
| `MATCHMAKING_SUBSCRIPTION` | `matchmaking-events-gateway` | matchmaking Pub/Sub サブスクリプション名 |

**ConfigMap (アプリ挙動):**

| 変数名 | デフォルト | 説明 |
|---|---|---|
| `ALLOWED_ORIGINS` | *(空)* | CORS 許可オリジン (カンマ区切り、prod 必須) |
| `MATCHMAKING_TIMEOUT_SEC` | `60` | プレイヤーごとのマッチメイク待ちタイムアウト秒 |
| `APP_MIN_VERSION` | `0.1.0` | 最低必要バージョン |
| `APP_LATEST_VERSION` | `0.1.0` | 最新バージョン |
| `APP_FORCE_UPDATE` | `false` | 強制アップデートフラグ |

## 公開パッケージ

| パッケージ | 言語 | 説明 |
|---|---|---|
| `packages/ws-constants/` | Go | WS メッセージ型定数 |
| `packages/ws-constants-npm/` | npm | 同上 (クライアント向け) |
| `packages/api-gateway/` | Go | REST / WS エンベロープ型 |
| `packages/api-gateway-npm/` | npm | 同上 (クライアント向け) |

SSoT: `data/ws_constants.yaml` + `data/models.yaml` → `python3 scripts/generate_types.py` で再生成。`*_gen.go` / `*_gen.ts` は自動生成 — 直接編集しない。

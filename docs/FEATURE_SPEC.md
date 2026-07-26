# Gateway 機能仕様書

このドキュメントは gateway サービスがビジネス要件として満たすべき振る舞いを定義する。実装方法ではなく **何を保証するか** を記述する。テストはこの仕様に従っていることを確認する観点で書く。

関連ドキュメント:
- 内部動作・配線・本番運用設定: [ARCHITECTURE.md](ARCHITECTURE.md)
- HTTP エンドポイント契約: [../data/openapi.yaml](../data/openapi.yaml)
- WS プロトコル契約: [WS_REFERENCE.md](WS_REFERENCE.md)
- DB スキーマ / イベントスキーマ: [DATA_DESIGN.md](DATA_DESIGN.md)

---

## サービス責務

gateway は以下の機能ドメインを所有する。

| 機能 | 主要な責務 |
|---|---|
| WS 終端 | クライアントとの WebSocket 接続の受付・維持・切断 |
| HTTP 終端 | Firebase ID Token 検証と下流 REST への委譲 |
| WS ⇄ 下流サービス橋渡し | クライアント発 WS メッセージを下流 REST へ委譲し、結果を該当 WS へ push |
| Pub/Sub → WS 通知 | 下流発の非同期イベントを購読し、該当プレイヤーの WS へ push |
| セッション整合性 | 切断タイマー・ターンタイマー・NPC ターン駆動・EXP 付与の冪等キーなど、ゲームセッションを WS 上で完結させるための整合性担保 |

gateway が保持するのは WS 接続 map、ゲームセッションのインメモリ索引、`gateway.game_players` テーブル（match_made 起点の冪等キー用途）、各種タイマーのみ。下流から受け取った JSON ペイロードは `json.RawMessage` で不透明なまま WS へ push する。

---

## WS 接続ライフサイクル

**エンドポイント**: `GET /ws?token=<token>`
**クライアント**: Firebase ID Token を指定（ローカルは `dev-token-{uid}` 可）

### 認証と接続確立契約

1. Firebase Admin SDK の `VerifyIDToken` で Firebase ID Token を検証する
2. accountclient 経由で `FindByFirebaseUID` を呼び、返ってきた PlayerID を接続コンテキストに保持する
3. いずれかに失敗した場合は HTTP 401 で upgrade を拒否（WS 接続を張らせない）
4. 接続確立後は `playerID` をキーとして自 Pod の接続マップに登録する

### 単一接続契約

同一 `playerID` の新規接続が到着した場合、**既存接続は close する**。

- これは多重ログイン・多重デバイスでのセッション競合を防ぐための不変条件
- 旧接続の close では「切断ハンドリング契約」のクリーンアップは走らない（接続マップは新接続に置き換え済みで、プレイヤーは接続を維持しているため）

### Keep-alive 契約

- サーバーは **15 秒間隔** で WS Ping フレームを送信
- クライアントの Pong が **5 秒以内** に返らなければ切断扱いとする
- タイマーは接続ごとに独立して動作する

### 切断ハンドリング契約

WS 切断時、プレイヤーの状態に応じて以下のクリーンアップを必ず実行する:

| 状態 | クリーンアップ |
|---|---|
| マッチ待機中 | matchmaking サービスへ best-effort cancel を送信 |
| ゲーム中 | 60 秒の **切断タイマー** を起動（「切断タイマー契約（ゲーム中のみ）」） |

いずれの状態にも該当しない場合は接続マップから除去するのみ。

### 切断タイマー契約（ゲーム中のみ）

1. **起動時**: 対戦相手に `opponent_disconnected` を push
2. **60 秒以内に再接続**: `opponent_reconnected` を push し、タイマーをキャンセル
3. **タイムアウト時**: battle へ forfeit (`reason: disconnect`) を送信し、両プレイヤーに `game_over` をブロードキャスト

**契約**: ゲーム中プレイヤーの切断は必ず 60 秒以内に決着する（再接続 or 強制 forfeit）。対戦相手がハングしたまま待たされることはない。

---

## マッチメイキング橋渡し

### キュー登録 (`matchmaking_start`)

1. クライアントから WS で `matchmaking_start(deckId)` を受信
2. matchmaking サービスへ REST で Enqueue 委譲
3. 成功時: プレイヤーごとの **マッチ待機タイムアウト** を起動（`MATCHMAKING_TIMEOUT_SEC`、デフォルト 60 秒）
4. 失敗時: `matchmaking_error` を WS push

### キュー取消 (`matchmaking_cancel`)

1. matchmaking サービスへ REST で Cancel 委譲
2. 404 は **正常系**（マッチ成立直後の race）として扱い、クライアントにはエラー通知しない
3. マッチ待機タイマーを停止

### マッチ待機タイムアウト契約

- `match_found` が到着するとタイマー停止
- `matchmaking_cancel` / WS 切断時もタイマー停止
- タイムアウト発火時: `matchmaking_error (retryable: true)` を push し、matchmaking サービスへ cancel を送信

### match_made → match_found 配信契約

`POST /internal/v1/pubsub/match-made` で push 配信を受信する（最大インスタンス数が 1 のため、配信は必ず唯一のインスタンスへ届く）。受信時:

1. `matchId` のインメモリ重複排除で多重配送を抑止
2. cardclient で両プレイヤーのデッキカードを取得
3. battleClient で PvP ゲームを作成（`matchId` に対して冪等）
4. `gateway.game_players` に両プレイヤーの行を挿入（EXP 付与冪等キー + playerNum 索引用途、「EXP 付与トリガと冪等性」）
5. 両プレイヤーの WS 接続があれば `match_found` を push
6. 上記いずれかに失敗した場合は dedup エントリをロールバックして **500** を返す（Pub/Sub がリトライ）

**配信保証**: at-least-once（exactly-once 配信はサポートされない）。同一メッセージが再送されても、手順 1 のプロセス内 `matchId` 重複排除により同一プロセス内では二重処理されない。

---

## ゲームセッション中継

### game_enter

1. `game_players` テーブルから `playerNum` を解決
2. GameRelay（インメモリ）にプレイヤーセッション `playerID → {gameID, playerNum}` を登録
3. `game_entered` を返却
4. `battle_start` / `turn_start` の合成イベントを組み立てて送信（プレイヤー名・レベルは battle の game state に含まれる player summary から取得）
5. `game_state` + `turn_controls` を全プレイヤーに送信

### game_action

1. GameRelay の `playerNum` を使って battle に ProcessAction を委譲
2. `action_performed` を対戦相手に送信（各プレイヤー視点の情報秘匿済み状態を個別取得）
3. NPC 対戦の場合、「NPC ターン駆動」のポーリング駆動に入る
4. `game_state` + `turn_controls` を全プレイヤーに送信
5. battle が `GameOver=true, winningPlayerNum, winReason` を返したら、ターンタイマーをキャンセルし `game_over` をブロードキャスト + EXP 付与トリガ（「EXP 付与トリガと冪等性」）

### ターンタイマー契約

- 各ゲーム状態更新時、アクティブプレイヤーの `timeBank` 秒 + 2 秒バッファでタイマーをリセット
- タイマー発火時、battle へ forfeit (`reason: turn_timeout`) を送信
- **発火時の race 防止**: `activePlayerID` がタイマー起動時から変わっている場合は forfeit を送らない（ターン交代済み）

### battle JSON のパススルー契約

battle からの `game_state` / `action_performed` 等のペイロードは **`json.RawMessage` で不透明なまま WS に push する**。

---

## EXP 付与トリガと冪等性

gateway はゲーム終了を検知して accountclient 経由で EXP 付与 RPC を呼ぶ。複数 Pod / 再送配信下でも二重付与が起きないよう、冪等キーで単一化する。

### 冪等性契約

- `gateway.game_players.exp_awarded` フラグを DB レベルの冪等キーとして使う
- フラグが `false → true` に **UPDATE できた Pod のみ** accountclient 経由で EXP 付与 RPC を呼ぶ
- `UPDATE ... WHERE exp_awarded = false` の影響行数 0 なら即座に return（他 Pod が既に付与済み）

### エラーハンドリング契約

- account への EXP 付与 RPC が失敗した場合、`exp_awarded` フラグは true のままにする（巻き戻さない）
- 理由: 巻き戻すと競合する Pod が再度付与を試みてしまい、二重付与のリスクが出る
- account 側で再集計可能な設計を前提とする（運用上のロスト許容）

---

## NPC ターン駆動

NPC のアクション生成は battle が 1 手単位で行う（`NpcPending=true` のレスポンスを返す）。gateway はこれをポーリングで駆動する。

### 駆動契約

1. `game_enter` で NPC が先手の局面を検出した場合、`POST /games/{id}/advance-npc` を呼んで初手を進める
2. `game_action` 応答または advance-npc 応答で `NpcPending=true` が続く限り、ループで `advance-npc` を呼び続ける
3. 各 advance-npc 応答ごとに `game_state` 更新を WS へ push する（1 手ずつ届く契約を維持）
4. ループは **最大 200 イテレーション** で打ち切る（battle 側のバグで NpcPending が永続した場合の保険）

---

## HTTP 委譲（REST パススルー）

gateway は `/api/v1/**` の REST リクエストを下流サービスへ委譲する。

### 委譲契約

- 認証階層（Public / Auth / Authenticated）に応じて認証ミドルウェアを適用
- 認証通過後、検証済み PlayerID を載せた内部認証 JWT を `X-Internal-Auth` ヘッダに付与して下流サービスへ HTTP 呼び出し
- 下流レスポンスは **ステータスコードとボディをそのまま** クライアントへ返す

### 認証階層

- **Public**: `health` / `version`、認証不要
- **Auth**: `register` / `login`。Firebase トークン検証し FirebaseUID → PlayerID 解決
- **Authenticated**: 上記以外。検証済み PlayerID を内部認証 JWT に載せて下流へ

下流サービス（ClusterIP のみに公開）は `X-Internal-Auth` の内部認証 JWT を検証し、JWT 内の PlayerID を **信頼する**。

---

## Pub/Sub push 受信

### match_made（「match_made → match_found 配信契約」で詳細）

- エンドポイント: `POST /internal/v1/pubsub/match-made` (At-least-once)
- 副作用: battle ゲーム作成 + DB 行挿入 + WS push
- gateway は match_made 専用の受け口としてこれ 1 本のみを持つ。他サービスが発行するイベントを WS 中継目的で購読しない (ADR-027)

---

## 認証信頼境界

- クライアント → gateway 入り口で Firebase ID Token を検証する
- 検証済み FirebaseUID を accountclient 経由で PlayerID に解決する
- gateway → 下流サービスは ClusterIP 経由でのみ呼び出し、PlayerID を載せた内部認証 JWT を `X-Internal-Auth` ヘッダで渡す
- 下流サービスは内部認証 JWT を検証し、JWT 内の PlayerID を信頼する

---

## Graceful shutdown

SIGTERM 受信時の契約:

1. HTTP サーバは新規リクエスト受付を止める
2. WS アクセプトを止める（新規 upgrade は 503）
3. 既存 WS 接続には close コードを送り、クライアントに再接続を促す
4. Pub/Sub subscriber は pull を止め、in-flight メッセージの処理完了を待つ
5. battle との in-flight 通信完了を待つ
6. タイムアウト超過時は強制キャンセル

k8s の preStop hook で traffic ドレイン時間を確保し、in-flight ゲームセッションへの影響を最小化する前提。

---

## エラーセマンティクス

### 下流サービスエラーの扱い

| 下流の応答 | gateway の挙動 |
|---|---|
| 2xx | そのままクライアントへ返す / WS push |
| 4xx | クライアントへそのまま返す（4xx = クライアント起因） |
| 5xx | WS では `matchmaking_error` 等の retryable エラーで通知。REST では 5xx のまま返す |
| 到達不能 | 5xx 扱い |

### エラーを握りつぶさない

下流サービス呼び出しの失敗は **必ずクライアント（または Pub/Sub nack）に伝搬する**。ログ出力のみで処理継続してはならない（CLAUDE.md 設計思想）。

### Cancel の 404

matchmaking の Cancel が 404 を返すのは、マッチ成立直後や WS 切断時の race で起きる正常系。gateway はこれをエラーとして扱わない。

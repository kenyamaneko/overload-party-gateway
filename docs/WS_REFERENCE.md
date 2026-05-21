# Overload Party - WebSocket API リファレンス

---

## 目次

1. [概要](#1-概要)
2. [認証と接続](#2-認証と接続)
3. [切断とタイムアウト](#3-切断とタイムアウト)
4. [Client → Server メッセージ](#4-client--server-メッセージ)
   - [Matchmaking](#matchmaking)
   - [Game](#game)
   - [NPC Battle](#npc-battle)
   - [Ping](#ping)
5. [Server → Client メッセージ](#5-server--client-メッセージ)
   - [Matchmaking](#matchmaking-1)
   - [Game State](#game-state)
   - [Game Flow](#game-flow)
   - [NPC Battle](#npc-battle-1)
   - [Connection Status](#connection-status)
   - [Pong](#pong)
6. [メッセージ一覧](#6-メッセージ一覧)

---

## 1. 概要

Gateway Server が提供する WebSocket API。リアルタイムのゲーム通信（マッチメイキング、ゲーム状態同期）に使用する。

- REST API 契約: [API_REFERENCE.md](API_REFERENCE.md)
- サービス責務・契約: [FEATURE_SPEC.md](FEATURE_SPEC.md)
- WS 終端方針・マッチメイキングの WS push 経路・Pod 単一性前提などの設計意図: [ARCHITECTURE.md](ARCHITECTURE.md)

### エンドポイント

| 環境 | URL |
|------|-----|
| ローカル | `ws://localhost:9001/ws` |
| dev | `wss://overloadparty-dev.keyandnotes.com/ws` |
| stg | `wss://overloadparty-stg.keyandnotes.com/ws` |
| prod | `wss://overloadparty.keyandnotes.com/ws` |

### メッセージフォーマット

すべてのメッセージは以下の JSON 形式で送受信する。

```json
{
  "type": "message_type",
  "data": { /* ペイロード */ }
}
```

`data` が不要なメッセージ（`ping`, `pong`, `matchmaking_cancel` 等）は `data` フィールドを省略できる。

---

## 2. 認証と接続

```
GET /ws?token={token}
```

### 本番環境

- `token` に Firebase ID Token を指定
- gateway が Firebase Token を検証し、accountclient 経由で PlayerID (UUID) を解決

### ローカル開発

- `token` に `dev-token-{uid}` を指定（空でも可）
- トークンが空の場合、`uid = "dev-anonymous"` として扱う
- 未登録の uid に対してプレイヤーを自動作成

### Keep-alive

接続後、サーバーは 15 秒間隔で WebSocket Ping を送信。クライアントは Pong を返す必要がある（ブラウザは自動応答）。Pong が 5 秒以内に返らない場合、サーバーは接続を切断する。

---

## 3. 切断とタイムアウト

- **切断タイムアウト:** ゲーム中にプレイヤーが切断した場合、60 秒以内に再接続しないと自動フォーフェイト（敗北扱い）となる
- **ターンタイムアウト:** 各ターンにはタイムバンク制限がある。超過すると自動フォーフェイトとなる
- **再接続:** 再接続時はクライアントが `game_enter` を送信し、通常の `game_state` + `turn_controls` を受け取る

---

## 4. Client → Server メッセージ

### Matchmaking

#### `matchmaking_start` — PvP マッチメイキング開始

<!-- BEGIN GENERATED: MatchmakingStartMessage -->
```jsonc
{
  "deck_id": 0 // デッキ ID
}
```
<!-- END GENERATED: MatchmakingStartMessage -->

**応答:** `matchmaking_started` / `error` (code: `matchmaking_error`)

サーバーはキューに入る前にデッキバリデーションを実行する:
- 枚数チェック（ちょうど30枚）
- 所持チェック（全カードを必要枚数所持しているか）
- 制限チェック（unlimited ≤ 3、semi_limited ≤ 2、limited ≤ 1）

バリデーション失敗時は `error` (code: `matchmaking_error`, retryable: `false`) を返す。

冪等: 既にマッチメイキング中の場合はデッキを更新して成功。

---

#### `matchmaking_cancel` — マッチメイキングをキャンセル

```json
{ "type": "matchmaking_cancel" }
```

**応答:** `matchmaking_cancelled`

---

### Game

#### `game_enter` — ゲームルームに参加

<!-- BEGIN GENERATED: GameEnterMessage -->
```jsonc
{
  "game_id": "string" // ゲームID（ULID）,
  "deck_id": 0 // デッキ ID
}
```
<!-- END GENERATED: GameEnterMessage -->

**応答:** `game_entered` → `game_state`（両プレイヤーに送信）

---

#### `game_action` — ゲームアクション実行

<!-- BEGIN GENERATED: GameActionMessage -->
```jsonc
{
  "game_id": "string" // ゲームID,
  "action_type": "string" // アクション種別,
  "data": {} // アクション固有データ
}
```
<!-- END GENERATED: GameActionMessage -->

**応答:** `game_state`（両プレイヤーに送信）
**エラー:** `action_rejected`
**ゲーム終了時:** `game_over`（両プレイヤーに送信）

---

#### `use_stamp` — スタンプ送信（演出のみ）

<!-- BEGIN GENERATED: UseStampMessage -->
```jsonc
{
  "game_id": "string" // ゲームID,
  "stamp_no": 0 // スタンプ番号
}
```
<!-- END GENERATED: UseStampMessage -->

**応答:** `stamp_used`（両プレイヤーにブロードキャスト）

---

### NPC Battle

#### `npc_battle_start` — NPC 対戦開始

NPC 対戦開始。即座にゲームが作成される（マッチメイキング不要）。

<!-- BEGIN GENERATED: NPCBattleStartMessage -->
```jsonc
{
  "deck_id": 0 // プレイヤーのデッキ ID,
  "npc_model": "string" // `/npc/models` で取得した NPC モデル ID
}
```
<!-- END GENERATED: NPCBattleStartMessage -->

サーバーはゲーム作成前にデッキバリデーションを実行する（内容は `matchmaking_start` と同一）。
バリデーション失敗時は `error` (code: `npc_battle_error`, retryable: `false`) を返す。
存在しない `npc_model` を指定した場合も同様にエラーを返す。

**応答:** `npc_battle_created` (Server → Client)

エラー時は `error` メッセージが返る。

ゲーム開始後は PvP と同じ `game_action` メッセージでアクションを送信する。NPC は自動応答する。ゲーム状態は `game_enter` → `game_state` メッセージで取得可能。

---

### Ping

#### `ping` — 生存確認

```json
{ "type": "ping" }
```

**応答:** `pong`

---

## 5. Server → Client メッセージ

### Matchmaking

#### `matchmaking_started`
```json
{ "type": "matchmaking_started" }
```

#### `matchmaking_cancelled`
```json
{ "type": "matchmaking_cancelled" }
```

#### `match_found` — マッチ成立

<!-- BEGIN GENERATED: MatchFoundMessage -->
```jsonc
{
  "game_id": "string" // ゲームID（ULID）
}
```
<!-- END GENERATED: MatchFoundMessage -->

---

### Game State

#### `game_entered`
```json
{
  "type": "game_entered",
  "data": { "game_id": "ULID" }
}
```

#### `game_state` — ゲーム状態（情報秘匿適用済み）

`data` は ClientGameState 型。`myView` (PlayerView) と `oppView` (OpponentView) を含む。

<!-- BEGIN GENERATED: ClientGameState -->
```jsonc
{
  "gameID": "string" // ゲームID（ULID）,
  "currentTurn": 0 // 現在のターン番号,
  "currentPhase": "string" // 現在のフェーズ（`selecting` / `draw` / `yield` / `main` / `battle` / `end`）,
  "activePlayer": 0 // アクティブプレイヤー番号（1 or 2）,
  "isMyTurn": false // 自分のターンか,
  "turnStartedAt": "2006-01-02T15:04:05Z" // ターン開始日時,
  "myView": "PlayerView" // 自分の視点,
  "oppView": "OpponentView" // 相手の視点（情報秘匿適用済み）
}
```
<!-- END GENERATED: ClientGameState -->

**PlayerView:**

<!-- BEGIN GENERATED: PlayerView -->
```jsonc
{
  "playerNum": 0 // プレイヤー番号（1 or 2）,
  "budget": 0 // 予算,
  "insightPool": 0 // Insight プール,
  "timeBank": 0 // タイムバンク（秒）,
  "field": "Field" // フィールド（Frontend/Backend/Support）,
  "hand": [] // 手札,
  "repoCount": 0 // リポジトリ残り枚数,
  "trashCount": 0 // トラッシュ枚数,
  "trash": [] // トラッシュ,
  "availableActions": [] // 実行可能アクション一覧（アクティブプレイヤーのみ）
}
```
<!-- END GENERATED: PlayerView -->

**OpponentView:**

<!-- BEGIN GENERATED: OpponentView -->
```jsonc
{
  "playerNum": 0 // プレイヤー番号,
  "budget": 0 // 予算,
  "insightPool": 0 // Insight プール,
  "timeBank": 0 // タイムバンク（秒）,
  "field": "OpponentField" // フィールド（サポートは非公開カード隠蔽）,
  "handCount": 0 // 手札枚数（内容は非公開）,
  "repoCount": 0 // リポジトリ残り枚数,
  "trashCount": 0 // トラッシュ枚数,
  "trash": [] // トラッシュ
}
```
<!-- END GENERATED: OpponentView -->

**DeployedResource:**

<!-- BEGIN GENERATED: DeployedResource -->
```jsonc
{
  "instanceID": "string" // インスタンスID,
  "cardID": "string" // カードID,
  "artNo": 0 // アート番号,
  "rank": null // ランク（S/M/L/XL）,
  "instanceFamily": null // インスタンスファミリー,
  "faceUp": false // 表向きか,
  "deployingTurnsLeft": 0 // デプロイ残りターン数（0=アクティブ）,
  "currentAV": 0 // 現在の可用性,
  "maxAV": 0 // 最大可用性,
  "currentTP": null // 現在のスループット（Compute のみ）,
  "maxTP": null // 最大スループット（Compute のみ）,
  "currentYield": null // 現在のイールド（Data のみ）,
  "maxYield": null // 最大イールド（Data のみ）,
  "damage": 0 // 受けたダメージ量,
  "temporaryEffects": [] // 一時効果リスト,
  "monetizedAmount": 0 // 収益化で配分された Insight 量,
  "hasAttacked": false // このターンに攻撃済みか,
  "effectUsedThisTurn": false // このターンにエフェクト使用済みか,
  "effectUsedThisGame": false // このゲームでエフェクト使用済みか,
  "deployedOnTurn": 0 // デプロイされたターン,
  "deployOrder": 0 // デプロイ順序,
  "elasticBonus": 0 // Elastic によるボーナス,
  "lastAttackTurn": 0 // 最後に攻撃したターン
}
```
<!-- END GENERATED: DeployedResource -->

**DeployedSupport:**

<!-- BEGIN GENERATED: DeployedSupport -->
```jsonc
{
  "instanceID": "string" // インスタンスID,
  "cardID": "string" // カードID,
  "artNo": 0 // アート番号,
  "faceUp": false // 表向きか,
  "deployingTurnsLeft": 0 // デプロイ残りターン数,
  "deployOrder": 0 // デプロイ順序,
  "effectUsedThisTurn": false // このターンにエフェクト使用済みか,
  "effectUsedThisGame": false // このゲームでエフェクト使用済みか,
  "targetInstanceID": null // Attachment 対象のインスタンスID
}
```
<!-- END GENERATED: DeployedSupport -->

**HiddenDeployedSupport:**

<!-- BEGIN GENERATED: HiddenDeployedSupport -->
```jsonc
{
  "instanceID": "string" // インスタンスID,
  "cardID": null // カードID（表向きの場合のみ）,
  "artNo": 0 // アート番号,
  "faceDown": false // 裏向きか,
  "peeked": false // 覗き見されたか
}
```
<!-- END GENERATED: HiddenDeployedSupport -->

**UndeployedCard:**

<!-- BEGIN GENERATED: UndeployedCard -->
```jsonc
{
  "instanceID": "string" // インスタンスID,
  "cardID": "string" // カードID,
  "artNo": 0 // アート番号
}
```
<!-- END GENERATED: UndeployedCard -->

**TemporaryEffect:**

<!-- BEGIN GENERATED: TemporaryEffect -->
```jsonc
{
  "effectType": "string" // 効果タイプ,
  "value": 0 // 効果値,
  "duration": "string" // 持続期間（`until_end_of_turn` / `permanent`）,
  "sourceID": "string" // 効果の発生源インスタンスID,
  "mode": "string" // バフモード（空文字=flat / `percent`=割合）
}
```
<!-- END GENERATED: TemporaryEffect -->

##### `availableActions` — 実行可能アクション一覧（カード操作のみ）

`myView` の配下に含まれる。サーバーが毎回の状態更新時にフェーズごとの有効アクションを計算し、クライアントはこれを元に操作可能なカードのハイライトやUI制御を行う（クライアント側にゲームロジックの重複を持たせない設計）。

カードに紐付かないゲームフロー制御（フェーズ終了、手札破棄）は `turn_controls` メッセージで別途通知される。

- **`playing` 状態**: アクティブプレイヤーのみに送信される。対戦相手の `game_state` にはこのフィールドは含まれない。
- **`finished` 状態**: 省略される。

| type | 追加フィールド | 説明 |
|------|--------------|------|
| `play_card` | `handInstanceID`, `cardID`, `validZones?`, `validTargets?`, `cost?`, `choiceOptions?` | 手札からカードをデプロイ。デプロイターン 0 なら即表向き、1以上なら裏向き配置。`validZones` はゾーン+スロット (例: `"frontend_0"`)。Attachment の場合は `validZones` にサポートゾーンスロット (例: `"support_0"`)、`validTargets` に対象リソースの instanceID が入る |
| `attack` | `sourceInstanceID`, `validTargets?` | フロントの表向き Compute で攻撃。相手フロントに表向きリソースあり→フロントのみ対象 |
| `scale_up` | `sourceInstanceID`, `targetRank`, `instanceFamily?`, `needsFamily?` | リソースをスケールアップ（無料）。`needsFamily=true` なら S→M でファミリー選択が必要 |
| `monetize` | `sourceInstanceID`, `remainingCapacity?` | バックエンド Compute に Insight を配分。`remainingCapacity` は残りスループット |
| `use_effect` | `sourceInstanceID`, `validTargets?`, `effectTargetType?`, `requiredCount?` | アクティブ効果を発動。`effectTargetType`: `"none"`, `"choice"`, `"all_opp"`, `"self"` |

---

### Game Flow

#### `turn_controls` — ゲームフロー制御

カードに紐付かないゲームフロー制御を通知する。`game_state` とは別メッセージとして、状態更新のたびにアクティブプレイヤーにのみ送信される。

<!-- BEGIN GENERATED: TurnControlsMessage -->
```jsonc
{
  "canEndPhase": false // 現在のフェーズを終了できるか（main / battle フェーズで `true`）,
  "discardRequired": 0 // 手札破棄が必要な枚数（end フェーズで手札 > 6 枚の場合のみ > 0）
}
```
<!-- END GENERATED: TurnControlsMessage -->

---

#### `game_over` — ゲーム終了

<!-- BEGIN GENERATED: GameOverMessage -->
```jsonc
{
  "game_id": "string" // ゲームID,
  "winning_player_num": 0 // 勝者のプレイヤー番号（1 or 2）,
  "win_reason": "string" // 勝因
}
```
<!-- END GENERATED: GameOverMessage -->

#### `action_rejected` — アクション拒否

<!-- BEGIN GENERATED: ActionRejectedMessage -->
```jsonc
{
  "game_id": "string" // ゲームID,
  "action_type": "string" // 拒否されたアクション種別,
  "reason": "string" // 拒否理由
}
```
<!-- END GENERATED: ActionRejectedMessage -->

#### `stamp_used` — スタンプ受信

<!-- BEGIN GENERATED: StampUsedMessage -->
```jsonc
{
  "game_id": "string" // ゲームID,
  "player_num": 0 // 送信したプレイヤー番号（1 or 2）,
  "stamp_no": 0 // スタンプ番号
}
```
<!-- END GENERATED: StampUsedMessage -->

#### `error` — エラー

<!-- BEGIN GENERATED: ErrorMessage -->
```jsonc
{
  "error_code": "string" // エラーコード,
  "message": "string" // エラー詳細メッセージ,
  "retryable": false // リトライ可能か
}
```
<!-- END GENERATED: ErrorMessage -->

**エラーコード一覧:**

| コード | 発生タイミング | retryable | 説明 |
|---|---|---|---|
| `invalid_message` | メッセージ受信時 | `false` | JSON パース失敗 |
| `invalid_data` | メッセージ受信時 | `false` | ペイロードのデシリアライズ失敗 |
| `matchmaking_error` | `matchmaking_start` | `true`/`false` | デッキバリデーション失敗、バトル上限超過、キュー登録失敗 |
| `npc_battle_error` | `npc_battle_start` | `true`/`false` | デッキバリデーション失敗、バトル上限超過、ゲーム作成失敗 |
| `game_state_error` | `game_enter` / ゲーム中 | `true` | バトルサーバーからの状態取得失敗 |
| `turn_controls_error` | ゲーム中 | `true` | ターン制御情報の取得失敗 |

---

#### `action_performed` — 対戦相手のアクション通知

対戦相手（NPC または PvP 相手）が実行した個別アクションを通知する。クライアントはこのメッセージをキューに積み、順番にアニメーション再生する。

<!-- BEGIN GENERATED: ActionPerformedMessage -->
```jsonc
{
  "sequence": 0 // シーケンス番号,
  "action_type": "string" // 実行されたアクション種別（`play_card`, `attack`, `scale_up`, `use_effect`, `monetize`, `end_phase`, `discard_hand`, `battle_start`, `turn_start`）,
  "action_data": {} // アクションの詳細データ（アクション種別により構造が異なる）,
  "state": {} // アクション実行後のゲーム状態（情報隠蔽適用済み）
}
```
<!-- END GENERATED: ActionPerformedMessage -->

**送信タイミング:**
- **NPC ターン**: `runNPCTurnIfNeeded` 内の各アクション実行後
- **PvP**: 相手プレイヤーのアクション実行後（自分のアクションには送信されない）
- **battle_start**: selecting 完了後、最初の `game_state` より前に送信
- **turn_start**: 各ターン開始時、draw フェーズの `game_state` より前に送信

**クライアント処理フロー:**
1. `action_performed` 受信 → アニメーションキューに追加
2. キューを順番に処理（各アクションにディレイを設けて再生）
3. 最後の `game_state` を ground truth として適用

> **Note:** 以下の `action_data` スキーマの SSoT は `data/event_schemas.yaml` である。フィールドを変更する場合は event_schemas.yaml を編集し、`generate_constants.py` を実行すること。

##### `battle_start` — バトル開始バナー

selecting フェーズ完了後、最初の game_state より前に送信される。各プレイヤーに自分視点の情報が届く。

```jsonc
{
  "my_name": "string" // 自分の表示名,
  "my_level": 0 // 自分のレベル,
  "opponent_name": "string" // 対戦相手の表示名（NPC の場合は陣営名）,
  "opponent_level": 0 // 対戦相手のレベル（NPC は固定 50）,
  "match_type": "string" // `npc` or `pvp`
}
```

NPC 表示名:
| Faction ID | 表示名 |
|------------|--------|
| SHE | Smile Horizon Express |
| Tenki | 天気使い |
| Sugar | しゅがーらぼ |
| Tuners | 調律部 |

##### `turn_start` — ターン開始バナー

各ターン開始時に送信される。draw フェーズの `game_state` より前に届く。

```jsonc
{
  "turn": 0 // ターン番号,
  "is_my_turn": false // このプレイヤーのターンか
}
```

**送信順序:**

```
[selecting 完了]
  → action_performed (battle_start)
  → action_performed (turn_start, turn=1)
  → game_state
  → turn_controls

[ターン切り替わり時]
  → action_performed (turn_start, turn=N)
  → game_state
  → turn_controls
```

---

### NPC Battle

#### `npc_battle_created` — NPC 対戦作成通知

<!-- BEGIN GENERATED: NPCBattleCreatedMessage -->
```jsonc
{
  "game_id": "string" // ゲームID（ULID）
}
```
<!-- END GENERATED: NPCBattleCreatedMessage -->

---

### Connection Status

#### `opponent_disconnected` — 対戦相手が切断
```json
{ "type": "opponent_disconnected" }
```

ペイロードなし。対戦相手が切断したことを通知する。

#### `opponent_reconnected` — 対戦相手が再接続
```json
{ "type": "opponent_reconnected" }
```

ペイロードなし。対戦相手が再接続したことを通知する。

---

### Pong

#### `pong`
```json
{ "type": "pong" }
```

---

## 6. メッセージ一覧

### Client → Server

| メッセージ | 応答 | 用途 |
|-----------|------|------|
| `matchmaking_start` | `matchmaking_started` / `error` | PvP マッチメイキング開始 |
| `matchmaking_cancel` | `matchmaking_cancelled` | マッチメイキングキャンセル |
| `game_enter` | `game_entered` → `game_state` | ゲームルーム参加 |
| `game_action` | `game_state` / `action_rejected` / `game_over` | ゲームアクション実行 |
| `use_stamp` | `stamp_used` | スタンプ送信 |
| `npc_battle_start` | `npc_battle_created` / `error` | NPC 対戦開始 |
| `ping` | `pong` | 生存確認 |

### Server → Client

| メッセージ | 契機 | 用途 |
|-----------|------|------|
| `matchmaking_started` | `matchmaking_start` 成功 | マッチメイキング開始通知 |
| `matchmaking_cancelled` | `matchmaking_cancel` 成功 | マッチメイキングキャンセル通知 |
| `match_found` | マッチ成立 | 対戦相手決定 |
| `game_entered` | `game_enter` 成功 | ゲームルーム参加完了 |
| `game_state` | 状態変更時 | ゲーム状態（情報秘匿適用済み） |
| `turn_controls` | 状態変更時 | フェーズ終了・手札破棄の制御 |
| `game_over` | ゲーム終了時 | 勝敗結果 |
| `action_rejected` | 不正アクション時 | アクション拒否 |
| `action_performed` | 相手アクション時 | 相手のアクション通知（アニメーション用） |
| `stamp_used` | `use_stamp` 受信時 | スタンプ受信 |
| `error` | エラー発生時 | エラー通知 |
| `npc_battle_created` | `npc_battle_start` 成功 | NPC 対戦作成通知 |
| `opponent_disconnected` | 相手切断時 | 対戦相手切断通知 |
| `opponent_reconnected` | 相手再接続時 | 対戦相手再接続通知 |
| `pong` | `ping` 受信時 | Ping 応答 |

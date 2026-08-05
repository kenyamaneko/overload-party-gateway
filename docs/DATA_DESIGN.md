# gateway スキーマ - データ設計

> **DDL の SSoT:** `db/schema.sql`

## 設計概要

gateway スキーマは人間プレイヤーとゲームスロット (1/2) の対応を管理する。battle は pure engine としてスロット番号のみを扱いプレイヤー ID を知らないため、このマッピングを gateway が所有する。

---

## テーブル構成

### game_players

プレイヤー ID マッピング。人間プレイヤーのスロットのみ（NPC スロットには行がない）。

- **PK:** `(game_id, player_num)`
- **INDEX:** `idx_gateway_game_players_player_id` ON `(player_id)`

<!-- BEGIN GENERATED: game_players -->
| カラム名 | 型 | Nullable | 説明 |
|---|---|---|---|
| `game_id` | VARCHAR(26) | No | battle.games(game_id) を参照 (app-level FK) |
| `player_num` | SMALLINT | No | 人間が座っているスロット番号 (1 or 2) |
| `player_id` | UUID | No | account.players(id) を参照 (app-level FK) |
| `exp_awarded` | BOOLEAN | No | 経験値付与済みフラグ（二重付与防止） |
<!-- END GENERATED: game_players -->

**設計判断:**
- battle と gateway のスキーマ分離は「battle がプレイヤー ID を知らない」というアーキテクチャ上の境界を DB レベルで表現している
- `exp_awarded` フラグは、ゲーム終了通知が重複した場合の冪等性を保証する。gateway はゲーム終了を検知すると、このフラグを確認してから account サービスに経験値加算 API を呼ぶ

**ライフサイクル:**
1. matchmaking の `match_made` Pub/Sub イベント受信時に INSERT（PvP の場合）
2. NPC 戦の場合は gateway が battle に CreateGame を呼んだ後に INSERT
3. ゲーム終了時に `exp_awarded = TRUE` に UPDATE

### invalidated_games

サーバの停止によって無効になった対戦。人間 2 人の対戦のみを対象とする。

- **PK:** `(game_id)`
- **INDEX:** `idx_gateway_invalidated_games_unfinished` ON `(invalidated_at) WHERE finished_at IS NULL`

<!-- BEGIN GENERATED: invalidated_games -->
| カラム名 | 型 | Nullable | 説明 |
|---|---|---|---|
| `game_id` | VARCHAR(26) | No | battle.games(game_id) を参照 (app-level FK) |
| `invalidated_at` | TIMESTAMPTZ | No | 停止時に無効として記録した時刻 |
| `finished_at` | TIMESTAMPTZ | Yes | battle 上で決着させた時刻。未決着は NULL |
<!-- END GENERATED: invalidated_games -->

**設計判断:**
- 停止時は記録だけを 1 文の INSERT で残し、battle 上での決着は次の起動で行う。強制終了までの猶予が短く、対戦ごとに下流サービスを呼ぶ形では取りこぼすため
- `finished_at` を決着が成功した後にだけ入れることで、途中で落ちた対戦が次の起動でやり直される。battle は決着済みの対戦への強制決着を拒むため、成功を確かめてから記録する
- 記録は決着後も残す。無効になった対戦への入室と切断猶予の評価を、その後も断れるようにするため

**ライフサイクル:**
1. 停止時に進行中の対戦を INSERT
2. 起動時に `finished_at IS NULL` の対戦を battle 上で決着させ、`finished_at` を UPDATE

---

## テーブル間リレーション

```
[battle.games] ─ ─ ─ (cross-schema, app-level)
  │
  └── 1:N ── game_players (PK: game_id, player_num)
                │
                └── ─ ─ ─ [account.players] (cross-schema, app-level)
```

---

## インデックス戦略

| インデックス | 対象 | 用途 |
|---|---|---|
| `idx_gateway_game_players_player_id` | `game_players(player_id)` | プレイヤーの進行中ゲーム検索。再接続時にプレイヤーが参加中のゲームを特定する |

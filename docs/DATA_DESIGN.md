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

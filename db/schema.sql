-- Overload Party Gateway - PostgreSQL DDL (SSoT for gateway-owned tables)
-- ADR-014 に従い gateway サービスが所有するテーブルのみをこのファイルで管理する。
--
-- Schema: gateway
-- Owner : overload-party-gateway
--
-- 備考:
--   - gateway.game_players は人間プレイヤーとゲームスロット (1/2) の対応を持つ。
--     battle はスロット番号のみを知る pure engine であり、プレイヤー ID を知らない。
--     gateway は match_made イベント (matchmaking → Pub/Sub) 受信時に INSERT し、
--     ゲーム終了時に exp_awarded を更新して経験値付与処理を担う。
--   - player_id は account.players を参照するが、スキーマをまたぐため FK は張らず
--     app-level integrity で担保する (ADR-014)。
--   - gateway.game_players には updated_at カラムが無いためトリガー関数は不要。

-- =============================================================================
-- Schemas
-- =============================================================================

CREATE SCHEMA IF NOT EXISTS gateway;

-- =============================================================================
-- Game Player Mapping (schema: gateway)
-- =============================================================================

CREATE TABLE gateway.game_players (
  game_id       VARCHAR(26) NOT NULL,            -- battle.games(game_id) を参照 (app-level FK)
  player_num    SMALLINT NOT NULL,               -- 人間が座っているスロット番号 (1 or 2)
  player_id     UUID NOT NULL,                   -- account.players(id) を参照 (app-level FK)
  exp_awarded   BOOLEAN NOT NULL DEFAULT FALSE,  -- 経験値付与済みフラグ（二重付与防止）
  PRIMARY KEY (game_id, player_num)
);

CREATE INDEX idx_gateway_game_players_player_id ON gateway.game_players(player_id);

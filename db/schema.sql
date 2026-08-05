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
--   - gateway.invalidated_games は停止で無効になった対戦を記録する。停止時は記録だけを
--     行い、battle 上での決着と消費バトル回数の返却は次の起動で行う。

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
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(), -- 行を作成した時刻。バトル回数を消費した時刻として account への返却に渡す
  PRIMARY KEY (game_id, player_num)
);

CREATE INDEX idx_gateway_game_players_player_id ON gateway.game_players(player_id);

-- =============================================================================
-- Match Dedup (schema: gateway)
-- =============================================================================

CREATE TABLE gateway.processed_matches (
  match_id     VARCHAR(64) NOT NULL,             -- matchmaking の match_id (mch_<ULID>)
  game_id      VARCHAR(26),                      -- battle.games(game_id) を参照 (app-level FK)。作成前は NULL
  notified     BOOLEAN NOT NULL DEFAULT FALSE,   -- 成立通知の送信済みフラグ（再配信での二重通知防止）
  claimed_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (match_id)
);

-- =============================================================================
-- Invalidated Games (schema: gateway)
-- =============================================================================

CREATE TABLE gateway.invalidated_games (
  game_id         VARCHAR(26) NOT NULL,          -- battle.games(game_id) を参照 (app-level FK)
  invalidated_at  TIMESTAMPTZ NOT NULL DEFAULT now(), -- 停止時に無効として記録した時刻
  finished_at     TIMESTAMPTZ,                   -- battle 上で決着させた時刻。未決着は NULL
  reverted_at     TIMESTAMPTZ,                   -- 消費バトル回数を両プレイヤーに戻した時刻。未返却は NULL
  PRIMARY KEY (game_id)
);

-- 記録は決着後も残り続けるため、起動時の走査が未決着の行だけを見るように部分索引を張る。
CREATE INDEX idx_gateway_invalidated_games_unfinished
  ON gateway.invalidated_games(invalidated_at) WHERE finished_at IS NULL;

-- 同じ理由で、返却待ちの走査が未返却の行だけを見るように部分索引を張る。
CREATE INDEX idx_gateway_invalidated_games_unreverted
  ON gateway.invalidated_games(invalidated_at) WHERE reverted_at IS NULL;

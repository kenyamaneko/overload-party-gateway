# gateway db/

Post ADR-014, gateway owns the `gateway` PostgreSQL schema. The DDL SSoT is
`db/schema.sql`, which is fetched by ops repo's db-migrate job (via
`ops/db-migrate/schemas.lock.yaml`) and applied to Cloud SQL with psqldef.

## Owned tables

- `gateway.game_players` — human-player ↔ game slot mapping. Gateway is the
  only service that knows player IDs; battle is a pure engine that only deals
  with slot numbers (1 / 2). Gateway inserts rows on `match_made` Pub/Sub
  receipt and updates `exp_awarded` when granting experience at game over.
  `player_id` references `account.players(id)` as an app-level (not SQL) FK
  because cross-schema FKs are forbidden by ADR-014. `game_id` references
  `battle.games(game_id)` the same way.

## Read-only cross-service reference

- `newsfeed.news_articles` — cloud news list. Owned by newsfeed (Cloud Run
  Job). Gateway only reads and proxies to client via `/api/v1/cloud-news`.
  The DDL lives in `overload-party-newsfeed/schema.sql`.

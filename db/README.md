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
- `gateway.processed_matches` — persistent dedup for `match_made` push
  redelivery. `match_id` is claimed before calling battle, and `game_id` is
  recorded once battle returns the created game. `notified` is set by the single
  call that wins the right to send the `match_found` notification. `game_id`
  references `battle.games(game_id)` as an app-level (not SQL) FK.

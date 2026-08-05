package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

var _ port.GamePlayerRepo = (*PgGamePlayerRepository)(nil)

// PgGamePlayerRepository は PostgreSQL による game_players のリポジトリです
type PgGamePlayerRepository struct {
	pool *pgxpool.Pool
}

// NewPgGamePlayerRepository は PgGamePlayerRepository を生成します
func NewPgGamePlayerRepository(pool *pgxpool.Pool) *PgGamePlayerRepository {
	return &PgGamePlayerRepository{pool: pool}
}

// InsertGamePlayer はゲームにプレイヤースロットを登録します
func (r *PgGamePlayerRepository) InsertGamePlayer(ctx context.Context, gameID string, playerNum int, playerID string) error {
	_, err := connFrom(ctx, r.pool).Exec(ctx,
		`INSERT INTO gateway.game_players (game_id, player_num, player_id)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (game_id, player_num) DO NOTHING`,
		gameID, playerNum, playerID,
	)
	if err != nil {
		return fmt.Errorf("insert game player: %w", err)
	}
	return nil
}

// LookupPlayerNum はゲーム内のプレイヤー番号を取得します
func (r *PgGamePlayerRepository) LookupPlayerNum(ctx context.Context, gameID string, playerID string) (int, error) {
	var playerNum int
	err := connFrom(ctx, r.pool).QueryRow(ctx,
		`SELECT player_num FROM gateway.game_players WHERE game_id = $1 AND player_id = $2`,
		gameID, playerID,
	).Scan(&playerNum)
	if err != nil {
		return 0, fmt.Errorf("lookup player num: %w", err)
	}
	return playerNum, nil
}

// LookupGamePlayers はゲームの全プレイヤーエントリを取得します
func (r *PgGamePlayerRepository) LookupGamePlayers(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
	rows, err := connFrom(ctx, r.pool).Query(ctx,
		`SELECT player_num, player_id, created_at FROM gateway.game_players WHERE game_id = $1 ORDER BY player_num`,
		gameID,
	)
	if err != nil {
		return nil, fmt.Errorf("lookup game players: %w", err)
	}
	defer rows.Close()

	var entries []port.GamePlayerEntry
	for rows.Next() {
		var e port.GamePlayerEntry
		if err := rows.Scan(&e.PlayerNum, &e.PlayerID, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan game player: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// CountPlayersByGame は指定したゲームごとの人間プレイヤーのスロット数を取得します
func (r *PgGamePlayerRepository) CountPlayersByGame(ctx context.Context, gameIDs []string) (map[string]int, error) {
	rows, err := connFrom(ctx, r.pool).Query(ctx,
		`SELECT game_id, count(*) FROM gateway.game_players WHERE game_id = ANY($1) GROUP BY game_id`,
		gameIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("count players by game: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int, len(gameIDs))
	for rows.Next() {
		var gameID string
		var count int
		if err := rows.Scan(&gameID, &count); err != nil {
			return nil, fmt.Errorf("scan player count: %w", err)
		}
		counts[gameID] = count
	}
	return counts, rows.Err()
}

// MarkExpAwarded は経験値付与済みフラグを設定します
func (r *PgGamePlayerRepository) MarkExpAwarded(ctx context.Context, gameID string) (bool, error) {
	tag, err := connFrom(ctx, r.pool).Exec(ctx,
		`UPDATE gateway.game_players SET exp_awarded = true
		 WHERE game_id = $1 AND player_num = 1 AND exp_awarded = false`,
		gameID,
	)
	if err != nil {
		return false, fmt.Errorf("mark exp awarded: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

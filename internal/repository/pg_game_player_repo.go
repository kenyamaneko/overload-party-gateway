package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

var _ port.GamePlayerRepo = (*PgGamePlayerRepository)(nil)

type PgGamePlayerRepository struct {
	pool *pgxpool.Pool
}

func NewPgGamePlayerRepository(pool *pgxpool.Pool) *PgGamePlayerRepository {
	return &PgGamePlayerRepository{pool: pool}
}

func (r *PgGamePlayerRepository) InsertGamePlayer(ctx context.Context, gameID string, playerNum int, playerID string) error {
	_, err := connFrom(ctx, r.pool).Exec(ctx,
		`INSERT INTO game_players (game_id, player_num, player_id)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (game_id, player_num) DO NOTHING`,
		gameID, playerNum, playerID,
	)
	if err != nil {
		return fmt.Errorf("insert game player: %w", err)
	}
	return nil
}

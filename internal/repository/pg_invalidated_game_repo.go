package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

var _ port.InvalidatedGameRepo = (*PgInvalidatedGameRepository)(nil)

// PgInvalidatedGameRepository は PostgreSQL による invalidated_games のリポジトリです
type PgInvalidatedGameRepository struct {
	pool *pgxpool.Pool
}

// NewPgInvalidatedGameRepository は PgInvalidatedGameRepository を生成します
func NewPgInvalidatedGameRepository(pool *pgxpool.Pool) *PgInvalidatedGameRepository {
	return &PgInvalidatedGameRepository{pool: pool}
}

// MarkInvalidated は対戦を無効として記録します
func (r *PgInvalidatedGameRepository) MarkInvalidated(ctx context.Context, gameIDs []string) error {
	if len(gameIDs) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO gateway.invalidated_games (game_id)
		 SELECT unnest($1::text[])
		 ON CONFLICT (game_id) DO NOTHING`,
		gameIDs,
	)
	if err != nil {
		return fmt.Errorf("mark games invalidated: %w", err)
	}
	return nil
}

// ListUnfinished は決着していない無効な対戦の ID を取得します
func (r *PgInvalidatedGameRepository) ListUnfinished(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT game_id FROM gateway.invalidated_games
		  WHERE finished_at IS NULL
		  ORDER BY invalidated_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("list unfinished invalidated games: %w", err)
	}
	defer rows.Close()

	var gameIDs []string
	for rows.Next() {
		var gameID string
		if err := rows.Scan(&gameID); err != nil {
			return nil, fmt.Errorf("scan invalidated game: %w", err)
		}
		gameIDs = append(gameIDs, gameID)
	}
	return gameIDs, rows.Err()
}

// MarkFinished は無効な対戦を決着済みとして記録します
func (r *PgInvalidatedGameRepository) MarkFinished(ctx context.Context, gameID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE gateway.invalidated_games SET finished_at = now()
		  WHERE game_id = $1 AND finished_at IS NULL`,
		gameID,
	)
	if err != nil {
		return fmt.Errorf("mark invalidated game finished: %w", err)
	}
	return nil
}

// ListRevertPending は決着済みで消費バトル回数を戻していない対戦の ID を取得します
func (r *PgInvalidatedGameRepository) ListRevertPending(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT game_id FROM gateway.invalidated_games
		  WHERE finished_at IS NOT NULL AND reverted_at IS NULL
		  ORDER BY invalidated_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("list revert pending invalidated games: %w", err)
	}
	defer rows.Close()

	var gameIDs []string
	for rows.Next() {
		var gameID string
		if err := rows.Scan(&gameID); err != nil {
			return nil, fmt.Errorf("scan revert pending game: %w", err)
		}
		gameIDs = append(gameIDs, gameID)
	}
	return gameIDs, rows.Err()
}

// MarkReverted は無効な対戦の消費バトル回数を返却済みとして記録します
func (r *PgInvalidatedGameRepository) MarkReverted(ctx context.Context, gameID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE gateway.invalidated_games SET reverted_at = now()
		  WHERE game_id = $1 AND reverted_at IS NULL`,
		gameID,
	)
	if err != nil {
		return fmt.Errorf("mark invalidated game reverted: %w", err)
	}
	return nil
}

// IsInvalidated は対戦が無効として記録済みかを取得します
func (r *PgInvalidatedGameRepository) IsInvalidated(ctx context.Context, gameID string) (bool, error) {
	var invalidated bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM gateway.invalidated_games WHERE game_id = $1)`,
		gameID,
	).Scan(&invalidated)
	if err != nil {
		return false, fmt.Errorf("check invalidated game: %w", err)
	}
	return invalidated, nil
}

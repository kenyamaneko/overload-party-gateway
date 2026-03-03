package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PgGameConfigRepository implements GameConfigRepo backed by PostgreSQL.
type PgGameConfigRepository struct {
	pool *pgxpool.Pool
}

func NewPgGameConfigRepository(pool *pgxpool.Pool) *PgGameConfigRepository {
	return &PgGameConfigRepository{pool: pool}
}

func (r *PgGameConfigRepository) GetInt64(ctx context.Context, key string, fallback int64) (int64, error) {
	row := r.pool.QueryRow(ctx, `SELECT value FROM game_config WHERE key = $1`, key)

	var raw json.RawMessage
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fallback, nil
		}
		return 0, fmt.Errorf("get game config %s: %w", key, err)
	}

	var v int64
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, fmt.Errorf("parse game config %s: %w", key, err)
	}
	return v, nil
}

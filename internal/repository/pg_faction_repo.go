package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

var _ port.FactionRepo = (*PgFactionRepository)(nil)

type PgFactionRepository struct {
	pool *pgxpool.Pool
}

func NewPgFactionRepository(pool *pgxpool.Pool) *PgFactionRepository {
	return &PgFactionRepository{pool: pool}
}

func (r *PgFactionRepository) AddPlayerFaction(ctx context.Context, playerID, faction, source string) error {
	_, err := connFrom(ctx, r.pool).Exec(ctx,
		`INSERT INTO player_factions (player_id, faction, source)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (player_id, faction) DO NOTHING`,
		playerID, faction, source,
	)
	if err != nil {
		return fmt.Errorf("insert player faction: %w", err)
	}
	return nil
}

func (r *PgFactionRepository) GetPlayerFactions(ctx context.Context, playerID string) ([]string, error) {
	rows, err := connFrom(ctx, r.pool).Query(ctx,
		`SELECT faction FROM player_factions WHERE player_id = $1`,
		playerID)
	if err != nil {
		return nil, fmt.Errorf("query player factions: %w", err)
	}
	defer rows.Close()

	var factions []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, fmt.Errorf("scan faction: %w", err)
		}
		factions = append(factions, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate factions: %w", err)
	}
	return factions, nil
}

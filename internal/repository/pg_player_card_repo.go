package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
)

// Compile-time interface check.
var _ PlayerCardRepo = (*PgPlayerCardRepository)(nil)

// PgPlayerCardRepository implements PlayerCardRepo backed by PostgreSQL.
type PgPlayerCardRepository struct {
	pool *pgxpool.Pool
}

func NewPgPlayerCardRepository(pool *pgxpool.Pool) *PgPlayerCardRepository {
	return &PgPlayerCardRepository{pool: pool}
}

// GetPlayerCards returns all player_cards for a player ordered by card_id.
func (r *PgPlayerCardRepository) GetPlayerCards(ctx context.Context, playerID string) ([]*model.PlayerCard, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT player_id, card_id, art_no, count
		 FROM player_cards WHERE player_id = $1 ORDER BY card_id, art_no`,
		playerID,
	)
	if err != nil {
		return nil, fmt.Errorf("query player cards: %w", err)
	}
	defer rows.Close()

	cards := make([]*model.PlayerCard, 0, 32)
	for rows.Next() {
		var pc model.PlayerCard
		if err := rows.Scan(&pc.PlayerID, &pc.CardID, &pc.ArtNo, &pc.Count); err != nil {
			return nil, fmt.Errorf("scan player card: %w", err)
		}
		cards = append(cards, &pc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate player cards: %w", err)
	}
	return cards, nil
}

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// Compile-time interface check.
var _ port.CardRepo = (*PgCardRepository)(nil)

// PgCardRepository implements CardRepo backed by PostgreSQL via pgxpool.
type PgCardRepository struct {
	pool *pgxpool.Pool
}

// NewPgCardRepository returns a new PgCardRepository.
func NewPgCardRepository(pool *pgxpool.Pool) *PgCardRepository {
	return &PgCardRepository{pool: pool}
}

// FindAll returns all active card definitions ordered by card_id.
func (r *PgCardRepository) FindAll(ctx context.Context) ([]*model.CardDefinition, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT card_id, card_name, resource_label, faction, card_type, resizable, elastic, stats, effect_text, effects, restriction, is_active, created_at, updated_at
		 FROM card_definitions WHERE is_active = true ORDER BY card_id`,
	)
	if err != nil {
		return nil, fmt.Errorf("query cards: %w", err)
	}
	defer rows.Close()

	var cards []*model.CardDefinition
	for rows.Next() {
		c, err := scanCardDefinition(rows)
		if err != nil {
			return nil, fmt.Errorf("scan card: %w", err)
		}
		cards = append(cards, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cards: %w", err)
	}
	return cards, nil
}

// FindByCardID returns a single card definition by its card ID.
// Returns (nil, nil) when no matching row exists.
func (r *PgCardRepository) FindByCardID(ctx context.Context, cardID string) (*model.CardDefinition, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT card_id, card_name, resource_label, faction, card_type, resizable, elastic, stats, effect_text, effects, restriction, is_active, created_at, updated_at
		 FROM card_definitions WHERE card_id = $1`,
		cardID,
	)

	c, err := scanCardDefinition(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find card by card_id: %w", err)
	}
	return c, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// pgxScannable is satisfied by both pgx.Row and pgx.Rows.
type pgxScannable interface {
	Scan(dest ...any) error
}

// scanCardDefinition scans a single row into a model.CardDefinition.
func scanCardDefinition(row pgxScannable) (*model.CardDefinition, error) {
	var c model.CardDefinition
	var stats, effects json.RawMessage
	err := row.Scan(
		&c.CardID,
		&c.CardName,
		&c.ResourceLabel,
		&c.Faction,
		&c.CardType,
		&c.Resizable,
		&c.Elastic,
		&stats,
		&c.EffectText,
		&effects,
		&c.Restriction,
		&c.IsActive,
		&c.CreatedAt,
		&c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	c.Stats = stats
	c.Effects = effects
	return &c, nil
}

package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// Compile-time interface check.
var _ port.UserSettingsRepo = (*PgUserSettingsRepository)(nil)

// PgUserSettingsRepository implements UserSettingsRepo backed by PostgreSQL.
type PgUserSettingsRepository struct {
	pool *pgxpool.Pool
}

func NewPgUserSettingsRepository(pool *pgxpool.Pool) *PgUserSettingsRepository {
	return &PgUserSettingsRepository{pool: pool}
}

// Get retrieves user settings for a player.
// Returns (nil, nil) when no row exists.
func (r *PgUserSettingsRepository) Get(ctx context.Context, playerID string) (*model.UserSettings, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT player_id, language, bgm_volume, se_volume, push_enabled, updated_at
		 FROM user_settings WHERE player_id = $1`,
		playerID,
	)

	var s model.UserSettings
	err := row.Scan(&s.PlayerID, &s.Language, &s.BgmVolume, &s.SeVolume, &s.PushEnabled, &s.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get user settings: %w", err)
	}
	return &s, nil
}

// Upsert inserts or updates user settings.
// If a transaction is present in the context, it participates in that transaction.
func (r *PgUserSettingsRepository) Upsert(ctx context.Context, s *model.UserSettings) error {
	db := connFrom(ctx, r.pool)

	now := time.Now()
	_, err := db.Exec(ctx,
		`INSERT INTO user_settings (player_id, language, bgm_volume, se_volume, push_enabled, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (player_id) DO UPDATE SET
		   language = EXCLUDED.language,
		   bgm_volume = EXCLUDED.bgm_volume,
		   se_volume = EXCLUDED.se_volume,
		   push_enabled = EXCLUDED.push_enabled,
		   updated_at = EXCLUDED.updated_at`,
		s.PlayerID, s.Language, s.BgmVolume, s.SeVolume, s.PushEnabled, now,
	)
	if err != nil {
		return fmt.Errorf("upsert user settings: %w", err)
	}
	s.UpdatedAt = now
	return nil
}

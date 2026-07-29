package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

var _ port.ProcessedMatchRepo = (*PgProcessedMatchRepository)(nil)

// PgProcessedMatchRepository は PostgreSQL による processed_matches のリポジトリです
type PgProcessedMatchRepository struct {
	pool *pgxpool.Pool
}

// NewPgProcessedMatchRepository は PgProcessedMatchRepository を生成します
func NewPgProcessedMatchRepository(pool *pgxpool.Pool) *PgProcessedMatchRepository {
	return &PgProcessedMatchRepository{pool: pool}
}

// Claim は matchId の処理を排他的に開始します。既に claim 済みなら claimed=false を返します。
func (r *PgProcessedMatchRepository) Claim(ctx context.Context, matchID string) (bool, error) {
	tag, err := connFrom(ctx, r.pool).Exec(ctx,
		`INSERT INTO gateway.processed_matches (match_id) VALUES ($1) ON CONFLICT (match_id) DO NOTHING`,
		matchID,
	)
	if err != nil {
		return false, fmt.Errorf("claim processed match: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// Release は battle 呼び出し前の失敗時に Claim を取り消します。
func (r *PgProcessedMatchRepository) Release(ctx context.Context, matchID string) error {
	_, err := connFrom(ctx, r.pool).Exec(ctx,
		`DELETE FROM gateway.processed_matches WHERE match_id = $1`,
		matchID,
	)
	if err != nil {
		return fmt.Errorf("release processed match: %w", err)
	}
	return nil
}

// RecordGameCreated は claim 済みの matchId に対して作成された battle game の ID を記録します。
func (r *PgProcessedMatchRepository) RecordGameCreated(ctx context.Context, matchID, gameID string) error {
	_, err := connFrom(ctx, r.pool).Exec(ctx,
		`UPDATE gateway.processed_matches SET game_id = $2 WHERE match_id = $1`,
		matchID, gameID,
	)
	if err != nil {
		return fmt.Errorf("record processed match game id: %w", err)
	}
	return nil
}

// GameIDFor は matchId に対して既に作成済みの battle game の ID を返します。未作成なら found=false です。
func (r *PgProcessedMatchRepository) GameIDFor(ctx context.Context, matchID string) (string, bool, error) {
	var gameID *string
	err := connFrom(ctx, r.pool).QueryRow(ctx,
		`SELECT game_id FROM gateway.processed_matches WHERE match_id = $1`,
		matchID,
	).Scan(&gameID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("lookup processed match game id: %w", err)
	}
	if gameID == nil {
		return "", false, nil
	}
	return *gameID, true, nil
}

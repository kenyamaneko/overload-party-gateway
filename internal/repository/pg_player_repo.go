package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/civil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
)

// Compile-time interface check.
var _ PlayerRepo = (*PgPlayerRepository)(nil)

// PgPlayerRepository implements PlayerRepo backed by PostgreSQL via pgxpool.
type PgPlayerRepository struct {
	pool *pgxpool.Pool
}

// NewPgPlayerRepository returns a new PgPlayerRepository.
func NewPgPlayerRepository(pool *pgxpool.Pool) *PgPlayerRepository {
	return &PgPlayerRepository{pool: pool}
}

// CreateWithTx inserts both a players row and a player_daily_battle row using the given DBTX.
func (r *PgPlayerRepository) CreateWithTx(ctx context.Context, db DBTX, player *model.Player, dailyBattle *model.PlayerDailyBattle) error {
	_, err := db.Exec(ctx,
		`INSERT INTO players (player_id, firebase_uid, username, level, exp, is_premium, equipped_icon_no, selected_faction, premium_expires_at, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		player.PlayerID,
		player.FirebaseUID,
		player.Username,
		player.Level,
		player.Exp,
		player.IsPremium,
		player.EquippedIconNo,
		player.SelectedFaction,
		player.PremiumExpiresAt,
		player.CreatedAt,
		player.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert player: %w", err)
	}

	lastResetTime := civilDateToTime(dailyBattle.LastResetDate)
	_, err = db.Exec(ctx,
		`INSERT INTO player_daily_battle (player_id, daily_battle_count, last_reset_date)
		 VALUES ($1,$2,$3)`,
		dailyBattle.PlayerID,
		dailyBattle.DailyBattleCount,
		lastResetTime,
	)
	if err != nil {
		return fmt.Errorf("insert daily battle: %w", err)
	}

	return nil
}

// Create inserts both a players row and a player_daily_battle row atomically.
func (r *PgPlayerRepository) Create(ctx context.Context, player *model.Player, dailyBattle *model.PlayerDailyBattle) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := r.CreateWithTx(ctx, tx, player, dailyBattle); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// FindByID looks up a player by primary key.
func (r *PgPlayerRepository) FindByID(ctx context.Context, playerID string) (*model.Player, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT player_id, firebase_uid, username, level, exp, is_premium, equipped_icon_no, selected_faction, premium_expires_at, created_at, updated_at
		 FROM players WHERE player_id = $1`,
		playerID,
	)

	p, err := scanPlayer(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find player by id: %w", err)
	}
	return p, nil
}

// FindByFirebaseUID looks up a player by their Firebase UID.
// Returns (nil, nil) when no matching row exists.
func (r *PgPlayerRepository) FindByFirebaseUID(ctx context.Context, firebaseUID string) (*model.Player, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT player_id, firebase_uid, username, level, exp, is_premium, equipped_icon_no, selected_faction, premium_expires_at, created_at, updated_at
		 FROM players WHERE firebase_uid = $1 LIMIT 1`,
		firebaseUID,
	)

	p, err := scanPlayer(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find player by firebase_uid: %w", err)
	}
	return p, nil
}

// GetDailyBattle returns the daily battle record for a player.
// Returns (nil, nil) when no matching row exists.
func (r *PgPlayerRepository) GetDailyBattle(ctx context.Context, playerID string) (*model.PlayerDailyBattle, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT player_id, daily_battle_count, last_reset_date
		 FROM player_daily_battle WHERE player_id = $1`,
		playerID,
	)

	db, err := scanDailyBattle(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get daily battle: %w", err)
	}
	return db, nil
}

// IncrementDailyBattle atomically increments the daily battle count.
// Resets the count if last_reset_date is before today. Returns the new count.
func (r *PgPlayerRepository) IncrementDailyBattle(ctx context.Context, playerID string, today civil.Date) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the row for the duration of the transaction.
	row := tx.QueryRow(ctx,
		`SELECT daily_battle_count, last_reset_date
		 FROM player_daily_battle WHERE player_id = $1 FOR UPDATE`,
		playerID,
	)

	var count int64
	var lastResetTime time.Time
	if err := row.Scan(&count, &lastResetTime); err != nil {
		return 0, fmt.Errorf("read daily battle: %w", err)
	}

	lastReset := timeToCivilDate(lastResetTime)
	if lastReset != today {
		// New day: reset counter.
		count = 1
	} else {
		count++
	}

	todayTime := civilDateToTime(today)
	_, err = tx.Exec(ctx,
		`UPDATE player_daily_battle SET daily_battle_count = $1, last_reset_date = $2 WHERE player_id = $3`,
		count, todayTime, playerID,
	)
	if err != nil {
		return 0, fmt.Errorf("update daily battle: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit tx: %w", err)
	}
	return count, nil
}

func (r *PgPlayerRepository) UpdateUsername(ctx context.Context, playerID string, username string) (*model.Player, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE players SET username = $1, updated_at = NOW()
		 WHERE player_id = $2
		 RETURNING player_id, firebase_uid, username, level, exp, is_premium, equipped_icon_no, selected_faction, premium_expires_at, created_at, updated_at`,
		username, playerID,
	)

	p, err := scanPlayer(row)
	if err != nil {
		return nil, fmt.Errorf("update username: %w", err)
	}
	return p, nil
}

func (r *PgPlayerRepository) UpdatePremium(ctx context.Context, playerID string, isPremium bool, expiresAt *time.Time) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE players SET is_premium = $1, premium_expires_at = $2, updated_at = $3
		 WHERE player_id = $4`,
		isPremium, expiresAt, time.Now(), playerID,
	)
	if err != nil {
		return fmt.Errorf("update player premium: %w", err)
	}
	return nil
}

func (r *PgPlayerRepository) UpdateFaction(ctx context.Context, playerID, faction string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE players SET selected_faction = $1, updated_at = $2
		 WHERE player_id = $3`,
		faction, time.Now(), playerID,
	)
	if err != nil {
		return fmt.Errorf("update player faction: %w", err)
	}
	return nil
}

// scanPlayer scans a single row into a model.Player.
func scanPlayer(row pgx.Row) (*model.Player, error) {
	var p model.Player
	err := row.Scan(
		&p.PlayerID,
		&p.FirebaseUID,
		&p.Username,
		&p.Level,
		&p.Exp,
		&p.IsPremium,
		&p.EquippedIconNo,
		&p.SelectedFaction,
		&p.PremiumExpiresAt,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// scanDailyBattle scans a single row into a model.PlayerDailyBattle.
// PostgreSQL DATE columns are scanned as time.Time, then converted to civil.Date.
func scanDailyBattle(row pgx.Row) (*model.PlayerDailyBattle, error) {
	var db model.PlayerDailyBattle
	var lastResetTime time.Time
	err := row.Scan(
		&db.PlayerID,
		&db.DailyBattleCount,
		&lastResetTime,
	)
	if err != nil {
		return nil, err
	}
	db.LastResetDate = timeToCivilDate(lastResetTime)
	return &db, nil
}

// civilDateToTime converts a civil.Date to a time.Time (midnight UTC).
func civilDateToTime(d civil.Date) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}

// timeToCivilDate converts a time.Time to a civil.Date.
func timeToCivilDate(t time.Time) civil.Date {
	return civil.DateOf(t)
}

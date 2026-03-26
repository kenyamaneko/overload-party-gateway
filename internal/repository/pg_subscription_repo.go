package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// Compile-time interface check.
var _ port.SubscriptionRepo = (*PgSubscriptionRepository)(nil)

// PgSubscriptionRepository implements SubscriptionRepo using PostgreSQL via pgxpool.
type PgSubscriptionRepository struct {
	pool *pgxpool.Pool
}

// NewPgSubscriptionRepository returns a new PgSubscriptionRepository.
func NewPgSubscriptionRepository(pool *pgxpool.Pool) *PgSubscriptionRepository {
	return &PgSubscriptionRepository{pool: pool}
}

func (r *PgSubscriptionRepository) CreateSubscription(ctx context.Context, sub *model.Subscription) error {
	db := connFrom(ctx, r.pool)
	err := db.QueryRow(ctx,
		`INSERT INTO subscriptions (player_id, product_id, platform, purchase_token, status, current_period_start, current_period_end, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING subscription_id`,
		sub.PlayerID, sub.ProductID,
		sub.Platform, sub.PurchaseToken, sub.Status,
		sub.CurrentPeriodStart, sub.CurrentPeriodEnd,
		sub.CreatedAt, sub.UpdatedAt,
	).Scan(&sub.SubscriptionID)
	if err != nil {
		return fmt.Errorf("create subscription: %w", err)
	}
	return nil
}

func (r *PgSubscriptionRepository) GetActiveSubscription(ctx context.Context, playerID string) (*model.Subscription, error) {
	db := connFrom(ctx, r.pool)
	row := db.QueryRow(ctx,
		`SELECT player_id, subscription_id, product_id, platform, purchase_token, status, current_period_start, current_period_end, created_at, updated_at
		 FROM subscriptions
		 WHERE player_id = $1 AND status = $2
		 ORDER BY created_at DESC
		 LIMIT 1`,
		playerID, model.SubscriptionStatusActive)

	s, err := scanSubscription(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query active subscription: %w", err)
	}
	return s, nil
}

func (r *PgSubscriptionRepository) FindSubscriptionByToken(ctx context.Context, purchaseToken string) (*model.Subscription, error) {
	db := connFrom(ctx, r.pool)
	row := db.QueryRow(ctx,
		`SELECT player_id, subscription_id, product_id, platform, purchase_token, status, current_period_start, current_period_end, created_at, updated_at
		 FROM subscriptions
		 WHERE purchase_token = $1
		 LIMIT 1`,
		purchaseToken)

	s, err := scanSubscription(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query subscription by token: %w", err)
	}
	return s, nil
}

// scanSubscription scans a single row into a model.Subscription.
func scanSubscription(row pgx.Row) (*model.Subscription, error) {
	var s model.Subscription
	err := row.Scan(
		&s.PlayerID, &s.SubscriptionID, &s.ProductID,
		&s.Platform, &s.PurchaseToken, &s.Status,
		&s.CurrentPeriodStart, &s.CurrentPeriodEnd,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *PgSubscriptionRepository) UpdateSubscription(ctx context.Context, sub *model.Subscription) error {
	db := connFrom(ctx, r.pool)
	_, err := db.Exec(ctx,
		`UPDATE subscriptions SET
			status = $1,
			current_period_start = $2,
			current_period_end = $3,
			updated_at = $4
		 WHERE player_id = $5 AND subscription_id = $6`,
		sub.Status,
		sub.CurrentPeriodStart, sub.CurrentPeriodEnd,
		sub.UpdatedAt,
		sub.PlayerID, sub.SubscriptionID,
	)
	if err != nil {
		return fmt.Errorf("update subscription: %w", err)
	}
	return nil
}

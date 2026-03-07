package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
)

// Compile-time interface check.
var _ ShopRepository = (*PgShopRepository)(nil)

// PgShopRepository implements ShopRepository using PostgreSQL via pgxpool.
type PgShopRepository struct {
	pool *pgxpool.Pool
}

// NewPgShopRepository returns a new PgShopRepository.
func NewPgShopRepository(pool *pgxpool.Pool) *PgShopRepository {
	return &PgShopRepository{pool: pool}
}

func (r *PgShopRepository) GetActiveProducts(ctx context.Context) ([]*model.Product, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT product_id, name, type, price, content, is_active
		 FROM products WHERE is_active = true`)
	if err != nil {
		return nil, fmt.Errorf("query products: %w", err)
	}
	defer rows.Close()

	var products []*model.Product
	for rows.Next() {
		var p model.Product
		var content []byte
		if err := rows.Scan(&p.ProductID, &p.Name, &p.Type, &p.Price, &content, &p.IsActive); err != nil {
			return nil, fmt.Errorf("scan product: %w", err)
		}
		p.Content = json.RawMessage(content)
		products = append(products, &p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate products: %w", err)
	}
	return products, nil
}

func (r *PgShopRepository) GetProductByID(ctx context.Context, productID string) (*model.Product, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT product_id, name, type, price, content, is_active
		 FROM products WHERE product_id = $1`,
		productID)

	var p model.Product
	var content []byte
	err := row.Scan(&p.ProductID, &p.Name, &p.Type, &p.Price, &content, &p.IsActive)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("read product: %w", err)
	}
	p.Content = json.RawMessage(content)
	return &p, nil
}

func (r *PgShopRepository) FindPurchaseByToken(ctx context.Context, playerID, purchaseToken string) (*model.OneTimePurchase, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT player_id, purchase_id, product_id, platform, purchase_token, purchased_at
		 FROM one_time_purchases
		 WHERE player_id = $1 AND purchase_token = $2
		 LIMIT 1`,
		playerID, purchaseToken)

	var p model.OneTimePurchase
	err := row.Scan(&p.PlayerID, &p.PurchaseID, &p.ProductID, &p.Platform, &p.PurchaseToken, &p.PurchasedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query purchase by token: %w", err)
	}
	return &p, nil
}

func (r *PgShopRepository) CreatePurchaseWithCards(ctx context.Context, purchase *model.OneTimePurchase, cards []*model.PlayerCard) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Idempotency check: if purchase_token already exists, skip.
	var existingID int64
	err = tx.QueryRow(ctx,
		`SELECT purchase_id FROM one_time_purchases
		 WHERE player_id = $1 AND purchase_token = $2 LIMIT 1`,
		purchase.PlayerID, purchase.PurchaseToken,
	).Scan(&existingID)
	if err == nil {
		// Already exists — idempotent success.
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("check existing purchase: %w", err)
	}

	err = tx.QueryRow(ctx,
		`INSERT INTO one_time_purchases (player_id, product_id, platform, purchase_token, purchased_at)
		 VALUES ($1,$2,$3,$4,$5) RETURNING purchase_id`,
		purchase.PlayerID, purchase.ProductID,
		purchase.Platform, purchase.PurchaseToken, purchase.PurchasedAt,
	).Scan(&purchase.PurchaseID)
	if err != nil {
		return fmt.Errorf("insert purchase: %w", err)
	}

	for _, card := range cards {
		_, err = tx.Exec(ctx,
			`INSERT INTO player_cards (player_id, card_no, illustration_variant, count)
			 VALUES ($1,$2,$3,$4)
			 ON CONFLICT (player_id, card_no, illustration_variant)
			 DO UPDATE SET count = player_cards.count + EXCLUDED.count`,
			card.PlayerID, card.CardNo,
			card.IllustrationVariant, card.Count,
		)
		if err != nil {
			return fmt.Errorf("insert player card: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit create purchase with cards: %w", err)
	}
	return nil
}

func (r *PgShopRepository) CreatePurchaseWithItem(ctx context.Context, purchase *model.OneTimePurchase, item *model.PlayerItem) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Idempotency check.
	var existingID int64
	err = tx.QueryRow(ctx,
		`SELECT purchase_id FROM one_time_purchases
		 WHERE player_id = $1 AND purchase_token = $2 LIMIT 1`,
		purchase.PlayerID, purchase.PurchaseToken,
	).Scan(&existingID)
	if err == nil {
		// Already exists — idempotent success.
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("check existing purchase: %w", err)
	}

	err = tx.QueryRow(ctx,
		`INSERT INTO one_time_purchases (player_id, product_id, platform, purchase_token, purchased_at)
		 VALUES ($1,$2,$3,$4,$5) RETURNING purchase_id`,
		purchase.PlayerID, purchase.ProductID,
		purchase.Platform, purchase.PurchaseToken, purchase.PurchasedAt,
	).Scan(&purchase.PurchaseID)
	if err != nil {
		return fmt.Errorf("insert purchase: %w", err)
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO player_items (player_id, item_type, item_no, acquired_at)
		 VALUES ($1,$2,$3,$4)`,
		item.PlayerID, item.ItemType, item.ItemNo, item.AcquiredAt,
	)
	if err != nil {
		return fmt.Errorf("insert player item: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit create purchase with item: %w", err)
	}
	return nil
}

func (r *PgShopRepository) InsertPlayerCards(ctx context.Context, cards []*model.PlayerCard) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, card := range cards {
		_, err = tx.Exec(ctx,
			`INSERT INTO player_cards (player_id, card_no, illustration_variant, count)
			 VALUES ($1,$2,$3,$4)
			 ON CONFLICT (player_id, card_no, illustration_variant)
			 DO UPDATE SET count = player_cards.count + EXCLUDED.count`,
			card.PlayerID, card.CardNo,
			card.IllustrationVariant, card.Count,
		)
		if err != nil {
			return fmt.Errorf("insert player card: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit insert player cards: %w", err)
	}
	return nil
}

func (r *PgShopRepository) InsertPlayerItems(ctx context.Context, items []*model.PlayerItem) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, item := range items {
		_, err = tx.Exec(ctx,
			`INSERT INTO player_items (player_id, item_type, item_no, acquired_at)
			 VALUES ($1,$2,$3,$4)`,
			item.PlayerID, item.ItemType, item.ItemNo, item.AcquiredAt,
		)
		if err != nil {
			return fmt.Errorf("insert player item: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit insert player items: %w", err)
	}
	return nil
}

func (r *PgShopRepository) GetPlayerOwnedFactions(ctx context.Context, playerID string) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT cd.faction
		 FROM player_cards pc
		 JOIN card_definitions cd ON pc.card_no = cd.card_no
		 WHERE pc.player_id = $1 AND cd.faction != 'Neutral'`,
		playerID)
	if err != nil {
		return nil, fmt.Errorf("query factions: %w", err)
	}
	defer rows.Close()

	var factions []string
	for rows.Next() {
		var faction string
		if err := rows.Scan(&faction); err != nil {
			return nil, fmt.Errorf("scan faction: %w", err)
		}
		factions = append(factions, faction)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate factions: %w", err)
	}
	return factions, nil
}

func (r *PgShopRepository) CreateSubscription(ctx context.Context, sub *model.Subscription) error {
	err := r.pool.QueryRow(ctx,
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

func (r *PgShopRepository) GetActiveSubscription(ctx context.Context, playerID string) (*model.Subscription, error) {
	row := r.pool.QueryRow(ctx,
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

func (r *PgShopRepository) FindSubscriptionByToken(ctx context.Context, purchaseToken string) (*model.Subscription, error) {
	row := r.pool.QueryRow(ctx,
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

func (r *PgShopRepository) UpdateSubscription(ctx context.Context, sub *model.Subscription) error {
	_, err := r.pool.Exec(ctx,
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

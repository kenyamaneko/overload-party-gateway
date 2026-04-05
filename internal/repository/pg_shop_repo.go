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
var _ port.ShopRepository = (*PgShopRepository)(nil)

// PgShopRepository implements ShopRepository using PostgreSQL via pgxpool.
type PgShopRepository struct {
	pool *pgxpool.Pool
}

// NewPgShopRepository returns a new PgShopRepository.
func NewPgShopRepository(pool *pgxpool.Pool) *PgShopRepository {
	return &PgShopRepository{pool: pool}
}

func (r *PgShopRepository) GetActiveProducts(ctx context.Context) ([]*model.Product, error) {
	rows, err := connFrom(ctx, r.pool).Query(ctx,
		`SELECT product_id, name, type, price, content, description, image_url, is_active
		 FROM products WHERE is_active = true`)
	if err != nil {
		return nil, fmt.Errorf("query products: %w", err)
	}
	defer rows.Close()

	var products []*model.Product
	for rows.Next() {
		var p model.Product
		var content []byte
		if err := rows.Scan(&p.ProductID, &p.Name, &p.Type, &p.Price, &content, &p.Description, &p.ImageURL, &p.IsActive); err != nil {
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
	row := connFrom(ctx, r.pool).QueryRow(ctx,
		`SELECT product_id, name, type, price, content, description, image_url, is_active
		 FROM products WHERE product_id = $1`,
		productID)

	var p model.Product
	var content []byte
	err := row.Scan(&p.ProductID, &p.Name, &p.Type, &p.Price, &content, &p.Description, &p.ImageURL, &p.IsActive)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("product %s: %w", productID, port.ErrNotFound)
		}
		return nil, fmt.Errorf("read product: %w", err)
	}
	p.Content = json.RawMessage(content)
	return &p, nil
}

func (r *PgShopRepository) FindPurchaseByToken(ctx context.Context, playerID, purchaseToken string) (*model.OneTimePurchase, error) {
	row := connFrom(ctx, r.pool).QueryRow(ctx,
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
	if txFromContext(ctx) != nil {
		return r.createPurchaseWithCardsInner(ctx, connFrom(ctx, r.pool), purchase, cards)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := r.createPurchaseWithCardsInner(ctx, tx, purchase, cards); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PgShopRepository) createPurchaseWithCardsInner(ctx context.Context, db dbtx, purchase *model.OneTimePurchase, cards []*model.PlayerCard) error {
	// Idempotency check: if purchase_token already exists, skip.
	var existingID int64
	err := db.QueryRow(ctx,
		`SELECT purchase_id FROM one_time_purchases
		 WHERE player_id = $1 AND purchase_token = $2 LIMIT 1`,
		purchase.PlayerID, purchase.PurchaseToken,
	).Scan(&existingID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("check existing purchase: %w", err)
	}

	err = db.QueryRow(ctx,
		`INSERT INTO one_time_purchases (player_id, product_id, platform, purchase_token, purchased_at)
		 VALUES ($1,$2,$3,$4,$5) RETURNING purchase_id`,
		purchase.PlayerID, purchase.ProductID,
		purchase.Platform, purchase.PurchaseToken, purchase.PurchasedAt,
	).Scan(&purchase.PurchaseID)
	if err != nil {
		return fmt.Errorf("insert purchase: %w", err)
	}

	for _, card := range cards {
		_, err = db.Exec(ctx,
			`INSERT INTO player_cards (player_id, card_id, art_no, count)
			 VALUES ($1,$2,$3,$4)
			 ON CONFLICT (player_id, card_id, art_no)
			 DO UPDATE SET count = player_cards.count + EXCLUDED.count`,
			card.PlayerID, card.CardID,
			card.ArtNo, card.Count,
		)
		if err != nil {
			return fmt.Errorf("insert player card: %w", err)
		}
	}
	return nil
}

func (r *PgShopRepository) CreatePurchaseWithItem(ctx context.Context, purchase *model.OneTimePurchase, item *model.PlayerItem) error {
	if txFromContext(ctx) != nil {
		return r.createPurchaseWithItemInner(ctx, connFrom(ctx, r.pool), purchase, item)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := r.createPurchaseWithItemInner(ctx, tx, purchase, item); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PgShopRepository) createPurchaseWithItemInner(ctx context.Context, db dbtx, purchase *model.OneTimePurchase, item *model.PlayerItem) error {
	// Idempotency check.
	var existingID int64
	err := db.QueryRow(ctx,
		`SELECT purchase_id FROM one_time_purchases
		 WHERE player_id = $1 AND purchase_token = $2 LIMIT 1`,
		purchase.PlayerID, purchase.PurchaseToken,
	).Scan(&existingID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("check existing purchase: %w", err)
	}

	err = db.QueryRow(ctx,
		`INSERT INTO one_time_purchases (player_id, product_id, platform, purchase_token, purchased_at)
		 VALUES ($1,$2,$3,$4,$5) RETURNING purchase_id`,
		purchase.PlayerID, purchase.ProductID,
		purchase.Platform, purchase.PurchaseToken, purchase.PurchasedAt,
	).Scan(&purchase.PurchaseID)
	if err != nil {
		return fmt.Errorf("insert purchase: %w", err)
	}

	_, err = db.Exec(ctx,
		`INSERT INTO player_items (player_id, item_type, item_no, acquired_at)
		 VALUES ($1,$2,$3,$4)`,
		item.PlayerID, item.ItemType, item.ItemNo, item.AcquiredAt,
	)
	if err != nil {
		return fmt.Errorf("insert player item: %w", err)
	}
	return nil
}

func (r *PgShopRepository) InsertPlayerCards(ctx context.Context, cards []*model.PlayerCard) error {
	if txFromContext(ctx) != nil {
		return r.insertPlayerCardsInner(ctx, connFrom(ctx, r.pool), cards)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := r.insertPlayerCardsInner(ctx, tx, cards); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PgShopRepository) insertPlayerCardsInner(ctx context.Context, db dbtx, cards []*model.PlayerCard) error {
	for _, card := range cards {
		_, err := db.Exec(ctx,
			`INSERT INTO player_cards (player_id, card_id, art_no, count)
			 VALUES ($1,$2,$3,$4)
			 ON CONFLICT (player_id, card_id, art_no)
			 DO UPDATE SET count = player_cards.count + EXCLUDED.count`,
			card.PlayerID, card.CardID,
			card.ArtNo, card.Count,
		)
		if err != nil {
			return fmt.Errorf("insert player card: %w", err)
		}
	}
	return nil
}

// InsertPlayerItems inserts player items atomically.
// If a transaction is present in the context, it participates in that transaction.
// Otherwise it wraps the inserts in its own transaction.
func (r *PgShopRepository) InsertPlayerItems(ctx context.Context, items []*model.PlayerItem) error {
	if txFromContext(ctx) != nil {
		return r.insertPlayerItemsInner(ctx, connFrom(ctx, r.pool), items)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := r.insertPlayerItemsInner(ctx, tx, items); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PgShopRepository) insertPlayerItemsInner(ctx context.Context, db dbtx, items []*model.PlayerItem) error {
	for _, item := range items {
		_, err := db.Exec(ctx,
			`INSERT INTO player_items (player_id, item_type, item_no, acquired_at)
			 VALUES ($1,$2,$3,$4)`,
			item.PlayerID, item.ItemType, item.ItemNo, item.AcquiredAt,
		)
		if err != nil {
			return fmt.Errorf("insert player item: %w", err)
		}
	}
	return nil
}

func (r *PgShopRepository) HasPlayerItem(ctx context.Context, playerID, itemType string, itemNo int64) (bool, error) {
	var exists bool
	err := connFrom(ctx, r.pool).QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM player_items
			WHERE player_id = $1 AND item_type = $2 AND item_no = $3
		)`,
		playerID, itemType, itemNo,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check player item: %w", err)
	}
	return exists, nil
}


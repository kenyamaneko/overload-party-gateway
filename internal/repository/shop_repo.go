package repository

import (
	"context"
	"time"

	"github.com/kenyamaneko/overload-party-common/model"
)

// ShopRepository defines the data access contract for the shop system.
type ShopRepository interface {
	// GetActiveProducts returns all products where is_active = true.
	GetActiveProducts(ctx context.Context) ([]*model.Product, error)

	// GetProductByID returns a single product by its ID.
	GetProductByID(ctx context.Context, productID string) (*model.Product, error)

	// FindPurchaseByToken checks if a purchase with the given token already exists.
	FindPurchaseByToken(ctx context.Context, playerID, purchaseToken string) (*model.OneTimePurchase, error)

	// CreatePurchaseWithCards atomically inserts a purchase record and player cards.
	CreatePurchaseWithCards(ctx context.Context, purchase *model.OneTimePurchase, cards []*model.PlayerCard) error

	// CreatePurchaseWithItem atomically inserts a purchase record and a player item.
	CreatePurchaseWithItem(ctx context.Context, purchase *model.OneTimePurchase, item *model.PlayerItem) error

	// InsertPlayerCards inserts player cards in bulk (used for initial faction selection).
	InsertPlayerCards(ctx context.Context, cards []*model.PlayerCard) error

	// InsertPlayerItems inserts player items in bulk (used for initial stamp grant).
	InsertPlayerItems(ctx context.Context, items []*model.PlayerItem) error

	// GetPlayerOwnedFactions returns the distinct factions the player owns cards from.
	GetPlayerOwnedFactions(ctx context.Context, playerID string) ([]string, error)

	// CreateSubscription inserts a new subscription record.
	CreateSubscription(ctx context.Context, sub *model.Subscription) error

	// GetActiveSubscription returns the player's active subscription, if any.
	GetActiveSubscription(ctx context.Context, playerID string) (*model.Subscription, error)

	// FindSubscriptionByToken looks up a subscription by its purchase token.
	FindSubscriptionByToken(ctx context.Context, purchaseToken string) (*model.Subscription, error)

	// UpdateSubscription updates an existing subscription record.
	UpdateSubscription(ctx context.Context, sub *model.Subscription) error

	// UpdatePlayerPremium updates a player's premium status and expiry.
	UpdatePlayerPremium(ctx context.Context, playerID string, isPremium bool, expiresAt *time.Time) error

	// UpdatePlayerFaction sets a player's selected_faction field.
	UpdatePlayerFaction(ctx context.Context, playerID, faction string) error
}

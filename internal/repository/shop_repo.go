package repository

import (
	"context"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
)

type ShopRepository interface {
	GetActiveProducts(ctx context.Context) ([]*model.Product, error)
	GetProductByID(ctx context.Context, productID string) (*model.Product, error)
	FindPurchaseByToken(ctx context.Context, playerID, purchaseToken string) (*model.OneTimePurchase, error)
	CreatePurchaseWithCards(ctx context.Context, purchase *model.OneTimePurchase, cards []*model.PlayerCard) error
	CreatePurchaseWithItem(ctx context.Context, purchase *model.OneTimePurchase, item *model.PlayerItem) error
	InsertPlayerCards(ctx context.Context, cards []*model.PlayerCard) error
	InsertPlayerItems(ctx context.Context, items []*model.PlayerItem) error
	GetPlayerOwnedFactions(ctx context.Context, playerID string) ([]string, error)
	CreateSubscription(ctx context.Context, sub *model.Subscription) error
	GetActiveSubscription(ctx context.Context, playerID string) (*model.Subscription, error)
	FindSubscriptionByToken(ctx context.Context, purchaseToken string) (*model.Subscription, error)
	UpdateSubscription(ctx context.Context, sub *model.Subscription) error
}

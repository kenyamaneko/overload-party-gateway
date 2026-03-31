package port

import (
	"context"
	"time"

	"cloud.google.com/go/civil"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
)

// PlayerRepo defines the data access contract for player operations.
type PlayerRepo interface {
	Create(ctx context.Context, player *model.Player, dailyBattle *model.PlayerDailyBattle) error
	FindByID(ctx context.Context, playerID string) (*model.Player, error)
	FindByFirebaseUID(ctx context.Context, firebaseUID string) (*model.Player, error)
	GetDailyBattle(ctx context.Context, playerID string) (*model.PlayerDailyBattle, error)
	IncrementDailyBattle(ctx context.Context, playerID string, today civil.Date) (int64, error)
	UpdateUsername(ctx context.Context, playerID string, username string) (*model.Player, error)
	UpdatePremium(ctx context.Context, playerID string, isPremium bool, expiresAt *time.Time) error
	UpdateFaction(ctx context.Context, playerID, faction string) error
	AddExp(ctx context.Context, playerID string, expGain, coeff int64) (*model.Player, error)
}

// DeckRepo defines the data access contract for deck operations.
type DeckRepo interface {
	Create(ctx context.Context, deck *model.Deck, cards []model.DeckCard) error
	FindByPlayerID(ctx context.Context, playerID string) ([]*model.Deck, error)
	FindByID(ctx context.Context, playerID string, deckID int64) (*model.Deck, error)
	GetDeckCards(ctx context.Context, playerID string, deckID int64) ([]model.DeckCard, error)
	Update(ctx context.Context, deck *model.Deck, cards []model.DeckCard) error
	Delete(ctx context.Context, playerID string, deckID int64) error
}

// PlayerCardRepo defines the data access contract for player card inventory.
type PlayerCardRepo interface {
	GetPlayerCards(ctx context.Context, playerID string) ([]*model.PlayerCard, error)
}

// CardRepo defines the data access contract for card definitions.
type CardRepo interface {
	FindAll(ctx context.Context) ([]*model.CardDefinition, error)
	FindByCardID(ctx context.Context, cardID string) (*model.CardDefinition, error)
}

// UserSettingsRepo defines the data access contract for user settings.
type UserSettingsRepo interface {
	Get(ctx context.Context, playerID string) (*model.UserSettings, error)
	Upsert(ctx context.Context, s *model.UserSettings) error
}

// GameConfigRepo defines the data access contract for server-side game configuration.
type GameConfigRepo interface {
	GetInt64(ctx context.Context, key string, fallback int64) (int64, error)
}

// NewsRepo defines the data access contract for cloud news articles.
type NewsRepo interface {
	List(ctx context.Context, limit int, offset int) ([]*model.NewsArticle, error)
}

// FactionRepo defines the data access contract for player faction ownership.
type FactionRepo interface {
	AddPlayerFaction(ctx context.Context, playerID, faction, source string) error
	GetPlayerFactions(ctx context.Context, playerID string) ([]string, error)
}

// ShopRepository defines the data access contract for products, purchases, and player inventory.
type ShopRepository interface {
	GetActiveProducts(ctx context.Context) ([]*model.Product, error)
	GetProductByID(ctx context.Context, productID string) (*model.Product, error)
	FindPurchaseByToken(ctx context.Context, playerID, purchaseToken string) (*model.OneTimePurchase, error)
	CreatePurchaseWithCards(ctx context.Context, purchase *model.OneTimePurchase, cards []*model.PlayerCard) error
	CreatePurchaseWithItem(ctx context.Context, purchase *model.OneTimePurchase, item *model.PlayerItem) error
	InsertPlayerCards(ctx context.Context, cards []*model.PlayerCard) error
	InsertPlayerItems(ctx context.Context, items []*model.PlayerItem) error
	GetPlayerOwnedFactions(ctx context.Context, playerID string) ([]string, error)
}

// SubscriptionRepo defines the data access contract for subscription lifecycle.
type SubscriptionRepo interface {
	CreateSubscription(ctx context.Context, sub *model.Subscription) error
	GetActiveSubscription(ctx context.Context, playerID string) (*model.Subscription, error)
	FindSubscriptionByToken(ctx context.Context, purchaseToken string) (*model.Subscription, error)
	UpdateSubscription(ctx context.Context, sub *model.Subscription) error
}

// StoryRepo defines the data access contract for story scenario operations.
type StoryRepo interface {
	ListActiveEpisodes(ctx context.Context) ([]*model.ScenarioEpisode, error)
	FindEpisodeByID(ctx context.Context, episodeID string) (*model.ScenarioEpisode, error)
	GetCompletedEpisodeIDs(ctx context.Context, playerID string) ([]string, error)
	GetUnlockContext(ctx context.Context, playerID string) (*model.StoryUnlockContext, error)
	MarkComplete(ctx context.Context, playerID, episodeID string) error
}

// TxRunner provides service-level transaction control.
// The callback receives a context carrying the transaction;
// repository implementations extract it transparently.
type TxRunner interface {
	RunInTx(ctx context.Context, fn func(ctx context.Context) error) error
}

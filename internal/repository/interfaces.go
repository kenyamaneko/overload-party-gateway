package repository

import (
	"context"
	"time"

	"cloud.google.com/go/civil"

	"github.com/kenyamaneko/overload-party-gateway/internal/model"
)

// PlayerRepo defines the data access contract for player operations.
type PlayerRepo interface {
	Create(ctx context.Context, player *model.Player, dailyBattle *model.PlayerDailyBattle) error
	CreateWithTx(ctx context.Context, db DBTX, player *model.Player, dailyBattle *model.PlayerDailyBattle) error
	FindByID(ctx context.Context, playerID string) (*model.Player, error)
	FindByFirebaseUID(ctx context.Context, firebaseUID string) (*model.Player, error)
	GetDailyBattle(ctx context.Context, playerID string) (*model.PlayerDailyBattle, error)
	IncrementDailyBattle(ctx context.Context, playerID string, today civil.Date) (int64, error)
	UpdateUsername(ctx context.Context, playerID string, username string) (*model.Player, error)
	UpdatePremium(ctx context.Context, playerID string, isPremium bool, expiresAt *time.Time) error
	UpdateFaction(ctx context.Context, playerID, faction string) error
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
	UpsertWithTx(ctx context.Context, db DBTX, s *model.UserSettings) error
}

// GameConfigRepo defines the data access contract for server-side game configuration.
type GameConfigRepo interface {
	GetInt64(ctx context.Context, key string, fallback int64) (int64, error)
}

// NewsRepo defines the data access contract for cloud news articles.
type NewsRepo interface {
	List(ctx context.Context, limit int, offset int) ([]*model.NewsArticle, error)
}


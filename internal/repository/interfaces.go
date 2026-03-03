package repository

import (
	"context"

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
}

// DeckRepo defines the data access contract for deck and player card operations.
type DeckRepo interface {
	Create(ctx context.Context, deck *model.Deck, cards []model.DeckCard) error
	FindByPlayerID(ctx context.Context, playerID string) ([]*model.Deck, error)
	FindByID(ctx context.Context, playerID string, deckID int64) (*model.Deck, error)
	GetDeckCards(ctx context.Context, playerID string, deckID int64) ([]model.DeckCard, error)
	GetDeckCardNos(ctx context.Context, playerID string, deckID int64) ([]int64, error)
	GetPlayerCards(ctx context.Context, playerID string) ([]*model.PlayerCard, error)
	Update(ctx context.Context, deck *model.Deck, cards []model.DeckCard) error
	Delete(ctx context.Context, playerID string, deckID int64) error
}

// CardRepo defines the data access contract for card definitions.
type CardRepo interface {
	FindAll(ctx context.Context) ([]*model.CardDefinition, error)
	FindByCardNo(ctx context.Context, cardNo int64) (*model.CardDefinition, error)
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

// Compile-time interface checks for PostgreSQL implementations.
var _ PlayerRepo = (*PgPlayerRepository)(nil)
var _ DeckRepo = (*PgDeckRepository)(nil)
var _ CardRepo = (*PgCardRepository)(nil)
var _ UserSettingsRepo = (*PgUserSettingsRepository)(nil)
var _ GameConfigRepo = (*PgGameConfigRepository)(nil)

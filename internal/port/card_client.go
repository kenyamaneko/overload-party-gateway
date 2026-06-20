package port

import (
	"context"

	apicard "github.com/kenyamaneko/overload-party-card/packages/api-card"
)

// DeckInitiatives はデッキがセットしたルーチン/スペシャル施策の ID を保持します。
type DeckInitiatives struct {
	RoutineID string
	SpecialID string
}

// CardClient は gateway が card サービスへアクセスするための port。
type CardClient interface {
	ListAllCards(ctx context.Context) ([]*apicard.CardDefinition, error)
	GetDeckCards(ctx context.Context, deckID int64) ([]apicard.DeckCard, DeckInitiatives, error)
	ValidateDeckForBattle(ctx context.Context, deckID int64) error
}

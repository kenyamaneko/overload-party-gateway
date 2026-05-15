package port

import (
	"context"

	apicard "github.com/kenyamaneko/overload-party-card/packages/api-card"
)

// CardClient は gateway が card サービスへアクセスするための port。
type CardClient interface {
	ListAllCards(ctx context.Context) ([]*apicard.CardDefinition, error)
	GetDeckCards(ctx context.Context, deckID int64) ([]apicard.DeckCard, error)
	ValidateDeckForBattle(ctx context.Context, deckID int64) error
}

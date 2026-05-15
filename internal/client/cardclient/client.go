// Package cardclient は card サービスへの HTTP クライアントを提供する。
// gateway 内部で必要な 3 endpoint (ListAllCards / ValidateDeckForBattle /
// GetDeckCards) のみを公開し、内部は apicardclient SDK に委譲する。
package cardclient

import (
	"context"
	"fmt"
	"net/http"

	apicard "github.com/kenyamaneko/overload-party-card/packages/api-card"
	"github.com/kenyamaneko/overload-party-card/packages/api-card/apicardclient"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
)

// Client は card サービスへの HTTP クライアント。apicardclient SDK の薄ラッパで、
// X-Internal-Auth header 注入と gateway 内部利用 method の絞り込みを担う。
type Client struct {
	api *apicardclient.Client
}

// New は card サービスクライアントを生成する。baseURL の解析失敗は実行不可なので panic する。
func New(baseURL string) *Client {
	api, err := apicardclient.New(baseURL,
		apicardclient.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			internalauth.InjectHeader(ctx, req.Header)
			return nil
		}),
	)
	if err != nil {
		panic(fmt.Sprintf("cardclient: %v", err))
	}
	return &Client{api: api}
}

// ListAllCards は test 用途で internalauth 注入の経路検証に使う。
func (c *Client) ListAllCards(ctx context.Context) ([]*apicard.CardDefinition, error) {
	cards, err := c.api.ListCards(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*apicard.CardDefinition, len(cards))
	for i := range cards {
		out[i] = &cards[i]
	}
	return out, nil
}

// GetDeckCards はマッチ成立時や NPC バトル開始時に gateway がデッキを resolve するために使う。
func (c *Client) GetDeckCards(ctx context.Context, deckID int64) ([]apicard.DeckCard, error) {
	_, cards, err := c.api.GetDeck(ctx, deckID)
	if err != nil {
		return nil, err
	}
	return cards, nil
}

// ValidateDeckForBattle は ws/manager の matchmaking_start / npc_battle 受付時の前段検査に使う。
func (c *Client) ValidateDeckForBattle(ctx context.Context, deckID int64) error {
	return c.api.ValidateDeckForBattle(ctx, deckID)
}

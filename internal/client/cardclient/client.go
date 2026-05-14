// Package cardclient は card サービスへの HTTP クライアントを提供する。
// gateway 内部で必要な 3 endpoint (ListAllCards / ValidateDeckForBattle /
// GetDeckCards) のみを公開し、内部は apicardclient SDK に委譲する。
//
// gateway を薄く保つ方針で client (TS) が card サービスへ直接 import する形に
// 切替わったため、gateway がプロキシしていた deck CRUD 系 method
// (ListPlayerCards / ListCardsWithOwnership / ListDecks / GetDeck (full) /
// CreateDeck / UpdateDeck / DeleteDeck) は production caller を失い、本パッケージから
// 削除した。
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

// ListAllCards は全カード定義を取得する。test 用途。
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

// GetDeckCards はバトル用デッキ構成を取得する。
// マッチ成立時や NPC バトル開始時に gateway がデッキを resolve するために使用する。
func (c *Client) GetDeckCards(ctx context.Context, deckID int64) ([]apicard.DeckCard, error) {
	_, cards, err := c.api.GetDeck(ctx, deckID)
	if err != nil {
		return nil, err
	}
	return cards, nil
}

// ValidateDeckForBattle はデッキがバトル使用可能か検証する。
// SDK が 400 を ErrDeckInvalid に変換して返す (sentinel は SDK 側で持つ)。
func (c *Client) ValidateDeckForBattle(ctx context.Context, deckID int64) error {
	return c.api.ValidateDeckForBattle(ctx, deckID)
}

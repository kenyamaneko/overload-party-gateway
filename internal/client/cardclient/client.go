// Package cardclient は card マイクロサービスへの HTTP クライアントを提供します。
// gateway がカードデータにアクセスする唯一の手段であり、ローカル DB へのフォールバックは行わない。
package cardclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	apicard "github.com/kenyamaneko/overload-party-card/packages/api-card"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
)

// ErrNotFound は card サービスが 404 を返した場合のエラーです
var ErrNotFound = errors.New("card service: not found")

// ErrDeckInvalid は validate-for-battle がデッキを拒否した場合のエラーです
var ErrDeckInvalid = errors.New("card service: deck invalid")

// Client は card サービスへの HTTP クライアントです
type Client struct {
	baseURL string
	http    *http.Client
}

// New は card サービスクライアントを生成します
func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// CardWithOwnership はカード定義に所有状態を付加したレスポンス型です
type CardWithOwnership struct {
	*apicard.CardDefinition
	IsOwned bool `json:"is_owned"`
}

// ListAllCards は全カード定義を取得します
func (c *Client) ListAllCards(ctx context.Context) ([]*apicard.CardDefinition, error) {
	var out []*apicard.CardDefinition
	if err := c.getJSON(ctx, "/internal/v1/cards", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListCardsWithOwnership はプレイヤーの所有状態付きカード一覧を取得します
func (c *Client) ListCardsWithOwnership(ctx context.Context) ([]*CardWithOwnership, error) {
	var out []*CardWithOwnership
	if err := c.getJSON(ctx, "/api/v1/cards/cards/with-ownership", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListPlayerCards はプレイヤーの所持カード一覧を取得します
func (c *Client) ListPlayerCards(ctx context.Context) ([]*apicard.PlayerCardWithDef, error) {
	var out []*apicard.PlayerCardWithDef
	if err := c.getJSON(ctx, "/api/v1/cards/cards", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetDeckCards はバトル用デッキ構成を取得します。
// マッチ成立時や NPC バトル開始時にデッキを解決するために使用する。
func (c *Client) GetDeckCards(ctx context.Context, deckID int64) ([]apicard.DeckCard, error) {
	_, cards, err := c.GetDeck(ctx, deckID)
	if err != nil {
		return nil, err
	}
	return cards, nil
}

// ListDecks はプレイヤーのデッキ一覧を取得します
func (c *Client) ListDecks(ctx context.Context) ([]*apicard.Deck, error) {
	var out []*apicard.Deck
	if err := c.getJSON(ctx, "/api/v1/cards/decks", &out); err != nil {
		return nil, err
	}
	return out, nil
}

type deckWithCards struct {
	Deck  *apicard.Deck      `json:"deck"`
	Cards []apicard.DeckCard `json:"cards"`
}

// GetDeck はデッキとカード構成を取得します
func (c *Client) GetDeck(ctx context.Context, deckID int64) (*apicard.Deck, []apicard.DeckCard, error) {
	var out deckWithCards
	path := "/api/v1/cards/decks/" + strconv.FormatInt(deckID, 10)
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, nil, err
	}
	return out.Deck, out.Cards, nil
}

// CreateDeck はデッキを新規作成します
func (c *Client) CreateDeck(ctx context.Context, req apicard.DeckCreateRequest) (*apicard.Deck, error) {
	var out apicard.Deck
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/cards/decks", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateDeck はデッキを更新します
func (c *Client) UpdateDeck(ctx context.Context, deckID int64, req apicard.DeckUpdateRequest) (*apicard.Deck, error) {
	var out apicard.Deck
	path := "/api/v1/cards/decks/" + strconv.FormatInt(deckID, 10)
	if err := c.doJSON(ctx, http.MethodPut, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteDeck はデッキを削除します
func (c *Client) DeleteDeck(ctx context.Context, deckID int64) error {
	path := "/api/v1/cards/decks/" + strconv.FormatInt(deckID, 10)
	return c.doJSON(ctx, http.MethodDelete, path, nil, nil)
}

// ValidateDeckForBattle はデッキがバトルに使用可能か検証します。
// 200 で nil、400 で ErrDeckInvalid（サービスのエラーメッセージ付き）を返す。
func (c *Client) ValidateDeckForBattle(ctx context.Context, deckID int64) error {
	path := "/api/v1/cards/decks/" + strconv.FormatInt(deckID, 10) + "/validate-for-battle"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("cardclient: build request: %w", err)
	}
	internalauth.InjectHeader(ctx, req.Header)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cardclient: do: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusBadRequest {
		return fmt.Errorf("%w: %s", ErrDeckInvalid, extractErrorMessage(body))
	}
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	return fmt.Errorf("cardclient: validate-for-battle: status %d body %s", resp.StatusCode, string(body))
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	return c.doJSON(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("cardclient: marshal body: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("cardclient: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	internalauth.InjectHeader(ctx, req.Header)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cardclient: do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cardclient: %s %s: status %d: %s", method, path, resp.StatusCode, string(raw))
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("cardclient: decode response: %w", err)
	}
	return nil
}

func extractErrorMessage(body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err == nil && e.Error != "" {
		return e.Error
	}
	return string(body)
}

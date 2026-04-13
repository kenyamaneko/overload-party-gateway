// Package shopclient は shop マイクロサービスへの HTTP クライアントを提供します。
// shop は商品、購入、サブスクリプション、ファクションセット付与を管理する。
package shopclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	apishop "github.com/kenyamaneko/overload-party-shop/packages/api-shop"
)

var (
	// ErrNotFound は shop サービスが 404 を返した場合のエラーです
	ErrNotFound = errors.New("shopclient: not found")
	// ErrAlreadyOwned は商品が既に所持されている場合のエラーです
	ErrAlreadyOwned = errors.New("shopclient: already owned")
	// ErrFactionAlreadySelected はファクションが既に選択済みの場合のエラーです
	ErrFactionAlreadySelected = errors.New("shopclient: faction already selected")
	// ErrInvalidFaction は無効なファクションが指定された場合のエラーです
	ErrInvalidFaction = errors.New("shopclient: invalid faction")
	// ErrReceiptInvalid はレシート検証に失敗した場合のエラーです
	ErrReceiptInvalid = errors.New("shopclient: receipt verification failed")
)

// Client は shop サービスへの HTTP クライアントです
type Client struct {
	baseURL string
	http    *http.Client
}

// New は shop サービスクライアントを生成します
func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// SelectFactionRequest はファクション選択リクエストの型です
type SelectFactionRequest struct {
	Faction string `json:"faction"`
}

// SelectFactionResponse はファクション選択レスポンスの型です
type SelectFactionResponse struct {
	Message      string `json:"message"`
	Faction      string `json:"faction"`
	CardsGranted int    `json:"cards_granted"`
}

// SelectFaction はファクション選択を処理します
func (c *Client) SelectFaction(ctx context.Context, playerID, faction string) (*SelectFactionResponse, error) {
	path := fmt.Sprintf("/internal/v1/players/%s/select-faction", url.PathEscape(playerID))
	var out SelectFactionResponse
	if err := c.doJSON(ctx, http.MethodPost, path, SelectFactionRequest{Faction: faction}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type productsResponse struct {
	Products []apishop.ProductResponse `json:"products"`
}

// GetProducts は商品一覧を取得します
func (c *Client) GetProducts(ctx context.Context, playerID string) ([]apishop.ProductResponse, error) {
	path := fmt.Sprintf("/internal/v1/players/%s/products", url.PathEscape(playerID))
	var out productsResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Products, nil
}

// Purchase は商品購入を処理します
func (c *Client) Purchase(ctx context.Context, playerID string, req apishop.PurchaseRequest) error {
	path := fmt.Sprintf("/internal/v1/players/%s/purchase", url.PathEscape(playerID))
	return c.doJSON(ctx, http.MethodPost, path, req, nil)
}

type subscribeResponse struct {
	Message   string     `json:"message"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// Subscribe はサブスクリプション登録を処理します
func (c *Client) Subscribe(ctx context.Context, playerID string, req apishop.PurchaseRequest) (*time.Time, error) {
	path := fmt.Sprintf("/internal/v1/players/%s/subscribe", url.PathEscape(playerID))
	var out subscribeResponse
	if err := c.doJSON(ctx, http.MethodPost, path, req, &out); err != nil {
		return nil, err
	}
	return out.ExpiresAt, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("shopclient: marshal: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("shopclient: new request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("shopclient: do: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		if out == nil {
			return nil
		}
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("shopclient: decode: %w", err)
		}
		return nil
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusConflict:
		raw, _ := io.ReadAll(resp.Body)
		msg := extractErrorMessage(raw)
		if bytes.Contains(raw, []byte("already owned")) {
			return fmt.Errorf("%w: %s", ErrAlreadyOwned, msg)
		}
		return fmt.Errorf("%w: %s", ErrFactionAlreadySelected, msg)
	case http.StatusBadRequest:
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: %s", ErrInvalidFaction, extractErrorMessage(raw))
	case http.StatusPaymentRequired:
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: %s", ErrReceiptInvalid, extractErrorMessage(raw))
	}
	raw, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("shopclient: %s %s: status %d: %s", method, path, resp.StatusCode, string(raw))
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

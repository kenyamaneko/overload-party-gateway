// Package matchmakingclient は matchmaking マイクロサービスへの HTTP クライアントを提供します。
// gateway はこのクライアント経由でプレイヤーの enqueue / cancel を行い、
// マッチ結果は Pub/Sub で非同期に受信する。
package matchmakingclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

var (
	// ErrNotFound は matchmaking サービスが 404 を返した場合のエラーです
	ErrNotFound = errors.New("matchmakingclient: not found")
	// ErrUnavailable は matchmaking サービスが 503 を返した場合のエラーです
	ErrUnavailable = errors.New("matchmakingclient: service unavailable")
)

// Client は matchmaking サービスへの HTTP クライアントです
type Client struct {
	baseURL string
	http    *http.Client
}

// New は matchmaking サービス��ライアントを生成します
func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 5 * time.Second},
	}
}

type enqueueBody struct {
	PlayerID string `json:"playerId"`
	DeckID   int64  `json:"deckId"`
}

type cancelBody struct {
	PlayerID string `json:"playerId"`
}

// Enqueue はプレイヤーをマッチメイキングキューに追加します
func (c *Client) Enqueue(ctx context.Context, playerID string, deckID int64) error {
	return c.post(ctx, "/internal/v1/enqueue", enqueueBody{PlayerID: playerID, DeckID: deckID})
}

// Cancel はプレイヤーをキューから除去します。
// 除去済みまたは未キュー時は nil を返し、通信エラーまたは 5xx の場合のみ non-nil を返す。
func (c *Client) Cancel(ctx context.Context, playerID string) error {
	err := c.post(ctx, "/internal/v1/cancel", cancelBody{PlayerID: playerID})
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func (c *Client) post(ctx context.Context, path string, body any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("matchmakingclient: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("matchmakingclient: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("matchmakingclient: do: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted:
		return nil
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode == http.StatusServiceUnavailable:
		return ErrUnavailable
	}
	raw, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("matchmakingclient: %s: status %d: %s", path, resp.StatusCode, string(raw))
}

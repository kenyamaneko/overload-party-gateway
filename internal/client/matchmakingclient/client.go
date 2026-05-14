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

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
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

// New は matchmaking サービスクライアントを生成します
func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 5 * time.Second},
	}
}

type enqueueBody struct {
	DeckID int64  `json:"deck_id"`
	Name   string `json:"name"`
	Level  int64  `json:"level"`
}

// Enqueue はプレイヤーをマッチメイキングキューに追加します。
// player_id は X-Internal-Auth JWT の sub から matchmaking 側で解決される。
func (c *Client) Enqueue(ctx context.Context, deckID int64, name string, level int64) error {
	return c.post(ctx, "/internal/v1/enqueue", enqueueBody{DeckID: deckID, Name: name, Level: level})
}

// Cancel はプレイヤーをキューから除去します。
// 除去済みまたは未キュー時は nil を返し、通信エラーまたは 5xx の場合のみ non-nil を返す。
// player_id は X-Internal-Auth JWT の sub から matchmaking 側で解決される。
func (c *Client) Cancel(ctx context.Context) error {
	err := c.post(ctx, "/internal/v1/cancel", nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

func (c *Client) post(ctx context.Context, path string, body any) error {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("matchmakingclient: marshal: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("matchmakingclient: new request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	internalauth.InjectHeader(ctx, req.Header)
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

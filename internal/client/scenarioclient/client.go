// Package scenarioclient は scenario マイクロサービスへの HTTP クライアントを提供します。
// scenario サービスはストーリーエピソード、進行状況、スクリプト配信を管理する。
package scenarioclient

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

	apiscenario "github.com/kenyamaneko/overload-party-scenario/packages/api-scenario"
)

var (
	// ErrNotFound は scenario サービスが 404 を返した場合のエラーです
	ErrNotFound = errors.New("scenarioclient: not found")
	// ErrLocked はエピソードがロック状態の場合のエラーです
	ErrLocked = errors.New("scenarioclient: episode locked")
)

// Client は scenario サービスへの HTTP クライアントです
type Client struct {
	baseURL string
	http    *http.Client
}

// New は scenario サービスクライアントを生成します
func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// ListEpisodesResponse はエピソード一覧のレスポンス型です
type ListEpisodesResponse struct {
	Episodes []apiscenario.EpisodeWithStatus `json:"episodes"`
}

// ListEpisodes はプレイヤーのエピソード一覧を取得します
func (c *Client) ListEpisodes(ctx context.Context, playerID, lang string) ([]apiscenario.EpisodeWithStatus, error) {
	path := fmt.Sprintf("/internal/v1/players/%s/scenarios", url.PathEscape(playerID))
	if lang != "" {
		path += "?lang=" + url.QueryEscape(lang)
	}
	var out ListEpisodesResponse
	if err := c.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return out.Episodes, nil
}

type scriptResponse struct {
	EpisodeID string `json:"episode_id"`
	Script    string `json:"script"`
}

// GetScript はエピソードのスクリプトを取得します
func (c *Client) GetScript(ctx context.Context, playerID, episodeID, lang string) (string, error) {
	path := fmt.Sprintf("/internal/v1/players/%s/scenarios/%s/script",
		url.PathEscape(playerID), url.PathEscape(episodeID))
	if lang != "" {
		path += "?lang=" + url.QueryEscape(lang)
	}
	var out scriptResponse
	if err := c.getJSON(ctx, path, &out); err != nil {
		return "", err
	}
	return out.Script, nil
}

// CompleteEpisode はエピソード完了を記録します
func (c *Client) CompleteEpisode(ctx context.Context, playerID, episodeID string) error {
	path := fmt.Sprintf("/internal/v1/players/%s/scenarios/%s/complete",
		url.PathEscape(playerID), url.PathEscape(episodeID))
	return c.doJSON(ctx, http.MethodPost, path, nil, nil)
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	return c.doJSON(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("scenarioclient: marshal: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("scenarioclient: new request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("scenarioclient: do: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		if out == nil {
			return nil
		}
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("scenarioclient: decode: %w", err)
		}
		return nil
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusForbidden:
		return ErrLocked
	}
	raw, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("scenarioclient: %s %s: status %d: %s", method, path, resp.StatusCode, string(raw))
}

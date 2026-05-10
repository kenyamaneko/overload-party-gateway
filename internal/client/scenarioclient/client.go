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

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
)

var (
	// ErrNotFound は scenario サービスが 404 を返した場合のエラーです
	ErrNotFound = errors.New("scenarioclient: not found")
	// ErrLocked はエピソードがロック状態の場合のエラーです
	ErrLocked = errors.New("scenarioclient: episode locked")
	// ErrInvalidRequest は scenario サービスが 400 を返した場合のエラーです。
	// onboarding 内 name 入力で account のバリデーションが scenario 経由で
	// 中継されるため、ユーザーへ 400 を伝えるのに使う。
	ErrInvalidRequest = errors.New("scenarioclient: invalid request")
	// ErrAlreadyOnboarded はオンボーディング完了済みプレイヤーが GET script や
	// POST complete を呼んだ場合のエラーです (scenario が 409 を返す)。
	ErrAlreadyOnboarded = errors.New("scenarioclient: already onboarded")
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

// GetOnboardingScript はオンボーディングシナリオ本文を返します。
func (c *Client) GetOnboardingScript(ctx context.Context, playerID, lang string) (string, error) {
	path := fmt.Sprintf("/internal/v1/players/%s/onboarding/script", url.PathEscape(playerID))
	if lang != "" {
		path += "?lang=" + url.QueryEscape(lang)
	}
	var out apiscenario.OnboardingScriptResponse
	if err := c.getJSON(ctx, path, &out); err != nil {
		return "", err
	}
	return out.Script, nil
}

// UpdateOnboardingName はオンボード内 name 入力で受け取った表示名を scenario に伝えます。
// scenario が account の validate REST を中継するので 400 はそのまま伝搬します。
func (c *Client) UpdateOnboardingName(ctx context.Context, playerID, name string) error {
	path := fmt.Sprintf("/internal/v1/players/%s/onboarding/name", url.PathEscape(playerID))
	return c.doJSON(ctx, http.MethodPut, path, apiscenario.OnboardingNameRequest{Name: name}, nil)
}

// SelectOnboardingFaction はオンボード内 faction 選択ステップを scenario に伝えます。
// scenario が SelectableFactions による検証と onboarding-faction-set publish を行います。
func (c *Client) SelectOnboardingFaction(ctx context.Context, playerID, initialFactionID string) error {
	path := fmt.Sprintf("/internal/v1/players/%s/onboarding/faction", url.PathEscape(playerID))
	return c.doJSON(ctx, http.MethodPost, path, apiscenario.OnboardingFactionRequest{InitialFactionID: initialFactionID}, nil)
}

// CompleteOnboarding はオンボーディング完了を記録し player-onboarded を発行します。
// faction は事前の SelectOnboardingFaction で account に永続化済みである前提のため
// 本リクエストは body を取りません。
func (c *Client) CompleteOnboarding(ctx context.Context, playerID string) (apiscenario.OnboardingCompleteResponse, error) {
	path := fmt.Sprintf("/internal/v1/players/%s/onboarding/complete", url.PathEscape(playerID))
	var out apiscenario.OnboardingCompleteResponse
	if err := c.doJSON(ctx, http.MethodPost, path, nil, &out); err != nil {
		return apiscenario.OnboardingCompleteResponse{}, err
	}
	return out, nil
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
	internalauth.InjectHeader(ctx, req.Header)
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
	case http.StatusBadRequest:
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: %s", ErrInvalidRequest, string(raw))
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusForbidden:
		return ErrLocked
	case http.StatusConflict:
		return ErrAlreadyOnboarded
	}
	raw, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("scenarioclient: %s %s: status %d: %s", method, path, resp.StatusCode, string(raw))
}

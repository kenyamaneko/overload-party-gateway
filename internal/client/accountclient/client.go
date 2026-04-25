// Package accountclient は account マイクロサービスへの HTTP クライアントを提供します。
// account はプレイヤー、設定、ファクション、バトル回数制限、経験値を管理する。
package accountclient

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

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
)

var (
	// ErrNotFound は account サービスが 404 を返した場合のエラーです
	ErrNotFound = errors.New("accountclient: not found")
	// ErrPlayerAlreadyRegistered はプレイヤーが既に登録済みの場合のエラーです
	ErrPlayerAlreadyRegistered = errors.New("accountclient: player already registered")
)

// Client は account サービスへの HTTP クライアントです
type Client struct {
	baseURL string
	http    *http.Client
}

// New は account サービスクライアントを生成します
func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

type registerRequest struct {
	FirebaseUID string `json:"firebase_uid"`
	Name        string `json:"name"`
}

type loginRequest struct {
	FirebaseUID string `json:"firebase_uid"`
}

// Player はプレイヤー情報の通信型です。
// account サービスの Player モデルをミラーする。
type Player struct {
	PlayerID         string     `json:"player_id"`
	FirebaseUID      string     `json:"firebase_uid"`
	Name             string     `json:"name"`
	Level            int64      `json:"level"`
	Exp              int64      `json:"exp"`
	IsPremium        bool       `json:"is_premium"`
	EquippedIconNo   *int64     `json:"equipped_icon_no,omitempty"`
	SelectedFaction  *string    `json:"selected_faction,omitempty"`
	PremiumExpiresAt *time.Time `json:"premium_expires_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// Register はプレイヤーを新規登録します
func (c *Client) Register(ctx context.Context, firebaseUID, name string) (*Player, error) {
	var out Player
	if err := c.doJSON(ctx, http.MethodPost, "/internal/v1/auth/register", registerRequest{FirebaseUID: firebaseUID, Name: name}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Login はプレイヤーのログインを処理します
func (c *Client) Login(ctx context.Context, firebaseUID string) (*Player, error) {
	var out Player
	if err := c.doJSON(ctx, http.MethodPost, "/internal/v1/auth/login", loginRequest{FirebaseUID: firebaseUID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FindByFirebaseUID は Firebase UID からプレイヤーを検索します。
// 未登録の場合は (nil, nil) を返す。middleware.PlayerResolve / DevAuth が
// 「未登録」と「通信エラー」を区別するためにこのシグネチャに依存している。
func (c *Client) FindByFirebaseUID(ctx context.Context, firebaseUID string) (*Player, error) {
	var out Player
	path := "/internal/v1/players/by-firebase-uid/" + url.PathEscape(firebaseUID)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

// GetPlayer はプレイヤー情報を取得します
func (c *Client) GetPlayer(ctx context.Context, playerID string) (*apiaccount.PlayerResponse, error) {
	var out apiaccount.PlayerResponse
	path := "/internal/v1/players/" + url.PathEscape(playerID)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type updateNameRequest struct {
	Name string `json:"name"`
}

// UpdateName はプレイヤー名を更新します
func (c *Client) UpdateName(ctx context.Context, playerID, name string) (*Player, error) {
	var out Player
	path := "/internal/v1/players/" + url.PathEscape(playerID) + "/name"
	if err := c.doJSON(ctx, http.MethodPut, path, updateNameRequest{Name: name}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetBattleLimit はバトル回数制限情報を取得します
func (c *Client) GetBattleLimit(ctx context.Context, playerID string) (*apiaccount.BattleLimitResponse, error) {
	var out apiaccount.BattleLimitResponse
	path := "/internal/v1/players/" + url.PathEscape(playerID) + "/battle-limit"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// IncrementBattleCount はバトル回数をインクリメントします
func (c *Client) IncrementBattleCount(ctx context.Context, playerID string) error {
	path := "/internal/v1/players/" + url.PathEscape(playerID) + "/battle-limit/increment"
	return c.doJSON(ctx, http.MethodPost, path, nil, nil)
}

type updatePremiumRequest struct {
	IsPremium       bool   `json:"is_premium"`
	ExpiresAtMillis *int64 `json:"expires_at_millis,omitempty"`
}

// UpdatePremium はプレミアムステータスを更新します
func (c *Client) UpdatePremium(ctx context.Context, playerID string, isPremium bool, expiresAt *time.Time) error {
	body := updatePremiumRequest{IsPremium: isPremium}
	if expiresAt != nil {
		ms := expiresAt.UnixMilli()
		body.ExpiresAtMillis = &ms
	}
	path := "/internal/v1/players/" + url.PathEscape(playerID) + "/premium"
	return c.doJSON(ctx, http.MethodPut, path, body, nil)
}

type updateFactionRequest struct {
	Faction string `json:"faction"`
}

// UpdateFaction はプレイヤーの選択ファクションを更新します
func (c *Client) UpdateFaction(ctx context.Context, playerID, faction string) error {
	path := "/internal/v1/players/" + url.PathEscape(playerID) + "/faction"
	return c.doJSON(ctx, http.MethodPut, path, updateFactionRequest{Faction: faction}, nil)
}

type grantFactionRequest struct {
	Faction string `json:"faction"`
	Source  string `json:"source"`
}

// GrantFaction はプレイヤーにファクションを付与します
func (c *Client) GrantFaction(ctx context.Context, playerID, faction, source string) error {
	path := "/internal/v1/players/" + url.PathEscape(playerID) + "/factions"
	return c.doJSON(ctx, http.MethodPost, path, grantFactionRequest{Faction: faction, Source: source}, nil)
}

type listFactionsResponse struct {
	Factions []string `json:"factions"`
}

// ListFactions はプレイヤーの所持ファクション一覧を取得します
func (c *Client) ListFactions(ctx context.Context, playerID string) ([]string, error) {
	var out listFactionsResponse
	path := "/internal/v1/players/" + url.PathEscape(playerID) + "/factions"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Factions, nil
}

type addExpRequest struct {
	ExpGain int64 `json:"exp_gain"`
}

// AddExp はプレイヤーに経験値を加算します
func (c *Client) AddExp(ctx context.Context, playerID string, expGain int64) error {
	path := "/internal/v1/players/" + url.PathEscape(playerID) + "/exp"
	return c.doJSON(ctx, http.MethodPost, path, addExpRequest{ExpGain: expGain}, nil)
}

type awardGameExpRequest struct {
	Player1ID string `json:"player1_id"`
	Player2ID string `json:"player2_id"`
	WinnerNum int64  `json:"winner_num"`
	Reason    string `json:"reason"`
	MatchType string `json:"match_type"`
}

// AwardGameExp はゲーム結果に基づく経験値を付与します
func (c *Client) AwardGameExp(ctx context.Context, p1ID, p2ID string, winnerNum int64, reason, matchType string) error {
	return c.doJSON(ctx, http.MethodPost, "/internal/v1/players/award-game-exp", awardGameExpRequest{
		Player1ID: p1ID, Player2ID: p2ID, WinnerNum: winnerNum, Reason: reason, MatchType: matchType,
	}, nil)
}

// GetSettings はユーザー設定を取得します
func (c *Client) GetSettings(ctx context.Context, playerID string) (*apiaccount.PlayerSettings, error) {
	var out apiaccount.PlayerSettings
	path := "/internal/v1/players/" + url.PathEscape(playerID) + "/settings"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateSettings はユーザー設定を更新します
func (c *Client) UpdateSettings(ctx context.Context, playerID string, req apiaccount.UpdateSettingsRequest) (*apiaccount.PlayerSettings, error) {
	var out apiaccount.PlayerSettings
	path := "/internal/v1/players/" + url.PathEscape(playerID) + "/settings"
	if err := c.doJSON(ctx, http.MethodPut, path, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("accountclient: marshal: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("accountclient: new request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("accountclient: do: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		if out == nil {
			return nil
		}
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("accountclient: decode: %w", err)
		}
		return nil
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusConflict:
		return ErrPlayerAlreadyRegistered
	}
	raw, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("accountclient: %s %s: status %d: %s", method, path, resp.StatusCode, string(raw))
}

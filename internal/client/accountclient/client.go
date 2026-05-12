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

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
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
}

type loginRequest struct {
	FirebaseUID string `json:"firebase_uid"`
}

// Player はプレイヤー情報の通信型です。
// account サービスの Player モデルをミラーする。
type Player struct {
	PlayerID         string     `json:"player_id"`
	FirebaseUID      string     `json:"firebase_uid"`
	Name             *string    `json:"name,omitempty"`
	Level            int64      `json:"level"`
	Exp              int64      `json:"exp"`
	IsPremium        bool       `json:"is_premium"`
	EquippedIconNo   *int64     `json:"equipped_icon_no,omitempty"`
	SelectedFaction  *string    `json:"selected_faction,omitempty"`
	PremiumExpiresAt *time.Time `json:"premium_expires_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// Register はプレイヤーを新規登録します。表示名は Register では渡さず、
// オンボーディング完了時の player-onboarded イベントで確定します。
func (c *Client) Register(ctx context.Context, firebaseUID string) (*Player, error) {
	var out Player
	if err := c.doJSON(ctx, http.MethodPost, "/internal/v1/auth/register", registerRequest{FirebaseUID: firebaseUID}, &out); err != nil {
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
	path := "/internal/v1/auth/by-firebase-uid/" + url.PathEscape(firebaseUID)
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

// GetBattleLimit はバトル回数制限情報を取得します
func (c *Client) GetBattleLimit(ctx context.Context) (*apiaccount.BattleLimitResponse, error) {
	var out apiaccount.BattleLimitResponse
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/account/me/battle-limit", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// IncrementBattleCount はバトル回数をインクリメントします
func (c *Client) IncrementBattleCount(ctx context.Context) error {
	return c.doJSON(ctx, http.MethodPost, "/api/v1/account/me/battle-limit/increment", nil, nil)
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
	internalauth.InjectHeader(ctx, req.Header)
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

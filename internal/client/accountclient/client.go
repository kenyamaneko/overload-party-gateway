// Package accountclient は account サービスへの HTTP クライアントを提供する。
package accountclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	"github.com/kenyamaneko/overload-party-account/packages/api-account/apiaccountclient"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
)

// Sentinel error は SDK の sentinel を re-export する。
var (
	ErrNotFound                = apiaccountclient.ErrNotFound
	ErrPlayerAlreadyRegistered = apiaccountclient.ErrConflict
)

// Client は account サービスへの HTTP クライアント。
type Client struct {
	api *apiaccountclient.Client
}

// New は account サービスクライアントを生成する。
func New(baseURL string) *Client {
	api, err := apiaccountclient.New(baseURL,
		apiaccountclient.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			internalauth.InjectHeader(ctx, req.Header)
			return nil
		}),
	)
	if err != nil {
		panic(fmt.Sprintf("accountclient: %v", err))
	}
	return &Client{api: api}
}

// Register はプレイヤーを新規登録する。
func (c *Client) Register(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
	return c.api.RegisterPlayer(ctx, apiaccount.RegisterRequest{FirebaseUID: firebaseUID})
}

// Login はプレイヤーのログインを処理する。
func (c *Client) Login(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
	return c.api.LoginPlayer(ctx, apiaccount.LoginRequest{FirebaseUID: firebaseUID})
}

// FindByFirebaseUID は Firebase UID からプレイヤーを検索し、未登録時は (nil, nil) を返す。
func (c *Client) FindByFirebaseUID(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
	player, err := c.api.GetPlayerByFirebaseUID(ctx, firebaseUID)
	if errors.Is(err, apiaccountclient.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return player, nil
}

// GetMe は呼び出し主体 (JWT sub) の player 情報を取得する。
func (c *Client) GetMe(ctx context.Context) (*apiaccount.PlayerResponse, error) {
	return c.api.GetPlayer(ctx)
}

// GetBattleLimit はバトル回数制限情報を取得する。
func (c *Client) GetBattleLimit(ctx context.Context) (*apiaccount.BattleLimitResponse, error) {
	return c.api.GetBattleLimit(ctx)
}

// IncrementBattleCount はバトル回数をインクリメントする。
func (c *Client) IncrementBattleCount(ctx context.Context) error {
	return c.api.IncrementBattleCount(ctx)
}

// AwardGameExp はゲーム結果に基づく経験値を付与する。
func (c *Client) AwardGameExp(ctx context.Context, p1ID, p2ID string, winnerNum int64, reason, matchType string) error {
	return c.api.AwardGameExp(ctx, apiaccount.AwardGameExpRequest{
		Player1ID: p1ID,
		Player2ID: p2ID,
		WinnerNum: winnerNum,
		Reason:    reason,
		MatchType: matchType,
	})
}

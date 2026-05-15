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
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// Client は account サービスへの HTTP クライアント。
type Client struct {
	api *apiaccountclient.Client
}

var _ port.AccountClient = (*Client)(nil)

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
	resp, err := c.api.RegisterPlayer(ctx, apiaccount.RegisterRequest{FirebaseUID: firebaseUID})
	return resp, toPortErr(err)
}

// Login はプレイヤーのログインを処理する。
func (c *Client) Login(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
	resp, err := c.api.LoginPlayer(ctx, apiaccount.LoginRequest{FirebaseUID: firebaseUID})
	return resp, toPortErr(err)
}

// FindByFirebaseUID は Firebase UID からプレイヤーを検索し、未登録時は (nil, nil) を返す。
func (c *Client) FindByFirebaseUID(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
	player, err := c.api.GetPlayerByFirebaseUID(ctx, firebaseUID)
	if errors.Is(err, apiaccountclient.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, toPortErr(err)
	}
	return player, nil
}

// GetMe は呼び出し主体 (JWT sub) の player 情報を取得する。
func (c *Client) GetMe(ctx context.Context) (*apiaccount.PlayerResponse, error) {
	resp, err := c.api.GetPlayer(ctx)
	return resp, toPortErr(err)
}

// GetBattleLimit はバトル回数制限情報を取得する。
func (c *Client) GetBattleLimit(ctx context.Context) (*apiaccount.BattleLimitResponse, error) {
	resp, err := c.api.GetBattleLimit(ctx)
	return resp, toPortErr(err)
}

// IncrementBattleCount はバトル回数をインクリメントする。
func (c *Client) IncrementBattleCount(ctx context.Context) error {
	return toPortErr(c.api.IncrementBattleCount(ctx))
}

// AwardGameExp はゲーム結果に基づく経験値を付与する。
func (c *Client) AwardGameExp(ctx context.Context, p1ID, p2ID string, winnerNum int64, reason, matchType string) error {
	return toPortErr(c.api.AwardGameExp(ctx, apiaccount.AwardGameExpRequest{
		Player1ID: p1ID,
		Player2ID: p2ID,
		WinnerNum: winnerNum,
		Reason:    reason,
		MatchType: matchType,
	}))
}

// toPortErr は SDK の sentinel を port の sentinel に変換する。
func toPortErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, apiaccountclient.ErrNotFound):
		return fmt.Errorf("%w: %v", port.ErrAccountNotFound, err)
	case errors.Is(err, apiaccountclient.ErrConflict):
		return fmt.Errorf("%w: %v", port.ErrPlayerAlreadyRegistered, err)
	}
	return err
}

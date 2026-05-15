// Package matchmakingclient は matchmaking サービスへの HTTP クライアントを提供する。
package matchmakingclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	apimatchmaking "github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking"
	"github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking/apimatchmakingclient"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// Client は matchmaking サービスへの HTTP クライアント。
type Client struct {
	api *apimatchmakingclient.Client
}

var _ port.MatchmakingClient = (*Client)(nil)

// New は matchmaking サービスクライアントを生成する。
func New(baseURL string) *Client {
	api, err := apimatchmakingclient.New(baseURL,
		apimatchmakingclient.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			internalauth.InjectHeader(ctx, req.Header)
			return nil
		}),
	)
	if err != nil {
		panic(fmt.Sprintf("matchmakingclient: %v", err))
	}
	return &Client{api: api}
}

// Enqueue はプレイヤーをマッチメイキングキューに追加する。
func (c *Client) Enqueue(ctx context.Context, deckID int64, name string, level int64) error {
	return toPortErr(c.api.EnqueuePlayer(ctx, apimatchmaking.EnqueueRequest{
		DeckID: deckID,
		Name:   name,
		Level:  level,
	}))
}

// Cancel はプレイヤーをマッチメイキングキューから除去する。
func (c *Client) Cancel(ctx context.Context) error {
	err := c.api.CancelPlayer(ctx)
	if errors.Is(err, apimatchmakingclient.ErrNotFound) {
		return nil
	}
	return toPortErr(err)
}

// toPortErr は SDK の sentinel を port の sentinel に変換する。
func toPortErr(err error) error {
	if errors.Is(err, apimatchmakingclient.ErrServiceUnavailable) {
		return fmt.Errorf("%w: %v", port.ErrMatchmakingUnavailable, err)
	}
	return err
}

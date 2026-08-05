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
	api        *apimatchmakingclient.Client
	instanceID string
}

var _ port.MatchmakingClient = (*Client)(nil)

// New は matchmaking サービスクライアントを生成する。
// instanceID は gateway プロセスを識別する値で、matchmaking はこれが切り替わったときに
// 待機を引き継げないキューを空にする。プロセスが生きている間は同じ値を送り続ける必要がある。
// httpClient には Cloud Run 上では ID トークンを付与するものを、ローカルでは素のものを渡す。
func New(baseURL, instanceID string, httpClient *http.Client) *Client {
	api, err := apimatchmakingclient.New(baseURL,
		apimatchmakingclient.WithHTTPClient(httpClient),
		apimatchmakingclient.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			internalauth.InjectHeader(ctx, req.Header)
			return nil
		}),
	)
	if err != nil {
		panic(fmt.Sprintf("matchmakingclient: %v", err))
	}
	return &Client{api: api, instanceID: instanceID}
}

// Enqueue はプレイヤーをマッチメイキングキューに追加する。
func (c *Client) Enqueue(ctx context.Context, deckID int64, name string, level int64) error {
	return toPortErr(c.api.EnqueuePlayer(ctx, apimatchmaking.EnqueueRequest{
		DeckID:            deckID,
		Name:              name,
		Level:             level,
		GatewayInstanceID: c.instanceID,
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

// ReportMatchAbandoned は成立したマッチを、配信先が接続していなかったため不成立として申告する。
func (c *Client) ReportMatchAbandoned(ctx context.Context, matchID string, playerIDs []string) error {
	return toPortErr(c.api.ReportMatchAbandoned(ctx, apimatchmaking.MatchAbandonedRequest{
		MatchID:   matchID,
		PlayerIDs: playerIDs,
		Reason:    apimatchmaking.MatchAbandonedRequestReasonPlayerNotConnected,
	}))
}

// toPortErr は SDK の sentinel を port の sentinel に変換する。
func toPortErr(err error) error {
	if errors.Is(err, apimatchmakingclient.ErrServiceUnavailable) {
		return fmt.Errorf("%w: %v", port.ErrMatchmakingUnavailable, err)
	}
	return err
}

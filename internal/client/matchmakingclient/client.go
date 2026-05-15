// Package matchmakingclient は matchmaking サービスへの HTTP クライアントを提供する。
// gateway 内部で必要な 2 endpoint (Enqueue / Cancel) のみを公開し、内部は
// apimatchmakingclient SDK に委譲する。
package matchmakingclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	apimatchmaking "github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking"
	"github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking/apimatchmakingclient"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
)

// ErrUnavailable は matchmaking サービスが 503 を返した場合の retryable 判定用。
// SDK の ErrServiceUnavailable を re-export し、ws/manager の matchmaking_start
// 失敗時 retry 経路から errors.Is で参照される。
var ErrUnavailable = apimatchmakingclient.ErrServiceUnavailable

// Client は matchmaking サービスへの HTTP クライアント。apimatchmakingclient SDK
// の薄ラッパで、X-Internal-Auth header 注入と gateway 内部利用 method の絞り込みを担う。
type Client struct {
	api *apimatchmakingclient.Client
}

// New は matchmaking サービスクライアントを生成する。baseURL の解析失敗は実行不可なので panic する。
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

// Enqueue は ws/manager の matchmaking_start 受付時に呼ぶ。player_id は
// X-Internal-Auth JWT の sub から matchmaking 側で解決される (gateway は渡さない)。
func (c *Client) Enqueue(ctx context.Context, deckID int64, name string, level int64) error {
	return c.api.EnqueuePlayer(ctx, apimatchmaking.EnqueueRequest{
		DeckID: deckID,
		Name:   name,
		Level:  level,
	})
}

// Cancel は ws/manager の matchmaking_cancel / disconnect 経路から呼ぶ。
// 除去済みまたは未キュー時は nil を返す (404 を no-op 扱い、retry を抑える)。
func (c *Client) Cancel(ctx context.Context) error {
	err := c.api.CancelPlayer(ctx)
	if errors.Is(err, apimatchmakingclient.ErrNotFound) {
		return nil
	}
	return err
}

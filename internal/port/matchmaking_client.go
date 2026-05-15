package port

import (
	"context"
	"errors"
)

// ErrMatchmakingUnavailable は matchmaking サービスが受付停止中 (503) のとき返る。
var ErrMatchmakingUnavailable = errors.New("port: matchmaking service unavailable")

// MatchmakingClient は gateway が matchmaking サービスへアクセスするための port。
type MatchmakingClient interface {
	Enqueue(ctx context.Context, deckID int64, name string, level int64) error
	Cancel(ctx context.Context) error
}

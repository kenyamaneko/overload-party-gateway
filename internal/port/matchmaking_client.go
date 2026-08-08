package port

import (
	"context"
)

// MatchmakingClient は gateway が matchmaking サービスへアクセスするための port。
type MatchmakingClient interface {
	Enqueue(ctx context.Context, deckID int64, name string, level int64) error
	Cancel(ctx context.Context) error
	// ReportMatchAbandoned は成立したマッチを、配信先が接続していなかったため不成立として申告する。
	ReportMatchAbandoned(ctx context.Context, matchID string, playerIDs []string) error
}

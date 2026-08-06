package port

import (
	"context"
	"time"
)

// TimerStore は gateway が保持するプレイヤーの切断猶予期限を、プロセスの外へ
// 写す読み書きを抽象化するインターフェースです。
// インメモリ状態が主であり、本 interface はプロセス終了後も期限を参照できる
// ようにするための写しの永続化を担う。
type TimerStore interface {
	// SetDisconnectDeadline はプレイヤーの切断猶予期限を書き込みます。
	SetDisconnectDeadline(ctx context.Context, playerID, gameID string, deadline time.Time) error
	// ClearDisconnectDeadline はプレイヤーの切断猶予期限を削除します。
	ClearDisconnectDeadline(ctx context.Context, playerID string) error
	// GetDisconnectDeadline はプレイヤーの切断猶予期限を読み出します。
	// 該当が無い場合は found=false を返します。
	GetDisconnectDeadline(ctx context.Context, playerID string) (deadline DisconnectDeadline, found bool, err error)
}

// DisconnectDeadline はプレイヤーの切断猶予期限です。
type DisconnectDeadline struct {
	GameID   string
	Deadline time.Time
}

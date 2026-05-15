package port

import (
	"context"
	"errors"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
)

// ErrAccountNotFound は account サービスがプレイヤーを見つけられなかった (404) とき返る。
var ErrAccountNotFound = errors.New("port: account player not found")

// ErrPlayerAlreadyRegistered は同一 Firebase UID で既に登録済み (409) のとき返る。
var ErrPlayerAlreadyRegistered = errors.New("port: player already registered")

// AccountClient は gateway が account サービスへアクセスするための port。
type AccountClient interface {
	Register(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error)
	Login(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error)
	FindByFirebaseUID(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error)
	GetMe(ctx context.Context) (*apiaccount.PlayerResponse, error)
	GetBattleLimit(ctx context.Context) (*apiaccount.BattleLimitResponse, error)
	IncrementBattleCount(ctx context.Context) error
	AwardGameExp(ctx context.Context, p1ID, p2ID string, winnerNum int64, reason, matchType string) error
}

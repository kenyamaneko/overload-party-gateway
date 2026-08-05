package port

import (
	"context"
	"errors"
	"time"

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
	// RevertBattleCount は無効になった対戦で消費したバトル回数を両プレイヤーに戻します。
	// consumedAt は回数を消費した時刻で、account はこの時刻のゲーム日から戻す先を決めます。
	RevertBattleCount(ctx context.Context, gameID, p1ID, p2ID string, consumedAt time.Time) error
}

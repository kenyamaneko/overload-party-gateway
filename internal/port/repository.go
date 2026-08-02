package port

import (
	"context"
)

// GamePlayerRepo はプレイヤーとゲームスロットの対応を管理します。
// gateway.game_players テーブルは gateway が所有し、battle はスロット番号
// (1/2) のみを扱う。gateway は match_made 時に行を挿入し、game over 時に
// exp_awarded を更新する。
type GamePlayerRepo interface {
	InsertGamePlayer(ctx context.Context, gameID string, playerNum int, playerID string) error
	LookupPlayerNum(ctx context.Context, gameID string, playerID string) (int, error)
	LookupGamePlayers(ctx context.Context, gameID string) ([]GamePlayerEntry, error)
	MarkExpAwarded(ctx context.Context, gameID string) (bool, error)
}

// GamePlayerEntry はゲーム内のプレイヤースロット情報を保持します
type GamePlayerEntry struct {
	PlayerNum int
	PlayerID  string
}

// GameConfigRepo はゲーム設定値の読み取りを抽象化するインターフェースです。
// キーが存在しない場合は ErrNotFound を返す（fail-fast）。
type GameConfigRepo interface {
	GetInt64(ctx context.Context, key string) (int64, error)
}

// ProcessedMatchRepo は match_made イベントの永続的な重複排除を管理します。
// battle のゲーム作成は matchId に対して冪等ではないため、Claim で処理を開始した
// matchId を記録し、battle 呼び出し前に失敗したら Release で記録を取り消し、
// battle 呼び出しが成功したら RecordGameCreated で結果の gameID を記録します。
// GameIDFor は再送時に battle を再呼び出しせず結果を復元するために使います。
type ProcessedMatchRepo interface {
	// Claim は matchId の処理を排他的に開始します。既に claim 済みなら claimed=false を返します。
	Claim(ctx context.Context, matchID string) (claimed bool, err error)
	// Release は battle 呼び出し前の失敗時に Claim を取り消します。
	Release(ctx context.Context, matchID string) error
	// RecordGameCreated は claim 済みの matchId に対して作成された battle game の ID を記録します。
	RecordGameCreated(ctx context.Context, matchID, gameID string) error
	// GameIDFor は matchId に対して既に作成済みの battle game の ID を返します。未作成なら found=false です。
	GameIDFor(ctx context.Context, matchID string) (gameID string, found bool, err error)
	// MarkNotified は成立通知の送信権を排他的に取得します。既に送信済みなら marked=false を返します。
	MarkNotified(ctx context.Context, matchID string) (marked bool, err error)
}

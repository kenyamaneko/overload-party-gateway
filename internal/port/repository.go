package port

import (
	"context"

	apigateway "github.com/kenyamaneko/overload-party-gateway/packages/api-gateway"
)

// NewsRepo はクラウドニュース記事のデータアクセス契約を定義します
type NewsRepo interface {
	List(ctx context.Context, limit int, offset int) ([]*apigateway.NewsArticle, error)
}

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

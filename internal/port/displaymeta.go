package port

import "context"

// DisplayMeta は対戦相手 / 観戦対象の WS payload 表示に用いる snapshot。
// match 成立時点の値で固定し、試合中の name 変更 / level 変動は反映しない。
type DisplayMeta struct {
	Name  string
	Level int
}

// DisplayMetaStore は player display meta snapshot の書き込み境界。
// match_made handler が対戦者 2 名分の snapshot を書き込む経路で使う。
type DisplayMetaStore interface {
	Put(ctx context.Context, gameID string, playerNum int, meta DisplayMeta) error
}

// DisplayMetaLookup は player display meta snapshot の読み出し境界。
// game state relay 経路でクライアントへ name / level を埋め込む際に使う。
// snapshot が存在しない場合は ErrNotFound を返す。
type DisplayMetaLookup interface {
	Get(ctx context.Context, gameID string, playerNum int) (DisplayMeta, error)
}

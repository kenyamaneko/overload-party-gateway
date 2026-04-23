package port

import (
	"context"

	apimatchmaking "github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking"
)

// MatchEventHandler はマッチ成立イベントを処理するインターフェースです。
// デッキ解決、battle 呼び出し、WS 通知を担う。
//
// payload 型は送信側 (matchmaking) の公開契約 apimatchmaking.MatchMadeEvent を
// そのまま使用し、gateway 側で重複定義しない。
type MatchEventHandler interface {
	HandleMatchMade(ctx context.Context, event apimatchmaking.MatchMadeEvent) error
}

// EventSubscriber は Pub/Sub トピックを購読しメッセージをルーティングするインターフェースです
type EventSubscriber interface {
	Run(ctx context.Context) error
	Close() error
}

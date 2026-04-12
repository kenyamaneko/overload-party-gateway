package port

import "context"

// MatchedPlayer はマッチメイキングサービスのペイロードに対応する構造体です
type MatchedPlayer struct {
	PlayerID string `json:"playerId"`
	DeckID   int64  `json:"deckId"`
}

// MatchMadeEvent は 2 人のプレイヤーがペアリングされた際に
// マッチメイキングサービスが publish するペイロードです。
// battle のトポロジは gateway が config から解決する。
type MatchMadeEvent struct {
	Type    string          `json:"type"`
	MatchID string          `json:"matchId"`
	Players []MatchedPlayer `json:"players"`
}

// MatchEventHandler はマッチ成立イベントを処理するインターフェースです。
// デッキ解決、battle 呼び出し、WS 通知を担う。
type MatchEventHandler interface {
	HandleMatchMade(ctx context.Context, event MatchMadeEvent) error
}

// EventSubscriber は Pub/Sub トピックを購読しメッセージをルーティングするインターフェースです
type EventSubscriber interface {
	Run(ctx context.Context) error
	Close() error
}

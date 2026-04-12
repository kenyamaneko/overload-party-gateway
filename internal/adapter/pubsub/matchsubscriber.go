// Package pubsub は gateway 用の Pub/Sub イベント subscriber を実装します。
//
// MatchSubscriber は Cloud Pub/Sub から `match_made` イベントを pull し、
// バトル作成 + WS プッシュバックに変換する。
//
// 全 Gateway Pod が同一 subscription `matchmaking-events-gateway` から pull するため、
// 1 メッセージは 1 Pod にのみ配信される（competing consumers）。
// 2 人のプレイヤーの WebSocket 接続を保持する Pod のみが push し、
// 他の Pod はメッセージを受信してセッション解決に失敗し ack する。
//
// matchId 重複排除はインメモリ（Pod 単位）。Pod 再起動でリセットされるが、
// Cloud Pub/Sub の Exactly-Once Delivery により ack 済みメッセージの再配信は発生しない。
package pubsub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"

	"cloud.google.com/go/pubsub"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// playerIDList はログメッセージ用にプレイヤー ID をフォーマットする。
func playerIDList(players []port.MatchedPlayer) string {
	ids := make([]string, 0, len(players))
	for _, p := range players {
		ids = append(ids, p.PlayerID)
	}
	return "[" + strings.Join(ids, ",") + "]"
}

// MatchSubscriber は Cloud Pub/Sub から match_made イベントを受信し
// port.MatchEventHandler 経由でディスパッチします
type MatchSubscriber struct {
	client         *pubsub.Client
	subscription   *pubsub.Subscription
	handler        port.MatchEventHandler
	processedMu    sync.Mutex
	processedMatch map[string]struct{}
}

// NewMatchSubscriber は指定 subscription から pull する subscriber を生成します。
// subscription は Terraform で事前作成されている必要がある。
func NewMatchSubscriber(ctx context.Context, projectID, subscriptionID string, handler port.MatchEventHandler) (*MatchSubscriber, error) {
	if projectID == "" {
		return nil, errors.New("matchsubscriber: projectID is empty")
	}
	if subscriptionID == "" {
		return nil, errors.New("matchsubscriber: subscriptionID is empty")
	}
	if handler == nil {
		return nil, errors.New("matchsubscriber: handler is nil")
	}
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("matchsubscriber: new client: %w", err)
	}
	sub := client.Subscription(subscriptionID)
	ok, err := sub.Exists(ctx)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("matchsubscriber: exists check: %w", err)
	}
	if !ok {
		_ = client.Close()
		return nil, fmt.Errorf("matchsubscriber: subscription %q not found in %q", subscriptionID, projectID)
	}
	return &MatchSubscriber{
		client:         client,
		subscription:   sub,
		handler:        handler,
		processedMatch: make(map[string]struct{}),
	}, nil
}

// Run は ctx がキャンセルされるか Receive がエラーを返すまでブロックします
func (s *MatchSubscriber) Run(ctx context.Context) error {
	log.Printf("matchsubscriber: pulling from %s", s.subscription.ID())
	return s.subscription.Receive(ctx, s.handle)
}

// Close は Pub/Sub クライアントをクローズします
func (s *MatchSubscriber) Close() error {
	return s.client.Close()
}

func (s *MatchSubscriber) handle(ctx context.Context, msg *pubsub.Message) {
	var event port.MatchMadeEvent
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		log.Printf("matchsubscriber: bad payload (will nack): %v raw=%s", err, string(msg.Data))
		msg.Nack()
		return
	}
	if event.Type != "match_made" {
		log.Printf("matchsubscriber: unknown event type %q, acking", event.Type)
		msg.Ack()
		return
	}

	if !s.markProcessed(event.MatchID) {
		log.Printf("matchsubscriber: duplicate matchId=%s, acking without re-handling", event.MatchID)
		msg.Ack()
		return
	}

	if err := s.handler.HandleMatchMade(ctx, event); err != nil {
		log.Printf("matchsubscriber: handler failed matchId=%s players=%s err=%v",
			event.MatchID, playerIDList(event.Players), err)
		s.unmark(event.MatchID)
		msg.Nack()
		return
	}
	msg.Ack()
}

// markProcessed はこの matchId がこの Pod で未処理の場合に true を返す。
func (s *MatchSubscriber) markProcessed(matchID string) bool {
	s.processedMu.Lock()
	defer s.processedMu.Unlock()
	if _, seen := s.processedMatch[matchID]; seen {
		return false
	}
	s.processedMatch[matchID] = struct{}{}
	return true
}

// unmark はハンドラー失敗後にリトライ可能なよう重複排除エントリをロールバックする。
func (s *MatchSubscriber) unmark(matchID string) {
	s.processedMu.Lock()
	defer s.processedMu.Unlock()
	delete(s.processedMatch, matchID)
}

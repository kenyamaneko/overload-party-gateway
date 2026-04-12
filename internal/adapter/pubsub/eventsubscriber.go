package pubsub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"cloud.google.com/go/pubsub"

	pubsubevents "github.com/kenyamaneko/overload-party-common/packages/pubsub-events"
	ws "github.com/kenyamaneko/overload-party-gateway/internal/handler/ws"
)

// WSPusher は接続中プレイヤーへのメッセージ送信に使用するインターフェースです。
// ConnectionHub の SendToPlayer シグネチャに対応する。
type WSPusher interface {
	SendToPlayer(playerID string, msg any)
}

// WSMessage は WS hub が送信する最小メッセージ形式です
type WSMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// HubWSPusher は ConnectionHub を EventSubscriber が必要とする WSPusher インターフェースに適合させます。
// メッセージ構造体を json.RawMessage にシリアライズし ws.WSMessage 型で Hub.SendToPlayer を呼び出す。
type HubWSPusher struct {
	Hub *ws.ConnectionHub
}

// SendToPlayer はプレイヤーにメッセージを送信します
func (h *HubWSPusher) SendToPlayer(playerID string, msg any) {
	m, ok := msg.(*WSMessage)
	if !ok {
		log.Printf("event subscriber: unexpected msg type %T for player %s", msg, playerID)
		return
	}
	h.Hub.SendToPlayer(playerID, &ws.WSMessage{
		Type: m.Type,
		Data: json.RawMessage(m.Data),
	})
}

// EventSubscriber は faction-selected / premium-updated イベントを WS push にルーティングする汎用 Pub/Sub subscriber です
type EventSubscriber struct {
	client       *pubsub.Client
	subscription *pubsub.Subscription
	pusher       WSPusher
	topicKind    string // "faction-selected" | "premium-updated" (for log messages)
}

// NewEventSubscriber はクロスサービスイベントトピック用の subscriber を生成します
func NewEventSubscriber(
	ctx context.Context,
	projectID, subscriptionID, topicKind string,
	pusher WSPusher,
) (*EventSubscriber, error) {
	if projectID == "" || subscriptionID == "" {
		return nil, errors.New("event subscriber: projectID and subscriptionID are required")
	}
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("event subscriber: new client: %w", err)
	}
	sub := client.Subscription(subscriptionID)
	ok, err := sub.Exists(ctx)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("event subscriber: exists check %q: %w", subscriptionID, err)
	}
	if !ok {
		_ = client.Close()
		return nil, fmt.Errorf("event subscriber: subscription %q not found", subscriptionID)
	}
	return &EventSubscriber{
		client:       client,
		subscription: sub,
		pusher:       pusher,
		topicKind:    topicKind,
	}, nil
}

// Run は ctx がキャンセルされるか Receive がエラーを返すまでブロックします
func (s *EventSubscriber) Run(ctx context.Context) error {
	log.Printf("event subscriber [%s]: pulling from %s", s.topicKind, s.subscription.ID())
	return s.subscription.Receive(ctx, s.handle)
}

// Close は Pub/Sub クライアントをクローズします
func (s *EventSubscriber) Close() error { return s.client.Close() }

func (s *EventSubscriber) handle(_ context.Context, msg *pubsub.Message) {
	var base struct {
		EventType string `json:"event_type"`
		PlayerID  string `json:"player_id"`
	}
	if err := json.Unmarshal(msg.Data, &base); err != nil {
		log.Printf("event subscriber [%s]: bad payload (ack+drop): %v", s.topicKind, err)
		msg.Ack()
		return
	}
	if base.PlayerID == "" {
		log.Printf("event subscriber [%s]: missing player_id (ack+drop)", s.topicKind)
		msg.Ack()
		return
	}

	var wsType string
	switch base.EventType {
	case pubsubevents.EventTypeFactionSelected:
		wsType = "faction_selection_complete"
	case pubsubevents.EventTypePremiumUpdated:
		wsType = "premium_update_complete"
	default:
		log.Printf("event subscriber [%s]: unknown event_type %q (ack+drop)", s.topicKind, base.EventType)
		msg.Ack()
		return
	}

	s.pusher.SendToPlayer(base.PlayerID, &WSMessage{
		Type: wsType,
		Data: msg.Data,
	})
	msg.Ack()
}

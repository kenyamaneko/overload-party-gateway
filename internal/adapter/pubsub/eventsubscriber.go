// Package pubsub は gateway のクロスサービス Pub/Sub subscriber を管理します。
//
// 各 subscriber は自 Pod に該当プレイヤーの WS 接続があるときだけ通知を push し、
// 接続がなければ ack して drop する (一過性通知・永続状態なし)。
// ADR-022 により subscriber は業務事実単位で分離され、1 subscriber = 1 topic = 1 WS message type
// の 1 対 1 対応になっている。
package pubsub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"cloud.google.com/go/pubsub/v2"

	apiscenario "github.com/kenyamaneko/overload-party-scenario/packages/api-scenario"
	apishop "github.com/kenyamaneko/overload-party-shop/packages/api-shop"

	ws "github.com/kenyamaneko/overload-party-gateway/internal/handler/ws"
	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

// WSPusher は接続中プレイヤーへのメッセージ送信に使用するインターフェースです。
// ConnectionHub の SendToPlayer シグネチャに対応する。
type WSPusher interface {
	SendToPlayer(playerID string, msg any)
}

// WSMessage は WS hub が送信する最小メッセージ形式です。
type WSMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// HubWSPusher は ConnectionHub を subscriber が必要とする WSPusher インターフェースに適合させます。
// メッセージ構造体を json.RawMessage にシリアライズし ws.WSMessage 型で Hub.SendToPlayer を呼び出す。
type HubWSPusher struct {
	Hub *ws.ConnectionHub
}

// SendToPlayer はプレイヤーにメッセージを送信します。
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

// PlayerOnboardedSubscriber は player-onboarded-gateway-sub からイベントを取得し、
// オンボーディング完了を WS `onboarding_complete` メッセージとして push します。
type PlayerOnboardedSubscriber struct {
	client     *pubsub.Client
	subscriber *pubsub.Subscriber
	pusher     WSPusher
}

// NewPlayerOnboardedSubscriber は PlayerOnboardedSubscriber を生成します。
func NewPlayerOnboardedSubscriber(
	ctx context.Context,
	projectID, subscriptionID string,
	pusher WSPusher,
) (*PlayerOnboardedSubscriber, error) {
	if projectID == "" || subscriptionID == "" {
		return nil, errors.New("player-onboarded subscriber: projectID and subscriptionID are required")
	}
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("player-onboarded subscriber: new client: %w", err)
	}
	return &PlayerOnboardedSubscriber{
		client:     client,
		subscriber: client.Subscriber(subscriptionID),
		pusher:     pusher,
	}, nil
}

// Run は ctx がキャンセルされるか Receive がエラーを返すまでブロックします。
func (s *PlayerOnboardedSubscriber) Run(ctx context.Context) error {
	log.Printf("player-onboarded subscriber: pulling from %s", s.subscriber.ID())
	return s.subscriber.Receive(ctx, s.handle)
}

// Close は Pub/Sub クライアントをクローズします。
func (s *PlayerOnboardedSubscriber) Close() error { return s.client.Close() }

func (s *PlayerOnboardedSubscriber) handle(_ context.Context, msg *pubsub.Message) {
	var ev apiscenario.PlayerOnboardedEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		log.Printf("player-onboarded subscriber: bad payload (ack+drop): %v", err)
		msg.Ack()
		return
	}
	if ev.EventType != apiscenario.EventTypePlayerOnboarded {
		log.Printf("player-onboarded subscriber: unknown event_type %q (ack+drop)", ev.EventType)
		msg.Ack()
		return
	}
	if ev.PlayerID == "" {
		log.Printf("player-onboarded subscriber: missing player_id (ack+drop)")
		msg.Ack()
		return
	}
	s.pusher.SendToPlayer(ev.PlayerID, &WSMessage{
		Type: genws.WSServerMsgOnboardingComplete,
		Data: msg.Data,
	})
	msg.Ack()
}

// FactionPurchasedSubscriber は faction-purchased-gateway-sub からイベントを取得し、
// faction 購入完了を WS `faction_purchase_complete` メッセージとして push します。
type FactionPurchasedSubscriber struct {
	client     *pubsub.Client
	subscriber *pubsub.Subscriber
	pusher     WSPusher
}

// NewFactionPurchasedSubscriber は FactionPurchasedSubscriber を生成します。
func NewFactionPurchasedSubscriber(
	ctx context.Context,
	projectID, subscriptionID string,
	pusher WSPusher,
) (*FactionPurchasedSubscriber, error) {
	if projectID == "" || subscriptionID == "" {
		return nil, errors.New("faction-purchased subscriber: projectID and subscriptionID are required")
	}
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("faction-purchased subscriber: new client: %w", err)
	}
	return &FactionPurchasedSubscriber{
		client:     client,
		subscriber: client.Subscriber(subscriptionID),
		pusher:     pusher,
	}, nil
}

// Run は ctx がキャンセルされるか Receive がエラーを返すまでブロックします。
func (s *FactionPurchasedSubscriber) Run(ctx context.Context) error {
	log.Printf("faction-purchased subscriber: pulling from %s", s.subscriber.ID())
	return s.subscriber.Receive(ctx, s.handle)
}

// Close は Pub/Sub クライアントをクローズします。
func (s *FactionPurchasedSubscriber) Close() error { return s.client.Close() }

func (s *FactionPurchasedSubscriber) handle(_ context.Context, msg *pubsub.Message) {
	var ev apishop.FactionPurchasedEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		log.Printf("faction-purchased subscriber: bad payload (ack+drop): %v", err)
		msg.Ack()
		return
	}
	if ev.EventType != apishop.EventTypeFactionPurchased {
		log.Printf("faction-purchased subscriber: unknown event_type %q (ack+drop)", ev.EventType)
		msg.Ack()
		return
	}
	if ev.PlayerID == "" {
		log.Printf("faction-purchased subscriber: missing player_id (ack+drop)")
		msg.Ack()
		return
	}
	s.pusher.SendToPlayer(ev.PlayerID, &WSMessage{
		Type: genws.WSServerMsgFactionPurchaseComplete,
		Data: msg.Data,
	})
	msg.Ack()
}

// PremiumUpdatedSubscriber は premium-updated-gateway-sub からイベントを取得し、
// premium 状態変化を WS `premium_update_complete` メッセージとして push します。
type PremiumUpdatedSubscriber struct {
	client     *pubsub.Client
	subscriber *pubsub.Subscriber
	pusher     WSPusher
}

// NewPremiumUpdatedSubscriber は PremiumUpdatedSubscriber を生成します。
func NewPremiumUpdatedSubscriber(
	ctx context.Context,
	projectID, subscriptionID string,
	pusher WSPusher,
) (*PremiumUpdatedSubscriber, error) {
	if projectID == "" || subscriptionID == "" {
		return nil, errors.New("premium-updated subscriber: projectID and subscriptionID are required")
	}
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("premium-updated subscriber: new client: %w", err)
	}
	return &PremiumUpdatedSubscriber{
		client:     client,
		subscriber: client.Subscriber(subscriptionID),
		pusher:     pusher,
	}, nil
}

// Run は ctx がキャンセルされるか Receive がエラーを返すまでブロックします。
func (s *PremiumUpdatedSubscriber) Run(ctx context.Context) error {
	log.Printf("premium-updated subscriber: pulling from %s", s.subscriber.ID())
	return s.subscriber.Receive(ctx, s.handle)
}

// Close は Pub/Sub クライアントをクローズします。
func (s *PremiumUpdatedSubscriber) Close() error { return s.client.Close() }

func (s *PremiumUpdatedSubscriber) handle(_ context.Context, msg *pubsub.Message) {
	var ev apishop.PremiumUpdatedEvent
	if err := json.Unmarshal(msg.Data, &ev); err != nil {
		log.Printf("premium-updated subscriber: bad payload (ack+drop): %v", err)
		msg.Ack()
		return
	}
	if ev.EventType != apishop.EventTypePremiumUpdated {
		log.Printf("premium-updated subscriber: unknown event_type %q (ack+drop)", ev.EventType)
		msg.Ack()
		return
	}
	if ev.PlayerID == "" {
		log.Printf("premium-updated subscriber: missing player_id (ack+drop)")
		msg.Ack()
		return
	}
	s.pusher.SendToPlayer(ev.PlayerID, &WSMessage{
		Type: genws.WSServerMsgPremiumUpdateComplete,
		Data: msg.Data,
	})
	msg.Ack()
}

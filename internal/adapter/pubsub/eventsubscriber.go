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
	"log/slog"

	"cloud.google.com/go/pubsub/v2"

	apiscenario "github.com/kenyamaneko/overload-party-scenario/packages/api-scenario"
	apishop "github.com/kenyamaneko/overload-party-shop/packages/api-shop"

	ws "github.com/kenyamaneko/overload-party-gateway/internal/handler/ws"
	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

// WSMessage は WS hub が送信する最小メッセージ形式です。
type WSMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// WSPusher は接続中プレイヤーへのメッセージ送信に使用するインターフェースです。
//
// Why: 過去は `msg any` 型で ConnectionHub.SendToPlayer のシグネチャに合わせていたが、
// subscriber 側は常に *WSMessage しか渡さない内部契約であり、any を維持する利点がない。
// 契約違反を型レベルで検出するため *WSMessage に厳密化している。
type WSPusher interface {
	SendToPlayer(playerID string, msg *WSMessage)
}

// HubWSPusher は ConnectionHub を subscriber が必要とする WSPusher インターフェースに適合させます。
// メッセージ構造体を json.RawMessage にシリアライズし ws.WSMessage 型で Hub.SendToPlayer を呼び出す。
type HubWSPusher struct {
	Hub *ws.ConnectionHub
}

// SendToPlayer はプレイヤーにメッセージを送信します。
func (h *HubWSPusher) SendToPlayer(playerID string, msg *WSMessage) {
	h.Hub.SendToPlayer(playerID, &ws.WSMessage{
		Type: msg.Type,
		Data: json.RawMessage(msg.Data),
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
	slog.Info("player-onboarded subscriber: pulling", "subscription", s.subscriber.ID())
	return s.subscriber.Receive(ctx, s.handle)
}

// Close は Pub/Sub クライアントをクローズします。
func (s *PlayerOnboardedSubscriber) Close() error { return s.client.Close() }

func (s *PlayerOnboardedSubscriber) handle(ctx context.Context, msg *pubsub.Message) {
	if ack := s.processEvent(ctx, msg.Data); ack {
		msg.Ack()
	} else {
		msg.Nack()
	}
}

// processEvent は Pub/Sub ペイロードを処理し、ack すべきか (true) / nack すべきか
// (false) を返す。*pubsub.Message への依存を handle に閉じ込めることで、
// ビジネスロジックを単体テストから直接検証できるようにする。
func (s *PlayerOnboardedSubscriber) processEvent(_ context.Context, data []byte) (ack bool) {
	var ev apiscenario.PlayerOnboardedEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		slog.Error("player-onboarded subscriber: bad payload (nack)",
			"error", err, "payload_len", len(data))
		return false
	}
	if ev.EventType != apiscenario.EventTypePlayerOnboarded {
		slog.Warn("player-onboarded subscriber: unknown event_type, acking",
			"event_type", ev.EventType, "event_id", ev.EventID)
		return true
	}
	if ev.PlayerID == "" {
		slog.Error("player-onboarded subscriber: missing player_id (nack)",
			"event_id", ev.EventID)
		return false
	}
	s.pusher.SendToPlayer(ev.PlayerID, &WSMessage{
		Type: genws.WSServerMsgOnboardingComplete,
		Data: data,
	})
	return true
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
	slog.Info("faction-purchased subscriber: pulling", "subscription", s.subscriber.ID())
	return s.subscriber.Receive(ctx, s.handle)
}

// Close は Pub/Sub クライアントをクローズします。
func (s *FactionPurchasedSubscriber) Close() error { return s.client.Close() }

func (s *FactionPurchasedSubscriber) handle(ctx context.Context, msg *pubsub.Message) {
	if ack := s.processEvent(ctx, msg.Data); ack {
		msg.Ack()
	} else {
		msg.Nack()
	}
}

// processEvent は Pub/Sub ペイロードを処理し、ack すべきか (true) / nack すべきか
// (false) を返す。
func (s *FactionPurchasedSubscriber) processEvent(_ context.Context, data []byte) (ack bool) {
	var ev apishop.FactionPurchasedEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		slog.Error("faction-purchased subscriber: bad payload (nack)",
			"error", err, "payload_len", len(data))
		return false
	}
	if ev.EventType != apishop.EventTypeFactionPurchased {
		slog.Warn("faction-purchased subscriber: unknown event_type, acking",
			"event_type", ev.EventType, "event_id", ev.EventID)
		return true
	}
	if ev.PlayerID == "" {
		slog.Error("faction-purchased subscriber: missing player_id (nack)",
			"event_id", ev.EventID)
		return false
	}
	s.pusher.SendToPlayer(ev.PlayerID, &WSMessage{
		Type: genws.WSServerMsgFactionPurchaseComplete,
		Data: data,
	})
	return true
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
	slog.Info("premium-updated subscriber: pulling", "subscription", s.subscriber.ID())
	return s.subscriber.Receive(ctx, s.handle)
}

// Close は Pub/Sub クライアントをクローズします。
func (s *PremiumUpdatedSubscriber) Close() error { return s.client.Close() }

func (s *PremiumUpdatedSubscriber) handle(ctx context.Context, msg *pubsub.Message) {
	if ack := s.processEvent(ctx, msg.Data); ack {
		msg.Ack()
	} else {
		msg.Nack()
	}
}

// processEvent は Pub/Sub ペイロードを処理し、ack すべきか (true) / nack すべきか
// (false) を返す。
func (s *PremiumUpdatedSubscriber) processEvent(_ context.Context, data []byte) (ack bool) {
	var ev apishop.PremiumUpdatedEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		slog.Error("premium-updated subscriber: bad payload (nack)",
			"error", err, "payload_len", len(data))
		return false
	}
	if ev.EventType != apishop.EventTypePremiumUpdated {
		slog.Warn("premium-updated subscriber: unknown event_type, acking",
			"event_type", ev.EventType, "event_id", ev.EventID)
		return true
	}
	if ev.PlayerID == "" {
		slog.Error("premium-updated subscriber: missing player_id (nack)",
			"event_id", ev.EventID)
		return false
	}
	s.pusher.SendToPlayer(ev.PlayerID, &WSMessage{
		Type: genws.WSServerMsgPremiumUpdateComplete,
		Data: data,
	})
	return true
}

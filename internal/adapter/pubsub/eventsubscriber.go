// Package pubsub は gateway のクロスサービス Pub/Sub subscriber を管理します。
//
// 各 subscriber は自 Pod に該当プレイヤーの WS 接続があるときだけ通知を push し、
// 接続がなければ ack して drop する (一過性通知・永続状態なし)。
// 1 subscriber = 1 topic = 1 WS message type の 1 対 1 対応。
package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	apiscenario "github.com/kenyamaneko/overload-party-scenario/packages/api-scenario"
	apishop "github.com/kenyamaneko/overload-party-shop/packages/api-shop"

	ws "github.com/kenyamaneko/overload-party-gateway/internal/handler/ws"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

// WSMessage は WS hub が送信する最小メッセージ形式です。
type WSMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// WSPusher は接続中プレイヤーへのメッセージ送信に使用するインターフェースです。
//
// Why: subscriber 側は常に *WSMessage しか渡さない内部契約のため、型を厳密化し
// any 経由の契約違反を compile time で検出する。
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

// PlayerOnboardedSubscriber は player-onboarded subscription からイベントを取得し、
// オンボーディング完了を WS `onboarding_complete` メッセージとして push します。
type PlayerOnboardedSubscriber struct {
	stream port.MessageStream
	pusher WSPusher
}

// NewPlayerOnboardedSubscriber は PlayerOnboardedSubscriber を生成します。
func NewPlayerOnboardedSubscriber(stream port.MessageStream, pusher WSPusher) *PlayerOnboardedSubscriber {
	return &PlayerOnboardedSubscriber{stream: stream, pusher: pusher}
}

// Run は ctx がキャンセルされるか stream がエラーを返すまでブロックします。
func (s *PlayerOnboardedSubscriber) Run(ctx context.Context) error {
	slog.Info("player-onboarded subscriber: consuming")
	return s.stream.Consume(ctx, s.processEvent)
}

// processEvent は 1 イベントを処理する。戻り値 nil = ack、非 nil = nack。
func (s *PlayerOnboardedSubscriber) processEvent(_ context.Context, data []byte) error {
	var ev apiscenario.PlayerOnboardedEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		slog.Error("player-onboarded subscriber: bad payload (nack)",
			"error", err, "payload_len", len(data))
		return fmt.Errorf("player-onboarded: bad payload: %w", err)
	}
	if ev.EventType != apiscenario.EventTypePlayerOnboarded {
		slog.Warn("player-onboarded subscriber: unknown event_type, acking",
			"event_type", ev.EventType, "event_id", ev.EventID)
		return nil
	}
	if ev.PlayerID == "" {
		slog.Error("player-onboarded subscriber: missing player_id (nack)",
			"event_id", ev.EventID)
		return fmt.Errorf("player-onboarded: missing player_id")
	}
	s.pusher.SendToPlayer(ev.PlayerID, &WSMessage{
		Type: genws.WSServerMsgOnboardingComplete,
		Data: data,
	})
	return nil
}

// FactionPurchasedSubscriber は faction-purchased subscription からイベントを取得し、
// faction 購入完了を WS `faction_purchase_complete` メッセージとして push します。
type FactionPurchasedSubscriber struct {
	stream port.MessageStream
	pusher WSPusher
}

// NewFactionPurchasedSubscriber は FactionPurchasedSubscriber を生成します。
func NewFactionPurchasedSubscriber(stream port.MessageStream, pusher WSPusher) *FactionPurchasedSubscriber {
	return &FactionPurchasedSubscriber{stream: stream, pusher: pusher}
}

// Run は ctx がキャンセルされるか stream がエラーを返すまでブロックします。
func (s *FactionPurchasedSubscriber) Run(ctx context.Context) error {
	slog.Info("faction-purchased subscriber: consuming")
	return s.stream.Consume(ctx, s.processEvent)
}

// processEvent は 1 イベントを処理する。戻り値 nil = ack、非 nil = nack。
func (s *FactionPurchasedSubscriber) processEvent(_ context.Context, data []byte) error {
	var ev apishop.FactionPurchasedEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		slog.Error("faction-purchased subscriber: bad payload (nack)",
			"error", err, "payload_len", len(data))
		return fmt.Errorf("faction-purchased: bad payload: %w", err)
	}
	if ev.EventType != apishop.EventTypeFactionPurchased {
		slog.Warn("faction-purchased subscriber: unknown event_type, acking",
			"event_type", ev.EventType, "event_id", ev.EventID)
		return nil
	}
	if ev.PlayerID == "" {
		slog.Error("faction-purchased subscriber: missing player_id (nack)",
			"event_id", ev.EventID)
		return fmt.Errorf("faction-purchased: missing player_id")
	}
	s.pusher.SendToPlayer(ev.PlayerID, &WSMessage{
		Type: genws.WSServerMsgFactionPurchaseComplete,
		Data: data,
	})
	return nil
}

// PremiumUpdatedSubscriber は premium-updated subscription からイベントを取得し、
// premium 状態変化を WS `premium_update_complete` メッセージとして push します。
type PremiumUpdatedSubscriber struct {
	stream port.MessageStream
	pusher WSPusher
}

// NewPremiumUpdatedSubscriber は PremiumUpdatedSubscriber を生成します。
func NewPremiumUpdatedSubscriber(stream port.MessageStream, pusher WSPusher) *PremiumUpdatedSubscriber {
	return &PremiumUpdatedSubscriber{stream: stream, pusher: pusher}
}

// Run は ctx がキャンセルされるか stream がエラーを返すまでブロックします。
func (s *PremiumUpdatedSubscriber) Run(ctx context.Context) error {
	slog.Info("premium-updated subscriber: consuming")
	return s.stream.Consume(ctx, s.processEvent)
}

// processEvent は 1 イベントを処理する。戻り値 nil = ack、非 nil = nack。
func (s *PremiumUpdatedSubscriber) processEvent(_ context.Context, data []byte) error {
	var ev apishop.PremiumUpdatedEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		slog.Error("premium-updated subscriber: bad payload (nack)",
			"error", err, "payload_len", len(data))
		return fmt.Errorf("premium-updated: bad payload: %w", err)
	}
	if ev.EventType != apishop.EventTypePremiumUpdated {
		slog.Warn("premium-updated subscriber: unknown event_type, acking",
			"event_type", ev.EventType, "event_id", ev.EventID)
		return nil
	}
	if ev.PlayerID == "" {
		slog.Error("premium-updated subscriber: missing player_id (nack)",
			"event_id", ev.EventID)
		return fmt.Errorf("premium-updated: missing player_id")
	}
	s.pusher.SendToPlayer(ev.PlayerID, &WSMessage{
		Type: genws.WSServerMsgPremiumUpdateComplete,
		Data: data,
	})
	return nil
}

package pubsub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	apimatchmaking "github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

var _ port.PushMessageProcessor = (*MatchSubscriber)(nil)

// MatchSubscriber は match_made イベントを受信し port.MatchEventHandler 経由で
// ディスパッチします。matchId ごとの重複排除は handler (port.ProcessedMatchRepo
// を持つ実装) が永続化して担うため、ここでは行いません。
type MatchSubscriber struct {
	handler port.MatchEventHandler
}

// NewMatchSubscriber は port.PushMessageProcessor を満たす MatchSubscriber を生成します。
func NewMatchSubscriber(handler port.MatchEventHandler) (*MatchSubscriber, error) {
	if handler == nil {
		return nil, errors.New("matchsubscriber: handler is nil")
	}
	return &MatchSubscriber{handler: handler}, nil
}

// ProcessMessage は match_made イベントを JSON デコードし、対象外の event_type を
// 除いて port.MatchEventHandler へディスパッチする。
func (s *MatchSubscriber) ProcessMessage(ctx context.Context, data []byte) error {
	var event apimatchmaking.MatchMadeEvent
	if err := json.Unmarshal(data, &event); err != nil {
		slog.Error("matchsubscriber: bad payload (nack)", "error", err, "payload_len", len(data))
		return fmt.Errorf("matchsubscriber: bad payload: %w", err)
	}
	if event.EventType != apimatchmaking.EventTypeMatchMade {
		slog.Warn("matchsubscriber: unknown event type, acking", "event_type", event.EventType)
		return nil
	}

	if err := s.handler.HandleMatchMade(ctx, event); err != nil {
		slog.Error("matchsubscriber: handler failed",
			"match_id", event.MatchID, "error", err)
		return fmt.Errorf("matchsubscriber: handler: %w", err)
	}
	return nil
}

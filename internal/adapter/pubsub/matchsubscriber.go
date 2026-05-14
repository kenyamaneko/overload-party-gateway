package pubsub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	apimatchmaking "github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// playerIDList はログメッセージ用にプレイヤー ID をフォーマットする。
func playerIDList(players []apimatchmaking.MatchedPlayer) string {
	ids := make([]string, 0, len(players))
	for _, p := range players {
		ids = append(ids, p.PlayerID)
	}
	return "[" + strings.Join(ids, ",") + "]"
}

// MatchSubscriber は match_made イベントを受信し port.MatchEventHandler 経由で
// ディスパッチします。Pod-local な matchId 重複排除 map を保持し、Exactly-Once
// Delivery で ack 済みメッセージが再配信されないことを前提に Pod 単位のみで dedup する。
type MatchSubscriber struct {
	stream         port.MessageStream
	handler        port.MatchEventHandler
	processedMu    sync.Mutex
	processedMatch map[string]struct{}
}

// NewMatchSubscriber は MessageStream から pull する subscriber を生成します。
func NewMatchSubscriber(stream port.MessageStream, handler port.MatchEventHandler) (*MatchSubscriber, error) {
	if handler == nil {
		return nil, errors.New("matchsubscriber: handler is nil")
	}
	return &MatchSubscriber{
		stream:         stream,
		handler:        handler,
		processedMatch: make(map[string]struct{}),
	}, nil
}

// Run は ctx がキャンセルされるか stream がエラーを返すまでブロックします。
func (s *MatchSubscriber) Run(ctx context.Context) error {
	slog.Info("matchsubscriber: consuming")
	return s.stream.Consume(ctx, s.processEvent)
}

// processEvent は 1 メッセージを処理する。戻り値 nil = ack、非 nil = nack。
func (s *MatchSubscriber) processEvent(ctx context.Context, data []byte) error {
	var event apimatchmaking.MatchMadeEvent
	if err := json.Unmarshal(data, &event); err != nil {
		slog.Error("matchsubscriber: bad payload (nack)", "error", err, "payload_len", len(data))
		return fmt.Errorf("matchsubscriber: bad payload: %w", err)
	}
	if event.EventType != apimatchmaking.EventTypeMatchMade {
		slog.Warn("matchsubscriber: unknown event type, acking", "event_type", event.EventType)
		return nil
	}

	if !s.markProcessed(event.MatchID) {
		slog.Info("matchsubscriber: duplicate matchId, acking without re-handling",
			"match_id", event.MatchID)
		return nil
	}

	if err := s.handler.HandleMatchMade(ctx, event); err != nil {
		slog.Error("matchsubscriber: handler failed",
			"match_id", event.MatchID, "players", playerIDList(event.Players), "error", err)
		s.unmark(event.MatchID)
		return fmt.Errorf("matchsubscriber: handler: %w", err)
	}
	return nil
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

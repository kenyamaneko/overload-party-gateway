package pubsub

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	apimatchmaking "github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking"
	"github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking/apimatchmakingfake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeMatchHandler は port.MatchEventHandler のテスト用スタブ。
// 受信した event と error を記録し、テストから振る舞いを制御できる。
type fakeMatchHandler struct {
	mu       sync.Mutex
	received []apimatchmaking.MatchMadeEvent
	err      error
}

func (h *fakeMatchHandler) HandleMatchMade(_ context.Context, event apimatchmaking.MatchMadeEvent) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.received = append(h.received, event)
	return h.err
}

func (h *fakeMatchHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.received)
}

// TestMatchSubscriber_Consumes は「matchmaking が publish した MatchMadeEvent を
// gateway が Start() → stream.Consume → processEvent の経路で適切に ack/nack
// 分岐する」仕様を、matchmaking 送信側 fake 経由で固定する。
func TestMatchSubscriber_Consumes(t *testing.T) {
	validPlayers := []apimatchmaking.MatchedPlayer{
		{PlayerID: "p-1", DeckID: 10},
		{PlayerID: "p-2", DeckID: 20},
	}

	tests := []struct {
		name          string
		publish       func(ctx context.Context, pub *apimatchmakingfake.Publisher, broker *apimatchmakingfake.Broker)
		handlerErr    error
		wantAck       bool
		assertHandler func(t *testing.T, h *fakeMatchHandler)
	}{
		{
			name: "正常系: handler に委譲して ACK",
			publish: func(ctx context.Context, pub *apimatchmakingfake.Publisher, _ *apimatchmakingfake.Broker) {
				_ = apimatchmakingfake.PublishMatchMade(ctx, pub, apimatchmaking.MatchMadeEvent{
					MatchID: "mch_1",
					Players: validPlayers,
				})
			},
			wantAck: true,
			assertHandler: func(t *testing.T, h *fakeMatchHandler) {
				require.Equal(t, 1, h.count())
				assert.Equal(t, "mch_1", h.received[0].MatchID)
				assert.Equal(t, apimatchmaking.EventTypeMatchMade, h.received[0].Type)
				require.Len(t, h.received[0].Players, 2)
				assert.Equal(t, "p-1", h.received[0].Players[0].PlayerID)
			},
		},
		{
			name: "不正 JSON: 握りつぶさず NACK (handler 未呼び出し)",
			publish: func(_ context.Context, _ *apimatchmakingfake.Publisher, broker *apimatchmakingfake.Broker) {
				broker.Publish(apimatchmaking.TopicMatchmakingEvents, []byte("not-json"))
			},
			wantAck: false,
			assertHandler: func(t *testing.T, h *fakeMatchHandler) {
				assert.Zero(t, h.count())
			},
		},
		{
			name: "未知の event type: 責務外として ACK (handler 未呼び出し)",
			publish: func(_ context.Context, _ *apimatchmakingfake.Publisher, broker *apimatchmakingfake.Broker) {
				payload, _ := json.Marshal(apimatchmaking.MatchMadeEvent{
					Type:    "unknown",
					MatchID: "mch_unk",
					Players: validPlayers,
				})
				broker.Publish(apimatchmaking.TopicMatchmakingEvents, payload)
			},
			wantAck: true,
			assertHandler: func(t *testing.T, h *fakeMatchHandler) {
				assert.Zero(t, h.count(), "event_type フィルタで handler に到達しない")
			},
		},
		{
			name: "handler 失敗: NACK でリトライ (dedup map から unmark 済み)",
			publish: func(ctx context.Context, pub *apimatchmakingfake.Publisher, _ *apimatchmakingfake.Broker) {
				_ = apimatchmakingfake.PublishMatchMade(ctx, pub, apimatchmaking.MatchMadeEvent{
					MatchID: "mch_fail",
					Players: validPlayers,
				})
			},
			handlerErr: errors.New("handler failed"),
			wantAck:    false,
			assertHandler: func(t *testing.T, h *fakeMatchHandler) {
				assert.Equal(t, 1, h.count(), "handler は呼ばれた")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			broker := apimatchmakingfake.NewBroker()
			pub := apimatchmakingfake.NewPublisher(broker)
			stream := apimatchmakingfake.NewStream(
				apimatchmakingfake.NewSubscriber(broker),
				apimatchmaking.TopicMatchmakingEvents,
			)

			handler := &fakeMatchHandler{err: tt.handlerErr}
			sub, err := NewMatchSubscriber(stream, handler)
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			started := make(chan struct{})
			go func() {
				close(started)
				_ = sub.Run(ctx)
			}()
			<-started

			tt.publish(ctx, pub, broker)

			handlerErr := stream.ExpectHandled(t, time.Second)
			assert.Equal(t, tt.wantAck, handlerErr == nil, "ack 判定 (nil=ack, err=%v)", handlerErr)

			tt.assertHandler(t, handler)
		})
	}
}

// 同一 matchId が 2 回届いた場合、2 回目は Pod-local dedup map により handler に
// 到達せず ACK される (matchmaking の Exactly-Once Delivery が万一破綻した場合の
// Pod 単位の safety net)。
func TestMatchSubscriber_DeduplicatesSameMatchID(t *testing.T) {
	broker := apimatchmakingfake.NewBroker()
	pub := apimatchmakingfake.NewPublisher(broker)
	stream := apimatchmakingfake.NewStream(
		apimatchmakingfake.NewSubscriber(broker),
		apimatchmaking.TopicMatchmakingEvents,
	)
	handler := &fakeMatchHandler{}
	sub, err := NewMatchSubscriber(stream, handler)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	go func() {
		close(started)
		_ = sub.Run(ctx)
	}()
	<-started

	ev := apimatchmaking.MatchMadeEvent{
		MatchID: "mch_dup",
		Players: []apimatchmaking.MatchedPlayer{
			{PlayerID: "p-1", DeckID: 10},
			{PlayerID: "p-2", DeckID: 20},
		},
	}
	require.NoError(t, apimatchmakingfake.PublishMatchMade(ctx, pub, ev))
	require.NoError(t, stream.ExpectHandled(t, time.Second))
	require.NoError(t, apimatchmakingfake.PublishMatchMade(ctx, pub, ev))
	require.NoError(t, stream.ExpectHandled(t, time.Second))

	assert.Equal(t, 1, handler.count(), "重複 matchId は Pod-local dedup で 1 回のみ handler に届く")
}

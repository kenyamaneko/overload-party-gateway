package pubsub

import (
	"context"
	"testing"
	"time"

	apiscenario "github.com/kenyamaneko/overload-party-scenario/packages/api-scenario"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

// TestPlayerOnboardedSubscriber_ProcessEvent は
// 「player-onboarded topic で受信した完了通知を WS onboarding_complete メッセージに
// 変換してプレイヤーに push する。ただし payload 不正は握りつぶさず NACK し、
// 責務外 (未知 event_type) は ACK で drop する」という ADR-022 の仕様を固定する。
func TestPlayerOnboardedSubscriber_ProcessEvent(t *testing.T) {
	validEvent := apiscenario.PlayerOnboardedEvent{
		EventType:        apiscenario.EventTypePlayerOnboarded,
		EventID:          "11111111-1111-1111-1111-111111111111",
		Timestamp:        time.Now().UTC(),
		PlayerID:         "p-1",
		DisplayName:      "name-1",
		InitialFactionID: "SHE",
	}
	validPayload := mustMarshal(t, validEvent)

	tests := []struct {
		name     string
		payload  []byte
		wantAck  bool
		assertFn func(t *testing.T, pusher *fakeWSPusher)
	}{
		{
			name:    "正常系: 接続中プレイヤーに onboarding_complete を push して ACK",
			payload: validPayload,
			wantAck: true,
			assertFn: func(t *testing.T, pusher *fakeWSPusher) {
				require.Len(t, pusher.sent, 1)
				assert.Equal(t, "p-1", pusher.sent[0].PlayerID)
				assert.Equal(t, genws.WSServerMsgOnboardingComplete, pusher.sent[0].Msg.Type)
				assert.JSONEq(t, string(validPayload), string(pusher.sent[0].Msg.Data))
			},
		},
		{
			name:    "不正 JSON: 握りつぶさず NACK。push も発火しない",
			payload: []byte("{broken"),
			wantAck: false,
			assertFn: func(t *testing.T, pusher *fakeWSPusher) {
				assert.Empty(t, pusher.sent)
			},
		},
		{
			name: "未知の event_type: 責務外として ACK (副作用なし)",
			payload: mustMarshal(t, apiscenario.PlayerOnboardedEvent{
				EventType: "unknown",
				EventID:   "22222222-2222-2222-2222-222222222222",
				PlayerID:  "p-2",
			}),
			wantAck: true,
			assertFn: func(t *testing.T, pusher *fakeWSPusher) {
				assert.Empty(t, pusher.sent)
			},
		},
		{
			name: "player_id 欠落: ペイロード仕様違反として NACK",
			payload: mustMarshal(t, apiscenario.PlayerOnboardedEvent{
				EventType:        apiscenario.EventTypePlayerOnboarded,
				EventID:          "33333333-3333-3333-3333-333333333333",
				DisplayName:      "anon",
				InitialFactionID: "Tenki",
			}),
			wantAck: false,
			assertFn: func(t *testing.T, pusher *fakeWSPusher) {
				assert.Empty(t, pusher.sent)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pusher := &fakeWSPusher{}
			s := &PlayerOnboardedSubscriber{pusher: pusher}
			ack := s.processEvent(context.Background(), tt.payload)
			assert.Equal(t, tt.wantAck, ack)
			tt.assertFn(t, pusher)
		})
	}
}

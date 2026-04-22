package pubsub

import (
	"context"
	"testing"
	"time"

	apishop "github.com/kenyamaneko/overload-party-shop/packages/api-shop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

// TestPremiumUpdatedSubscriber_ProcessEvent は
// 「premium-updated topic で受信した状態変化を WS premium_update_complete に
// 変換してプレイヤーに push する」仕様 (ADR-022) を固定する。
func TestPremiumUpdatedSubscriber_ProcessEvent(t *testing.T) {
	expiresAt := time.Now().Add(30 * 24 * time.Hour).UTC()
	validEvent := apishop.PremiumUpdatedEvent{
		EventType:        apishop.EventTypePremiumUpdated,
		EventID:          "11111111-1111-1111-1111-111111111111",
		Timestamp:        time.Now().UTC(),
		PlayerID:         "p-1",
		IsPremium:        true,
		PremiumExpiresAt: &expiresAt,
		Source:           apishop.PremiumUpdatedSourceShop,
	}
	validPayload := mustMarshal(t, validEvent)

	tests := []struct {
		name     string
		payload  []byte
		wantAck  bool
		assertFn func(t *testing.T, pusher *fakeWSPusher)
	}{
		{
			name:    "正常系: 接続中プレイヤーに premium_update_complete を push して ACK",
			payload: validPayload,
			wantAck: true,
			assertFn: func(t *testing.T, pusher *fakeWSPusher) {
				require.Len(t, pusher.sent, 1)
				assert.Equal(t, "p-1", pusher.sent[0].PlayerID)
				assert.Equal(t, genws.WSServerMsgPremiumUpdateComplete, pusher.sent[0].Msg.Type)
				assert.JSONEq(t, string(validPayload), string(pusher.sent[0].Msg.Data))
			},
		},
		{
			name:    "不正 JSON: 握りつぶさず NACK。push も発火しない",
			payload: []byte("<xml/>"),
			wantAck: false,
			assertFn: func(t *testing.T, pusher *fakeWSPusher) {
				assert.Empty(t, pusher.sent)
			},
		},
		{
			name: "未知の event_type: 責務外として ACK",
			payload: mustMarshal(t, apishop.PremiumUpdatedEvent{
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
			payload: mustMarshal(t, apishop.PremiumUpdatedEvent{
				EventType: apishop.EventTypePremiumUpdated,
				EventID:   "33333333-3333-3333-3333-333333333333",
				IsPremium: false,
				Source:    apishop.PremiumUpdatedSourceShop,
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
			s := &PremiumUpdatedSubscriber{pusher: pusher}
			ack := s.processEvent(context.Background(), tt.payload)
			assert.Equal(t, tt.wantAck, ack)
			tt.assertFn(t, pusher)
		})
	}
}

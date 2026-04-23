package pubsub

import (
	"context"
	"testing"
	"time"

	apishop "github.com/kenyamaneko/overload-party-shop/packages/api-shop"
	"github.com/kenyamaneko/overload-party-shop/packages/api-shop/apishopfake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

// TestFactionPurchasedSubscriber_Consumes は「faction-purchased topic で受信した
// 購入完了を WS faction_purchase_complete に変換してプレイヤーに push する」仕様を
// Start() → stream.Consume → processEvent の経路で固定する。
func TestFactionPurchasedSubscriber_Consumes(t *testing.T) {
	tests := []struct {
		name         string
		publish      func(ctx context.Context, pub *apishopfake.Publisher, broker *apishopfake.Broker)
		wantAck      bool
		assertPusher func(t *testing.T, pusher *fakeWSPusher)
	}{
		{
			name: "正常系: 接続中プレイヤーに faction_purchase_complete を push して ACK",
			publish: func(ctx context.Context, pub *apishopfake.Publisher, _ *apishopfake.Broker) {
				_ = apishopfake.PublishFactionPurchased(ctx, pub, apishop.FactionPurchasedEvent{
					PlayerID: "p-1",
					Faction:  "SHE",
				})
			},
			wantAck: true,
			assertPusher: func(t *testing.T, pusher *fakeWSPusher) {
				require.Len(t, pusher.sent, 1)
				assert.Equal(t, "p-1", pusher.sent[0].PlayerID)
				assert.Equal(t, genws.WSServerMsgFactionPurchaseComplete, pusher.sent[0].Msg.Type)
			},
		},
		{
			name: "不正 JSON: 握りつぶさず NACK",
			publish: func(_ context.Context, _ *apishopfake.Publisher, broker *apishopfake.Broker) {
				broker.Publish(apishop.TopicFactionPurchased, []byte("not-json"))
			},
			wantAck: false,
			assertPusher: func(t *testing.T, pusher *fakeWSPusher) {
				assert.Empty(t, pusher.sent)
			},
		},
		{
			name: "未知の event_type: 責務外として ACK",
			publish: func(_ context.Context, _ *apishopfake.Publisher, broker *apishopfake.Broker) {
				broker.Publish(apishop.TopicFactionPurchased, mustMarshal(t, apishop.FactionPurchasedEvent{
					EventType: "unknown",
					EventID:   "22222222-2222-2222-2222-222222222222",
					PlayerID:  "p-2",
					Faction:   "Tenki",
				}))
			},
			wantAck: true,
			assertPusher: func(t *testing.T, pusher *fakeWSPusher) {
				assert.Empty(t, pusher.sent)
			},
		},
		{
			name: "player_id 欠落: ペイロード仕様違反として NACK",
			publish: func(_ context.Context, _ *apishopfake.Publisher, broker *apishopfake.Broker) {
				broker.Publish(apishop.TopicFactionPurchased, mustMarshal(t, apishop.FactionPurchasedEvent{
					EventType: apishop.EventTypeFactionPurchased,
					EventID:   "33333333-3333-3333-3333-333333333333",
					Faction:   "Sugar",
				}))
			},
			wantAck: false,
			assertPusher: func(t *testing.T, pusher *fakeWSPusher) {
				assert.Empty(t, pusher.sent)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			broker := apishopfake.NewBroker()
			pub := apishopfake.NewPublisher(broker)
			stream := apishopfake.NewStream(apishopfake.NewSubscriber(broker), apishop.TopicFactionPurchased)

			pusher := &fakeWSPusher{}
			sub := NewFactionPurchasedSubscriber(stream, pusher)

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
			tt.assertPusher(t, pusher)
		})
	}
}

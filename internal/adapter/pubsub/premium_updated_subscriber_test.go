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

// TestPremiumUpdatedSubscriber_Consumes は「premium-updated topic で受信した
// premium 状態変化を WS premium_update_complete に変換して push する」仕様を
// Start() → stream.Consume → processEvent の経路で固定する。
func TestPremiumUpdatedSubscriber_Consumes(t *testing.T) {
	tests := []struct {
		name         string
		publish      func(ctx context.Context, pub *apishopfake.Publisher, broker *apishopfake.Broker)
		wantAck      bool
		assertPusher func(t *testing.T, pusher *fakeWSPusher)
	}{
		{
			name: "正常系: 接続中プレイヤーに premium_update_complete を push して ACK",
			publish: func(ctx context.Context, pub *apishopfake.Publisher, _ *apishopfake.Broker) {
				_ = apishopfake.PublishPremiumUpdated(ctx, pub, apishop.PremiumUpdatedEvent{
					PlayerID:  "p-1",
					IsPremium: true,
				})
			},
			wantAck: true,
			assertPusher: func(t *testing.T, pusher *fakeWSPusher) {
				require.Len(t, pusher.sent, 1)
				assert.Equal(t, "p-1", pusher.sent[0].PlayerID)
				assert.Equal(t, genws.WSServerMsgPremiumUpdateComplete, pusher.sent[0].Msg.Type)
			},
		},
		{
			name: "不正 JSON: 握りつぶさず NACK",
			publish: func(_ context.Context, _ *apishopfake.Publisher, broker *apishopfake.Broker) {
				broker.Publish(apishop.TopicPremiumUpdated, []byte("not-json"))
			},
			wantAck: false,
			assertPusher: func(t *testing.T, pusher *fakeWSPusher) {
				assert.Empty(t, pusher.sent)
			},
		},
		{
			name: "未知の event_type: 責務外として ACK",
			publish: func(_ context.Context, _ *apishopfake.Publisher, broker *apishopfake.Broker) {
				broker.Publish(apishop.TopicPremiumUpdated, mustMarshal(t, apishop.PremiumUpdatedEvent{
					EventType: "unknown",
					EventID:   "22222222-2222-2222-2222-222222222222",
					PlayerID:  "p-2",
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
				broker.Publish(apishop.TopicPremiumUpdated, mustMarshal(t, apishop.PremiumUpdatedEvent{
					EventType: apishop.EventTypePremiumUpdated,
					EventID:   "33333333-3333-3333-3333-333333333333",
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
			stream := apishopfake.NewStream(apishopfake.NewSubscriber(broker), apishop.TopicPremiumUpdated)

			pusher := &fakeWSPusher{}
			sub := NewPremiumUpdatedSubscriber(stream, pusher)

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

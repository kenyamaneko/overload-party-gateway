package pubsub

import (
	"context"
	"testing"
	"time"

	apiscenario "github.com/kenyamaneko/overload-party-scenario/packages/api-scenario"
	"github.com/kenyamaneko/overload-party-scenario/packages/api-scenario/apiscenariofake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

// TestPlayerOnboardedSubscriber_Consumes は「player-onboarded topic で受信した
// オンボーディング完了を WS onboarding_complete に変換して push する」仕様を
// Start() → stream.Consume → processEvent の経路で固定する。
func TestPlayerOnboardedSubscriber_Consumes(t *testing.T) {
	tests := []struct {
		name         string
		publish      func(ctx context.Context, pub *apiscenariofake.Publisher, broker *apiscenariofake.Broker)
		wantAck      bool
		assertPusher func(t *testing.T, pusher *fakeWSPusher)
	}{
		{
			name: "正常系: 接続中プレイヤーに onboarding_complete を push して ACK",
			publish: func(ctx context.Context, pub *apiscenariofake.Publisher, _ *apiscenariofake.Broker) {
				_ = apiscenariofake.PublishPlayerOnboarded(ctx, pub, apiscenario.PlayerOnboardedEvent{
					PlayerID:         "p-1",
					InitialFactionID: "SHE",
				})
			},
			wantAck: true,
			assertPusher: func(t *testing.T, pusher *fakeWSPusher) {
				require.Len(t, pusher.sent, 1)
				assert.Equal(t, "p-1", pusher.sent[0].PlayerID)
				assert.Equal(t, genws.WSServerMsgOnboardingComplete, pusher.sent[0].Msg.Type)
			},
		},
		{
			name: "不正 JSON: 握りつぶさず NACK",
			publish: func(_ context.Context, _ *apiscenariofake.Publisher, broker *apiscenariofake.Broker) {
				broker.Publish(apiscenario.TopicPlayerOnboarded, []byte("not-json"))
			},
			wantAck: false,
			assertPusher: func(t *testing.T, pusher *fakeWSPusher) {
				assert.Empty(t, pusher.sent)
			},
		},
		{
			name: "未知の event_type: 責務外として ACK",
			publish: func(_ context.Context, _ *apiscenariofake.Publisher, broker *apiscenariofake.Broker) {
				broker.Publish(apiscenario.TopicPlayerOnboarded, mustMarshal(t, apiscenario.PlayerOnboardedEvent{
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
			publish: func(_ context.Context, _ *apiscenariofake.Publisher, broker *apiscenariofake.Broker) {
				broker.Publish(apiscenario.TopicPlayerOnboarded, mustMarshal(t, apiscenario.PlayerOnboardedEvent{
					EventType:        apiscenario.EventTypePlayerOnboarded,
					EventID:          "33333333-3333-3333-3333-333333333333",
					InitialFactionID: "Tenki",
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
			broker := apiscenariofake.NewBroker()
			pub := apiscenariofake.NewPublisher(broker)
			stream := apiscenariofake.NewStream(apiscenariofake.NewSubscriber(broker), apiscenario.TopicPlayerOnboarded)

			pusher := &fakeWSPusher{}
			sub := NewPlayerOnboardedSubscriber(stream, pusher)

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

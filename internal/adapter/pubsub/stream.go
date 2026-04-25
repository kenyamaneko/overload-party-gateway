package pubsub

import (
	"context"
	"errors"
	"fmt"

	"cloud.google.com/go/pubsub/v2"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// Stream は Cloud Pub/Sub subscription を port.MessageStream として
// 露出する adapter。cloud.google.com/go/pubsub/v2 の SDK 型依存は本 adapter に
// 閉じ、subscriber 本体 (processEvent ロジック) は SDK を知らなくて済む。
type Stream struct {
	client     *pubsub.Client
	subscriber *pubsub.Subscriber
}

// NewStream は projectID / subscriptionID に接続した Stream を返す。
// Close() 呼び出しまで内部 *pubsub.Client が保持されるため、main.go 側で defer Close が必須。
func NewStream(ctx context.Context, projectID, subscriptionID string) (*Stream, error) {
	if projectID == "" || subscriptionID == "" {
		return nil, errors.New("pubsub: projectID and subscriptionID are required")
	}
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("pubsub: new client: %w", err)
	}
	return &Stream{
		client:     client,
		subscriber: client.Subscriber(subscriptionID),
	}, nil
}

// Consume は subscription からメッセージを取得し handler に渡す。
// handler が nil を返したら Ack、非 nil を返したら Nack する。
// ctx キャンセル時は Receive が nil を返すため、Consume も nil で終了する。
func (s *Stream) Consume(ctx context.Context, handler port.MessageHandler) error {
	return s.subscriber.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
		if err := handler(ctx, msg.Data); err != nil {
			msg.Nack()
			return
		}
		msg.Ack()
	})
}

// Close は Cloud Pub/Sub client を閉じる。
func (s *Stream) Close() error {
	return s.client.Close()
}

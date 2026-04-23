package pubsub

import (
	"context"
	"errors"
	"fmt"

	"cloud.google.com/go/pubsub/v2"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// GCPMessageStream は GCP Pub/Sub subscription を port.MessageStream として
// 露出する adapter。cloud.google.com/go/pubsub/v2 の SDK 型依存は本 adapter に
// 閉じ、subscriber 本体 (processEvent ロジック) は SDK を知らなくて済む。
type GCPMessageStream struct {
	client     *pubsub.Client
	subscriber *pubsub.Subscriber
}

// NewGCPMessageStream は projectID / subscriptionID に接続した GCPMessageStream を返す。
// Close() 呼び出しまで内部 *pubsub.Client が保持されるため、main.go 側で defer Close が必須。
func NewGCPMessageStream(ctx context.Context, projectID, subscriptionID string) (*GCPMessageStream, error) {
	if projectID == "" || subscriptionID == "" {
		return nil, errors.New("GCPMessageStream: projectID and subscriptionID are required")
	}
	client, err := pubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("GCPMessageStream: new client: %w", err)
	}
	return &GCPMessageStream{
		client:     client,
		subscriber: client.Subscriber(subscriptionID),
	}, nil
}

// Consume は subscription からメッセージを取得し handler に渡す。
// handler が nil を返したら Ack、非 nil を返したら Nack する。
// ctx キャンセル時は Receive が nil を返すため、Consume も nil で終了する。
func (s *GCPMessageStream) Consume(ctx context.Context, handler port.MessageHandler) error {
	return s.subscriber.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
		if err := handler(ctx, msg.Data); err != nil {
			msg.Nack()
			return
		}
		msg.Ack()
	})
}

// Close は GCP Pub/Sub client を閉じる。
func (s *GCPMessageStream) Close() error {
	return s.client.Close()
}

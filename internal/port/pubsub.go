package port

import "context"

// PushMessageProcessor は Pub/Sub push 配信で受信した 1 メッセージのペイロード
// (base64 復号済みバイト列) を処理するインターフェースです。gateway は Cloud
// Pub/Sub SDK の型に直接依存せず、本 interface を通して配信 HTTP エンドポイント
// から adapter 層のディスパッチ処理を呼び出します。
type PushMessageProcessor interface {
	ProcessMessage(ctx context.Context, data []byte) error
}

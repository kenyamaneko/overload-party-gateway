// Package pubsub は gateway の Pub/Sub イベントディスパッチを管理する。
//
// gateway は matchmaking サービスからの match 成立通知を、HTTP push 配信の
// エンドポイント (internal/handler/rest) 経由で受け取り、該当プレイヤーの
// WS 接続があれば push する。Pub/Sub はサービス間連携専用とし、他サービスが
// publish するイベントを subscribe する fan-out 配線は持たない。
package pubsub

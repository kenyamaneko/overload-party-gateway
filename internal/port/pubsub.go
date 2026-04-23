package port

import "context"

// MessageHandler は 1 メッセージの処理結果を ack / nack の形で返す。
// nil = ack (正常処理 or 責務外として ACK できるケース)、非 nil = nack
// (ペイロード不正や副作用失敗で再配信を要するケース)。
//
// type alias として定義することで、同形シグネチャを持つ fake 側の Consume
// メソッド (apishopfake.Stream 等) が追加 adapter なしで MessageStream を
// 直接満たせる。
type MessageHandler = func(ctx context.Context, data []byte) error

// MessageStream は Pub/Sub subscription の抽象境界。gateway は GCP Pub/Sub
// SDK の型に直接依存せず、本 interface を通してメッセージを受け取る
// (Clean Architecture の adapter 層ポート)。
//
// Consume は ctx がキャンセルされるまで handler をメッセージ毎に呼び出し、
// 戻り値で ack / nack 制御する。ctx キャンセルは正常終了扱いで nil を返す
// 契約。
type MessageStream interface {
	Consume(ctx context.Context, handler MessageHandler) error
}

package ws

import (
	"encoding/json"
	"fmt"

	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

// mustMarshal は内部で構築した値を JSON 化する。
// 内部構造体の Marshal が失敗するのは設定ミス（unsupported な型をフィールドに含む等）に
// 起因する純粋なバグなので fail-fast にする。silent な nil 返却はクライアントに不正な
// メッセージを送り続ける原因になるため避ける。
func mustMarshal(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("BUG: mustMarshal failed: %v (type: %T)", err, v))
	}
	return data
}

// sendError は単一接続にエラーメッセージを送信する。
func sendError(conn *Connection, code, message string) {
	conn.SendMessage(&WSMessage{
		Type: genws.WSServerMsgError,
		Data: mustMarshal(ErrorMessage{ErrorCode: code, Message: message}),
	})
}

// sendErrorToPlayer は hub 経由でプレイヤーにエラーメッセージを送信する。
func sendErrorToPlayer(hub *ConnectionHub, playerID, code, message string) {
	hub.SendToPlayer(playerID, &WSMessage{
		Type: genws.WSServerMsgError,
		Data: mustMarshal(ErrorMessage{ErrorCode: code, Message: message}),
	})
}

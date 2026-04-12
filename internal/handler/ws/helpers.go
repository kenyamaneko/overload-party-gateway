package ws

import (
	"encoding/json"
	"log"

	"github.com/kenyamaneko/overload-party-gateway/internal/constants"
)

func mustMarshal(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("BUG: mustMarshal failed: %v (type: %T)", err, v)
		return nil
	}
	return data
}

// sendError は単一接続にエラーメッセージを送信する。
func sendError(conn *Connection, code, message string, retryable bool) {
	conn.SendMessage(&WSMessage{
		Type: constants.WSMsgError,
		Data: mustMarshal(ErrorMessage{Code: code, Message: message, Retryable: retryable}),
	})
}

// sendErrorToPlayer は hub 経由でプレイヤーにエラーメッセージを送信する。
func sendErrorToPlayer(hub *ConnectionHub, playerID, code, message string, retryable bool) {
	hub.SendToPlayer(playerID, &WSMessage{
		Type: constants.WSMsgError,
		Data: mustMarshal(ErrorMessage{Code: code, Message: message, Retryable: retryable}),
	})
}

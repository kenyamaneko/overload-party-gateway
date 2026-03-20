package ws

import (
	"encoding/json"

	"github.com/kenyamaneko/overload-party-gateway/internal/constants"
)

// mustMarshal marshals v to JSON, returning nil on error.
// Safe to use for well-known struct types that cannot fail to marshal.
func mustMarshal(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

// sendError sends an error message to a single connection.
func sendError(conn *Connection, code, message string, retryable bool) {
	conn.SendMessage(&WSMessage{
		Type: constants.WSMsgError,
		Data: mustMarshal(ErrorMessage{Code: code, Message: message, Retryable: retryable}),
	})
}

// sendErrorToPlayer sends an error message to a player via the hub.
func sendErrorToPlayer(hub *ConnectionHub, playerID, code, message string, retryable bool) {
	hub.SendToPlayer(playerID, &WSMessage{
		Type: constants.WSMsgError,
		Data: mustMarshal(ErrorMessage{Code: code, Message: message, Retryable: retryable}),
	})
}

// Package ws は gateway WebSocket メッセージタイプの定数を提供する。
//
// data/asyncapi.yaml が channel address (= type 値) の SSoT。
// 本ファイルは asyncapi.yaml に存在する channel に加え、payload を持たない
// envelope のみで完結するメッセージ (例: pong / game_state / turn_controls
// 等の battle 由来 raw JSON のみを data に積むタイプ) も列挙する。
// asyncapi.yaml と本ファイルの drift は drift_test.go で検出される。
package ws

// WS server message types (server → client).
const (
	WSServerMsgGameState            = "game_state"
	WSServerMsgGameOver             = "game_over"
	WSServerMsgError                = "error"
	WSServerMsgGameEntered          = "game_entered"
	WSServerMsgMatchmakingStarted   = "matchmaking_started"
	WSServerMsgMatchmakingCancelled = "matchmaking_cancelled"
	WSServerMsgActionRejected       = "action_rejected"
	WSServerMsgStampUsed            = "stamp_used"
	WSServerMsgPong                 = "pong"
	WSServerMsgMatchFound           = "match_found"
	WSServerMsgActionPerformed      = "action_performed"
	WSServerMsgTurnControls         = "turn_controls"
	WSServerMsgNpcBattleCreated     = "npc_battle_created"
	WSServerMsgOpponentDisconnected = "opponent_disconnected"
	WSServerMsgOpponentReconnected  = "opponent_reconnected"
	WSServerMsgGameStateRestore     = "game_state_restore"
	WSServerMsgServerShutdown       = "server_shutdown"
)

// WS client message types (client → server).
const (
	WSClientMsgGameEnter         = "game_enter"
	WSClientMsgMatchmakingStart  = "matchmaking_start"
	WSClientMsgMatchmakingCancel = "matchmaking_cancel"
	WSClientMsgGameAction        = "game_action"
	WSClientMsgUseStamp          = "use_stamp"
	WSClientMsgPing              = "ping"
	WSClientMsgNpcBattleStart    = "npc_battle_start"
)

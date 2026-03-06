package constants

// WebSocket message type constants (wire format).
// Must match client expectations from overload-party-common.
const (
	// Server → Client
	WSMsgGameState            = "game_state"
	WSMsgGameOver             = "game_over"
	WSMsgError                = "error"
	WSMsgGameEntered          = "game_entered"
	WSMsgMatchmakingStarted   = "matchmaking_started"
	WSMsgMatchmakingCancelled = "matchmaking_cancelled"
	WSMsgActionRejected       = "action_rejected"
	WSMsgStampUsed            = "stamp_used"
	WSMsgPong                 = "pong"
	WSMsgMatchFound           = "match_found"
	WSMsgGameStateRestore     = "game_state_restore"
	WSMsgActionPerformed      = "action_performed"
	WSMsgTurnControls         = "turn_controls"
	WSMsgNpcBattleCreated     = "npc_battle_created"

	// Client → Server
	WSMsgGameEnter         = "game_enter"
	WSMsgMatchmakingStart  = "matchmaking_start"
	WSMsgMatchmakingCancel = "matchmaking_cancel"
	WSMsgGameAction        = "game_action"
	WSMsgUseStamp          = "use_stamp"
	WSMsgPing              = "ping"
	WSMsgNpcBattleStart    = "npc_battle_start"

	// Win reasons
	WinReasonDisconnect = "disconnect"
)

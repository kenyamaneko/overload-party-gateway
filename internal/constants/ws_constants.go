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
	WSMsgActionPerformed      = "action_performed"
	WSMsgTurnControls         = "turn_controls"
	WSMsgNpcBattleCreated        = "npc_battle_created"
	WSMsgOpponentDisconnected    = "opponent_disconnected"
	WSMsgOpponentReconnected     = "opponent_reconnected"

	// Spectate: Server → Client
	WSMsgSpectateJoined         = "spectate_joined"
	WSMsgSpectateError          = "spectate_error"
	WSMsgSpectateUpdate         = "spectate_update"
	WSMsgSpectateEnded          = "spectate_ended"
	WSMsgSpectateStampBroadcast = "spectate_stamp_broadcast"

	// Client → Server
	WSMsgGameEnter         = "game_enter"
	WSMsgMatchmakingStart  = "matchmaking_start"
	WSMsgMatchmakingCancel = "matchmaking_cancel"
	WSMsgGameAction        = "game_action"
	WSMsgUseStamp          = "use_stamp"
	WSMsgPing              = "ping"
	WSMsgNpcBattleStart    = "npc_battle_start"

	// Spectate: Client → Server
	WSMsgSpectateJoin  = "spectate_join"
	WSMsgSpectateLeave = "spectate_leave"
	WSMsgSpectateStamp = "spectate_stamp"

	// Win reasons
	WinReasonDisconnect  = "disconnect"
	WinReasonTurnTimeout = "turn_timeout"
)

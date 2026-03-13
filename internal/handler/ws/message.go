package ws

import "encoding/json"

// WSMessage is the envelope for all WebSocket messages.
type WSMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// --- Client → Server ---

type GameEnterMessage struct {
	GameID string `json:"game_id"`
	DeckID int64  `json:"deck_id"`
}

type MatchmakingStartMessage struct {
	DeckID int64 `json:"deck_id"`
}

type GameActionMessage struct {
	GameID     string          `json:"game_id"`
	ActionType string          `json:"action_type"`
	Data       json.RawMessage `json:"data"`
}

type UseStampMessage struct {
	GameID  string `json:"game_id"`
	StampNo int64  `json:"stamp_no"`
}

type NPCBattleStartMessage struct {
	DeckID     int64  `json:"deck_id"`
	NPCFaction string `json:"npc_faction"`
}

// --- Server → Client ---

type ErrorMessage struct {
	Code      string `json:"error_code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type MatchFoundMessage struct {
	GameID    string `json:"game_id"`
	Player1ID string `json:"player1_id"`
	Player2ID string `json:"player2_id"`
}

type GameOverMessage struct {
	GameID    string `json:"game_id"`
	WinnerNum int64  `json:"winner_num"`
	WinReason string `json:"win_reason"`
}

type ActionRejectedMessage struct {
	GameID     string `json:"game_id"`
	ActionType string `json:"action_type"`
	Reason     string `json:"reason"`
}

type TurnControlsMessage struct {
	CanEndPhase     bool `json:"can_end_phase"`
	DiscardRequired int  `json:"discard_required"`
}

type StampUsedMessage struct {
	GameID   string `json:"game_id"`
	PlayerID string `json:"player_id"`
	StampNo  int64  `json:"stamp_no"`
}

type NPCBattleCreatedMessage struct {
	GameID    string `json:"game_id"`
	Player1ID string `json:"player1_id"`
	Player2ID string `json:"player2_id"`
}

// --- Spectate: Client → Server ---

type SpectateJoinMessage struct {
	GameID string `json:"game_id"`
}

type SpectateLeaveMessage struct {
	GameID string `json:"game_id"`
}

type SpectateStampMessage struct {
	GameID  string `json:"game_id"`
	StampNo int64  `json:"stamp_no"`
}

// --- Spectate: Server → Client ---

type SpectateJoinedMessage struct {
	GameID    string          `json:"game_id"`
	Player1ID string          `json:"player1_id"`
	Player2ID string          `json:"player2_id"`
	State     json.RawMessage `json:"state"`
}

type SpectateErrorMessage struct {
	Code    string `json:"error_code"`
	Message string `json:"message"`
}

type SpectateEndedMessage struct {
	GameID    string `json:"game_id"`
	WinnerNum int64  `json:"winner_num"`
	WinReason string `json:"win_reason"`
}

type SpectateStampBroadcastMessage struct {
	GameID      string `json:"game_id"`
	SpectatorID string `json:"spectator_id"`
	StampNo     int64  `json:"stamp_no"`
}

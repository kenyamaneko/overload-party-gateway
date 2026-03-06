package service

import (
	"context"
	"encoding/json"
)

// BattleClient is the interface for communicating with the battle server REST API.
// Gateway uses this to delegate game creation, action processing, and state retrieval.
type BattleClient interface {
	// JoinQueue adds a player to the matchmaking queue.
	JoinQueue(ctx context.Context, playerID string, deckID int64) error
	// LeaveQueue removes a player from the matchmaking queue.
	LeaveQueue(ctx context.Context, playerID string)
	// StartNPCBattle creates a new NPC game for the player.
	StartNPCBattle(ctx context.Context, playerID string, deckID int64, npcFaction string) (*GameCreatedResult, error)
	// ProcessAction processes a game action and returns the result.
	ProcessAction(ctx context.Context, gameID, playerID, actionType string, data json.RawMessage) (*ActionResult, error)
	// GetGameStateForPlayer returns the info-hidden game state for a specific player.
	GetGameStateForPlayer(ctx context.Context, gameID, playerID string) (json.RawMessage, error)
	// GetTurnControlsForPlayer returns the turn controls for a specific player, or nil if not their turn.
	GetTurnControlsForPlayer(ctx context.Context, gameID, playerID string) (*TurnControls, error)
}

// GameCreatedResult is returned when a new game is created.
type GameCreatedResult struct {
	GameID    string `json:"game_id"`
	Player1ID string `json:"player1_id"`
	Player2ID string `json:"player2_id"`
}

// ActionResult is returned after a game action is processed.
type ActionResult struct {
	GameOver  bool   `json:"game_over"`
	WinnerNum int64  `json:"winner_num"`
	WinReason string `json:"win_reason"`
}

// TurnControls describes available in-game controls for the active player.
type TurnControls struct {
	CanEndPhase     bool `json:"can_end_phase"`
	DiscardRequired int  `json:"discard_required"`
}

// BattleEvent is a server-initiated game event (e.g. battle_start, turn_start).
type BattleEvent struct {
	Type        string
	Turn        int64
	MatchType   string
	Player1Info *BattlePlayerInfo
	Player2Info *BattlePlayerInfo
}

// BattlePlayerInfo carries display info for a battle participant.
type BattlePlayerInfo struct {
	PlayerID string
	Name     string
	Level    int64
}

// MockBattleClient is a no-op implementation of BattleClient for local development.
// Replace with a real HTTP client when connecting to the actual battle server.
type MockBattleClient struct{}

func NewMockBattleClient() *MockBattleClient {
	return &MockBattleClient{}
}

func (m *MockBattleClient) JoinQueue(_ context.Context, _ string, _ int64) error {
	return nil
}

func (m *MockBattleClient) LeaveQueue(_ context.Context, _ string) {}

func (m *MockBattleClient) StartNPCBattle(_ context.Context, playerID string, _ int64, _ string) (*GameCreatedResult, error) {
	return &GameCreatedResult{
		GameID:    "mock-game-id",
		Player1ID: playerID,
		Player2ID: "npc",
	}, nil
}

func (m *MockBattleClient) ProcessAction(_ context.Context, _, _, _ string, _ json.RawMessage) (*ActionResult, error) {
	return &ActionResult{}, nil
}

func (m *MockBattleClient) GetGameStateForPlayer(_ context.Context, _, _ string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (m *MockBattleClient) GetTurnControlsForPlayer(_ context.Context, _, _ string) (*TurnControls, error) {
	return nil, nil
}

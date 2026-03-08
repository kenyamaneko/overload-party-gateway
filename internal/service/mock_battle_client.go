package service

import (
	"context"
	"encoding/json"
)

// MockBattleClient is a no-op implementation of BattleClient for local development and testing.
type MockBattleClient struct{}

func NewMockBattleClient() *MockBattleClient {
	return &MockBattleClient{}
}

func (m *MockBattleClient) StartNPCBattle(_ context.Context, playerID string, _ int64, _ []BattleDeckCard, _ string) (*GameCreatedResult, error) {
	return &GameCreatedResult{
		GameID:    "mock-game-id",
		Player1ID: playerID,
		Player2ID: "npc",
	}, nil
}

func (m *MockBattleClient) CreatePvPGame(_ context.Context, player1ID string, _ int64, _ []BattleDeckCard, player2ID string, _ int64, _ []BattleDeckCard) (*GameCreatedResult, error) {
	return &GameCreatedResult{
		GameID:    "mock-pvp-game-id",
		Player1ID: player1ID,
		Player2ID: player2ID,
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

func (m *MockBattleClient) GetGameLog(_ context.Context, _ string) (json.RawMessage, error) {
	return json.RawMessage(`{"entries":[]}`), nil
}

func (m *MockBattleClient) GetGameLogText(_ context.Context, _ string) ([]byte, error) {
	return []byte("(no game log)"), nil
}

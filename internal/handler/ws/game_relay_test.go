package ws

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/constants"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

// --- Mock BattleClient ---

type mockBattleClient struct {
	// getGameStateForPlayer returns the configured state per (gameID, playerID).
	states map[string]json.RawMessage
	// processActionResult is returned by ProcessAction.
	processActionResult *service.ActionResult
	processActionErr    error
	// turnControls is returned by GetTurnControlsForPlayer.
	turnControls    json.RawMessage
	turnControlsErr error
	// advanceNpcResult is returned by AdvanceNpcTurn.
	advanceNpcResult *service.ActionResult
	advanceNpcErr    error
}

func newMockBattleClient() *mockBattleClient {
	return &mockBattleClient{
		states: make(map[string]json.RawMessage),
	}
}

func stateKey(gameID, playerID string) string { return gameID + ":" + playerID }

func (m *mockBattleClient) SetState(gameID, playerID string, state json.RawMessage) {
	m.states[stateKey(gameID, playerID)] = state
}

func (m *mockBattleClient) GetNPCModels(_ context.Context) (json.RawMessage, error) {
	return json.RawMessage(`[]`), nil
}

func (m *mockBattleClient) StartNPCBattle(_ context.Context, _ string, _ int64, _ []service.BattleDeckCard, _ string) (*service.GameCreatedResult, error) {
	return nil, nil
}

func (m *mockBattleClient) CreatePvPGame(_ context.Context, _ string, _ int64, _ []service.BattleDeckCard, _ string, _ int64, _ []service.BattleDeckCard) (*service.GameCreatedResult, error) {
	return nil, nil
}

func (m *mockBattleClient) ProcessAction(_ context.Context, _, _, _ string, _ json.RawMessage) (*service.ActionResult, error) {
	return m.processActionResult, m.processActionErr
}

func (m *mockBattleClient) GetGameStateForPlayer(_ context.Context, gameID, playerID string) (json.RawMessage, error) {
	s, ok := m.states[stateKey(gameID, playerID)]
	if !ok {
		return json.RawMessage(`{}`), nil
	}
	return s, nil
}

func (m *mockBattleClient) GetTurnControlsForPlayer(_ context.Context, _, _ string) (json.RawMessage, error) {
	return m.turnControls, m.turnControlsErr
}

func (m *mockBattleClient) AdvanceNpcTurn(_ context.Context, _, _ string) (*service.ActionResult, error) {
	return m.advanceNpcResult, m.advanceNpcErr
}

func (m *mockBattleClient) GetGameLog(_ context.Context, _ string) (json.RawMessage, error) {
	return nil, nil
}

func (m *mockBattleClient) GetGameLogText(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}

// --- helpers ---

// newTestRelay creates a GameRelay backed by a noop hub and mock battle client.
func newTestRelay() (*GameRelay, *mockBattleClient) {
	bc := newMockBattleClient()
	hub := NewConnectionHub(HubCallbacks{
		GetGameID:           func(string) (string, bool) { return "", false },
		OnDisconnectTimeout: func(string, string) {},
	})
	relay := NewGameRelay(hub, bc)
	return relay, bc
}

// ========================================================================
// T-2: Passthrough behavior tests
// ========================================================================

func TestBattleStateMeta_Parsing(t *testing.T) {
	raw := json.RawMessage(`{
		"currentTurn": 5,
		"isMyTurn": true,
		"myView": {
			"timeBank": 120,
			"hand": [1,2,3],
			"field": {"cards": []}
		},
		"opponentView": {"handCount": 4}
	}`)

	var meta battleStateMeta
	err := json.Unmarshal(raw, &meta)
	require.NoError(t, err)
	assert.Equal(t, int64(5), meta.CurrentTurn)
	assert.True(t, meta.IsMyTurn)
	assert.Equal(t, int64(120), meta.MyView.TimeBank)
}

func TestBattleStateMeta_IsNotMyTurn(t *testing.T) {
	raw := json.RawMessage(`{
		"currentTurn": 3,
		"isMyTurn": false,
		"myView": {"timeBank": 0}
	}`)

	var meta battleStateMeta
	err := json.Unmarshal(raw, &meta)
	require.NoError(t, err)
	assert.Equal(t, int64(3), meta.CurrentTurn)
	assert.False(t, meta.IsMyTurn)
	assert.Equal(t, int64(0), meta.MyView.TimeBank)
}

func TestBattleStateMeta_EmptyJSON(t *testing.T) {
	raw := json.RawMessage(`{}`)

	var meta battleStateMeta
	err := json.Unmarshal(raw, &meta)
	require.NoError(t, err)
	assert.Equal(t, int64(0), meta.CurrentTurn)
	assert.False(t, meta.IsMyTurn)
	assert.Equal(t, int64(0), meta.MyView.TimeBank)
}

func TestTransformActionData_NonTurnStart_Passthrough(t *testing.T) {
	relay, _ := newTestRelay()

	evt := service.ActionEvent{
		Sequence:  1,
		EventType: "play_card",
		EventData: map[string]interface{}{
			"card_id": "card_001",
			"slot":    float64(2),
		},
	}
	rawState := json.RawMessage(`{"currentTurn": 1, "isMyTurn": true, "myView": {"timeBank": 60}}`)

	result := relay.transformActionData(evt, rawState)
	require.NotNil(t, result)

	var parsed map[string]interface{}
	err := json.Unmarshal(result, &parsed)
	require.NoError(t, err)
	assert.Equal(t, "card_001", parsed["card_id"])
	assert.Equal(t, float64(2), parsed["slot"])
}

func TestTransformActionData_TurnStart_InjectsIsMyTurn(t *testing.T) {
	relay, _ := newTestRelay()

	evt := service.ActionEvent{
		Sequence:  2,
		EventType: constants.EventTypeTurnStart,
		EventData: map[string]interface{}{
			"turn":         float64(3),
			"activePlayer": "player_1",
		},
	}

	t.Run("isMyTurn=true", func(t *testing.T) {
		rawState := json.RawMessage(`{"currentTurn": 3, "isMyTurn": true, "myView": {"timeBank": 90}}`)
		result := relay.transformActionData(evt, rawState)
		require.NotNil(t, result)

		var parsed map[string]interface{}
		err := json.Unmarshal(result, &parsed)
		require.NoError(t, err)
		assert.Equal(t, float64(3), parsed["turn"])
		assert.Equal(t, true, parsed["is_my_turn"])
		// activePlayer should NOT be present (replaced by is_my_turn)
		_, hasActivePlayer := parsed["activePlayer"]
		assert.False(t, hasActivePlayer)
	})

	t.Run("isMyTurn=false", func(t *testing.T) {
		rawState := json.RawMessage(`{"currentTurn": 3, "isMyTurn": false, "myView": {"timeBank": 0}}`)
		result := relay.transformActionData(evt, rawState)
		require.NotNil(t, result)

		var parsed map[string]interface{}
		err := json.Unmarshal(result, &parsed)
		require.NoError(t, err)
		assert.Equal(t, float64(3), parsed["turn"])
		assert.Equal(t, false, parsed["is_my_turn"])
	})
}

func TestTransformActionData_TurnStart_InvalidState_FallsBackToEventData(t *testing.T) {
	relay, _ := newTestRelay()

	evt := service.ActionEvent{
		Sequence:  1,
		EventType: constants.EventTypeTurnStart,
		EventData: map[string]interface{}{
			"turn":         float64(1),
			"activePlayer": "player_1",
		},
	}

	// Invalid JSON state -- cannot parse battleStateMeta, falls back to marshaling EventData as-is.
	rawState := json.RawMessage(`not valid json`)
	result := relay.transformActionData(evt, rawState)
	require.NotNil(t, result)

	var parsed map[string]interface{}
	err := json.Unmarshal(result, &parsed)
	require.NoError(t, err)
	assert.Equal(t, float64(1), parsed["turn"])
	assert.Equal(t, "player_1", parsed["activePlayer"])
}

func TestGameState_PassthroughAsRawMessage(t *testing.T) {
	// Verify that GetGameStateForPlayer returns the raw JSON unchanged.
	bc := newMockBattleClient()
	originalState := json.RawMessage(`{"currentTurn":7,"isMyTurn":true,"myView":{"timeBank":45,"hand":[{"id":"c1"},{"id":"c2"}]},"opponentView":{"handCount":3}}`)
	bc.SetState("game_1", "player_A", originalState)

	ctx := context.Background()
	state, err := bc.GetGameStateForPlayer(ctx, "game_1", "player_A")
	require.NoError(t, err)

	// The raw bytes should be identical -- no transformation.
	assert.JSONEq(t, string(originalState), string(state))
}

// ========================================================================
// T-3: Unit tests for game relay logic
// ========================================================================

func TestJoinGame(t *testing.T) {
	relay, _ := newTestRelay()

	relay.JoinGame("p1", "game_1")

	// playerGames should map p1 → game_1
	gid, ok := relay.GameIDForPlayer("p1")
	assert.True(t, ok)
	assert.Equal(t, "game_1", gid)

	// gameMembers should contain p1
	relay.mu.RLock()
	members := relay.gameMembers["game_1"]
	relay.mu.RUnlock()
	assert.Contains(t, members, "p1")
}

func TestJoinGame_TwoPlayers(t *testing.T) {
	relay, _ := newTestRelay()

	relay.JoinGame("p1", "game_1")
	relay.JoinGame("p2", "game_1")

	relay.mu.RLock()
	members := relay.gameMembers["game_1"]
	relay.mu.RUnlock()
	assert.Len(t, members, 2)
	assert.Contains(t, members, "p1")
	assert.Contains(t, members, "p2")
}

func TestJoinGame_DuplicateJoin(t *testing.T) {
	relay, _ := newTestRelay()

	relay.JoinGame("p1", "game_1")
	relay.JoinGame("p1", "game_1")

	relay.mu.RLock()
	members := relay.gameMembers["game_1"]
	relay.mu.RUnlock()
	assert.Len(t, members, 1, "duplicate join should not add player twice")
}

func TestLeaveGame(t *testing.T) {
	relay, _ := newTestRelay()

	relay.JoinGame("p1", "game_1")
	relay.JoinGame("p2", "game_1")

	relay.LeaveGame("p1")

	// p1 should no longer have a game
	_, ok := relay.GameIDForPlayer("p1")
	assert.False(t, ok)

	// p2 should still be in the game
	gid, ok := relay.GameIDForPlayer("p2")
	assert.True(t, ok)
	assert.Equal(t, "game_1", gid)

	relay.mu.RLock()
	members := relay.gameMembers["game_1"]
	relay.mu.RUnlock()
	assert.Len(t, members, 1)
	assert.Contains(t, members, "p2")
}

func TestLeaveGame_LastPlayer_CleansUpGame(t *testing.T) {
	relay, _ := newTestRelay()

	relay.RegisterGameMeta("game_1", "p1", "p2", constants.MatchTypePvp)
	relay.JoinGame("p1", "game_1")
	relay.JoinGame("p2", "game_1")

	relay.LeaveGame("p1")
	relay.LeaveGame("p2")

	relay.mu.RLock()
	_, gameMembersExist := relay.gameMembers["game_1"]
	_, gameMetaExists := relay.gameMeta["game_1"]
	relay.mu.RUnlock()
	assert.False(t, gameMembersExist, "gameMembers should be cleaned up when last player leaves")
	assert.False(t, gameMetaExists, "gameMeta should be cleaned up when last player leaves")
}

func TestLeaveGame_NotInGame(t *testing.T) {
	relay, _ := newTestRelay()

	// Leaving when not in any game should not panic or error.
	relay.LeaveGame("unknown_player")

	_, ok := relay.GameIDForPlayer("unknown_player")
	assert.False(t, ok)
}

func TestRegisterGameMeta(t *testing.T) {
	relay, _ := newTestRelay()

	relay.RegisterGameMeta("game_1", "p1", "p2", constants.MatchTypePvp)

	relay.mu.RLock()
	meta, ok := relay.gameMeta["game_1"]
	relay.mu.RUnlock()
	require.True(t, ok)
	assert.Equal(t, "p1", meta.player1ID)
	assert.Equal(t, "p2", meta.player2ID)
	assert.Equal(t, constants.MatchTypePvp, meta.matchType)
}

func TestRegisterGameMeta_NPC(t *testing.T) {
	relay, _ := newTestRelay()

	relay.RegisterGameMeta("game_2", "p1", "npc_bot", constants.MatchTypeNpc)

	relay.mu.RLock()
	meta, ok := relay.gameMeta["game_2"]
	relay.mu.RUnlock()
	require.True(t, ok)
	assert.Equal(t, constants.MatchTypeNpc, meta.matchType)
}

func TestGameIDForPlayer(t *testing.T) {
	relay, _ := newTestRelay()

	// Not in any game
	_, ok := relay.GameIDForPlayer("p1")
	assert.False(t, ok)

	// After joining
	relay.JoinGame("p1", "game_1")
	gid, ok := relay.GameIDForPlayer("p1")
	assert.True(t, ok)
	assert.Equal(t, "game_1", gid)

	// After leaving
	relay.LeaveGame("p1")
	_, ok = relay.GameIDForPlayer("p1")
	assert.False(t, ok)
}

func TestLeaveAllPlayers(t *testing.T) {
	relay, _ := newTestRelay()

	relay.RegisterGameMeta("game_1", "p1", "p2", constants.MatchTypePvp)
	relay.JoinGame("p1", "game_1")
	relay.JoinGame("p2", "game_1")

	// Mark exp as awarded to verify cleanup
	relay.mu.Lock()
	relay.expAwarded["game_1"] = true
	relay.mu.Unlock()

	relay.leaveAllPlayers("game_1")

	_, ok1 := relay.GameIDForPlayer("p1")
	_, ok2 := relay.GameIDForPlayer("p2")
	assert.False(t, ok1)
	assert.False(t, ok2)

	relay.mu.RLock()
	_, membersExist := relay.gameMembers["game_1"]
	_, expExists := relay.expAwarded["game_1"]
	relay.mu.RUnlock()
	assert.False(t, membersExist, "gameMembers should be cleaned up")
	assert.False(t, expExists, "expAwarded should be cleaned up")
}

// ========================================================================
// Helper function tests
// ========================================================================

func TestAppendUnique(t *testing.T) {
	tests := []struct {
		name     string
		initial  []string
		add      string
		expected []string
	}{
		{
			name:     "append to empty",
			initial:  nil,
			add:      "a",
			expected: []string{"a"},
		},
		{
			name:     "append new element",
			initial:  []string{"a", "b"},
			add:      "c",
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "no duplicate",
			initial:  []string{"a", "b"},
			add:      "a",
			expected: []string{"a", "b"},
		},
		{
			name:     "no duplicate at end",
			initial:  []string{"a", "b"},
			add:      "b",
			expected: []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := appendUnique(tt.initial, tt.add)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRemoveString(t *testing.T) {
	tests := []struct {
		name     string
		initial  []string
		remove   string
		expected []string
	}{
		{
			name:     "remove existing element",
			initial:  []string{"a", "b", "c"},
			remove:   "b",
			expected: []string{"a", "c"},
		},
		{
			name:     "remove first element",
			initial:  []string{"a", "b", "c"},
			remove:   "a",
			expected: []string{"b", "c"},
		},
		{
			name:     "remove last element",
			initial:  []string{"a", "b", "c"},
			remove:   "c",
			expected: []string{"a", "b"},
		},
		{
			name:     "remove from single element",
			initial:  []string{"a"},
			remove:   "a",
			expected: []string{},
		},
		{
			name:     "remove nonexistent",
			initial:  []string{"a", "b"},
			remove:   "z",
			expected: []string{"a", "b"},
		},
		{
			name:     "remove from empty",
			initial:  []string{},
			remove:   "a",
			expected: []string{},
		},
		{
			name:     "remove from nil",
			initial:  nil,
			remove:   "a",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeString(tt.initial, tt.remove)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ========================================================================
// mustMarshal tests
// ========================================================================

func TestMustMarshal(t *testing.T) {
	t.Run("simple map", func(t *testing.T) {
		result := mustMarshal(map[string]string{"key": "value"})
		assert.JSONEq(t, `{"key":"value"}`, string(result))
	})

	t.Run("struct", func(t *testing.T) {
		msg := ErrorMessage{Code: "test", Message: "hello", Retryable: true}
		result := mustMarshal(msg)
		require.NotNil(t, result)

		var parsed ErrorMessage
		err := json.Unmarshal(result, &parsed)
		require.NoError(t, err)
		assert.Equal(t, "test", parsed.Code)
		assert.Equal(t, "hello", parsed.Message)
		assert.True(t, parsed.Retryable)
	})

	t.Run("nil returns null json", func(t *testing.T) {
		result := mustMarshal(nil)
		assert.Equal(t, "null", string(result))
	})
}

// ========================================================================
// JoinGame → playerGames re-assignment (player switching games)
// ========================================================================

func TestJoinGame_SwitchGame(t *testing.T) {
	relay, _ := newTestRelay()

	relay.JoinGame("p1", "game_1")
	relay.JoinGame("p1", "game_2")

	// p1 should now be in game_2
	gid, ok := relay.GameIDForPlayer("p1")
	assert.True(t, ok)
	assert.Equal(t, "game_2", gid)

	// p1 should be in game_2's members
	relay.mu.RLock()
	members2 := relay.gameMembers["game_2"]
	relay.mu.RUnlock()
	assert.Contains(t, members2, "p1")
}

// ========================================================================
// sendToOpponent via NotifyOpponentDisconnected/Reconnected
// ========================================================================

func TestNotifyOpponentDisconnected_NoMeta(t *testing.T) {
	relay, _ := newTestRelay()

	// No meta registered -- should not panic.
	relay.NotifyOpponentDisconnected("p1", "nonexistent_game")
}

func TestNotifyOpponentReconnected_NoMeta(t *testing.T) {
	relay, _ := newTestRelay()

	// No meta registered -- should not panic.
	relay.NotifyOpponentReconnected("p1", "nonexistent_game")
}

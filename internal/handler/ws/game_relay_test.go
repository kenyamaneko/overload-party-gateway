package ws

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

var errFake = errors.New("fake battle error")

// --- Mock BattleClient ---

type mockBattleClient struct {
	processActionResult *service.ActionResult
	processActionErr    error
	turnControls        json.RawMessage
	turnControlsErr     error
	advanceNpcResult    *service.ActionResult
	advanceNpcErr       error
	// advanceNpcQueue, if non-nil, is popped from on each AdvanceNpcTurn call
	// (overrides advanceNpcResult). Enables tests that need successive results.
	advanceNpcQueue []*service.ActionResult
	advanceNpcCalls int
}

func newMockBattleClient() *mockBattleClient {
	return &mockBattleClient{}
}

func (m *mockBattleClient) GetNPCModels(_ context.Context) (json.RawMessage, error) {
	return json.RawMessage(`[]`), nil
}

func (m *mockBattleClient) ListNpcModels(_ context.Context) ([]service.NpcModelEntry, error) {
	return nil, nil
}

func (m *mockBattleClient) StartNPCBattle(_ context.Context, _ []service.BattleDeckCard, _ string, _, _ service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
	return nil, nil
}

func (m *mockBattleClient) CreatePvPGame(_ context.Context, _, _ []service.BattleDeckCard, _, _ service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
	return nil, nil
}

func (m *mockBattleClient) ProcessAction(_ context.Context, _ string, _ int, _ string, _ json.RawMessage) (*service.ActionResult, error) {
	return m.processActionResult, m.processActionErr
}

func (m *mockBattleClient) GetGameStateForPlayer(_ context.Context, _ string, _ int) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (m *mockBattleClient) GetTurnControlsForPlayer(_ context.Context, _ string, _ int) (json.RawMessage, error) {
	return m.turnControls, m.turnControlsErr
}

func (m *mockBattleClient) AdvanceNpcTurn(_ context.Context, _ string) (*service.ActionResult, error) {
	m.advanceNpcCalls++
	// queue モードと固定エラーモードは排他。混在するとテストの意図が曖昧になるため、
	// セットアップミスを早期検出する。
	if m.advanceNpcQueue != nil && m.advanceNpcErr != nil {
		panic("mockBattleClient: advanceNpcQueue と advanceNpcErr の同時セットは禁止（モード排他）")
	}
	if m.advanceNpcQueue != nil {
		if len(m.advanceNpcQueue) == 0 {
			panic("mockBattleClient: advanceNpcQueue exhausted — test setup has fewer results than AdvanceNpcTurn calls")
		}
		r := m.advanceNpcQueue[0]
		m.advanceNpcQueue = m.advanceNpcQueue[1:]
		return r, nil
	}
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
// 依存 (account / repo / lookup) は nil のまま。EXP 付与を触るテストはフィールドに直接代入するか、
// 専用ヘルパー (exp_award_test.go の setupAwardRelay 等) を使う。
func newTestRelay() (*GameRelay, *mockBattleClient) {
	bc := newMockBattleClient()
	hub := NewConnectionHub(HubCallbacks{
		GetGameID:           func(string) (string, bool) { return "", false },
		OnDisconnectTimeout: func(string, string) {},
	})
	relay := NewGameRelay(hub, bc, nil, nil)
	return relay, bc
}

// ========================================================================
// T-2: Passthrough behavior tests
// ========================================================================

// TestBattleStateMeta_Parsing は battle server の状態 JSON が最小射影に正しくパースされることを検証する。
func TestBattleStateMeta_Parsing(t *testing.T) {
	tests := []struct {
		name            string
		raw             string
		wantCurrentTurn int64
		wantIsMyTurn    bool
		wantTimeBank    int64
	}{
		{
			name: "full payload",
			raw: `{
				"currentTurn": 5,
				"isMyTurn": true,
				"myView": {
					"timeBank": 120,
					"hand": [1,2,3],
					"field": {"cards": []}
				},
				"opponentView": {"handCount": 4}
			}`,
			wantCurrentTurn: 5,
			wantIsMyTurn:    true,
			wantTimeBank:    120,
		},
		{
			name: "not my turn",
			raw: `{
				"currentTurn": 3,
				"isMyTurn": false,
				"myView": {"timeBank": 0}
			}`,
			wantCurrentTurn: 3,
			wantIsMyTurn:    false,
			wantTimeBank:    0,
		},
		{
			name:            "empty json defaults to zero values",
			raw:             `{}`,
			wantCurrentTurn: 0,
			wantIsMyTurn:    false,
			wantTimeBank:    0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var meta battleStateMeta
			err := json.Unmarshal(json.RawMessage(tt.raw), &meta)
			require.NoError(t, err)
			assert.Equal(t, tt.wantCurrentTurn, meta.CurrentTurn)
			assert.Equal(t, tt.wantIsMyTurn, meta.IsMyTurn)
			assert.Equal(t, tt.wantTimeBank, meta.MyView.TimeBank)
		})
	}
}

// ========================================================================
// runNpcTurns: NPC action loop
// ========================================================================

func TestRunNpcTurns_NilInitialResult(t *testing.T) {
	relay, bc := newTestRelay()
	ctx := context.Background()

	result := relay.runNpcTurns(ctx, "g1", "p1", nil)

	assert.Nil(t, result)
	assert.Equal(t, 0, bc.advanceNpcCalls, "should not call AdvanceNpcTurn when initial result is nil")
}

func TestRunNpcTurns_InitialNotPending(t *testing.T) {
	relay, bc := newTestRelay()
	ctx := context.Background()
	initial := &service.ActionResult{NpcPending: false}

	result := relay.runNpcTurns(ctx, "g1", "p1", initial)

	assert.Same(t, initial, result)
	assert.Equal(t, 0, bc.advanceNpcCalls, "should not loop when initial NpcPending=false")
}

func TestRunNpcTurns_InitialGameOver(t *testing.T) {
	relay, bc := newTestRelay()
	ctx := context.Background()
	initial := &service.ActionResult{NpcPending: true, GameOver: true}

	result := relay.runNpcTurns(ctx, "g1", "p1", initial)

	assert.Same(t, initial, result)
	assert.Equal(t, 0, bc.advanceNpcCalls, "should not loop when initial GameOver=true")
}

func TestRunNpcTurns_LoopsUntilNotPending(t *testing.T) {
	relay, bc := newTestRelay()
	ctx := context.Background()
	bc.advanceNpcQueue = []*service.ActionResult{
		{NpcPending: true},  // second NPC action, still pending
		{NpcPending: false}, // third call resolves — control back to human
	}
	initial := &service.ActionResult{NpcPending: true}

	result := relay.runNpcTurns(ctx, "g1", "p1", initial)

	require.NotNil(t, result)
	assert.False(t, result.NpcPending)
	assert.False(t, result.GameOver)
	assert.Equal(t, 2, bc.advanceNpcCalls)
}

func TestRunNpcTurns_StopsOnGameOver(t *testing.T) {
	relay, bc := newTestRelay()
	ctx := context.Background()
	bc.advanceNpcQueue = []*service.ActionResult{
		{NpcPending: true, GameOver: true, WinningPlayerNum: 2, WinReason: "lp_zero"},
	}
	initial := &service.ActionResult{NpcPending: true}

	result := relay.runNpcTurns(ctx, "g1", "p1", initial)

	require.NotNil(t, result)
	assert.True(t, result.GameOver)
	assert.Equal(t, int64(2), result.WinningPlayerNum)
	assert.Equal(t, 1, bc.advanceNpcCalls, "should stop immediately on GameOver")
}

func TestRunNpcTurns_StopsOnError(t *testing.T) {
	relay, bc := newTestRelay()
	ctx := context.Background()
	bc.advanceNpcErr = errFake
	initial := &service.ActionResult{NpcPending: true}

	result := relay.runNpcTurns(ctx, "g1", "p1", initial)

	assert.Same(t, initial, result, "returns last good result on error")
	assert.Equal(t, 1, bc.advanceNpcCalls)
}

func TestRunNpcTurns_IterationCapReached(t *testing.T) {
	relay, bc := newTestRelay()
	ctx := context.Background()

	// キューはループ上限を1つ超えるサイズで用意する。上限到達時に i==maxNpcTurnIterations で
	// 抜けるため、最後の1件はポップされない。
	queue := make([]*service.ActionResult, maxNpcTurnIterations+1)
	for i := range queue {
		queue[i] = &service.ActionResult{NpcPending: true}
	}
	bc.advanceNpcQueue = queue
	initial := &service.ActionResult{NpcPending: true}

	result := relay.runNpcTurns(ctx, "g1", "p1", initial)

	require.NotNil(t, result)
	assert.True(t, result.NpcPending, "cap interrupts the loop so NpcPending remains true")
	assert.Equal(t, maxNpcTurnIterations, bc.advanceNpcCalls, "should stop after exactly maxNpcTurnIterations calls")
}

// ========================================================================
// T-3: Unit tests for game relay logic
// ========================================================================

func TestJoinGame(t *testing.T) {
	relay, _ := newTestRelay()

	relay.JoinGame("p1", "game_1", 1)

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

	relay.JoinGame("p1", "game_1", 1)
	relay.JoinGame("p2", "game_1", 2)

	relay.mu.RLock()
	members := relay.gameMembers["game_1"]
	relay.mu.RUnlock()
	assert.Len(t, members, 2)
	assert.Contains(t, members, "p1")
	assert.Contains(t, members, "p2")
}

func TestJoinGame_DuplicateJoin(t *testing.T) {
	relay, _ := newTestRelay()

	relay.JoinGame("p1", "game_1", 1)
	relay.JoinGame("p1", "game_1", 1)

	relay.mu.RLock()
	members := relay.gameMembers["game_1"]
	relay.mu.RUnlock()
	assert.Len(t, members, 1, "duplicate join should not add player twice")
}

func TestLeaveGame(t *testing.T) {
	relay, _ := newTestRelay()

	relay.JoinGame("p1", "game_1", 1)
	relay.JoinGame("p2", "game_1", 2)

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

	relay.JoinGame("p1", "game_1", 1)
	relay.JoinGame("p2", "game_1", 2)

	relay.LeaveGame("p1")
	relay.LeaveGame("p2")

	relay.mu.RLock()
	_, gameMembersExist := relay.gameMembers["game_1"]
	relay.mu.RUnlock()
	assert.False(t, gameMembersExist, "gameMembers should be cleaned up when last player leaves")
}

func TestLeaveGame_NotInGame(t *testing.T) {
	relay, _ := newTestRelay()

	// Leaving when not in any game should not panic or error.
	relay.LeaveGame("unknown_player")

	_, ok := relay.GameIDForPlayer("unknown_player")
	assert.False(t, ok)
}

func TestJoinGame_CachesPlayerNum(t *testing.T) {
	relay, _ := newTestRelay()

	relay.JoinGame("p1", "game_1", 1)
	relay.JoinGame("p2", "game_1", 2)

	assert.Equal(t, 1, relay.resolvePlayerNum("p1"))
	assert.Equal(t, 2, relay.resolvePlayerNum("p2"))
}

func TestResolvePlayerNum_NotInGame(t *testing.T) {
	relay, _ := newTestRelay()
	assert.Equal(t, 0, relay.resolvePlayerNum("unknown"))
}

func TestGameIDForPlayer(t *testing.T) {
	relay, _ := newTestRelay()

	// Not in any game
	_, ok := relay.GameIDForPlayer("p1")
	assert.False(t, ok)

	// After joining
	relay.JoinGame("p1", "game_1", 1)
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

	relay.JoinGame("p1", "game_1", 1)
	relay.JoinGame("p2", "game_1", 2)

	relay.leaveAllPlayers("game_1")

	_, ok1 := relay.GameIDForPlayer("p1")
	_, ok2 := relay.GameIDForPlayer("p2")
	assert.False(t, ok1)
	assert.False(t, ok2)

	relay.mu.RLock()
	_, membersExist := relay.gameMembers["game_1"]
	relay.mu.RUnlock()
	assert.False(t, membersExist, "gameMembers should be cleaned up")
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
		msg := ErrorMessage{ErrorCode: "test", Message: "hello", Retryable: true}
		result := mustMarshal(msg)
		require.NotNil(t, result)

		var parsed ErrorMessage
		err := json.Unmarshal(result, &parsed)
		require.NoError(t, err)
		assert.Equal(t, "test", parsed.ErrorCode)
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

	relay.JoinGame("p1", "game_1", 1)
	relay.JoinGame("p1", "game_2", 1)

	// p1 should now be in game_2
	gid, ok := relay.GameIDForPlayer("p1")
	assert.True(t, ok)
	assert.Equal(t, "game_2", gid)

	// p1 should be in game_2's members
	relay.mu.RLock()
	members2 := relay.gameMembers["game_2"]
	members1 := relay.gameMembers["game_1"]
	relay.mu.RUnlock()
	assert.Contains(t, members2, "p1")

	// p1 should be removed from game_1's members
	assert.NotContains(t, members1, "p1")
}

// ========================================================================
// sendToOpponent via NotifyOpponentDisconnected/Reconnected
// ========================================================================

func TestNotifyOpponentDisconnected_NoMembers(t *testing.T) {
	relay, _ := newTestRelay()

	// No members registered -- should not panic.
	relay.NotifyOpponentDisconnected("p1", "nonexistent_game")
}

func TestNotifyOpponentReconnected_NoMembers(t *testing.T) {
	relay, _ := newTestRelay()

	// No members registered -- should not panic.
	relay.NotifyOpponentReconnected("p1", "nonexistent_game")
}

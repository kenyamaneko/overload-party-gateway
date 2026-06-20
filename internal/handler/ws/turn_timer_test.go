package ws

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// turnTimer の振る舞いは「同じ gameID で連続 reset すると古いタイマーは発火しない」
// 「cancel 後は発火しない」「timeBank<=0 で no-op」が契約。
// 実発火時には battle server 呼び出しが起こるため、battle 呼び出しを伴わない範囲で検証する。

// TestResetTurnTimer_NonPositiveTimeBank_DoesNothing は timeBank<=0 のときタイマーを登録しないことを検証する。
func TestResetTurnTimer_NonPositiveTimeBank_DoesNothing(t *testing.T) {
	tests := []struct {
		name            string
		timeBankSeconds int64
	}{
		{"zero", 0},
		{"negative", -5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			relay, _ := newTestRelay()
			relay.JoinGame("p1", "g1", 1)

			relay.resetTurnTimer("g1", "p1", tt.timeBankSeconds)

			relay.timerMu.Lock()
			_, ok := relay.turnTimers["g1"]
			relay.timerMu.Unlock()
			assert.False(t, ok, "no timer should be registered when timeBank<=0")
		})
	}
}

func TestResetTurnTimer_RegistersTimer(t *testing.T) {
	relay, _ := newTestRelay()
	relay.JoinGame("p1", "g1", 1)

	// 十分大きな timeBank を渡し、テスト中には発火させない。
	relay.resetTurnTimer("g1", "p1", 60)

	relay.timerMu.Lock()
	info, ok := relay.turnTimers["g1"]
	relay.timerMu.Unlock()
	require.True(t, ok)
	assert.Equal(t, "p1", info.activePlayerID)

	// 後始末
	relay.cancelTurnTimer("g1")
}

func TestResetTurnTimer_ReplacesExistingTimer(t *testing.T) {
	relay, _ := newTestRelay()
	relay.JoinGame("p1", "g1", 1)
	relay.JoinGame("p2", "g1", 2)

	relay.resetTurnTimer("g1", "p1", 60)
	relay.resetTurnTimer("g1", "p2", 60) // 別プレイヤーで上書き

	relay.timerMu.Lock()
	info, ok := relay.turnTimers["g1"]
	relay.timerMu.Unlock()
	require.True(t, ok)
	assert.Equal(t, "p2", info.activePlayerID, "timer should be replaced with new active player")

	relay.cancelTurnTimer("g1")
}

func TestCancelTurnTimer_RemovesTimer(t *testing.T) {
	relay, _ := newTestRelay()
	relay.JoinGame("p1", "g1", 1)

	relay.resetTurnTimer("g1", "p1", 60)
	relay.cancelTurnTimer("g1")

	relay.timerMu.Lock()
	_, ok := relay.turnTimers["g1"]
	relay.timerMu.Unlock()
	assert.False(t, ok)
}

func TestCancelTurnTimer_NonExistent_NoPanic(t *testing.T) {
	relay, _ := newTestRelay()
	relay.cancelTurnTimer("nonexistent_game")
}

// TestResetTurnTimer_OldTimerDoesNotFireAfterReset は「ターン交代済みの旧プレイヤーへの誤 forfeit を
// 防止」する分岐を契約として検証する。短い timeBank で反応を見ると不安定なので、
// 旧プレイヤーが activePlayerID として残っていないことだけ検証する。
func TestResetTurnTimer_OldTimerEntryIsReplaced(t *testing.T) {
	relay, _ := newTestRelay()
	relay.JoinGame("p1", "g1", 1)
	relay.JoinGame("p2", "g1", 2)

	relay.resetTurnTimer("g1", "p1", 60)
	relay.resetTurnTimer("g1", "p2", 60)

	relay.timerMu.Lock()
	info := relay.turnTimers["g1"]
	relay.timerMu.Unlock()
	require.NotNil(t, info)
	assert.NotEqual(t, "p1", info.activePlayerID,
		"after reset to p2, info.activePlayerID must not be p1 — guard prevents firing forfeit on stale player")

	relay.cancelTurnTimer("g1")
}

// TestTurnTimerNetworkBuffer_Constant はバッファの正の値であることを検証する
// （0 だと境界での誤 forfeit が発生する。負だと意味的におかしい）。
func TestTurnTimerNetworkBuffer_Constant(t *testing.T) {
	assert.Greater(t, turnTimerNetworkBuffer, time.Duration(0),
		"buffer must be positive to absorb network latency")
}

func TestIsCanceled(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"context.Canceled", context.Canceled, true},
		{"context.DeadlineExceeded", context.DeadlineExceeded, true},
		{"wrapped Canceled", errors.New("wrap: " + context.Canceled.Error()), false}, // bare string ≠ wrap
		{"other error", errFake, false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isCanceled(tt.err))
		})
	}
}

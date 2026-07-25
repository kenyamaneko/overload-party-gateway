package ws

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResetTurnTimer(t *testing.T) {
	// 実発火時には battle server 呼び出しが起こるため、発火を伴わない範囲で振る舞いを検証する。
	// timeBank は十分大きな値を渡し、テスト中には発火させない。
	t.Run("ターンタイマーの登録・更新", func(t *testing.T) {
		nonPositiveCases := []struct {
			name            string
			timeBankSeconds int64
		}{
			{name: "timeBank が 0 のとき、タイマーを登録しない", timeBankSeconds: 0},
			{name: "timeBank が -5 のとき、タイマーを登録しない", timeBankSeconds: -5},
		}
		for _, tc := range nonPositiveCases {
			t.Run(tc.name, func(t *testing.T) {
				relay, _ := newTestRelay()
				relay.JoinGame("p1", "g1", 1)

				relay.resetTurnTimer("g1", "p1", tc.timeBankSeconds)

				relay.timerMu.Lock()
				_, ok := relay.turnTimers["g1"]
				relay.timerMu.Unlock()
				assert.False(t, ok, "no timer should be registered when timeBank<=0")
			})
		}

		t.Run("正の timeBank のとき、activePlayerID 付きで登録される", func(t *testing.T) {
			relay, _ := newTestRelay()
			relay.JoinGame("p1", "g1", 1)

			relay.resetTurnTimer("g1", "p1", 60)

			relay.timerMu.Lock()
			info, ok := relay.turnTimers["g1"]
			relay.timerMu.Unlock()
			require.True(t, ok)
			assert.Equal(t, "p1", info.activePlayerID)

			relay.cancelTurnTimer("g1")
		})

		t.Run("別プレイヤー p2 で再登録すると、activePlayerID が p2 に置き換わる", func(t *testing.T) {
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
		})

		t.Run("再登録すると、旧プレイヤー p1 は activePlayerID に残らない", func(t *testing.T) {
			// ターン交代済みの旧プレイヤーへの誤 forfeit を防ぐ分岐を契約として確かめる。
			// 実際の発火を短い timeBank で観測すると不安定なので、旧プレイヤーが
			// activePlayerID として残っていないことだけを検証する。
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
		})
	})
}

func TestResetTurnTimer_TimerStoreMirroring(t *testing.T) {
	t.Run("ターン期限の TimerStore への書き込み", func(t *testing.T) {
		t.Run("正の timeBank のとき、発火時刻を絶対時刻として書き込む", func(t *testing.T) {
			relay, _ := newTestRelay()
			store := &fakeTimerStore{}
			relay.timerStore = store
			relay.JoinGame("p1", "g1", 1)
			before := time.Now()

			relay.resetTurnTimer("g1", "p1", 60)

			calls := store.snapshotSetTurnCalls()
			require.Len(t, calls, 1)
			assert.Equal(t, "g1", calls[0].gameID)
			assert.Equal(t, "p1", calls[0].activePlayerID)
			wantDeadline := before.Add(60*time.Second + turnTimerNetworkBuffer)
			assert.WithinDuration(t, wantDeadline, calls[0].deadline, 2*time.Second)

			relay.cancelTurnTimer("g1")
		})

		t.Run("timeBank が 0 のとき、書き込まず期限を削除する", func(t *testing.T) {
			relay, _ := newTestRelay()
			store := &fakeTimerStore{}
			relay.timerStore = store
			relay.JoinGame("p1", "g1", 1)

			relay.resetTurnTimer("g1", "p1", 0)

			assert.Empty(t, store.snapshotSetTurnCalls())
			assert.Equal(t, []string{"g1"}, store.snapshotClearTurnCalls())
		})

		t.Run("TimerStore が未設定のとき、パニックしない", func(t *testing.T) {
			relay, _ := newTestRelay()
			relay.JoinGame("p1", "g1", 1)

			assert.NotPanics(t, func() { relay.resetTurnTimer("g1", "p1", 60) })
			relay.cancelTurnTimer("g1")
		})

		t.Run("TimerStore への書き込みが失敗しても、パニックしない", func(t *testing.T) {
			relay, _ := newTestRelay()
			relay.timerStore = &fakeTimerStore{setTurnErr: errors.New("redis down")}
			relay.JoinGame("p1", "g1", 1)

			assert.NotPanics(t, func() { relay.resetTurnTimer("g1", "p1", 60) })
			relay.cancelTurnTimer("g1")
		})
	})
}

func TestCancelTurnTimer_TimerStoreMirroring(t *testing.T) {
	t.Run("ターン期限の TimerStore からの削除", func(t *testing.T) {
		t.Run("取り消すと、TimerStore の期限も削除される", func(t *testing.T) {
			relay, _ := newTestRelay()
			store := &fakeTimerStore{}
			relay.timerStore = store
			relay.JoinGame("p1", "g1", 1)
			relay.resetTurnTimer("g1", "p1", 60)

			relay.cancelTurnTimer("g1")

			assert.Equal(t, []string{"g1"}, store.snapshotClearTurnCalls())
		})

		t.Run("TimerStore が未設定のとき、パニックしない", func(t *testing.T) {
			relay, _ := newTestRelay()

			assert.NotPanics(t, func() { relay.cancelTurnTimer("g1") })
		})
	})
}

func TestCancelTurnTimer(t *testing.T) {
	t.Run("ターンタイマーの取消", func(t *testing.T) {
		t.Run("登録済みタイマーを取り消すと、削除される", func(t *testing.T) {
			relay, _ := newTestRelay()
			relay.JoinGame("p1", "g1", 1)

			relay.resetTurnTimer("g1", "p1", 60)
			relay.cancelTurnTimer("g1")

			relay.timerMu.Lock()
			_, ok := relay.turnTimers["g1"]
			relay.timerMu.Unlock()
			assert.False(t, ok)
		})

		t.Run("存在しないゲームを取り消しても、パニックしない", func(t *testing.T) {
			relay, _ := newTestRelay()
			relay.cancelTurnTimer("nonexistent_game")
		})
	})
}

func TestTurnTimerNetworkBuffer_Constant(t *testing.T) {
	t.Run("ネットワークバッファ定数", func(t *testing.T) {
		t.Run("turnTimerNetworkBuffer が正の値である", func(t *testing.T) {
			// 0 だと境界での誤 forfeit が発生し、負だと意味的におかしい。
			assert.Greater(t, turnTimerNetworkBuffer, time.Duration(0),
				"buffer must be positive to absorb network latency")
		})
	})
}

func TestIsCanceled(t *testing.T) {
	t.Run("コンテキストキャンセル判定", func(t *testing.T) {
		tests := []struct {
			name string
			err  error
			want bool
		}{
			{name: "context.Canceled のとき、true になる", err: context.Canceled, want: true},
			{name: "context.DeadlineExceeded のとき、true になる", err: context.DeadlineExceeded, want: true},
			// bare string ≠ wrap: 文字列に Canceled を含むだけでは errors.Is に一致しない。
			{name: "文字列に Canceled を含むだけのエラーのとき、false になる", err: errors.New("wrap: " + context.Canceled.Error()), want: false},
			{name: "無関係なエラーのとき、false になる", err: errFake, want: false},
			{name: "nil のとき、false になる", err: nil, want: false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.want, isCanceled(tt.err))
			})
		}
	})
}

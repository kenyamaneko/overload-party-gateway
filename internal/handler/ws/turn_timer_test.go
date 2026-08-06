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
	t.Run("ターンタイマーの登録・更新", func(t *testing.T) {
		nonPositiveCases := []struct {
			name            string
			timeBankSeconds int64
		}{
			{name: "timeBankが0のとき、タイマーを登録しない", timeBankSeconds: 0},
			{name: "timeBankが -5のとき、タイマーを登録しない", timeBankSeconds: -5},
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
	})
}

// manualFireClock は AfterFunc に渡された関数を実時間で走らせず、captured() 経由で
// テストが任意のタイミングで手動起動できるようにする Clock テストダブル。
type manualFireClock struct {
	lastFn func()
}

func (c *manualFireClock) Now() time.Time { return time.Now() }

func (c *manualFireClock) AfterFunc(_ time.Duration, f func()) Timer {
	c.lastFn = f
	return noopTimer{}
}

func (c *manualFireClock) captured() func() { return c.lastFn }

type noopTimer struct{}

func (noopTimer) Stop() bool { return true }

func TestResetTurnTimer_StaleTimerGuard(t *testing.T) {
	t.Run("タイマー交代の競合", func(t *testing.T) {
		t.Run("ターン交代の直後に交代前のタイマーが発火しても、交代前のアクティブプレイヤーはフォーフェイトされない", func(t *testing.T) {
			bc := newMockBattleClient()
			hub := NewConnectionHub(HubCallbacks{
				GetGameID:           func(string) (string, bool) { return "", false },
				OnDisconnectTimeout: func(string, string) {},
			}, DefaultDisconnectTimeout, nil)
			clock := &manualFireClock{}
			relay := NewGameRelay(hub, bc, nil, nil, nil, nil, WithClock(clock))
			relay.JoinGame("p1", "g1", 1)
			relay.JoinGame("p2", "g1", 2)

			relay.resetTurnTimer("g1", "p1", 60)
			staleFn := clock.captured()

			relay.resetTurnTimer("g1", "p2", 60) // p1のタイマーをStopしてp2で置き換える

			staleFn() // Stop後もp1側のコールバックが実行された状況を模す

			assert.Empty(t, bc.snapshotProcessActionCalls())
		})
	})
}

func TestResetTurnTimer_TimerStoreMirroring(t *testing.T) {
	t.Run("ターン期限のバックアップの書き込み", func(t *testing.T) {
		t.Run("正のtimeBankのとき、発火時刻を絶対時刻として書き込む", func(t *testing.T) {
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
			wantDeadline := before.Add(60*time.Second + 2*time.Second)
			assert.WithinDuration(t, wantDeadline, calls[0].deadline, 2*time.Second)

			relay.cancelTurnTimer("g1")
		})

		t.Run("timeBankが0のとき、書き込まず期限を削除する", func(t *testing.T) {
			relay, _ := newTestRelay()
			store := &fakeTimerStore{}
			relay.timerStore = store
			relay.JoinGame("p1", "g1", 1)

			relay.resetTurnTimer("g1", "p1", 0)

			assert.Empty(t, store.snapshotSetTurnCalls())
			assert.Equal(t, []string{"g1"}, store.snapshotClearTurnCalls())
		})

		t.Run("バックアップが未設定のとき、パニックしない", func(t *testing.T) {
			relay, _ := newTestRelay()
			relay.JoinGame("p1", "g1", 1)

			assert.NotPanics(t, func() { relay.resetTurnTimer("g1", "p1", 60) })
			relay.cancelTurnTimer("g1")
		})

		t.Run("バックアップの書き込みが失敗しても、パニックしない", func(t *testing.T) {
			relay, _ := newTestRelay()
			relay.timerStore = &fakeTimerStore{setTurnErr: errors.New("redis down")}
			relay.JoinGame("p1", "g1", 1)

			assert.NotPanics(t, func() { relay.resetTurnTimer("g1", "p1", 60) })
			relay.cancelTurnTimer("g1")
		})
	})
}

func TestCancelTurnTimer_TimerStoreMirroring(t *testing.T) {
	t.Run("ターン期限のバックアップの削除", func(t *testing.T) {
		t.Run("取り消すと、バックアップの期限も削除される", func(t *testing.T) {
			relay, _ := newTestRelay()
			store := &fakeTimerStore{}
			relay.timerStore = store
			relay.JoinGame("p1", "g1", 1)
			relay.resetTurnTimer("g1", "p1", 60)

			relay.cancelTurnTimer("g1")

			assert.Equal(t, []string{"g1"}, store.snapshotClearTurnCalls())
		})

		t.Run("バックアップが未設定のとき、パニックしない", func(t *testing.T) {
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

func TestIsCanceled(t *testing.T) {
	t.Run("コンテキストキャンセル判定", func(t *testing.T) {
		tests := []struct {
			name string
			err  error
			want bool
		}{
			{name: "context.Canceledのとき、trueになる", err: context.Canceled, want: true},
			{name: "context.DeadlineExceededのとき、trueになる", err: context.DeadlineExceeded, want: true},
			// bare string ≠ wrap: 文字列に Canceled を含むだけでは errors.Is に一致しない。
			{name: "文字列にCanceledを含むだけのエラーのとき、falseになる", err: errors.New("wrap: " + context.Canceled.Error()), want: false},
			{name: "無関係なエラーのとき、falseになる", err: errFake, want: false},
			{name: "nilのとき、falseになる", err: nil, want: false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.want, isCanceled(tt.err))
			})
		}
	})
}

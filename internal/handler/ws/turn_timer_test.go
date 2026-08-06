package ws

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	gamelogic "github.com/kenyamaneko/overload-party-battle/packages/game-logic-constants-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAfterFuncClock は Clock.AfterFunc に渡されたコールバックを実行せず捕捉する。
// テストは捕捉したコールバックを直接呼び出すことでタイマー発火を即時に再現する。
type fakeAfterFuncClock struct {
	mu    sync.Mutex
	calls []func()
}

func (c *fakeAfterFuncClock) Now() time.Time { return time.Unix(0, 0) }

func (c *fakeAfterFuncClock) AfterFunc(_ time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, f)
	return noopTimer{}
}

type noopTimer struct{}

func (noopTimer) Stop() bool { return true }

// newTestRelayWithClock は fakeAfterFuncClock を差し込んだ GameRelay を返す。
// resetTurnTimer が登録するコールバックを実発火させずに捕捉し、手動で呼び出せるようにする。
func newTestRelayWithClock(clock *fakeAfterFuncClock) (*GameRelay, *mockBattleClient) {
	bc := newMockBattleClient()
	hub := NewConnectionHub(HubCallbacks{
		GetGameID:           func(string) (string, bool) { return "", false },
		OnDisconnectTimeout: func(string, string) {},
	}, DefaultDisconnectTimeout, nil)
	relay := NewGameRelay(hub, bc, nil, nil, nil, nil, WithClock(clock))
	return relay, bc
}

func TestResetTurnTimer(t *testing.T) {
	// 実発火時には battle server 呼び出しが起こるため、発火を伴わない範囲で振る舞いを検証する。
	// timeBank は十分大きな値を渡し、テスト中には発火させない。
	t.Run("timeBankがターンタイマー登録要否の境界になる", func(t *testing.T) {
		nonPositiveCases := []struct {
			name            string
			timeBankSeconds int64
		}{
			{name: "timeBankが0のとき、タイマーを登録しない", timeBankSeconds: 0},
			{name: "timeBankが -5のとき、タイマーを登録しない", timeBankSeconds: -5},
		}
		for _, tc := range nonPositiveCases {
			t.Run(tc.name, func(t *testing.T) {
				clock := &fakeAfterFuncClock{}
				relay, _ := newTestRelayWithClock(clock)
				relay.JoinGame("p1", "g1", 1)

				relay.resetTurnTimer("g1", "p1", tc.timeBankSeconds)

				assert.Empty(t, clock.calls, "no timer should be scheduled when timeBank<=0")
			})
		}
	})

	t.Run("ターンタイマー発火時の強制終了送信", func(t *testing.T) {
		t.Run("登録したタイマーが発火すると、そのプレイヤーの強制終了が送信される", func(t *testing.T) {
			clock := &fakeAfterFuncClock{}
			relay, bc := newTestRelayWithClock(clock)
			relay.JoinGame("p1", "g1", 1)

			relay.resetTurnTimer("g1", "p1", 60)
			require.Len(t, clock.calls, 1)
			clock.calls[0]()

			calls := bc.snapshotProcessActionCalls()
			require.Len(t, calls, 1)
			assert.Equal(t, gamelogic.ActionTypeForfeit, calls[0].actionType)
			assert.Equal(t, 1, calls[0].playerNum)
		})

		t.Run("ターンがp1からp2に代わった後、p2のタイマーが発火すると、p2の強制終了が送信される", func(t *testing.T) {
			clock := &fakeAfterFuncClock{}
			relay, bc := newTestRelayWithClock(clock)
			relay.JoinGame("p1", "g1", 1)
			relay.JoinGame("p2", "g1", 2)

			relay.resetTurnTimer("g1", "p1", 60)
			relay.resetTurnTimer("g1", "p2", 60)
			require.Len(t, clock.calls, 2)
			clock.calls[1]()

			calls := bc.snapshotProcessActionCalls()
			require.Len(t, calls, 1)
			assert.Equal(t, gamelogic.ActionTypeForfeit, calls[0].actionType)
			assert.Equal(t, 2, calls[0].playerNum)
		})

		t.Run("ターンがp1からp2に代わった後、旧タイマー(p1)が発火しても、強制終了は送信されない", func(t *testing.T) {
			// time.AfterFunc の Stop() は、コールバックの実行が既に始まっていると
			// 呼び出しても止められないことがある。この競合を、fake clock で捕捉した
			// コールバックを直接呼び出すことで再現する。
			clock := &fakeAfterFuncClock{}
			relay, bc := newTestRelayWithClock(clock)
			relay.JoinGame("p1", "g1", 1)
			relay.JoinGame("p2", "g1", 2)

			relay.resetTurnTimer("g1", "p1", 60)
			relay.resetTurnTimer("g1", "p2", 60)
			require.Len(t, clock.calls, 2)
			clock.calls[0]()

			assert.Empty(t, bc.snapshotProcessActionCalls())
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

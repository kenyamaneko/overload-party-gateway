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

		t.Run("正のtimeBankのとき、activePlayerID付きで登録される", func(t *testing.T) {
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

		t.Run("別プレイヤーp2で再登録すると、activePlayerIDがp2に置き換わる", func(t *testing.T) {
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

		t.Run("再登録すると、旧プレイヤーp1はactivePlayerIDに残らない", func(t *testing.T) {
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
		t.Run("ネットワークバッファが正の値である", func(t *testing.T) {
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

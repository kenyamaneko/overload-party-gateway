package ws

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gamelogic "github.com/kenyamaneko/overload-party-battle/packages/game-logic-constants-go"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

// newDisconnectResolutionRelay は BDR-002 の決着ロジックを検証するための GameRelay を返す。
// 実際の ConnectionHub を使うことで hub.IsConnected / hub.DisconnectDeadlineExpired が
// テストごとの接続状況を正しく反映する。
func newDisconnectResolutionRelay(entries []port.GamePlayerEntry, timerStore *fakeTimerStore) (*GameRelay, *mockBattleClient, *ConnectionHub) {
	bc := newMockBattleClient()
	var store port.TimerStore
	if timerStore != nil {
		store = timerStore
	}
	hub := NewConnectionHub(HubCallbacks{
		GetGameID:           func(string) (string, bool) { return "", false },
		OnDisconnectTimeout: func(string, string) {},
	}, store)
	repo := &mockGamePlayerRepo{lookupEntries: entries}
	relay := NewGameRelay(hub, bc, nil, repo, store)
	return relay, bc, hub
}

func TestOpponentPlayerID(t *testing.T) {
	t.Run("対戦相手の playerID 解決", func(t *testing.T) {
		tests := []struct {
			name    string
			entries []port.GamePlayerEntry
			selfID  string
			want    string
		}{
			{
				name: "2 人制で自分が 1P のとき、2P の playerID を返す",
				entries: []port.GamePlayerEntry{
					{PlayerNum: 1, PlayerID: "p1"},
					{PlayerNum: 2, PlayerID: "p2"},
				},
				selfID: "p1",
				want:   "p2",
			},
			{
				name: "2 人制で自分が 2P のとき、1P の playerID を返す",
				entries: []port.GamePlayerEntry{
					{PlayerNum: 1, PlayerID: "p1"},
					{PlayerNum: 2, PlayerID: "p2"},
				},
				selfID: "p2",
				want:   "p1",
			},
			{
				name: "エントリが 1 件 (NPC 戦) のとき、空文字を返す",
				entries: []port.GamePlayerEntry{
					{PlayerNum: 1, PlayerID: "p1"},
				},
				selfID: "p1",
				want:   "",
			},
			{
				name:    "エントリが 0 件のとき、空文字を返す",
				entries: nil,
				selfID:  "p1",
				want:    "",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.want, opponentPlayerID(tt.entries, tt.selfID))
			})
		}
	})
}

func TestHandleDisconnectTimeout_BDR002(t *testing.T) {
	entries := []port.GamePlayerEntry{
		{PlayerNum: 1, PlayerID: "p1"},
		{PlayerNum: 2, PlayerID: "p2"},
	}

	t.Run("切断猶予切れの決着判定", func(t *testing.T) {
		t.Run("対戦相手が接続中のとき、forfeit を実行する", func(t *testing.T) {
			relay, bc, hub := newDisconnectResolutionRelay(entries, nil)
			relay.JoinGame("p1", "g1", 1)
			hub.Register(NewConnection(nil, "p2"))
			bc.processActionResult = &service.ActionResult{}

			relay.HandleDisconnectTimeout("p1", "g1")

			require.Len(t, bc.processActionCalls, 1)
			assert.Equal(t, "g1", bc.processActionCalls[0].gameID)
			assert.Equal(t, 1, bc.processActionCalls[0].playerNum)
			assert.Equal(t, gamelogic.ActionTypeForfeit, bc.processActionCalls[0].actionType)
		})

		t.Run("対戦相手も切断中のとき、forfeit を実行せずターンタイマーも継続する", func(t *testing.T) {
			relay, bc, _ := newDisconnectResolutionRelay(entries, nil)
			relay.JoinGame("p1", "g1", 1)
			relay.resetTurnTimer("g1", "p1", 60)

			relay.HandleDisconnectTimeout("p1", "g1")

			assert.Empty(t, bc.processActionCalls, "must not forfeit while the opponent is also disconnected")
			relay.timerMu.Lock()
			_, stillRunning := relay.turnTimers["g1"]
			relay.timerMu.Unlock()
			assert.True(t, stillRunning, "turn timer must not be canceled while resolution is deferred")

			relay.cancelTurnTimer("g1")
		})

		t.Run("NPC 戦 (対戦相手が居ない) のとき、forfeit を実行する", func(t *testing.T) {
			relay, bc, _ := newDisconnectResolutionRelay([]port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "p1"}}, nil)
			relay.JoinGame("p1", "g1", 1)
			bc.processActionResult = &service.ActionResult{}

			relay.HandleDisconnectTimeout("p1", "g1")

			require.Len(t, bc.processActionCalls, 1)
		})

		t.Run("LookupGamePlayers が失敗するとき、forfeit を実行しない", func(t *testing.T) {
			relay, bc, _ := newDisconnectResolutionRelay(nil, nil)
			relay.gamePlayerRepo = &mockGamePlayerRepo{lookupErr: errors.New("db down")}
			relay.JoinGame("p1", "g1", 1)

			relay.HandleDisconnectTimeout("p1", "g1")

			assert.Empty(t, bc.processActionCalls)
		})
	})
}

func TestResolveStaleDisconnect(t *testing.T) {
	entries := []port.GamePlayerEntry{
		{PlayerNum: 1, PlayerID: "p1"},
		{PlayerNum: 2, PlayerID: "p2"},
	}

	t.Run("復帰時点での両者の猶予評価", func(t *testing.T) {
		t.Run("対戦相手が接続中のとき、何もしない", func(t *testing.T) {
			relay, bc, hub := newDisconnectResolutionRelay(entries, nil)
			hub.Register(NewConnection(nil, "p2"))

			relay.resolveStaleDisconnect("g1", "p1", false)

			assert.Empty(t, bc.processActionCalls)
		})

		t.Run("対戦相手が切断中だが猶予内のとき、何もしない", func(t *testing.T) {
			store := &fakeTimerStore{
				getDisconnectFound:  true,
				getDisconnectReturn: portDisconnectDeadline("g1", time.Now().Add(time.Minute)),
			}
			relay, bc, _ := newDisconnectResolutionRelay(entries, store)

			relay.resolveStaleDisconnect("g1", "p1", false)

			assert.Empty(t, bc.processActionCalls)
		})

		t.Run("対戦相手の猶予が切れており復帰した本人は猶予内だったとき、対戦相手の forfeit を実行する", func(t *testing.T) {
			store := &fakeTimerStore{
				getDisconnectFound:  true,
				getDisconnectReturn: portDisconnectDeadline("g1", time.Now().Add(-time.Minute)),
			}
			relay, bc, hub := newDisconnectResolutionRelay(entries, store)
			relay.JoinGame("p2", "g1", 2)
			hub.Register(NewConnection(nil, "p1")) // 復帰した本人は接続済み
			bc.processActionResult = &service.ActionResult{}

			relay.resolveStaleDisconnect("g1", "p1", false)

			require.Len(t, bc.processActionCalls, 1)
			assert.Equal(t, 2, bc.processActionCalls[0].playerNum, "forfeit must be attributed to the still-disconnected opponent")
			assert.Equal(t, gamelogic.ActionTypeForfeit, bc.processActionCalls[0].actionType)
		})

		t.Run("両者とも猶予が切れているとき、forfeit を実行せず未決着のまま残す", func(t *testing.T) {
			store := &fakeTimerStore{
				getDisconnectFound:  true,
				getDisconnectReturn: portDisconnectDeadline("g1", time.Now().Add(-time.Minute)),
			}
			relay, bc, _ := newDisconnectResolutionRelay(entries, store)

			relay.resolveStaleDisconnect("g1", "p1", true)

			assert.Empty(t, bc.processActionCalls, "must not declare either side a winner when both grace periods expired")
		})

		t.Run("NPC 戦 (対戦相手が居ない) のとき、何もしない", func(t *testing.T) {
			relay, bc, _ := newDisconnectResolutionRelay([]port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "p1"}}, nil)

			relay.resolveStaleDisconnect("g1", "p1", false)

			assert.Empty(t, bc.processActionCalls)
		})

		t.Run("gamePlayerRepo が無いとき、パニックしない", func(t *testing.T) {
			relay, _ := newTestRelay()

			assert.NotPanics(t, func() { relay.resolveStaleDisconnect("g1", "p1", false) })
		})

		t.Run("LookupGamePlayers が失敗するとき、forfeit を実行しない", func(t *testing.T) {
			relay, bc, _ := newDisconnectResolutionRelay(nil, nil)
			relay.gamePlayerRepo = &mockGamePlayerRepo{lookupErr: errors.New("db down")}

			relay.resolveStaleDisconnect("g1", "p1", false)

			assert.Empty(t, bc.processActionCalls)
		})
	})
}

func TestHandleReconnect(t *testing.T) {
	t.Run("復帰処理", func(t *testing.T) {
		t.Run("呼ぶと、対戦相手への通知と猶予評価の両方が行われる", func(t *testing.T) {
			entries := []port.GamePlayerEntry{
				{PlayerNum: 1, PlayerID: "p1"},
				{PlayerNum: 2, PlayerID: "p2"},
			}
			store := &fakeTimerStore{
				getDisconnectFound:  true,
				getDisconnectReturn: portDisconnectDeadline("g1", time.Now().Add(-time.Minute)),
			}
			relay, bc, hub := newDisconnectResolutionRelay(entries, store)
			relay.JoinGame("p1", "g1", 1)
			relay.JoinGame("p2", "g1", 2)
			hub.Register(NewConnection(nil, "p1"))
			bc.processActionResult = &service.ActionResult{}

			relay.HandleReconnect("p1", "g1", false)

			require.Len(t, bc.processActionCalls, 1, "stale disconnect evaluation must run")
			assert.Equal(t, 2, bc.processActionCalls[0].playerNum)
		})
	})
}

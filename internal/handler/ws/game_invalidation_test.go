package ws

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gamelogic "github.com/kenyamaneko/overload-party-battle/packages/game-logic-constants-go"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

// invalidationFixture は停止時の無効化と起動時の後始末を検証するための GameRelay 一式。
type invalidationFixture struct {
	relay            *GameRelay
	hub              *ConnectionHub
	battle           *mockBattleClient
	gamePlayers      *mockGamePlayerRepo
	invalidatedGames *fakeInvalidatedGameRepo
	timers           *fakeTimerStore
}

// newInvalidationFixture は entries のプレイヤーが参加する対戦を扱う fixture を組み立てる。
func newInvalidationFixture(entries []port.GamePlayerEntry, playerCounts map[string]int) *invalidationFixture {
	battle := newMockBattleClient()
	timers := &fakeTimerStore{}
	hub := NewConnectionHub(HubCallbacks{
		GetGameID:           func(string) (string, bool) { return "", false },
		OnDisconnectTimeout: func(string, string) {},
	}, DefaultDisconnectTimeout, timers)
	gamePlayers := &mockGamePlayerRepo{lookupEntries: entries, playerCounts: playerCounts}
	invalidatedGames := newFakeInvalidatedGameRepo()
	return &invalidationFixture{
		relay:            NewGameRelay(hub, battle, nil, gamePlayers, invalidatedGames, timers),
		hub:              hub,
		battle:           battle,
		gamePlayers:      gamePlayers,
		invalidatedGames: invalidatedGames,
		timers:           timers,
	}
}

func pvpEntries() []port.GamePlayerEntry {
	return []port.GamePlayerEntry{
		{PlayerNum: 1, PlayerID: "p1"},
		{PlayerNum: 2, PlayerID: "p2"},
	}
}

func TestInvalidateActiveGames(t *testing.T) {
	t.Run("停止時の対戦の無効化", func(t *testing.T) {
		t.Run("人間 2 人の対戦が進行中のとき、その対戦が無効として記録される", func(t *testing.T) {
			f := newInvalidationFixture(pvpEntries(), map[string]int{"g1": 2})
			f.relay.JoinGame("p1", "g1", 1)
			f.relay.JoinGame("p2", "g1", 2)

			f.relay.InvalidateActiveGames(context.Background())

			assert.Equal(t, []string{"g1"}, f.invalidatedGames.snapshotInvalidated())
		})

		t.Run("人間 1 人の対戦だけが進行中のとき、無効として記録される対戦は無い", func(t *testing.T) {
			f := newInvalidationFixture([]port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "p1"}}, map[string]int{"g1": 1})
			f.relay.JoinGame("p1", "g1", 1)

			f.relay.InvalidateActiveGames(context.Background())

			assert.Empty(t, f.invalidatedGames.snapshotInvalidated())
		})

		t.Run("進行中の対戦が無いとき、無効として記録される対戦は無い", func(t *testing.T) {
			f := newInvalidationFixture(pvpEntries(), map[string]int{"g1": 2})

			f.relay.InvalidateActiveGames(context.Background())

			assert.Empty(t, f.invalidatedGames.snapshotInvalidated())
		})

		t.Run("プレイヤー数の取得に失敗するとき、無効として記録される対戦は無い", func(t *testing.T) {
			f := newInvalidationFixture(pvpEntries(), nil)
			f.gamePlayers.playerCountsErr = errors.New("db down")
			f.relay.JoinGame("p1", "g1", 1)

			f.relay.InvalidateActiveGames(context.Background())

			assert.Empty(t, f.invalidatedGames.snapshotInvalidated())
		})

		t.Run("プレイヤーの切断で既に決着した対戦は、無効として記録されない", func(t *testing.T) {
			f := newInvalidationFixture(pvpEntries(), map[string]int{"g1": 2})
			f.relay.JoinGame("p1", "g1", 1)
			f.relay.JoinGame("p2", "g1", 2)
			f.hub.Register(NewConnection(nil, "p2"))
			f.battle.processActionResult = &service.ActionResult{
				GameOver:         true,
				WinningPlayerNum: 2,
				WinReason:        gamelogic.WinReasonDisconnect,
			}

			f.relay.HandleDisconnectTimeout("p1", "g1")
			f.relay.InvalidateActiveGames(context.Background())

			calls := f.battle.snapshotProcessActionCalls()
			require.Len(t, calls, 1)
			assert.Equal(t, gamelogic.ActionTypeForfeit, calls[0].actionType)
			assert.Empty(t, f.invalidatedGames.snapshotInvalidated())
		})
	})
}

func TestRecoverInvalidatedGames(t *testing.T) {
	t.Run("無効になった対戦の起動時の後始末", func(t *testing.T) {
		t.Run("決着していない記録があるとき、その対戦の両者強制決着が battle に要求される", func(t *testing.T) {
			f := newInvalidationFixture(pvpEntries(), map[string]int{"g1": 2})
			require.NoError(t, f.invalidatedGames.MarkInvalidated(context.Background(), []string{"g1"}))
			f.battle.processActionResult = &service.ActionResult{}

			f.relay.RecoverInvalidatedGames(context.Background())

			calls := f.battle.snapshotProcessActionCalls()
			require.Len(t, calls, 1)
			assert.Equal(t, "g1", calls[0].gameID)
			assert.Equal(t, gamelogic.ActionTypeForfeitBoth, calls[0].actionType)
		})

		t.Run("決着に成功したとき、その対戦の両プレイヤーの切断猶予の期限が消える", func(t *testing.T) {
			f := newInvalidationFixture(pvpEntries(), map[string]int{"g1": 2})
			require.NoError(t, f.invalidatedGames.MarkInvalidated(context.Background(), []string{"g1"}))
			f.battle.processActionResult = &service.ActionResult{}

			f.relay.RecoverInvalidatedGames(context.Background())

			assert.ElementsMatch(t, []string{"p1", "p2"}, f.timers.snapshotClearDisconnectCalls())
		})

		t.Run("決着に成功した対戦は、次の起動で再び要求されない", func(t *testing.T) {
			f := newInvalidationFixture(pvpEntries(), map[string]int{"g1": 2})
			require.NoError(t, f.invalidatedGames.MarkInvalidated(context.Background(), []string{"g1"}))
			f.battle.processActionResult = &service.ActionResult{}

			f.relay.RecoverInvalidatedGames(context.Background())
			f.relay.RecoverInvalidatedGames(context.Background())

			assert.Len(t, f.battle.snapshotProcessActionCalls(), 1)
		})

		t.Run("決着の要求が失敗した対戦は、次の起動で再び要求される", func(t *testing.T) {
			f := newInvalidationFixture(pvpEntries(), map[string]int{"g1": 2})
			require.NoError(t, f.invalidatedGames.MarkInvalidated(context.Background(), []string{"g1"}))
			f.battle.processActionErr = errors.New("battle down")

			f.relay.RecoverInvalidatedGames(context.Background())
			f.battle.processActionErr = nil
			f.battle.processActionResult = &service.ActionResult{}
			f.relay.RecoverInvalidatedGames(context.Background())

			calls := f.battle.snapshotProcessActionCalls()
			require.Len(t, calls, 2)
			assert.Equal(t, "g1", calls[1].gameID)
			assert.Equal(t, gamelogic.ActionTypeForfeitBoth, calls[1].actionType)
		})

		t.Run("決着済みの記録が失敗した対戦は、次の起動で再び要求される", func(t *testing.T) {
			f := newInvalidationFixture(pvpEntries(), map[string]int{"g1": 2})
			require.NoError(t, f.invalidatedGames.MarkInvalidated(context.Background(), []string{"g1"}))
			f.battle.processActionResult = &service.ActionResult{}
			f.invalidatedGames.markFinishedErr = errors.New("db down")

			f.relay.RecoverInvalidatedGames(context.Background())
			f.invalidatedGames.markFinishedErr = nil
			f.relay.RecoverInvalidatedGames(context.Background())

			assert.Len(t, f.battle.snapshotProcessActionCalls(), 2)
		})

		t.Run("記録の一覧の取得に失敗するとき、battle へ何も要求しない", func(t *testing.T) {
			f := newInvalidationFixture(pvpEntries(), map[string]int{"g1": 2})
			f.invalidatedGames.listUnfinishedErr = errors.New("db down")

			f.relay.RecoverInvalidatedGames(context.Background())

			assert.Empty(t, f.battle.snapshotProcessActionCalls())
		})
	})
}

func TestStaleDisconnectOnInvalidatedGame(t *testing.T) {
	t.Run("無効になった対戦の切断猶予の評価", func(t *testing.T) {
		t.Run("対戦が無効として記録済みのとき、両者の猶予が切れていても決着させない", func(t *testing.T) {
			f := newInvalidationFixture(pvpEntries(), map[string]int{"g1": 2})
			f.timers.getDisconnectFound = true
			f.timers.getDisconnectReturn = portDisconnectDeadline("g1", time.Now().Add(-time.Minute))
			require.NoError(t, f.invalidatedGames.MarkInvalidated(context.Background(), []string{"g1"}))
			f.hub.Register(NewConnection(nil, "p1"))

			f.relay.resolveStaleDisconnect("g1", "p1", true)

			assert.Empty(t, f.battle.snapshotProcessActionCalls())
		})

		t.Run("無効かどうかの確認に失敗するとき、決着させない", func(t *testing.T) {
			f := newInvalidationFixture(pvpEntries(), map[string]int{"g1": 2})
			f.timers.getDisconnectFound = true
			f.timers.getDisconnectReturn = portDisconnectDeadline("g1", time.Now().Add(-time.Minute))
			f.invalidatedGames.isInvalidatedErr = errors.New("db down")
			f.hub.Register(NewConnection(nil, "p1"))

			f.relay.resolveStaleDisconnect("g1", "p1", true)

			assert.Empty(t, f.battle.snapshotProcessActionCalls())
		})

		t.Run("対戦が無効として記録されていないとき、両者の猶予が切れていれば両者強制決着で終わる", func(t *testing.T) {
			f := newInvalidationFixture(pvpEntries(), map[string]int{"g1": 2})
			f.timers.getDisconnectFound = true
			f.timers.getDisconnectReturn = portDisconnectDeadline("g1", time.Now().Add(-time.Minute))
			f.hub.Register(NewConnection(nil, "p1"))
			f.battle.processActionResult = &service.ActionResult{}

			f.relay.resolveStaleDisconnect("g1", "p1", true)

			calls := f.battle.snapshotProcessActionCalls()
			require.Len(t, calls, 1)
			assert.Equal(t, gamelogic.ActionTypeForfeitBoth, calls[0].actionType)
		})
	})
}

func TestGameEnterOnInvalidatedGame(t *testing.T) {
	t.Run("無効になった対戦への入室", func(t *testing.T) {
		enterData := mustMarshal(GameEnterMessage{GameID: "g1"})

		t.Run("対戦が無効として記録済みのとき、無効になったことを伝えるエラーが返る", func(t *testing.T) {
			f := newInvalidationFixture(pvpEntries(), map[string]int{"g1": 2})
			require.NoError(t, f.invalidatedGames.MarkInvalidated(context.Background(), []string{"g1"}))
			conn := NewConnection(nil, "p1")

			f.relay.HandleGameEnter(conn, enterData)

			msg := findSentMessage(t, conn, genws.WSServerMsgError)
			var errMsg ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &errMsg))
			assert.Equal(t, "game_invalidated", errMsg.ErrorCode)
			assert.False(t, errMsg.Retryable)
		})

		t.Run("無効かどうかの確認に失敗するとき、やり直せるエラーが返る", func(t *testing.T) {
			f := newInvalidationFixture(pvpEntries(), map[string]int{"g1": 2})
			f.invalidatedGames.isInvalidatedErr = errors.New("db down")
			conn := NewConnection(nil, "p1")

			f.relay.HandleGameEnter(conn, enterData)

			msg := findSentMessage(t, conn, genws.WSServerMsgError)
			var errMsg ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &errMsg))
			assert.Equal(t, "game_error", errMsg.ErrorCode)
			assert.True(t, errMsg.Retryable)
		})

		t.Run("対戦が無効として記録されていないとき、入室できる", func(t *testing.T) {
			f := newInvalidationFixture(pvpEntries(), map[string]int{"g1": 2})
			f.gamePlayers.lookupPlayerNum = 1
			conn := NewConnection(nil, "p1")

			f.relay.HandleGameEnter(conn, enterData)

			msg := findSentMessage(t, conn, genws.WSServerMsgGameEntered)
			var entered map[string]string
			require.NoError(t, json.Unmarshal(msg.Data, &entered))
			assert.Equal(t, "g1", entered["game_id"])
		})
	})
}

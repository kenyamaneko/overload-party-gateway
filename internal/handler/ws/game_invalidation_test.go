package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	"github.com/kenyamaneko/overload-party-account/packages/api-account/apiaccountserverfake"
	gamelogic "github.com/kenyamaneko/overload-party-battle/packages/game-logic-constants-go"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

// gamePlayersCreatedAt はテストの対戦で game_players の行が作られた時刻。
var gamePlayersCreatedAt = time.Date(2026, 8, 5, 3, 0, 0, 0, time.UTC)

// gamePlayersCreatedAtMillis は gamePlayersCreatedAt の UNIX ミリ秒。
const gamePlayersCreatedAtMillis int64 = 1785898800000

// invalidationFixture は停止時の無効化と起動時の後始末を検証するための GameRelay 一式。
type invalidationFixture struct {
	relay            *GameRelay
	hub              *ConnectionHub
	battle           *mockBattleClient
	gamePlayers      *mockGamePlayerRepo
	invalidatedGames *fakeInvalidatedGameRepo
	timers           *fakeTimerStore
	account          *fakeAccountRevert
}

// newInvalidationFixture は entries のプレイヤーが参加する対戦を扱う fixture を組み立てる。
func newInvalidationFixture(t *testing.T, entries []port.GamePlayerEntry, playerCounts map[string]int) *invalidationFixture {
	t.Helper()
	battle := newMockBattleClient()
	timers := &fakeTimerStore{}
	hub := NewConnectionHub(HubCallbacks{
		GetGameID:           func(string) (string, bool) { return "", false },
		OnDisconnectTimeout: func(string, string) {},
	}, DefaultDisconnectTimeout, timers)
	gamePlayers := &mockGamePlayerRepo{lookupEntries: entries, playerCounts: playerCounts}
	invalidatedGames := newFakeInvalidatedGameRepo()
	account := startFakeAccountRevert(t)
	return &invalidationFixture{
		relay:            NewGameRelay(hub, battle, accountclient.New(account.url, &http.Client{}), gamePlayers, invalidatedGames),
		hub:              hub,
		battle:           battle,
		gamePlayers:      gamePlayers,
		invalidatedGames: invalidatedGames,
		timers:           timers,
		account:          account,
	}
}

// fakeAccountRevert は account の返却 API が受け取ったリクエストを集める偽の account サービス。
type fakeAccountRevert struct {
	url string

	mu       sync.Mutex
	received []apiaccount.RevertBattleCountRequest
	// status が 0 でないとき、返却の応答をそのステータスにする。
	status int
}

func startFakeAccountRevert(t *testing.T) *fakeAccountRevert {
	t.Helper()
	f := &fakeAccountRevert{}
	srv := apiaccountserverfake.NewServer()
	srv.RevertBattleCountFn = func(req apiaccount.RevertBattleCountRequest) (int, any) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.received = append(f.received, req)
		if f.status != 0 {
			return f.status, nil
		}
		return http.StatusNoContent, nil
	}
	t.Cleanup(srv.Close)
	f.url = srv.URL()
	return f
}

// snapshotReceived は返却 API が受け取ったリクエストを受信順に返す。
func (f *fakeAccountRevert) snapshotReceived() []apiaccount.RevertBattleCountRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]apiaccount.RevertBattleCountRequest(nil), f.received...)
}

// setStatus は返却 API が返すステータスを差し替える。
func (f *fakeAccountRevert) setStatus(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = status
}

func pvpEntries() []port.GamePlayerEntry {
	return []port.GamePlayerEntry{
		{PlayerNum: 1, PlayerID: "p1", CreatedAt: gamePlayersCreatedAt},
		{PlayerNum: 2, PlayerID: "p2", CreatedAt: gamePlayersCreatedAt},
	}
}

func npcEntries() []port.GamePlayerEntry {
	return []port.GamePlayerEntry{
		{PlayerNum: 1, PlayerID: "p1", CreatedAt: gamePlayersCreatedAt},
	}
}

func TestInvalidateActiveGames(t *testing.T) {
	t.Run("[停止時の対戦保護]停止時の対戦の無効化", func(t *testing.T) {
		t.Run("人間2人の対戦が進行中のとき、その対戦が無効として記録される", func(t *testing.T) {
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})
			f.relay.JoinGame("p1", "g1", 1)
			f.relay.JoinGame("p2", "g1", 2)

			f.relay.InvalidateActiveGames(context.Background())

			assert.Equal(t, []string{"g1"}, f.invalidatedGames.snapshotInvalidated())
		})

		t.Run("人間1人の対戦だけが進行中のとき、無効として記録される対戦は無い", func(t *testing.T) {
			f := newInvalidationFixture(t, npcEntries(), map[string]int{"g1": 1})
			f.relay.JoinGame("p1", "g1", 1)

			f.relay.InvalidateActiveGames(context.Background())

			assert.Empty(t, f.invalidatedGames.snapshotInvalidated())
		})

		t.Run("進行中の対戦が無いとき、無効として記録される対戦は無い", func(t *testing.T) {
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})

			f.relay.InvalidateActiveGames(context.Background())

			assert.Empty(t, f.invalidatedGames.snapshotInvalidated())
		})

		t.Run("プレイヤー数の取得に失敗するとき、無効として記録される対戦は無い", func(t *testing.T) {
			f := newInvalidationFixture(t, pvpEntries(), nil)
			f.gamePlayers.playerCountsErr = errors.New("db down")
			f.relay.JoinGame("p1", "g1", 1)

			f.relay.InvalidateActiveGames(context.Background())

			assert.Empty(t, f.invalidatedGames.snapshotInvalidated())
		})

		t.Run("プレイヤーの切断で既に決着した対戦は、無効として記録されない", func(t *testing.T) {
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})
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

func TestManagerShutdown(t *testing.T) {
	t.Run("[停止時の対戦保護]停止時の一連の処理", func(t *testing.T) {
		t.Run("進行中の対戦があるとき、その対戦が無効として記録され、登録されている接続はサーバー更新を理由に閉じられる", func(t *testing.T) {
			battle := newMockBattleClient()
			gamePlayers := &mockGamePlayerRepo{lookupEntries: pvpEntries(), playerCounts: map[string]int{"g1": 2}}
			invalidatedGames := newFakeInvalidatedGameRepo()
			m := NewManager(battle, nil, nil, noopMatchmakingClient{}, gamePlayers, nil, invalidatedGames, 0, nil, nil, DefaultDisconnectTimeout)
			conn := NewConnection(nil, "p1")
			m.Hub.Register(conn)
			m.Relay.JoinGame("p1", "g1", 1)
			m.Relay.JoinGame("p2", "g1", 2)

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			m.Shutdown(ctx)

			assert.Equal(t, []string{"g1"}, invalidatedGames.snapshotInvalidated())
			conn.mu.Lock()
			isClosed, reason := conn.isClosed, conn.closeReason
			conn.mu.Unlock()
			assert.True(t, isClosed)
			assert.Equal(t, genws.WSServerMsgServerUpdate, reason)
		})
	})
}

func TestRecoverInvalidatedGames(t *testing.T) {
	t.Run("[停止時の対戦保護]無効になった対戦の起動時の後始末", func(t *testing.T) {
		t.Run("決着していない対戦があるとき、その対戦の両者強制決着がbattleに要求される", func(t *testing.T) {
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})
			require.NoError(t, f.invalidatedGames.MarkInvalidated(context.Background(), []string{"g1"}))
			f.battle.processActionResult = &service.ActionResult{}

			f.relay.RecoverInvalidatedGames(context.Background())

			calls := f.battle.snapshotProcessActionCalls()
			require.Len(t, calls, 1)
			assert.Equal(t, "g1", calls[0].gameID)
			assert.Equal(t, gamelogic.ActionTypeForfeitBoth, calls[0].actionType)
		})

		t.Run("対戦の決着に成功したとき、両プレイヤーの切断猶予の期限が消える", func(t *testing.T) {
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})
			require.NoError(t, f.invalidatedGames.MarkInvalidated(context.Background(), []string{"g1"}))
			f.battle.processActionResult = &service.ActionResult{}

			f.relay.RecoverInvalidatedGames(context.Background())

			assert.ElementsMatch(t, []string{"p1", "p2"}, f.timers.snapshotClearDisconnectCalls())
		})

		t.Run("決着に成功した対戦は、次の起動で再び要求されない", func(t *testing.T) {
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})
			require.NoError(t, f.invalidatedGames.MarkInvalidated(context.Background(), []string{"g1"}))
			f.battle.processActionResult = &service.ActionResult{}

			f.relay.RecoverInvalidatedGames(context.Background())
			f.relay.RecoverInvalidatedGames(context.Background())

			assert.Len(t, f.battle.snapshotProcessActionCalls(), 1)
		})

		t.Run("決着の要求が失敗した対戦は、次の起動で再び要求される", func(t *testing.T) {
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})
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
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})
			require.NoError(t, f.invalidatedGames.MarkInvalidated(context.Background(), []string{"g1"}))
			f.battle.processActionResult = &service.ActionResult{}
			f.invalidatedGames.markFinishedErr = errors.New("db down")

			f.relay.RecoverInvalidatedGames(context.Background())
			f.invalidatedGames.markFinishedErr = nil
			f.relay.RecoverInvalidatedGames(context.Background())

			assert.Len(t, f.battle.snapshotProcessActionCalls(), 2)
		})

		t.Run("決着していない対戦の一覧の取得に失敗するとき、battleへ何も要求しない", func(t *testing.T) {
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})
			f.invalidatedGames.listUnfinishedErr = errors.New("db down")

			f.relay.RecoverInvalidatedGames(context.Background())

			assert.Empty(t, f.battle.snapshotProcessActionCalls())
		})
	})
}

func TestRevertBattleCountOfInvalidatedGames(t *testing.T) {
	t.Run("[停止時の対戦保護]無効になった対戦の消費バトル回数の返却", func(t *testing.T) {
		markInvalidated := func(t *testing.T, f *invalidationFixture, gameID string) {
			t.Helper()
			require.NoError(t, f.invalidatedGames.MarkInvalidated(context.Background(), []string{gameID}))
			f.battle.processActionResult = &service.ActionResult{}
		}

		t.Run("決着に成功した対戦のとき、両プレイヤーの消費バトル回数が対戦の識別子と消費時刻を添えて戻される", func(t *testing.T) {
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})
			markInvalidated(t, f, "g1")

			f.relay.RecoverInvalidatedGames(context.Background())

			received := f.account.snapshotReceived()
			require.Len(t, received, 1)
			assert.Equal(t, "g1", received[0].GameID)
			assert.Equal(t, "p1", received[0].Player1ID)
			assert.Equal(t, "p2", received[0].Player2ID)
			assert.Equal(t, gamePlayersCreatedAtMillis, received[0].ConsumedAtMillis)
		})

		t.Run("スロット1の行が後から作られたとき、早い方の作成時刻が消費時刻としてaccountに渡る", func(t *testing.T) {
			entries := []port.GamePlayerEntry{
				{PlayerNum: 1, PlayerID: "p1", CreatedAt: gamePlayersCreatedAt.Add(7 * time.Second)},
				{PlayerNum: 2, PlayerID: "p2", CreatedAt: gamePlayersCreatedAt},
			}
			f := newInvalidationFixture(t, entries, map[string]int{"g1": 2})
			markInvalidated(t, f, "g1")

			f.relay.RecoverInvalidatedGames(context.Background())

			received := f.account.snapshotReceived()
			require.Len(t, received, 1)
			assert.Equal(t, gamePlayersCreatedAtMillis, received[0].ConsumedAtMillis)
		})

		t.Run("消費バトル回数を戻すことに成功した対戦は、次の起動でも重ねて戻されない", func(t *testing.T) {
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})
			markInvalidated(t, f, "g1")

			f.relay.RecoverInvalidatedGames(context.Background())
			f.relay.RecoverInvalidatedGames(context.Background())

			assert.Len(t, f.account.snapshotReceived(), 1)
		})

		t.Run("消費バトル回数を戻すことに失敗した対戦は、次の起動でもう一度戻される", func(t *testing.T) {
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})
			markInvalidated(t, f, "g1")
			f.account.setStatus(http.StatusInternalServerError)

			f.relay.RecoverInvalidatedGames(context.Background())
			f.account.setStatus(http.StatusNoContent)
			f.relay.RecoverInvalidatedGames(context.Background())

			received := f.account.snapshotReceived()
			require.Len(t, received, 2)
			assert.Equal(t, "g1", received[1].GameID)
			assert.Equal(t, gamePlayersCreatedAtMillis, received[1].ConsumedAtMillis)
		})

		t.Run("消費バトル回数を返却済みとする記録に失敗した対戦は、次の起動でもう一度戻される", func(t *testing.T) {
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})
			markInvalidated(t, f, "g1")
			f.invalidatedGames.markRevertedErr = errors.New("db down")

			f.relay.RecoverInvalidatedGames(context.Background())
			f.invalidatedGames.markRevertedErr = nil
			f.relay.RecoverInvalidatedGames(context.Background())

			assert.Len(t, f.account.snapshotReceived(), 2)
		})

		t.Run("決着の要求が失敗した対戦は、消費バトル回数が戻されない", func(t *testing.T) {
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})
			require.NoError(t, f.invalidatedGames.MarkInvalidated(context.Background(), []string{"g1"}))
			f.battle.processActionErr = errors.New("battle down")

			f.relay.RecoverInvalidatedGames(context.Background())

			assert.Empty(t, f.account.snapshotReceived())
		})

		t.Run("返却待ちの一覧の取得に失敗するとき、消費バトル回数が戻されない", func(t *testing.T) {
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})
			markInvalidated(t, f, "g1")
			f.invalidatedGames.listRevertPendingErr = errors.New("db down")

			f.relay.RecoverInvalidatedGames(context.Background())

			assert.Empty(t, f.account.snapshotReceived())
		})

		t.Run("プレイヤーの切断で決着した対戦が停止に巻き込まれても、消費バトル回数が戻されない", func(t *testing.T) {
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})
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
			f.relay.RecoverInvalidatedGames(context.Background())

			assert.Empty(t, f.account.snapshotReceived())
		})

		t.Run("人間1人の対戦が停止に巻き込まれたとき、消費バトル回数が戻されない", func(t *testing.T) {
			f := newInvalidationFixture(t, npcEntries(), map[string]int{"g1": 1})
			f.relay.JoinGame("p1", "g1", 1)
			f.battle.processActionResult = &service.ActionResult{}

			f.relay.InvalidateActiveGames(context.Background())
			f.relay.RecoverInvalidatedGames(context.Background())

			assert.Empty(t, f.account.snapshotReceived())
		})

		t.Run("人間1人の対戦の記録が残っているとき、消費バトル回数が戻されない", func(t *testing.T) {
			f := newInvalidationFixture(t, npcEntries(), map[string]int{"g1": 1})
			markInvalidated(t, f, "g1")

			f.relay.RecoverInvalidatedGames(context.Background())

			assert.Empty(t, f.account.snapshotReceived())
		})
	})
}

func TestStaleDisconnectOnInvalidatedGame(t *testing.T) {
	t.Run("[停止時の対戦保護]無効になった対戦の切断猶予の評価", func(t *testing.T) {
		t.Run("対戦が無効として記録済みのとき、両者の切断猶予が切れていても決着させない", func(t *testing.T) {
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})
			f.timers.getDisconnectFound = true
			f.timers.getDisconnectReturn = portDisconnectDeadline("g1", time.Now().Add(-time.Minute))
			require.NoError(t, f.invalidatedGames.MarkInvalidated(context.Background(), []string{"g1"}))
			f.hub.Register(NewConnection(nil, "p1"))

			f.relay.resolveStaleDisconnect("g1", "p1", true)

			assert.Empty(t, f.battle.snapshotProcessActionCalls())
		})

		t.Run("対戦が無効かどうかの確認に失敗するとき、決着させない", func(t *testing.T) {
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})
			f.timers.getDisconnectFound = true
			f.timers.getDisconnectReturn = portDisconnectDeadline("g1", time.Now().Add(-time.Minute))
			f.invalidatedGames.isInvalidatedErr = errors.New("db down")
			f.hub.Register(NewConnection(nil, "p1"))

			f.relay.resolveStaleDisconnect("g1", "p1", true)

			assert.Empty(t, f.battle.snapshotProcessActionCalls())
		})

		t.Run("対戦が無効として記録されていないとき、両者の切断猶予が切れていれば両者強制決着で終わる", func(t *testing.T) {
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})
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
	t.Run("[停止時の対戦保護]無効になった対戦への入室", func(t *testing.T) {
		enterData := mustMarshal(GameEnterMessage{GameID: "g1"})

		t.Run("対戦が無効として記録済みのとき、無効になったことを伝えるエラーが返る", func(t *testing.T) {
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})
			require.NoError(t, f.invalidatedGames.MarkInvalidated(context.Background(), []string{"g1"}))
			conn := NewConnection(nil, "p1")

			f.relay.HandleGameEnter(conn, enterData)

			msg := findSentMessage(t, conn, genws.WSServerMsgError)
			var errMsg ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &errMsg))
			assert.Equal(t, "game_invalidated", errMsg.ErrorCode)
			assert.False(t, errMsg.Retryable)
		})

		t.Run("対戦が無効かどうかの確認に失敗するとき、やり直せるエラーが返る", func(t *testing.T) {
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})
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
			f := newInvalidationFixture(t, pvpEntries(), map[string]int{"g1": 2})
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

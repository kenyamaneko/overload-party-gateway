package ws

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
	wsconst "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

func alwaysFailProcessAction(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
	return nil, errors.New("battle process action failed")
}

type turnTimerFixture struct {
	relay          *GameRelay
	clock          *fakeClock
	battle         *stubBattleClient
	gameID         string
	activeClient   *testClientConn
	opponentClient *testClientConn
}

// newTurnTimerFixture は player-active / player-opponent の双方をソケット接続し、
// 両者をゲームへ参加登録した状態を作る。processAction には強制決着要求
// (ProcessAction) の戻り値を差し替えるフックを渡す。
func newTurnTimerFixture(t *testing.T, processAction func(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error)) *turnTimerFixture {
	t.Helper()
	const gameID = "game-timeout"
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	battle := &stubBattleClient{processActionFunc: processAction}
	gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gid string) ([]port.GamePlayerEntry, error) {
		return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-active"}, {PlayerNum: 2, PlayerID: "player-opponent"}}, nil
	}}
	hub := newTestHub(t, hubDeps{clock: clock})
	relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers, clock: clock})
	factory := newTestSocketFactory(t, hub)
	activeClient, _ := factory.connect(t, "player-active")
	opponentClient, _ := factory.connect(t, "player-opponent")
	relay.JoinGame("player-active", gameID, 1)
	relay.JoinGame("player-opponent", gameID, 2)
	return &turnTimerFixture{relay: relay, clock: clock, battle: battle, gameID: gameID, activeClient: activeClient, opponentClient: opponentClient}
}

type turnTimeoutSuccessFixture struct {
	relay          *GameRelay
	account        *stubAccountClient
	activeClient   *testClientConn
	opponentClient *testClientConn
	gameID         string
}

// newTurnTimeoutSuccessFixture は player-active のターン切れ強制決着が対戦サービスで成功し、
// 対戦終了と判定されるシナリオの構成一式を用意する。各サブテストが同じ WS メッセージ
// チャネルを共有せず単独実行できるよう、呼び出しごとに独立した接続一式を作る。
func newTurnTimeoutSuccessFixture(t *testing.T) *turnTimeoutSuccessFixture {
	t.Helper()
	const gameID = "game-timeout-success"
	state := newRawGameState(t, minimalClientGameState())
	battle := &stubBattleClient{
		processActionFunc: func(ctx context.Context, gid string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
			return &service.ActionResult{GameOver: true, WinningPlayerNum: 2, WinReason: "turn_timeout"}, nil
		},
		getGameStateForPlayerFunc: func(ctx context.Context, gid string, playerNum int) (json.RawMessage, error) {
			return state, nil
		},
	}
	account := &stubAccountClient{}
	gamePlayers := &stubGamePlayerRepo{
		lookupGamePlayersFunc: func(ctx context.Context, gid string) ([]port.GamePlayerEntry, error) {
			return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-active"}, {PlayerNum: 2, PlayerID: "player-opponent"}}, nil
		},
		markExpAwardedFunc: func(ctx context.Context, gid string) (bool, error) { return true, nil },
	}
	hub := newTestHub(t, hubDeps{})
	relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, accountClient: account, gamePlayerRepo: gamePlayers})
	factory := newTestSocketFactory(t, hub)
	activeClient, _ := factory.connect(t, "player-active")
	opponentClient, _ := factory.connect(t, "player-opponent")
	relay.JoinGame("player-active", gameID, 1)
	relay.JoinGame("player-opponent", gameID, 2)
	return &turnTimeoutSuccessFixture{relay: relay, account: account, activeClient: activeClient, opponentClient: opponentClient, gameID: gameID}
}

func TestResetTurnTimer(t *testing.T) {
	t.Run("[ターン進行]ターンタイマーの設定・更新", func(t *testing.T) {
		t.Run("タイムバンクが0秒のとき、期限切れによる強制決着は発生しない", func(t *testing.T) {
			f := newTurnTimerFixture(t, alwaysFailProcessAction)

			f.relay.resetTurnTimer(f.gameID, "player-active", 0)
			f.clock.Advance(24 * time.Hour)

			f.activeClient.expectNoMessage(t)
			assert.Empty(t, f.battle.processActionCallsSnapshot())
		})

		t.Run("タイムバンクが1秒のとき、設定から3秒が経過するまでは何も起こらない", func(t *testing.T) {
			f := newTurnTimerFixture(t, alwaysFailProcessAction)

			f.relay.resetTurnTimer(f.gameID, "player-active", 1)
			f.clock.Advance(3*time.Second - time.Nanosecond)

			f.activeClient.expectNoMessage(t)
			assert.Empty(t, f.battle.processActionCallsSnapshot())
		})

		t.Run("タイムバンクが1秒のとき、設定から3秒が経過すると、そのときアクティブなプレイヤーのターン切れを理由とする強制決着が処理される", func(t *testing.T) {
			f := newTurnTimerFixture(t, alwaysFailProcessAction)

			f.relay.resetTurnTimer(f.gameID, "player-active", 1)
			f.clock.Advance(3 * time.Second)

			f.activeClient.readMessage(t)
			calls := f.battle.processActionCallsSnapshot()
			require.Len(t, calls, 1)
			assert.Equal(t, 1, calls[0].playerNum)
		})

		t.Run("対戦に既存のタイマーがある状態で、同じアクティブプレイヤー・同じタイムバンクのままタイマーを設定し直したとき、既存のタイマーがそのまま継続し、期限は変わらない", func(t *testing.T) {
			f := newTurnTimerFixture(t, alwaysFailProcessAction)

			f.relay.resetTurnTimer(f.gameID, "player-active", 1)
			f.clock.Advance(2 * time.Second)
			f.relay.resetTurnTimer(f.gameID, "player-active", 1)
			f.clock.Advance(1 * time.Second)

			f.activeClient.readMessage(t)
		})

		t.Run("対戦に既存のタイマーがある状態で、同じアクティブプレイヤーのままタイムバンクが変化した状態でタイマーを設定し直したとき、期限は新しい設定に置き換わる", func(t *testing.T) {
			f := newTurnTimerFixture(t, alwaysFailProcessAction)

			f.relay.resetTurnTimer(f.gameID, "player-active", 1)
			f.clock.Advance(2 * time.Second)
			f.relay.resetTurnTimer(f.gameID, "player-active", 5)
			f.clock.Advance(1 * time.Second)
			f.activeClient.expectNoMessage(t)

			f.clock.Advance(6 * time.Second)
			f.activeClient.readMessage(t)
		})

		t.Run("対戦に既存のタイマーがある状態で、異なるアクティブプレイヤーでタイマーを設定し直したとき、期限は新しい設定に置き換わり、元の期限が来ても元のプレイヤーの強制決着は発生しない", func(t *testing.T) {
			f := newTurnTimerFixture(t, alwaysFailProcessAction)

			f.relay.resetTurnTimer(f.gameID, "player-active", 1)
			f.clock.Advance(2 * time.Second)
			f.relay.resetTurnTimer(f.gameID, "player-opponent", 1)
			f.clock.Advance(1 * time.Second)
			f.activeClient.expectNoMessage(t)
			f.opponentClient.expectNoMessage(t)

			f.clock.Advance(2 * time.Second)
			f.opponentClient.readMessage(t)
			f.activeClient.expectNoMessage(t)

			calls := f.battle.processActionCallsSnapshot()
			require.Len(t, calls, 1)
			assert.Equal(t, 2, calls[0].playerNum)
		})
	})
}

func TestCancelTurnTimer(t *testing.T) {
	t.Run("[ターン進行]ターンタイマーの取り消し", func(t *testing.T) {
		t.Run("対戦にタイマーが設定されているとき、取り消すと、その期限が来ても強制決着は発生しない", func(t *testing.T) {
			f := newTurnTimerFixture(t, alwaysFailProcessAction)
			f.relay.resetTurnTimer(f.gameID, "player-active", 1)

			f.relay.cancelTurnTimer(f.gameID)
			f.clock.Advance(10 * time.Second)

			f.activeClient.expectNoMessage(t)
			assert.Empty(t, f.battle.processActionCallsSnapshot())
		})

		t.Run("対戦にタイマーが設定されていないとき、取り消し操作をしてもエラーにならない", func(t *testing.T) {
			f := newTurnTimerFixture(t, alwaysFailProcessAction)

			assert.NotPanics(t, func() { f.relay.cancelTurnTimer("game-without-any-timer") })
		})
	})
}

func TestHandleTurnTimeout(t *testing.T) {
	t.Run("[ターン進行]ターン制限時間超過時の処理", func(t *testing.T) {
		t.Run("対戦相手も切断中のとき、時間切れになっても、誰にも通知が届かず、対戦サービスへの時間切れの登録は行われない(処理は先送りされる)", func(t *testing.T) {
			battle := &stubBattleClient{}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gid string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-active"}, {PlayerNum: 2, PlayerID: "player-opponent"}}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})

			relay.handleTurnTimeout("game-1", "player-active")

			assert.Empty(t, battle.processActionCallsSnapshot())
		})

		t.Run("対戦相手の切断状況の確認に失敗したとき(異常系)、時間切れになっても、誰にも通知が届かず、対戦サービスへの時間切れの登録は行われない", func(t *testing.T) {
			battle := &stubBattleClient{}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gid string) ([]port.GamePlayerEntry, error) {
				return nil, errors.New("lookup game players failed")
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})

			relay.handleTurnTimeout("game-1", "player-active")

			assert.Empty(t, battle.processActionCallsSnapshot())
		})

		t.Run("対戦相手が接続中で、時間切れになったプレイヤーの識別に失敗したとき(異常系)、誰にも通知が届かず、対戦サービスへの時間切れの登録は行われない", func(t *testing.T) {
			battle := &stubBattleClient{}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gid string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "someone-else"}}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})

			relay.handleTurnTimeout("game-1", "player-active")

			assert.Empty(t, battle.processActionCallsSnapshot())
		})

		t.Run("対戦相手が接続中で、対戦サービスへの時間切れの登録が失敗したとき(異常系)、時間切れになった本人へ、登録に失敗した旨のエラー通知が届く", func(t *testing.T) {
			f := newTurnTimerFixture(t, alwaysFailProcessAction)

			f.relay.handleTurnTimeout(f.gameID, "player-active")

			msg := f.activeClient.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "turn_timeout_failed", payload.ErrorCode)
			f.opponentClient.expectNoMessage(t)
		})

		t.Run("対戦相手が接続中で、対戦サービスへの登録が成功し対戦が終了と判定されたとき", func(t *testing.T) {
			t.Run("その時点の参加登録者全員に最新の対戦状態が届く", func(t *testing.T) {
				f := newTurnTimeoutSuccessFixture(t)

				f.relay.handleTurnTimeout(f.gameID, "player-active")

				f.activeClient.readMessagesOfType(t, wsconst.WSServerMsgGameState)
				f.opponentClient.readMessagesOfType(t, wsconst.WSServerMsgGameState)
			})

			t.Run("その時点の参加登録者全員へターン切れを理由とする対戦終了の通知が届く", func(t *testing.T) {
				f := newTurnTimeoutSuccessFixture(t)

				f.relay.handleTurnTimeout(f.gameID, "player-active")

				activeMsgs := f.activeClient.readMessagesOfType(t, wsconst.WSServerMsgGameState, wsconst.WSServerMsgGameOver)
				opponentMsgs := f.opponentClient.readMessagesOfType(t, wsconst.WSServerMsgGameState, wsconst.WSServerMsgGameOver)
				var activePayload, opponentPayload GameOverMessage
				require.NoError(t, json.Unmarshal(activeMsgs[1].Data, &activePayload))
				require.NoError(t, json.Unmarshal(opponentMsgs[1].Data, &opponentPayload))
				assert.Equal(t, "turn_timeout", activePayload.WinReason)
				assert.Equal(t, "turn_timeout", opponentPayload.WinReason)
			})

			t.Run("経験値付与処理が呼び出される", func(t *testing.T) {
				f := newTurnTimeoutSuccessFixture(t)

				f.relay.handleTurnTimeout(f.gameID, "player-active")

				assert.Len(t, f.account.awardGameExpCallsSnapshot(), 1)
			})

			t.Run("その後全員がこの対戦の参加登録者でなくなる", func(t *testing.T) {
				f := newTurnTimeoutSuccessFixture(t)

				f.relay.handleTurnTimeout(f.gameID, "player-active")

				gid, ok := f.relay.GameIDForPlayer("player-active")
				assert.False(t, ok)
				assert.Empty(t, gid)
				gid, ok = f.relay.GameIDForPlayer("player-opponent")
				assert.False(t, ok)
				assert.Empty(t, gid)
			})
		})

		t.Run("対戦相手が接続中で、対戦サービスへの登録が成功したが対戦が終了と判定されなかったとき、対戦状態の配信も対戦終了の通知も行われない", func(t *testing.T) {
			gameID := "game-timeout-continue"
			battle := &stubBattleClient{
				processActionFunc: func(ctx context.Context, gid string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
					return &service.ActionResult{GameOver: false}, nil
				},
			}
			gamePlayers := &stubGamePlayerRepo{lookupGamePlayersFunc: func(ctx context.Context, gid string) ([]port.GamePlayerEntry, error) {
				return []port.GamePlayerEntry{{PlayerNum: 1, PlayerID: "player-active"}, {PlayerNum: 2, PlayerID: "player-opponent"}}, nil
			}}
			hub := newTestHub(t, hubDeps{})
			relay := newTestGameRelay(t, relayDeps{hub: hub, battleClient: battle, gamePlayerRepo: gamePlayers})
			factory := newTestSocketFactory(t, hub)
			activeClient, _ := factory.connect(t, "player-active")
			opponentClient, _ := factory.connect(t, "player-opponent")
			relay.JoinGame("player-active", gameID, 1)
			relay.JoinGame("player-opponent", gameID, 2)

			relay.handleTurnTimeout(gameID, "player-active")

			activeClient.expectNoMessage(t)
			opponentClient.expectNoMessage(t)
		})
	})
}

func TestIsCanceled(t *testing.T) {
	t.Run("[ターン進行]エラー要因が中断によるものかどうかの判定", func(t *testing.T) {
		t.Run("呼び出し元の停止によりコンテキストが取り消されたことが原因のエラーのとき、中断によるものと判定される", func(t *testing.T) {
			assert.True(t, isCanceled(context.Canceled))
		})

		t.Run("コンテキストの期限切れが原因のエラーのとき、中断によるものと判定される", func(t *testing.T) {
			assert.True(t, isCanceled(context.DeadlineExceeded))
		})

		t.Run("呼び出し元の停止によるコンテキスト取り消しでもコンテキストの期限切れでもない原因のエラーのとき、中断によるものではないと判定される", func(t *testing.T) {
			assert.False(t, isCanceled(errors.New("some other failure")))
		})

		t.Run("エラーが無い(nil)とき、中断によるものではないと判定される", func(t *testing.T) {
			assert.False(t, isCanceled(nil))
		})
	})
}

package ws

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	apibattle "github.com/kenyamaneko/overload-party-battle/packages/api-battle-rpc-go"
	gamelogic "github.com/kenyamaneko/overload-party-battle/packages/game-logic-constants-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

func TestWSTurnTimeout(t *testing.T) {
	t.Run("[ターン管理]ターンタイムアウト", func(t *testing.T) {
		t.Run("プレイヤー1がタイムバンクとネットワーク猶予を両方過ぎてもアクションを送らないとき、プレイヤー2の勝利を示すgame_overが両者に届く", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			const gameID = "TST-GAME-TURN-TIMEOUT"
			srv.setupPvPGame(gameID, "uid-tt-p1", "p-tt-1", "uid-tt-p2", "p-tt-2")
			var gameOver atomic.Bool
			srv.battle.GetGameStateFn = func(gID string, pNum int) (int, any) {
				state := fakeGameState(gID, "TST-P1", "TST-P2")
				if pNum == 1 && !gameOver.Load() {
					state.IsMyTurn = true
					state.MyView.TimeBank = 1
				}
				return http.StatusOK, state
			}
			rec := &processActionRecorder{}
			srv.battle.ProcessActionFn = func(gID string, req apibattle.GameActionRequest) (int, any) {
				rec.fn(gID, req)
				gameOver.Store(true)
				return http.StatusOK, apibattle.ActionResult{
					GameOver:         true,
					WinningPlayerNum: 2,
					WinReason:        gamelogic.WinReasonTurnTimeout,
				}
			}
			p1Conn := srv.enterGame(t, "uid-tt-p1", gameID)
			p2Conn := srv.enterGame(t, "uid-tt-p2", gameID)

			p1Over := readUntilType(t, p1Conn, genws.WSServerMsgGameOver)
			var over1 GameOverMessage
			require.NoError(t, json.Unmarshal(p1Over.Data, &over1))
			assert.Equal(t, gamelogic.WinReasonTurnTimeout, over1.WinReason)
			assert.EqualValues(t, 2, over1.WinningPlayerNum)

			p2Over := readUntilType(t, p2Conn, genws.WSServerMsgGameOver)
			var over2 GameOverMessage
			require.NoError(t, json.Unmarshal(p2Over.Data, &over2))
			assert.Equal(t, gamelogic.WinReasonTurnTimeout, over2.WinReason)

			calls := rec.snapshotCalls()
			require.Len(t, calls, 1)
			assert.Equal(t, gamelogic.ActionTypeForfeit, calls[0].ActionType)
			assert.EqualValues(t, 1, calls[0].PlayerNum)
		})

		t.Run("タイムバンクの期限を過ぎた直後のネットワーク猶予内にアクションが届くと、強制終了にならず本来のアクションとして処理される", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			const gameID = "TST-GAME-TURN-BUFFER"
			srv.setupPvPGame(gameID, "uid-tb-p1", "p-tb-1", "uid-tb-p2", "p-tb-2")
			var actionHandled atomic.Bool
			srv.battle.GetGameStateFn = func(gID string, pNum int) (int, any) {
				state := fakeGameState(gID, "TST-P1", "TST-P2")
				if pNum == 1 {
					state.IsMyTurn = true
					if actionHandled.Load() {
						// アクション処理後、battle server が計時を更新した状態を模す
						// (処理済みアクション以前の期限をそのまま残さない)。
						state.MyView.TimeBank = 60
					} else {
						state.MyView.TimeBank = 1
					}
				}
				return http.StatusOK, state
			}
			rec := &processActionRecorder{}
			srv.battle.ProcessActionFn = func(gID string, req apibattle.GameActionRequest) (int, any) {
				rec.fn(gID, req)
				actionHandled.Store(true)
				return http.StatusOK, apibattle.ActionResult{
					Events: []apibattle.ActionEvent{
						{Sequence: 1, EventType: "end_turn", State: map[string]interface{}{"ok": true}},
					},
				}
			}
			p1Conn := srv.enterGame(t, "uid-tb-p1", gameID)
			_ = srv.enterGame(t, "uid-tb-p2", gameID)

			// timeBank(1s)は過ぎたが turnTimerNetworkBuffer(2s) の猶予内にアクションを送る。
			time.Sleep(1200 * time.Millisecond)
			require.NoError(t, p1Conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgGameAction,
				Data: mustMarshal(GameActionMessage{GameID: gameID, ActionType: "end_turn"}),
			}))

			readUntilActionPerformed(t, p1Conn, "end_turn")

			calls := rec.snapshotCalls()
			require.Len(t, calls, 1)
			assert.Equal(t, "end_turn", calls[0].ActionType)
		})

		t.Run("ターンが交代した後は、交代前のアクティブプレイヤーがタイムバンクとネットワーク猶予を両方過ぎても強制終了されない", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			const gameID = "TST-GAME-TURN-SWITCH"
			srv.setupPvPGame(gameID, "uid-ts-p1", "p-ts-1", "uid-ts-p2", "p-ts-2")
			var turnSwitched atomic.Bool
			srv.battle.GetGameStateFn = func(gID string, pNum int) (int, any) {
				state := fakeGameState(gID, "TST-P1", "TST-P2")
				switch {
				case pNum == 1 && !turnSwitched.Load():
					state.IsMyTurn = true
					state.MyView.TimeBank = 1
				case pNum == 2 && turnSwitched.Load():
					state.IsMyTurn = true
					state.MyView.TimeBank = 60
				}
				return http.StatusOK, state
			}
			rec := &processActionRecorder{}
			srv.battle.ProcessActionFn = func(gID string, req apibattle.GameActionRequest) (int, any) {
				rec.fn(gID, req)
				if req.ActionType == "switch_turn" {
					turnSwitched.Store(true)
				}
				return http.StatusOK, apibattle.ActionResult{
					Events: []apibattle.ActionEvent{
						{Sequence: 1, EventType: "switch_turn", State: map[string]interface{}{"ok": true}},
					},
				}
			}
			_ = srv.enterGame(t, "uid-ts-p1", gameID)
			p2Conn := srv.enterGame(t, "uid-ts-p2", gameID)

			require.NoError(t, p2Conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgGameAction,
				Data: mustMarshal(GameActionMessage{GameID: gameID, ActionType: "switch_turn"}),
			}))
			readUntilActionPerformed(t, p2Conn, "switch_turn")

			// p1の元の期限 (timeBank 1s + buffer 2s) を過ぎるまで待つ。
			time.Sleep(3500 * time.Millisecond)

			calls := rec.snapshotCalls()
			require.Len(t, calls, 1)
			assert.Equal(t, "switch_turn", calls[0].ActionType)
		})
	})
}

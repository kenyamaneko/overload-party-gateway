package ws

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apibattle "github.com/kenyamaneko/overload-party-battle/packages/api-battle-rpc-go"
	gamelogic "github.com/kenyamaneko/overload-party-battle/packages/game-logic-constants-go"

	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

// processActionRecorder は apibattlerpcserverfake.Server.ProcessActionFn への呼出を記録し、
// 1 件のイベントを含む ActionResult を返す。
type processActionRecorder struct {
	mu    sync.Mutex
	calls []apibattle.GameActionRequest
}

func (r *processActionRecorder) fn(_ string, req apibattle.GameActionRequest) (int, any) {
	r.mu.Lock()
	r.calls = append(r.calls, req)
	r.mu.Unlock()
	return http.StatusOK, apibattle.ActionResult{
		Events: []apibattle.ActionEvent{{
			EventType: "test_action_resolved",
			Sequence:  1,
			State:     map[string]interface{}{"key": "value"},
		}},
	}
}

func (r *processActionRecorder) snapshotCalls() []apibattle.GameActionRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]apibattle.GameActionRequest(nil), r.calls...)
}

func TestWSGameAction(t *testing.T) {
	t.Run("ゲーム内行動", func(t *testing.T) {
		t.Run("有効な行動を送ると、action_performedに続いて盤面状態が届き行動内容がbattleへ転送される", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			const gameID = "TST-GAME-ACTION-OK"
			srv.setupPvPGame(gameID, "uid-action-p1", "p-action-1", "uid-action-p2", "p-action-2")
			rec := &processActionRecorder{}
			srv.battle.ProcessActionFn = rec.fn
			conn := srv.enterGame(t, "uid-action-p1", gameID)

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgGameAction,
				Data: mustMarshal(GameActionMessage{
					GameID:     gameID,
					ActionType: "play_card",
					Data:       mustMarshal(map[string]interface{}{"target": "card-1"}),
				}),
			}))

			action := readUntilActionPerformed(t, conn, "test_action_resolved")
			assert.EqualValues(t, 1, action.Sequence)
			readUntilType(t, conn, genws.WSServerMsgGameState)

			calls := rec.snapshotCalls()
			require.Len(t, calls, 1)
			assert.Equal(t, "play_card", calls[0].ActionType)
			assert.EqualValues(t, 1, calls[0].PlayerNum)
		})

		t.Run("盤面が拒否する行動を送ると、拒否理由付きでaction_rejectedが返る", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			const gameID = "TST-GAME-ACTION-REJECT"
			srv.setupPvPGame(gameID, "uid-reject-p1", "p-reject-1", "uid-reject-p2", "p-reject-2")
			srv.battle.ProcessActionFn = func(string, apibattle.GameActionRequest) (int, any) {
				return http.StatusBadRequest, map[string]string{"error": "invalid move"}
			}
			conn := srv.enterGame(t, "uid-reject-p1", gameID)

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgGameAction,
				Data: mustMarshal(GameActionMessage{GameID: gameID, ActionType: "play_card"}),
			}))

			msg := readUntilType(t, conn, genws.WSServerMsgActionRejected)
			var rejected ActionRejectedMessage
			require.NoError(t, json.Unmarshal(msg.Data, &rejected))
			assert.Equal(t, "play_card", rejected.ActionType)
			assert.Equal(t, "invalid move", rejected.Reason)
		})

		t.Run("行動でゲームが終了すると、両プレイヤーにgame_overが届く", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			const gameID = "TST-GAME-ACTION-OVER"
			srv.setupPvPGame(gameID, "uid-over-p1", "p-over-1", "uid-over-p2", "p-over-2")
			srv.battle.ProcessActionFn = func(string, apibattle.GameActionRequest) (int, any) {
				return http.StatusOK, apibattle.ActionResult{
					GameOver:         true,
					WinningPlayerNum: 1,
					WinReason:        gamelogic.WinReasonBudgetZero,
				}
			}
			p1Conn := srv.enterGame(t, "uid-over-p1", gameID)
			p2Conn := srv.enterGame(t, "uid-over-p2", gameID)

			require.NoError(t, p1Conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgGameAction,
				Data: mustMarshal(GameActionMessage{GameID: gameID, ActionType: "finishing_move"}),
			}))

			for _, conn := range []*websocket.Conn{p1Conn, p2Conn} {
				msg := readUntilType(t, conn, genws.WSServerMsgGameOver)
				var over GameOverMessage
				require.NoError(t, json.Unmarshal(msg.Data, &over))
				assert.EqualValues(t, 1, over.WinningPlayerNum)
				assert.Equal(t, gamelogic.WinReasonBudgetZero, over.WinReason)
			}
		})

		t.Run("ゲームに登録されていないプレイヤーが行動を送ると、game_errorが返る", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-action-unregistered", "player-action-unregistered")
			conn := srv.dial(t, "uid-action-unregistered")

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgGameAction,
				Data: mustMarshal(GameActionMessage{GameID: "TST-GAME-NO-SUCH-ACTION", ActionType: "play_card"}),
			}))

			errMsg := readUntilType(t, conn, genws.WSServerMsgError)
			errBody := decodeError(t, errMsg)
			assert.Equal(t, "game_error", errBody.ErrorCode)
		})

		t.Run("dataがオブジェクトでないgame_actionを送ると、invalid_dataのエラーが返る", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-action-bad-data", "player-action-bad-data")
			conn := srv.dial(t, "uid-action-bad-data")

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgGameAction,
				Data: json.RawMessage(`"not-an-object"`),
			}))

			errMsg := readUntilType(t, conn, genws.WSServerMsgError)
			errBody := decodeError(t, errMsg)
			assert.Equal(t, "invalid_data", errBody.ErrorCode)
		})
	})
}

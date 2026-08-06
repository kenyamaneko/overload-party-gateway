package ws

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

// readUntilType は無関係なフレームを読み飛ばすため、フレームが送られていないことの確認には使えない。
func readNextMessage(t *testing.T, conn *websocket.Conn) WSMessage {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(wsReadWait)))
	_, data, err := conn.ReadMessage()
	require.NoError(t, err)
	var msg WSMessage
	require.NoError(t, json.Unmarshal(data, &msg))
	return msg
}

func TestWSUseStamp(t *testing.T) {
	t.Run("スタンプ使用", func(t *testing.T) {
		t.Run("スタンプを使うと、自分と対戦相手の両方にstamp_usedが届く", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			const gameID = "TST-GAME-STAMP-OK"
			srv.setupPvPGame(gameID, "uid-stamp-p1", "p-stamp-1", "uid-stamp-p2", "p-stamp-2")
			p1Conn := srv.enterGame(t, "uid-stamp-p1", gameID)
			p2Conn := srv.enterGame(t, "uid-stamp-p2", gameID)

			// PlayerNum が常に 1 になり偽陽性になるのを避けるため、スロット 2 で送らせる。
			require.NoError(t, p2Conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgUseStamp,
				Data: mustMarshal(UseStampMessage{GameID: gameID, StampNo: 3}),
			}))

			for _, conn := range []*websocket.Conn{p1Conn, p2Conn} {
				msg := readUntilType(t, conn, genws.WSServerMsgStampUsed)
				var stamp StampUsedMessage
				require.NoError(t, json.Unmarshal(msg.Data, &stamp))
				assert.EqualValues(t, 2, stamp.PlayerNum)
				assert.EqualValues(t, 3, stamp.StampNo)
			}
		})

		t.Run("ゲームに登録されていないプレイヤーがスタンプを使っても、フレームは送られない", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-stamp-unregistered", "player-stamp-unregistered")
			conn := srv.dial(t, "uid-stamp-unregistered")

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgUseStamp,
				Data: mustMarshal(UseStampMessage{GameID: "TST-GAME-NO-SUCH-STAMP", StampNo: 1}),
			}))
			require.NoError(t, conn.WriteJSON(WSMessage{Type: genws.WSClientMsgPing}))

			msg := readNextMessage(t, conn)
			assert.Equal(t, genws.WSServerMsgPong, msg.Type)
		})

		t.Run("dataが不正なuse_stampを送っても、フレームは送られない", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-stamp-bad-data", "player-stamp-bad-data")
			conn := srv.dial(t, "uid-stamp-bad-data")

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgUseStamp,
				Data: json.RawMessage(`"not-an-object"`),
			}))
			require.NoError(t, conn.WriteJSON(WSMessage{Type: genws.WSClientMsgPing}))

			msg := readNextMessage(t, conn)
			assert.Equal(t, genws.WSServerMsgPong, msg.Type)
		})
	})
}

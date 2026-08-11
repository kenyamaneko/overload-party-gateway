package ws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	wsconst "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

func TestReadPump(t *testing.T) {
	t.Run("[接続管理]WebSocket接続から受信したメッセージの処理", func(t *testing.T) {
		t.Run("受信したデータが正しいメッセージ形式(ping)のとき、対応する応答(pong)がその接続に返る", func(t *testing.T) {
			account := registeredAccountStub("player-readpump-valid")
			manager := newTestManager(t, managerDeps{account: account})
			h := NewHandler(manager, nil, account, nil)
			serverURL := newHandlerTestServer(t, h)

			rawConn, _, err := dialUpgrade(t, serverURL, "dev-token-uid-readpump-valid", nil)
			require.NoError(t, err)
			client := newTestClientConn(t, rawConn)

			require.NoError(t, rawConn.WriteMessage(websocket.TextMessage, []byte(`{"type":"`+wsconst.WSClientMsgPing+`"}`)))

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgPong, msg.Type)
		})

		t.Run(`受信したデータがJSONとして解釈できない形式のとき、その接続に error_code が invalid_message のエラーが返り(エラー内容 "invalid JSON")、それ以降に受信したメッセージの処理は継続する`, func(t *testing.T) {
			account := registeredAccountStub("player-readpump-invalid")
			manager := newTestManager(t, managerDeps{account: account})
			h := NewHandler(manager, nil, account, nil)
			serverURL := newHandlerTestServer(t, h)

			rawConn, _, err := dialUpgrade(t, serverURL, "dev-token-uid-readpump-invalid", nil)
			require.NoError(t, err)
			client := newTestClientConn(t, rawConn)

			require.NoError(t, rawConn.WriteMessage(websocket.TextMessage, []byte("not-json")))

			msg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgError, msg.Type)
			var payload ErrorMessage
			require.NoError(t, json.Unmarshal(msg.Data, &payload))
			assert.Equal(t, "invalid_message", payload.ErrorCode)
			assert.Equal(t, "invalid JSON", payload.Message)

			require.NoError(t, rawConn.WriteMessage(websocket.TextMessage, []byte(`{"type":"`+wsconst.WSClientMsgPing+`"}`)))
			nextMsg := client.readMessage(t)
			assert.Equal(t, wsconst.WSServerMsgPong, nextMsg.Type)
		})
	})
}

func TestSendRaw(t *testing.T) {
	t.Run("[接続管理]送信バッファの上限到達時の送信", func(t *testing.T) {
		t.Run("送信チャネルが上限まで埋まっているとき、それ以上の送信はブロックされずに破棄される", func(t *testing.T) {
			connCh := make(chan *Connection, 1)
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wsConn, err := testUpgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				// 送信チャネルを外部から消費させずに埋めきるため、WritePump はあえて起動しない。
				connCh <- NewConnection(wsConn, "player-buffer-full")
			})
			server := httptest.NewServer(handler)
			t.Cleanup(server.Close)

			wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/"
			rawClientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = rawClientConn.Close() })

			var serverConn *Connection
			select {
			case serverConn = <-connCh:
			case <-time.After(testMessageTimeout):
				t.Fatal("server did not accept the connection in time")
			}

			queuedData, err := json.Marshal(&WSMessage{Type: "queued"})
			require.NoError(t, err)
			for i := 0; i < sendBufferSize; i++ {
				serverConn.SendRaw(queuedData)
			}

			overflowData, err := json.Marshal(&WSMessage{Type: "overflow"})
			require.NoError(t, err)
			done := make(chan struct{})
			go func() {
				serverConn.SendRaw(overflowData)
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(testNoMessageWindow):
				t.Fatal("SendRaw blocked when the send buffer was already full")
			}

			go serverConn.WritePump()
			client := newTestClientConn(t, rawClientConn)
			for i := 0; i < sendBufferSize; i++ {
				msg := client.readMessage(t)
				assert.Equal(t, "queued", msg.Type)
			}
			client.expectNoMessage(t)
		})
	})
}

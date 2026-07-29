package ws

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

// newTestWSPair は httptest サーバー上に WS 接続を確立し、サーバー側 Connection と
// クライアント側 *websocket.Conn を返す。WritePump のみを起動する（close フレームの送出は
// WritePump が担うため）。ReadPump は hub/manager への依存を避けるため起動しない。
func newTestWSPair(t *testing.T) (*Connection, *websocket.Conn) {
	t.Helper()

	upgrader := websocket.Upgrader{}
	connCh := make(chan *Connection, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wsConn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		serverConn := NewConnection(wsConn, "p1")
		go serverConn.WritePump()
		connCh <- serverConn
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientConn.Close() })

	serverConn := <-connCh
	return serverConn, clientConn
}

// readNotifyThenClose はクライアント側接続から終了通知メッセージと、それに続く close
// フレームを読み取って返す。
func readNotifyThenClose(t *testing.T, clientConn *websocket.Conn) (notifyType string, closeErr *websocket.CloseError) {
	t.Helper()

	_ = clientConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	msgType, data, err := clientConn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, msgType)

	var envelope WSMessage
	require.NoError(t, json.Unmarshal(data, &envelope))

	_ = clientConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _, err = clientConn.ReadMessage()
	require.Error(t, err)

	var ce *websocket.CloseError
	require.True(t, errors.As(err, &ce), "expected *websocket.CloseError, got %T: %v", err, err)

	return envelope.Type, ce
}

func TestConnectionShutdown(t *testing.T) {
	t.Run("SIGTERM 時の終了通知", func(t *testing.T) {
		t.Run("終了通知メッセージを送信した後、異常な切断と区別できる close コードと終了理由を添えて接続を閉じる", func(t *testing.T) {
			serverConn, clientConn := newTestWSPair(t)

			serverConn.Shutdown(websocket.CloseGoingAway, genws.WSServerMsgServerShutdown)

			notifyType, closeErr := readNotifyThenClose(t, clientConn)
			assert.Equal(t, genws.WSServerMsgServerShutdown, notifyType)
			assert.Equal(t, websocket.CloseGoingAway, closeErr.Code)
			assert.Equal(t, genws.WSServerMsgServerShutdown, closeErr.Text)
		})

		t.Run("終了処理を重ねて実行しても、終了通知と close フレームは一度ずつしか届かない", func(t *testing.T) {
			serverConn, clientConn := newTestWSPair(t)

			serverConn.Shutdown(websocket.CloseGoingAway, genws.WSServerMsgServerShutdown)
			serverConn.Shutdown(websocket.CloseGoingAway, genws.WSServerMsgServerShutdown)

			notifyType, closeErr := readNotifyThenClose(t, clientConn)
			assert.Equal(t, genws.WSServerMsgServerShutdown, notifyType)
			assert.Equal(t, websocket.CloseGoingAway, closeErr.Code)
		})

		t.Run("クローズ済みの接続に対して終了処理を行っても、待たされずに戻る", func(t *testing.T) {
			serverConn, _ := newTestWSPair(t)
			serverConn.Close()

			done := make(chan struct{})
			go func() {
				serverConn.Shutdown(websocket.CloseGoingAway, genws.WSServerMsgServerShutdown)
				close(done)
			}()

			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("終了処理が、既にクローズ済みの接続に対して戻らず待ち続けた")
			}
		})
	})
}

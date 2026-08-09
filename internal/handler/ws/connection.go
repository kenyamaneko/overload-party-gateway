package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

const (
	pingInterval   = 15 * time.Second
	pongTimeout    = 5 * time.Second
	writeTimeout   = 10 * time.Second
	maxMsgSize     = 4096
	sendBufferSize = 64

	// WebSocket 接続ごとの読み書きバッファサイズ（gorilla/websocket Upgrader 用）
	wsReadBufferSize  = 1024
	wsWriteBufferSize = 1024
)

// Connection は WebSocket 接続を表します
type Connection struct {
	conn     *websocket.Conn
	playerID string
	send     chan []byte
	mu       sync.Mutex
	// isClosed は send チャネルの close / ctx の cancel 済みを表す。true になった後は
	// 新規の送信を受け付けない。
	isClosed bool
	// connClosed は下層ソケットの close 済みを表す。isClosed とは別に管理し、
	// Shutdown 経由の close では WritePump が close フレームを書き終えるまで
	// ソケットの close を遅らせられるようにする。
	connClosed bool
	// closeCode / closeReason は WritePump が最終的に送出する close フレームの内容。
	// beginClose が isClosed を立てる際に必ず設定する。
	closeCode   int
	closeReason string
	// writeDone は closeConn（下層ソケットの実クローズ）が完了すると close される。
	// Shutdown はこれを待つことで、プロセスが終了する前に close フレームの書き込みが
	// 確実に完了しているようにする。
	writeDone chan struct{}

	// ctx は接続が閉じられた時点で cancel される。
	// 下流の HTTP 呼び出しに引き回すことで、WS 切断時に in-flight な処理を即座に打ち切る。
	ctx    context.Context
	cancel context.CancelFunc
}

// NewConnection は WebSocket Connection を生成します
func NewConnection(conn *websocket.Conn, playerID string) *Connection {
	ctx, cancel := context.WithCancel(context.Background())
	return &Connection{
		conn:      conn,
		playerID:  playerID,
		send:      make(chan []byte, sendBufferSize),
		writeDone: make(chan struct{}),
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Context は接続生存期間に紐づく context を返します。
// 接続が Close されると cancel されます。
func (c *Connection) Context() context.Context {
	return c.ctx
}

// PlayerID は接続のプレイヤー ID を返します
func (c *Connection) PlayerID() string {
	return c.playerID
}

// SendMessage はメッセージを JSON にマーシャルして送信します
func (c *Connection) SendMessage(msg *WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		slog.Warn("marshal ws message failed", "error", err)
		return
	}
	c.SendRaw(data)
}

// SendRaw はマーシャル済みバイト列を送信します。
// 同一メッセージを複数接続にブロードキャストする際に json.Marshal の重複を避ける。
func (c *Connection) SendRaw(data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.isClosed {
		return
	}

	select {
	case c.send <- data:
	default:
		slog.Warn("send buffer full, dropping message", "player_id", c.playerID)
	}
}

// ReadPump は WebSocket からメッセージを読み取り Manager にディスパッチします
func (c *Connection) ReadPump(hub *ConnectionHub, manager *Manager) {
	defer func() {
		hub.Unregister(c)
		c.Close()
	}()

	c.conn.SetReadLimit(maxMsgSize)
	// deadline 設定の失敗は接続が既に閉じている場合のみ。直後の ReadMessage でエラー検出される。
	_ = c.conn.SetReadDeadline(time.Now().Add(pingInterval + pongTimeout))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pingInterval + pongTimeout))
		return nil
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Warn("ws read error", "player_id", c.playerID, "error", err)
			}
			return
		}

		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			c.SendMessage(&WSMessage{
				Type: genws.WSServerMsgError,
				Data: mustMarshal(ErrorMessage{ErrorCode: "invalid_message", Message: "invalid JSON"}),
			})
			continue
		}

		manager.HandleMessage(c, &msg)
	}
}

// WritePump はバッファからメッセージを読み取り WebSocket に書き込みます
func (c *Connection) WritePump() {
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		c.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			// deadline/close の失敗は接続が既に閉じている場合のみ。直後の WriteMessage または return → Close で処理される。
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if !ok {
				c.mu.Lock()
				code, reason := c.closeCode, c.closeReason
				c.mu.Unlock()
				_ = c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason))
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout)) // 失敗時は次の PingMessage で検出
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Close は接続を即座にクローズします。close フレームの送出は待たず、下層ソケットも
// 同時にクローズします。SIGTERM 時に終了を伝えてから閉じるには Shutdown を使ってください。
func (c *Connection) Close() {
	c.beginClose(websocket.CloseNormalClosure, "")
	c.closeConn()
}

// Shutdown はサーバー更新の通知メッセージを送出したうえで、WS close フレームに code と reason を
// 載せてから接続を閉じます。close コードにより、クライアントはこの切断を異常な切断と
// 区別できます。呼び出し元が終了処理の完了を確認できるよう、下層ソケットが実際に
// クローズされるまで待って返ります。
func (c *Connection) Shutdown(code int, reason string) {
	c.SendMessage(&WSMessage{Type: genws.WSServerMsgServerUpdate})
	c.beginClose(code, reason)
	// 下層ソケットの close は WritePump が close フレームを書き終えた後、
	// その defer 経由の Close 呼び出しが担う。ここで待たないと、close フレームの
	// 書き込みを待たずにプロセスが終了しうる。
	<-c.writeDone
}

// beginClose は close フレームの code/reason を確定し、send チャネルの close と ctx の
// cancel を一度だけ行う。一度実行されると以降の呼び出しは no-op。
func (c *Connection) beginClose(code int, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.isClosed {
		return
	}
	c.isClosed = true
	c.closeCode = code
	c.closeReason = reason
	// 下流呼び出しを即座に打ち切るため close より先に cancel する。
	if c.cancel != nil {
		c.cancel()
	}
	close(c.send)
}

// closeConn は下層ソケットを一度だけクローズし、待機中の Shutdown 呼び出し元に完了を知らせる。
func (c *Connection) closeConn() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.connClosed {
		return
	}
	c.connClosed = true
	if c.conn != nil {
		_ = c.conn.Close()
	}
	close(c.writeDone)
}

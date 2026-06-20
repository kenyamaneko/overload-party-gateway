package ws

import (
	"context"
	"encoding/json"
	"log"
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
	isClosed bool

	// ctx は接続が閉じられた時点で cancel される。
	// 下流の HTTP 呼び出しに引き回すことで、WS 切断時に in-flight な処理を即座に打ち切る。
	ctx    context.Context
	cancel context.CancelFunc
}

// NewConnection は WebSocket Connection を生成します
func NewConnection(conn *websocket.Conn, playerID string) *Connection {
	ctx, cancel := context.WithCancel(context.Background())
	return &Connection{
		conn:     conn,
		playerID: playerID,
		send:     make(chan []byte, sendBufferSize),
		ctx:      ctx,
		cancel:   cancel,
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
		log.Printf("marshal ws message: %v", err)
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
		log.Printf("send buffer full for player %s, dropping message", c.playerID)
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
				log.Printf("ws read error for player %s: %v", c.playerID, err)
			}
			return
		}

		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			c.SendMessage(&WSMessage{
				Type: genws.WSServerMsgError,
				Data: mustMarshal(ErrorMessage{ErrorCode: "invalid_message", Message: "invalid JSON", Retryable: false}),
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
				_ = c.conn.WriteMessage(websocket.CloseMessage, nil)
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

// Close は接続をクローズします
func (c *Connection) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.isClosed {
		c.isClosed = true
		// 下流呼び出しを即座に打ち切るため close より先に cancel する。
		if c.cancel != nil {
			c.cancel()
		}
		close(c.send)
		if c.conn != nil {
			_ = c.conn.Close()
		}
	}
}

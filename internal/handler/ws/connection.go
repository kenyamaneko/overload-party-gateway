package ws

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/kenyamaneko/overload-party-gateway/internal/constants"
)

const (
	pingInterval   = 15 * time.Second
	pongTimeout    = 5 * time.Second
	writeTimeout   = 10 * time.Second
	maxMsgSize     = 4096
	sendBufferSize = 64

	// WebSocket接続ごとの読み書きバッファサイズ（gorilla/websocket Upgrader用）。
	wsReadBufferSize  = 1024
	wsWriteBufferSize = 1024
)

type Connection struct {
	conn     *websocket.Conn
	playerID string
	send     chan []byte
	mu       sync.Mutex
	closed   bool
}

func NewConnection(conn *websocket.Conn, playerID string) *Connection {
	return &Connection{
		conn:     conn,
		playerID: playerID,
		send:     make(chan []byte, sendBufferSize),
	}
}

func (c *Connection) PlayerID() string {
	return c.playerID
}

func (c *Connection) SendMessage(msg *WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("marshal ws message: %v", err)
		return
	}
	c.SendRaw(data)
}

// SendRaw sends pre-marshaled bytes to the client without additional marshaling.
// Use this when the same message is broadcast to multiple connections to avoid
// redundant json.Marshal calls.
func (c *Connection) SendRaw(data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}

	select {
	case c.send <- data:
	default:
		log.Printf("send buffer full for player %s, dropping message", c.playerID)
	}
}

// MessageHandler is the interface for processing incoming WebSocket messages.
type MessageHandler interface {
	HandleMessage(conn *Connection, msg *WSMessage)
}

// ConnectionLifecycle is the interface for connection registration/unregistration.
type ConnectionLifecycle interface {
	Register(conn *Connection)
	Unregister(conn *Connection)
}

func (c *Connection) ReadPump(lifecycle ConnectionLifecycle, handler MessageHandler) {
	defer func() {
		lifecycle.Unregister(c)
		c.Close()
	}()

	c.conn.SetReadLimit(maxMsgSize)
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
				Type: constants.WSMsgError,
				Data: mustMarshal(ErrorMessage{Code: "invalid_message", Message: "invalid JSON", Retryable: false}),
			})
			continue
		}

		handler.HandleMessage(c, &msg)
	}
}

func (c *Connection) WritePump() {
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		c.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Connection) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.send)
		if c.conn != nil {
			c.conn.Close()
		}
	}
}

func mustMarshal(v interface{}) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

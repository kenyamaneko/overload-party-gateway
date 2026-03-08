package ws

import (
	"log"
	"sync"
	"time"
)

const disconnectTimeout = 60 * time.Second

type disconnectInfo struct {
	playerID string
	gameID   string
	timer    *time.Timer
}

// DisconnectCallback is called when a player disconnects while in a game.
// It receives the playerID and gameID so the caller can start a forfeit timer.
type DisconnectCallback func(playerID, gameID string)

// ConnectionHub manages WebSocket connections and disconnect timers.
type ConnectionHub struct {
	mu          sync.RWMutex
	connections map[string]*Connection
	disconnects map[string]*disconnectInfo

	// getGameID resolves the gameID for a player, used during disconnect.
	getGameID func(playerID string) (string, bool)
	// onDisconnectTimeout is called when a disconnect timer fires.
	onDisconnectTimeout DisconnectCallback
}

func NewConnectionHub(
	getGameID func(playerID string) (string, bool),
	onDisconnectTimeout DisconnectCallback,
) *ConnectionHub {
	return &ConnectionHub{
		connections:         make(map[string]*Connection),
		disconnects:         make(map[string]*disconnectInfo),
		getGameID:           getGameID,
		onDisconnectTimeout: onDisconnectTimeout,
	}
}

func (h *ConnectionHub) Register(conn *Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if info, ok := h.disconnects[conn.playerID]; ok {
		info.timer.Stop()
		delete(h.disconnects, conn.playerID)
		log.Printf("player %s reconnected", conn.playerID)
	}

	if old, ok := h.connections[conn.playerID]; ok {
		old.Close()
	}
	h.connections[conn.playerID] = conn
}

func (h *ConnectionHub) Unregister(conn *Connection) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if existing, ok := h.connections[conn.playerID]; !ok || existing != conn {
		return
	}
	delete(h.connections, conn.playerID)

	if gameID, inGame := h.getGameID(conn.playerID); inGame {
		timer := time.AfterFunc(disconnectTimeout, func() {
			h.mu.Lock()
			delete(h.disconnects, conn.playerID)
			h.mu.Unlock()

			log.Printf("player %s disconnect timeout expired for game %s, forfeit", conn.playerID, gameID)
			h.onDisconnectTimeout(conn.playerID, gameID)
		})
		h.disconnects[conn.playerID] = &disconnectInfo{
			playerID: conn.playerID,
			gameID:   gameID,
			timer:    timer,
		}
		log.Printf("player %s disconnected, %v timeout started", conn.playerID, disconnectTimeout)
	}
}

func (h *ConnectionHub) SendToPlayer(playerID string, msg *WSMessage) {
	h.mu.RLock()
	conn, ok := h.connections[playerID]
	h.mu.RUnlock()
	if ok {
		conn.SendMessage(msg)
	}
}

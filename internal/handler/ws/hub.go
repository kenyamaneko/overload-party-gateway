package ws

import (
	"log"
	"sync"
	"time"
)

const disconnectTimeout = 60 * time.Second

type disconnectInfo struct {
	gameID string
	timer  *time.Timer
}

// HubCallbacks groups the callback functions that ConnectionHub invokes
// in response to connection lifecycle events.
type HubCallbacks struct {
	// GetGameID resolves the gameID for a player, used during disconnect.
	GetGameID func(playerID string) (string, bool)
	// OnDisconnectTimeout is called when a disconnect timer fires (forfeit).
	OnDisconnectTimeout func(playerID, gameID string)
	// OnSpectatorDisconnect cleans up spectator state when a connection closes.
	OnSpectatorDisconnect func(playerID string)
	// OnMatchmakingLeave removes a player from the matchmaking queue on disconnect.
	OnMatchmakingLeave func(playerID string)
	// OnGameDisconnect notifies the opponent that a player has disconnected.
	OnGameDisconnect func(playerID, gameID string)
	// OnGameReconnect notifies the opponent that a disconnected player has returned.
	OnGameReconnect func(playerID, gameID string)
}

// ConnectionHub manages WebSocket connections and disconnect timers.
type ConnectionHub struct {
	mu          sync.RWMutex
	connections map[string]*Connection
	disconnects map[string]*disconnectInfo

	cb HubCallbacks
}

func NewConnectionHub(cb HubCallbacks) *ConnectionHub {
	return &ConnectionHub{
		connections: make(map[string]*Connection),
		disconnects: make(map[string]*disconnectInfo),
		cb:          cb,
	}
}

func (h *ConnectionHub) Register(conn *Connection) {
	h.mu.Lock()
	var reconnectGameID string
	if info, ok := h.disconnects[conn.playerID]; ok {
		info.timer.Stop()
		reconnectGameID = info.gameID
		delete(h.disconnects, conn.playerID)
		log.Printf("player %s reconnected", conn.playerID)
	}

	if old, ok := h.connections[conn.playerID]; ok {
		old.Close()
	}
	h.connections[conn.playerID] = conn
	h.mu.Unlock()

	// Notify opponent outside the lock to avoid deadlock.
	if reconnectGameID != "" && h.cb.OnGameReconnect != nil {
		h.cb.OnGameReconnect(conn.playerID, reconnectGameID)
	}
}

func (h *ConnectionHub) Unregister(conn *Connection) {
	h.mu.Lock()
	if existing, ok := h.connections[conn.playerID]; !ok || existing != conn {
		h.mu.Unlock()
		return
	}
	delete(h.connections, conn.playerID)

	gameID, inGame := h.cb.GetGameID(conn.playerID)
	if inGame {
		timer := time.AfterFunc(disconnectTimeout, func() {
			h.mu.Lock()
			delete(h.disconnects, conn.playerID)
			h.mu.Unlock()

			log.Printf("player %s disconnect timeout expired for game %s, forfeit", conn.playerID, gameID)
			h.cb.OnDisconnectTimeout(conn.playerID, gameID)
		})
		h.disconnects[conn.playerID] = &disconnectInfo{
			gameID: gameID,
			timer:  timer,
		}
		log.Printf("player %s disconnected, %v timeout started", conn.playerID, disconnectTimeout)
	}
	h.mu.Unlock()

	// All callbacks below run outside the lock to avoid deadlock,
	// since they may call SendToPlayer which acquires RLock.
	if h.cb.OnSpectatorDisconnect != nil {
		h.cb.OnSpectatorDisconnect(conn.playerID)
	}
	if h.cb.OnMatchmakingLeave != nil {
		h.cb.OnMatchmakingLeave(conn.playerID)
	}
	if inGame && h.cb.OnGameDisconnect != nil {
		h.cb.OnGameDisconnect(conn.playerID, gameID)
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

// SendRawToPlayer sends pre-marshaled bytes to a player without additional marshaling.
func (h *ConnectionHub) SendRawToPlayer(playerID string, data []byte) {
	h.mu.RLock()
	conn, ok := h.connections[playerID]
	h.mu.RUnlock()
	if ok {
		conn.SendRaw(data)
	}
}

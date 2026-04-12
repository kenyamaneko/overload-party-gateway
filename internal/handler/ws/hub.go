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

// HubCallbacks は ConnectionHub が接続ライフサイクルイベント時に呼び出すコールバック群です
type HubCallbacks struct {
	// GetGameID は切断時にプレイヤーの gameID を解決する
	GetGameID func(playerID string) (string, bool)
	// OnDisconnectTimeout は切断タイマー発火時（forfeit）に呼ばれる
	OnDisconnectTimeout func(playerID, gameID string)
	// OnSpectatorDisconnect は接続終了時に観戦状態をクリーンアップする
	OnSpectatorDisconnect func(playerID string)
	// OnMatchmakingLeave は切断時にマッチメイキングキューからプレイヤーを除去する
	OnMatchmakingLeave func(playerID string)
	// OnGameDisconnect は対戦相手に切断を通知する
	OnGameDisconnect func(playerID, gameID string)
	// OnGameReconnect は切断していたプレイヤーの復帰を対戦相手に通知する
	OnGameReconnect func(playerID, gameID string)
}

// ConnectionHub は WebSocket 接続と切断タイマーを管理します
type ConnectionHub struct {
	mu          sync.RWMutex
	connections map[string]*Connection
	disconnects map[string]*disconnectInfo

	cb HubCallbacks
}

// NewConnectionHub は ConnectionHub を生成します
func NewConnectionHub(cb HubCallbacks) *ConnectionHub {
	return &ConnectionHub{
		connections: make(map[string]*Connection),
		disconnects: make(map[string]*disconnectInfo),
		cb:          cb,
	}
}

// Register は新しい WebSocket 接続を登録します
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

	// デッドロック防止のためロック外で対戦相手に通知
	if reconnectGameID != "" && h.cb.OnGameReconnect != nil {
		h.cb.OnGameReconnect(conn.playerID, reconnectGameID)
	}
}

// Unregister は WebSocket 接続を解除し切断タイマーを開始します
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

	// SendToPlayer が RLock を取得するためデッドロック防止でロック外で実行
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

// SendToPlayer は指定プレイヤーにメッセージを送信します
func (h *ConnectionHub) SendToPlayer(playerID string, msg *WSMessage) {
	h.mu.RLock()
	conn, ok := h.connections[playerID]
	h.mu.RUnlock()
	if ok {
		conn.SendMessage(msg)
	}
}

// SendRawToPlayer はマーシャル済みバイト列をプレイヤーに送信します
func (h *ConnectionHub) SendRawToPlayer(playerID string, data []byte) {
	h.mu.RLock()
	conn, ok := h.connections[playerID]
	h.mu.RUnlock()
	if ok {
		conn.SendRaw(data)
	}
}

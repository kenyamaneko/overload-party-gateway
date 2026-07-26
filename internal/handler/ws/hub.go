package ws

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

const disconnectTimeout = 60 * time.Second

// ShutdownNotifyTimeout は SIGTERM 受信時、WS 接続へ終了を通知してから閉じるまでの上限。
const ShutdownNotifyTimeout = 3 * time.Second

// shutdownNotifier は Shutdown が個々の接続に対して行う操作の最小契約。
// Connection から切り出すことで、テストが実ソケットを介さずに振る舞いを検証できる。
type shutdownNotifier interface {
	Shutdown(code int, reason string)
}

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
	// isShuttingDown は Shutdown 開始後に true になる。Unregister はこの間、
	// 対戦相手への切断通知を抑止する（サーバー都合の切断を相手の切断と誤認させないため）。
	isShuttingDown bool

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
	suppressOpponentNotify := h.isShuttingDown
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
	if h.cb.OnMatchmakingLeave != nil {
		h.cb.OnMatchmakingLeave(conn.playerID)
	}
	// シャットダウン中は対戦中の両者がほぼ同時に切断されるため、相手の切断としては通知しない。
	if inGame && h.cb.OnGameDisconnect != nil && !suppressOpponentNotify {
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

// Shutdown は呼び出し時点で登録中の接続へ終了を通知してから閉じます。ctx の期限までに完了しなかった
// 接続は待たずに諦めます（ベストエフォート）。対戦中の接続の切断猶予には関与しません。
func (h *ConnectionHub) Shutdown(ctx context.Context) {
	h.mu.Lock()
	h.isShuttingDown = true
	notifiers := make([]shutdownNotifier, 0, len(h.connections))
	for _, conn := range h.connections {
		notifiers = append(notifiers, conn)
	}
	h.mu.Unlock()

	shutdownAll(ctx, notifiers, websocket.CloseGoingAway, genws.WSServerMsgServerShutdown)
}

// shutdownAll は各 notifier の Shutdown を並行に呼び出し、ctx の期限まで完了を待つ。
// 期限に間に合わなかった呼び出しは待たずに諦める（呼び出し自体は動き続ける）。
func shutdownAll(ctx context.Context, notifiers []shutdownNotifier, code int, reason string) {
	if len(notifiers) == 0 {
		return
	}

	var wg sync.WaitGroup
	var notified atomic.Int64
	for _, n := range notifiers {
		n := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.Shutdown(code, reason)
			notified.Add(1)
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		log.Printf("ws shutdown: timed out before deadline, notified %d/%d connection(s)",
			notified.Load(), len(notifiers))
	}
}

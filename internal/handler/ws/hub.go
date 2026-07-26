package ws

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// disconnectTimeout は切断したプレイヤーに与える再接続猶予。
// アプリの再起動を伴う復帰に届くよう 120 秒とする。
const disconnectTimeout = 120 * time.Second

// timerMirrorTimeout は TimerStore への書き込み・削除の上限時間。写しの失敗が
// 対戦の進行を止めないよう、短い上限で打ち切る。
const timerMirrorTimeout = 2 * time.Second

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
	// OnGameReconnect は切断していたプレイヤーの復帰を対戦相手に通知する。
	// wasLate は復帰したプレイヤー自身の猶予期限が既に過ぎていたかを表す。
	OnGameReconnect func(playerID, gameID string, wasLate bool)
}

// ConnectionHub は WebSocket 接続と切断タイマーを管理します
type ConnectionHub struct {
	mu          sync.RWMutex
	connections map[string]*Connection
	disconnects map[string]*disconnectInfo

	cb HubCallbacks

	// timerStore は切断猶予期限を Redis へ写す。未設定 (nil) の場合は写しを行わない
	// （ローカル開発など Redis を使わない環境向け）。
	timerStore port.TimerStore
}

// NewConnectionHub は ConnectionHub を生成します。timerStore は nil 可（写しを行わない）。
func NewConnectionHub(cb HubCallbacks, timerStore port.TimerStore) *ConnectionHub {
	return &ConnectionHub{
		connections: make(map[string]*Connection),
		disconnects: make(map[string]*disconnectInfo),
		cb:          cb,
		timerStore:  timerStore,
	}
}

// Register は新しい WebSocket 接続を登録します
func (h *ConnectionHub) Register(conn *Connection) {
	h.mu.Lock()
	var reconnectGameID string
	var stillInMemory bool
	if info, ok := h.disconnects[conn.playerID]; ok {
		info.timer.Stop()
		reconnectGameID = info.gameID
		stillInMemory = true
		delete(h.disconnects, conn.playerID)
		log.Printf("player %s reconnected", conn.playerID)
	}

	if old, ok := h.connections[conn.playerID]; ok {
		old.Close()
	}
	h.connections[conn.playerID] = conn
	h.mu.Unlock()

	// インメモリのタイマーがまだ発火していなければ、それだけで猶予内と判定できる
	// (wasLate=false)。インメモリに記録が無い場合はローカルタイマーが既に発火済みか
	// プロセス再起動をまたいだ可能性があるため、削除する前に TimerStore の写しを見て
	// 猶予切れかどうかを判定する。写しの読み出しが失敗した場合は対象ゲームが分からず
	// 評価しようがないため、対戦相手の未決着評価を持ち越せないことを ERROR ログで残す。
	wasLate := false
	if !stillInMemory {
		dl, found, err := h.mirrorGetDisconnectDeadline(conn.playerID)
		if err != nil {
			log.Printf("ERROR: player %s reconnect: read mirrored disconnect deadline: %v, stale disconnect resolution skipped", conn.playerID, err)
		} else if found {
			reconnectGameID = dl.GameID
			wasLate = !time.Now().Before(dl.Deadline)
		}
	}

	// デッドロック防止のためロック外で対戦相手に通知
	h.mirrorClearDisconnectDeadline(conn.playerID)
	if reconnectGameID != "" && h.cb.OnGameReconnect != nil {
		h.cb.OnGameReconnect(conn.playerID, reconnectGameID, wasLate)
	}
}

// IsConnected はプレイヤーが現在 WS 接続を保持しているかを返します。
func (h *ConnectionHub) IsConnected(playerID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.connections[playerID]
	return ok
}

// IsDisconnectDeadlineExpired はプレイヤーの切断猶予期限が既に過ぎているかを返します。
// インメモリのタイマーが残っている間は猶予内と判定し、消えている場合のみ
// TimerStore の写しを読み出して判定します。記録が見つからない場合は
// 猶予切れとは判定しません (false を返す)。写しの読み出しが失敗した場合は
// 期限切れかどうか判定できないため、呼び出し元が判断できるよう err を返します。
func (h *ConnectionHub) IsDisconnectDeadlineExpired(playerID string) (expired bool, err error) {
	h.mu.RLock()
	_, stillDisconnected := h.disconnects[playerID]
	h.mu.RUnlock()
	if stillDisconnected {
		return false, nil
	}

	dl, found, err := h.mirrorGetDisconnectDeadline(playerID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	return !time.Now().Before(dl.Deadline), nil
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
	var deadline time.Time
	if inGame {
		deadline = time.Now().Add(disconnectTimeout)
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
	if inGame {
		h.mirrorSetDisconnectDeadline(conn.playerID, gameID, deadline)
	}
	if h.cb.OnMatchmakingLeave != nil {
		h.cb.OnMatchmakingLeave(conn.playerID)
	}
	if inGame && h.cb.OnGameDisconnect != nil {
		h.cb.OnGameDisconnect(conn.playerID, gameID)
	}
}

// mirrorSetDisconnectDeadline は切断猶予期限を TimerStore へ書き込む。
// 失敗しても対戦は継続するため、警告ログのみで済ませる。
func (h *ConnectionHub) mirrorSetDisconnectDeadline(playerID, gameID string, deadline time.Time) {
	if h.timerStore == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), timerMirrorTimeout)
	defer cancel()
	if err := h.timerStore.SetDisconnectDeadline(ctx, playerID, gameID, deadline); err != nil {
		log.Printf("WARN: mirror disconnect deadline for player %s: %v", playerID, err)
	}
}

// mirrorClearDisconnectDeadline は切断猶予期限の写しを TimerStore から削除する。
// 失敗しても対戦は継続するため、警告ログのみで済ませる。
func (h *ConnectionHub) mirrorClearDisconnectDeadline(playerID string) {
	if h.timerStore == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), timerMirrorTimeout)
	defer cancel()
	if err := h.timerStore.ClearDisconnectDeadline(ctx, playerID); err != nil {
		log.Printf("WARN: clear mirrored disconnect deadline for player %s: %v", playerID, err)
	}
}

// mirrorGetDisconnectDeadline は切断猶予期限の写しを TimerStore から読み出す。
// timerStore が未設定の場合は found=false を返します。
func (h *ConnectionHub) mirrorGetDisconnectDeadline(playerID string) (port.DisconnectDeadline, bool, error) {
	if h.timerStore == nil {
		return port.DisconnectDeadline{}, false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), timerMirrorTimeout)
	defer cancel()
	return h.timerStore.GetDisconnectDeadline(ctx, playerID)
}

// ClearDisconnectDeadline は切断猶予期限の写しを TimerStore から削除します。
// ゲーム終了などプレイヤーの切断猶予が不要になった時点で GameRelay から呼ばれます。
func (h *ConnectionHub) ClearDisconnectDeadline(playerID string) {
	h.mirrorClearDisconnectDeadline(playerID)
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

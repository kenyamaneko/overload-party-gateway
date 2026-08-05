package ws

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

// DefaultDisconnectTimeout は切断したプレイヤーに与える再接続猶予の既定値。
// アプリの再起動を伴う復帰に届くよう 120 秒とする。
const DefaultDisconnectTimeout = 120 * time.Second

// timerMirrorTimeout は TimerStore への書き込み・削除の上限時間。写しの失敗が
// 対戦の進行を止めないよう、短い上限で打ち切る。
const timerMirrorTimeout = 2 * time.Second

// ShutdownNotifyTimeout は SIGTERM 受信時、WS 接続へ終了を通知してから閉じるまでの上限。
const ShutdownNotifyTimeout = 3 * time.Second

// shutdownNotifier は Shutdown が個々の接続に対して行う操作の最小契約。
// Connection から切り出すことで、テストが実ソケットを介さずに振る舞いを検証できる。
type shutdownNotifier interface {
	Shutdown(code int, reason string)
}

type disconnectInfo struct {
	gameID string
	timer  Timer
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
	// isShuttingDown は Shutdown 開始後に true になる。Unregister はこの間、
	// 対戦相手への切断通知を抑止する（サーバー都合の切断を相手の切断と誤認させないため）。
	isShuttingDown bool

	cb HubCallbacks

	// timerStore は切断猶予期限を Redis へ写す。未設定 (nil) の場合は写しを行わない
	// （ローカル開発など Redis を使わない環境向け）。
	timerStore port.TimerStore

	// disconnectTimeout は切断したプレイヤーに与える再接続猶予。
	disconnectTimeout time.Duration

	clock Clock
}

// HubOption は ConnectionHub の生成時オプションを表す。
type HubOption func(*ConnectionHub)

// WithHubClock は ConnectionHub が使う Clock を上書きする。
func WithHubClock(clock Clock) HubOption {
	return func(h *ConnectionHub) { h.clock = clock }
}

// NewConnectionHub は ConnectionHub を生成します。timerStore は nil 可（写しを行わない）。
func NewConnectionHub(cb HubCallbacks, disconnectTimeout time.Duration, timerStore port.TimerStore, opts ...HubOption) *ConnectionHub {
	h := &ConnectionHub{
		connections:       make(map[string]*Connection),
		disconnects:       make(map[string]*disconnectInfo),
		cb:                cb,
		timerStore:        timerStore,
		disconnectTimeout: disconnectTimeout,
		clock:             systemClock{},
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
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
		slog.Info("player reconnected", "player_id", conn.playerID)
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
			slog.Error("read mirrored disconnect deadline on reconnect, stale disconnect resolution skipped", "player_id", conn.playerID, "error", err)
		} else if found {
			reconnectGameID = dl.GameID
			wasLate = !h.clock.Now().Before(dl.Deadline)
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
	return !h.clock.Now().Before(dl.Deadline), nil
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
	suppressOpponentNotify := h.isShuttingDown
	if inGame {
		deadline = h.clock.Now().Add(h.disconnectTimeout)
		timer := h.clock.AfterFunc(h.disconnectTimeout, func() {
			h.mu.Lock()
			delete(h.disconnects, conn.playerID)
			h.mu.Unlock()

			slog.Info("player disconnect timeout expired, forfeit", "player_id", conn.playerID, "game_id", gameID)
			h.cb.OnDisconnectTimeout(conn.playerID, gameID)
		})
		h.disconnects[conn.playerID] = &disconnectInfo{
			gameID: gameID,
			timer:  timer,
		}
		slog.Info("player disconnected, timeout started", "player_id", conn.playerID, "timeout", h.disconnectTimeout)
	}
	h.mu.Unlock()

	// SendToPlayer が RLock を取得するためデッドロック防止でロック外で実行
	if inGame {
		h.mirrorSetDisconnectDeadline(conn.playerID, gameID, deadline)
	}
	if h.cb.OnMatchmakingLeave != nil {
		h.cb.OnMatchmakingLeave(conn.playerID)
	}
	// シャットダウン中は対戦中の両者がほぼ同時に切断されるため、相手の切断としては通知しない。
	if inGame && h.cb.OnGameDisconnect != nil && !suppressOpponentNotify {
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
		slog.Warn("mirror disconnect deadline failed", "player_id", playerID, "error", err)
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
		slog.Warn("clear mirrored disconnect deadline failed", "player_id", playerID, "error", err)
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
	h.SendToPlayerIfConnected(playerID, msg)
}

// SendToPlayerIfConnected は指定プレイヤーが接続していればメッセージを送信し、送信できたかを返します。
func (h *ConnectionHub) SendToPlayerIfConnected(playerID string, msg *WSMessage) bool {
	h.mu.RLock()
	conn, ok := h.connections[playerID]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	conn.SendMessage(msg)
	return true
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

// Shutdown は呼び出し時点で登録中の接続へサーバー更新を通知してから閉じます。ctx の期限までに完了しなかった
// 接続は待たずに諦めます（ベストエフォート）。対戦中の接続の切断猶予には関与しません。
func (h *ConnectionHub) Shutdown(ctx context.Context) {
	h.mu.Lock()
	h.isShuttingDown = true
	notifiers := make([]shutdownNotifier, 0, len(h.connections))
	for _, conn := range h.connections {
		notifiers = append(notifiers, conn)
	}
	h.mu.Unlock()

	shutdownAll(ctx, notifiers, websocket.CloseGoingAway, genws.WSServerMsgServerUpdate)
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
		slog.Warn("ws shutdown timed out before deadline",
			"notified", notified.Load(), "total", len(notifiers))
	}
}

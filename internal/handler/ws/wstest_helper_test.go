package ws

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	apibattle "github.com/kenyamaneko/overload-party-battle/packages/api-battle-rpc-go"
	apicard "github.com/kenyamaneko/overload-party-card/packages/api-card"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

const testMessageTimeout = 2 * time.Second
const testNoMessageWindow = 200 * time.Millisecond

// fakeTimer は Timer を満たすテスト用実装で、fakeClock からのみ生成される。
type fakeTimer struct {
	clock   *fakeClock
	f       func()
	at      time.Time
	stopped bool
}

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	if t.stopped {
		return false
	}
	t.stopped = true
	return true
}

// fakeClock は Clock を満たすテスト用実装で、Advance で明示的に時刻を進めた
// ときにだけ期限が来たタイマーのコールバックを実行する。
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

func newFakeClock(now time.Time) *fakeClock {
	return &fakeClock{now: now}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{clock: c, f: f, at: c.now.Add(d)}
	c.timers = append(c.timers, t)
	return t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []func()
	for _, t := range c.timers {
		if !t.stopped && !t.at.After(c.now) {
			t.stopped = true
			due = append(due, t.f)
		}
	}
	c.mu.Unlock()
	for _, f := range due {
		f()
	}
}

var testUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// testSocketFactory は httptest サーバー上で本物の WebSocket ハンドシェイクを行い、
// サーバー側の *Connection を hub に登録するテストヘルパーである。
type testSocketFactory struct {
	server *httptest.Server
	hub    *ConnectionHub
	connCh chan *Connection
}

func newTestSocketFactory(t *testing.T, hub *ConnectionHub) *testSocketFactory {
	t.Helper()
	f := &testSocketFactory{hub: hub, connCh: make(chan *Connection, 1)}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wsConn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c := NewConnection(wsConn, r.URL.Query().Get("player_id"))
		hub.Register(c)
		go c.WritePump()
		f.connCh <- c
	})
	f.server = httptest.NewServer(handler)
	t.Cleanup(f.server.Close)
	return f
}

// connect はプレイヤー ID で新しいクライアント側ソケットを接続し、クライアント側
// ハンドルと、hub に登録済みのサーバー側 *Connection の両方を返す。
func (f *testSocketFactory) connect(t *testing.T, playerID string) (*testClientConn, *Connection) {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(f.server.URL, "http") + "/?player_id=" + url.QueryEscape(playerID)
	raw, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	var serverConn *Connection
	select {
	case serverConn = <-f.connCh:
	case <-time.After(testMessageTimeout):
		t.Fatal("test socket factory: server did not register the connection in time")
	}
	return newTestClientConn(t, raw), serverConn
}

// testClientConn はサーバーから届くメッセージをバックグラウンドで読み続け、
// チャネル越しに公開する。gorilla/websocket は SetReadDeadline によるタイムアウトを
// 一度でも起こすと、以降そのコネクションの全ての読み取りが同じエラーを返し続ける
// (readErr が固定される) ため、テストの assertion では毎回デッドラインを設定した
// 読み取りを繰り返さず、常時稼働するリーダーゴルーチンとチャネルの select で待つ。
type testClientConn struct {
	conn *websocket.Conn
	msgs chan WSMessage
	errs chan error
}

func newTestClientConn(t *testing.T, conn *websocket.Conn) *testClientConn {
	t.Helper()
	c := &testClientConn{conn: conn, msgs: make(chan WSMessage, 256), errs: make(chan error, 1)}
	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				select {
				case c.errs <- err:
				default:
				}
				return
			}
			var msg WSMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				select {
				case c.errs <- err:
				default:
				}
				return
			}
			c.msgs <- msg
		}
	}()
	t.Cleanup(func() { _ = conn.Close() })
	return c
}

func (c *testClientConn) readMessage(t *testing.T) WSMessage {
	t.Helper()
	// select は複数 case が同時に受信可能だとランダムに選ぶため、msgs を先に非ブロッキングで確認する。
	select {
	case msg := <-c.msgs:
		return msg
	default:
	}
	select {
	case msg := <-c.msgs:
		return msg
	case err := <-c.errs:
		t.Fatalf("connection closed while waiting for a message: %v", err)
	case <-time.After(testMessageTimeout):
		t.Fatal("expected a message to arrive")
	}
	return WSMessage{}
}

func (c *testClientConn) readMessagesOfType(t *testing.T, types ...string) []WSMessage {
	t.Helper()
	msgs := make([]WSMessage, 0, len(types))
	for _, want := range types {
		msg := c.readMessage(t)
		require.Equal(t, want, msg.Type)
		msgs = append(msgs, msg)
	}
	return msgs
}

func (c *testClientConn) expectNoMessage(t *testing.T) {
	t.Helper()
	select {
	case msg := <-c.msgs:
		t.Fatalf("expected no message to arrive, got type=%q", msg.Type)
	case err := <-c.errs:
		t.Fatalf("expected no message, but the connection closed: %v", err)
	case <-time.After(testNoMessageWindow):
	}
}

func (c *testClientConn) expectClosed(t *testing.T) {
	t.Helper()
	select {
	case <-c.errs:
	case msg := <-c.msgs:
		t.Fatalf("expected the connection to close, got message type=%q instead", msg.Type)
	case <-time.After(testMessageTimeout):
		t.Fatal("expected the connection to close")
	}
}

// --- port.AccountClient ---

type awardGameExpCall struct {
	p1ID, p2ID string
	winnerNum  int64
	reason     string
	matchType  string
}

type revertBattleCountCall struct {
	gameID, p1ID, p2ID string
	consumedAt         time.Time
}

type stubAccountClient struct {
	mu sync.Mutex

	registerFunc             func(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error)
	loginFunc                func(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error)
	findByFirebaseUIDFunc    func(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error)
	getMeFunc                func(ctx context.Context) (*apiaccount.PlayerResponse, error)
	getBattleLimitFunc       func(ctx context.Context) (*apiaccount.BattleLimitResponse, error)
	incrementBattleCountFunc func(ctx context.Context) error
	awardGameExpFunc         func(ctx context.Context, p1ID, p2ID string, winnerNum int64, reason, matchType string) error
	revertBattleCountFunc    func(ctx context.Context, gameID, p1ID, p2ID string, consumedAt time.Time) error

	incrementBattleCountCalls int
	awardGameExpCalls         []awardGameExpCall
	revertBattleCountCalls    []revertBattleCountCall
}

func (s *stubAccountClient) Register(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
	if s.registerFunc == nil {
		panic("stubAccountClient: Register is not used by this test")
	}
	return s.registerFunc(ctx, firebaseUID)
}

func (s *stubAccountClient) Login(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
	if s.loginFunc == nil {
		panic("stubAccountClient: Login is not used by this test")
	}
	return s.loginFunc(ctx, firebaseUID)
}

func (s *stubAccountClient) FindByFirebaseUID(ctx context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
	if s.findByFirebaseUIDFunc == nil {
		panic("stubAccountClient: FindByFirebaseUID is not used by this test")
	}
	return s.findByFirebaseUIDFunc(ctx, firebaseUID)
}

func (s *stubAccountClient) GetMe(ctx context.Context) (*apiaccount.PlayerResponse, error) {
	if s.getMeFunc == nil {
		panic("stubAccountClient: GetMe is not used by this test")
	}
	return s.getMeFunc(ctx)
}

func (s *stubAccountClient) GetBattleLimit(ctx context.Context) (*apiaccount.BattleLimitResponse, error) {
	if s.getBattleLimitFunc == nil {
		panic("stubAccountClient: GetBattleLimit is not used by this test")
	}
	return s.getBattleLimitFunc(ctx)
}

func (s *stubAccountClient) IncrementBattleCount(ctx context.Context) error {
	s.mu.Lock()
	s.incrementBattleCountCalls++
	s.mu.Unlock()
	if s.incrementBattleCountFunc == nil {
		return nil
	}
	return s.incrementBattleCountFunc(ctx)
}

func (s *stubAccountClient) incrementBattleCountCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.incrementBattleCountCalls
}

func (s *stubAccountClient) AwardGameExp(ctx context.Context, p1ID, p2ID string, winnerNum int64, reason, matchType string) error {
	s.mu.Lock()
	s.awardGameExpCalls = append(s.awardGameExpCalls, awardGameExpCall{p1ID, p2ID, winnerNum, reason, matchType})
	s.mu.Unlock()
	if s.awardGameExpFunc == nil {
		return nil
	}
	return s.awardGameExpFunc(ctx, p1ID, p2ID, winnerNum, reason, matchType)
}

func (s *stubAccountClient) awardGameExpCallsSnapshot() []awardGameExpCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]awardGameExpCall(nil), s.awardGameExpCalls...)
}

func (s *stubAccountClient) RevertBattleCount(ctx context.Context, gameID, p1ID, p2ID string, consumedAt time.Time) error {
	s.mu.Lock()
	s.revertBattleCountCalls = append(s.revertBattleCountCalls, revertBattleCountCall{gameID, p1ID, p2ID, consumedAt})
	s.mu.Unlock()
	if s.revertBattleCountFunc == nil {
		return nil
	}
	return s.revertBattleCountFunc(ctx, gameID, p1ID, p2ID, consumedAt)
}

func (s *stubAccountClient) revertBattleCountCallsSnapshot() []revertBattleCountCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]revertBattleCountCall(nil), s.revertBattleCountCalls...)
}

var _ port.AccountClient = (*stubAccountClient)(nil)

// --- port.CardClient ---

type stubCardClient struct {
	mu sync.Mutex

	getDeckCardsFunc          func(ctx context.Context, deckID int64) ([]apicard.DeckCard, port.DeckInitiatives, error)
	validateDeckForBattleFunc func(ctx context.Context, deckID int64) error

	validateDeckForBattleCalls []int64
}

func (s *stubCardClient) GetDeckCards(ctx context.Context, deckID int64) ([]apicard.DeckCard, port.DeckInitiatives, error) {
	if s.getDeckCardsFunc == nil {
		panic("stubCardClient: GetDeckCards is not used by this test")
	}
	return s.getDeckCardsFunc(ctx, deckID)
}

func (s *stubCardClient) ValidateDeckForBattle(ctx context.Context, deckID int64) error {
	s.mu.Lock()
	s.validateDeckForBattleCalls = append(s.validateDeckForBattleCalls, deckID)
	s.mu.Unlock()
	if s.validateDeckForBattleFunc == nil {
		panic("stubCardClient: ValidateDeckForBattle is not used by this test")
	}
	return s.validateDeckForBattleFunc(ctx, deckID)
}

func (s *stubCardClient) validateDeckForBattleCallsSnapshot() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.validateDeckForBattleCalls...)
}

var _ port.CardClient = (*stubCardClient)(nil)

// --- port.MatchmakingClient ---

type reportMatchAbandonedCall struct {
	matchID   string
	playerIDs []string
}

type enqueueCall struct {
	deckID int64
	name   string
	level  int64
}

type stubMatchmakingClient struct {
	mu sync.Mutex

	enqueueFunc               func(ctx context.Context, deckID int64, name string, level int64) error
	cancelFunc                func(ctx context.Context) error
	reportMatchAbandonedFunc  func(ctx context.Context, matchID string, playerIDs []string) error
	reportMatchAbandonedCalls []reportMatchAbandonedCall
	cancelCalls               int
	enqueueCalls              []enqueueCall
}

func (s *stubMatchmakingClient) Enqueue(ctx context.Context, deckID int64, name string, level int64) error {
	s.mu.Lock()
	s.enqueueCalls = append(s.enqueueCalls, enqueueCall{deckID, name, level})
	s.mu.Unlock()
	if s.enqueueFunc == nil {
		panic("stubMatchmakingClient: Enqueue is not used by this test")
	}
	return s.enqueueFunc(ctx, deckID, name, level)
}

func (s *stubMatchmakingClient) enqueueCallsSnapshot() []enqueueCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]enqueueCall(nil), s.enqueueCalls...)
}

// Cancel は接続断のたびに登録済みマッチメイキング待機列から退出させるため常に
// 呼ばれうる。個別に検証したいテストだけ cancelFunc を設定すればよい。
func (s *stubMatchmakingClient) Cancel(ctx context.Context) error {
	s.mu.Lock()
	s.cancelCalls++
	s.mu.Unlock()
	if s.cancelFunc == nil {
		return nil
	}
	return s.cancelFunc(ctx)
}

func (s *stubMatchmakingClient) cancelCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelCalls
}

func (s *stubMatchmakingClient) ReportMatchAbandoned(ctx context.Context, matchID string, playerIDs []string) error {
	s.mu.Lock()
	s.reportMatchAbandonedCalls = append(s.reportMatchAbandonedCalls, reportMatchAbandonedCall{matchID, append([]string(nil), playerIDs...)})
	s.mu.Unlock()
	if s.reportMatchAbandonedFunc == nil {
		return nil
	}
	return s.reportMatchAbandonedFunc(ctx, matchID, playerIDs)
}

func (s *stubMatchmakingClient) reportMatchAbandonedCallsSnapshot() []reportMatchAbandonedCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]reportMatchAbandonedCall(nil), s.reportMatchAbandonedCalls...)
}

var _ port.MatchmakingClient = (*stubMatchmakingClient)(nil)

// --- port.GamePlayerRepo ---

type insertGamePlayerCall struct {
	gameID    string
	playerNum int
	playerID  string
}

type stubGamePlayerRepo struct {
	mu sync.Mutex

	insertGamePlayerFunc   func(ctx context.Context, gameID string, playerNum int, playerID string) error
	lookupPlayerNumFunc    func(ctx context.Context, gameID string, playerID string) (int, error)
	lookupGamePlayersFunc  func(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error)
	markExpAwardedFunc     func(ctx context.Context, gameID string) (bool, error)
	countPlayersByGameFunc func(ctx context.Context, gameIDs []string) (map[string]int, error)

	insertGamePlayerCalls   []insertGamePlayerCall
	markExpAwardedCalls     []string
	countPlayersByGameCalls [][]string
}

func (s *stubGamePlayerRepo) InsertGamePlayer(ctx context.Context, gameID string, playerNum int, playerID string) error {
	s.mu.Lock()
	s.insertGamePlayerCalls = append(s.insertGamePlayerCalls, insertGamePlayerCall{gameID, playerNum, playerID})
	s.mu.Unlock()
	if s.insertGamePlayerFunc == nil {
		return nil
	}
	return s.insertGamePlayerFunc(ctx, gameID, playerNum, playerID)
}

func (s *stubGamePlayerRepo) insertGamePlayerCallsSnapshot() []insertGamePlayerCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]insertGamePlayerCall(nil), s.insertGamePlayerCalls...)
}

func (s *stubGamePlayerRepo) LookupPlayerNum(ctx context.Context, gameID string, playerID string) (int, error) {
	if s.lookupPlayerNumFunc == nil {
		panic("stubGamePlayerRepo: LookupPlayerNum is not used by this test")
	}
	return s.lookupPlayerNumFunc(ctx, gameID, playerID)
}

func (s *stubGamePlayerRepo) LookupGamePlayers(ctx context.Context, gameID string) ([]port.GamePlayerEntry, error) {
	if s.lookupGamePlayersFunc == nil {
		panic("stubGamePlayerRepo: LookupGamePlayers is not used by this test")
	}
	return s.lookupGamePlayersFunc(ctx, gameID)
}

func (s *stubGamePlayerRepo) MarkExpAwarded(ctx context.Context, gameID string) (bool, error) {
	s.mu.Lock()
	s.markExpAwardedCalls = append(s.markExpAwardedCalls, gameID)
	s.mu.Unlock()
	if s.markExpAwardedFunc == nil {
		panic("stubGamePlayerRepo: MarkExpAwarded is not used by this test")
	}
	return s.markExpAwardedFunc(ctx, gameID)
}

func (s *stubGamePlayerRepo) markExpAwardedCallsSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.markExpAwardedCalls...)
}

func (s *stubGamePlayerRepo) CountPlayersByGame(ctx context.Context, gameIDs []string) (map[string]int, error) {
	s.mu.Lock()
	s.countPlayersByGameCalls = append(s.countPlayersByGameCalls, append([]string(nil), gameIDs...))
	s.mu.Unlock()
	if s.countPlayersByGameFunc == nil {
		panic("stubGamePlayerRepo: CountPlayersByGame is not used by this test")
	}
	return s.countPlayersByGameFunc(ctx, gameIDs)
}

func (s *stubGamePlayerRepo) countPlayersByGameCallsSnapshot() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]string(nil), s.countPlayersByGameCalls...)
}

var _ port.GamePlayerRepo = (*stubGamePlayerRepo)(nil)

// --- port.InvalidatedGameRepo ---

type stubInvalidatedGameRepo struct {
	mu sync.Mutex

	markInvalidatedFunc   func(ctx context.Context, gameIDs []string) error
	listUnfinishedFunc    func(ctx context.Context) ([]string, error)
	markFinishedFunc      func(ctx context.Context, gameID string) error
	listRevertPendingFunc func(ctx context.Context) ([]string, error)
	markRevertedFunc      func(ctx context.Context, gameID string) error
	isInvalidatedFunc     func(ctx context.Context, gameID string) (bool, error)

	markInvalidatedCalls   [][]string
	markFinishedCalls      []string
	markRevertedCalls      []string
	listRevertPendingCalls int
}

func (s *stubInvalidatedGameRepo) MarkInvalidated(ctx context.Context, gameIDs []string) error {
	s.mu.Lock()
	s.markInvalidatedCalls = append(s.markInvalidatedCalls, append([]string(nil), gameIDs...))
	s.mu.Unlock()
	if s.markInvalidatedFunc == nil {
		return nil
	}
	return s.markInvalidatedFunc(ctx, gameIDs)
}

func (s *stubInvalidatedGameRepo) markInvalidatedCallsSnapshot() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]string(nil), s.markInvalidatedCalls...)
}

func (s *stubInvalidatedGameRepo) ListUnfinished(ctx context.Context) ([]string, error) {
	if s.listUnfinishedFunc == nil {
		panic("stubInvalidatedGameRepo: ListUnfinished is not used by this test")
	}
	return s.listUnfinishedFunc(ctx)
}

func (s *stubInvalidatedGameRepo) MarkFinished(ctx context.Context, gameID string) error {
	s.mu.Lock()
	s.markFinishedCalls = append(s.markFinishedCalls, gameID)
	s.mu.Unlock()
	if s.markFinishedFunc == nil {
		return nil
	}
	return s.markFinishedFunc(ctx, gameID)
}

func (s *stubInvalidatedGameRepo) markFinishedCallsSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.markFinishedCalls...)
}

func (s *stubInvalidatedGameRepo) ListRevertPending(ctx context.Context) ([]string, error) {
	s.mu.Lock()
	s.listRevertPendingCalls++
	s.mu.Unlock()
	if s.listRevertPendingFunc == nil {
		panic("stubInvalidatedGameRepo: ListRevertPending is not used by this test")
	}
	return s.listRevertPendingFunc(ctx)
}

func (s *stubInvalidatedGameRepo) listRevertPendingCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listRevertPendingCalls
}

func (s *stubInvalidatedGameRepo) MarkReverted(ctx context.Context, gameID string) error {
	s.mu.Lock()
	s.markRevertedCalls = append(s.markRevertedCalls, gameID)
	s.mu.Unlock()
	if s.markRevertedFunc == nil {
		return nil
	}
	return s.markRevertedFunc(ctx, gameID)
}

func (s *stubInvalidatedGameRepo) markRevertedCallsSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.markRevertedCalls...)
}

func (s *stubInvalidatedGameRepo) IsInvalidated(ctx context.Context, gameID string) (bool, error) {
	if s.isInvalidatedFunc == nil {
		panic("stubInvalidatedGameRepo: IsInvalidated is not used by this test")
	}
	return s.isInvalidatedFunc(ctx, gameID)
}

var _ port.InvalidatedGameRepo = (*stubInvalidatedGameRepo)(nil)

// --- port.ProcessedMatchRepo ---

type recordGameCreatedCall struct {
	matchID, gameID string
}

type stubProcessedMatchRepo struct {
	mu sync.Mutex

	claimFunc             func(ctx context.Context, matchID string) (bool, error)
	releaseFunc           func(ctx context.Context, matchID string) error
	recordGameCreatedFunc func(ctx context.Context, matchID, gameID string) error
	gameIDForFunc         func(ctx context.Context, matchID string) (string, bool, error)
	markNotifiedFunc      func(ctx context.Context, matchID string) (bool, error)

	recordGameCreatedCalls []recordGameCreatedCall
}

func (s *stubProcessedMatchRepo) Claim(ctx context.Context, matchID string) (bool, error) {
	if s.claimFunc == nil {
		panic("stubProcessedMatchRepo: Claim is not used by this test")
	}
	return s.claimFunc(ctx, matchID)
}

func (s *stubProcessedMatchRepo) Release(ctx context.Context, matchID string) error {
	if s.releaseFunc == nil {
		panic("stubProcessedMatchRepo: Release is not used by this test")
	}
	return s.releaseFunc(ctx, matchID)
}

func (s *stubProcessedMatchRepo) RecordGameCreated(ctx context.Context, matchID, gameID string) error {
	s.mu.Lock()
	s.recordGameCreatedCalls = append(s.recordGameCreatedCalls, recordGameCreatedCall{matchID, gameID})
	s.mu.Unlock()
	if s.recordGameCreatedFunc == nil {
		return nil
	}
	return s.recordGameCreatedFunc(ctx, matchID, gameID)
}

func (s *stubProcessedMatchRepo) recordGameCreatedCallsSnapshot() []recordGameCreatedCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recordGameCreatedCall(nil), s.recordGameCreatedCalls...)
}

func (s *stubProcessedMatchRepo) GameIDFor(ctx context.Context, matchID string) (string, bool, error) {
	if s.gameIDForFunc == nil {
		panic("stubProcessedMatchRepo: GameIDFor is not used by this test")
	}
	return s.gameIDForFunc(ctx, matchID)
}

func (s *stubProcessedMatchRepo) MarkNotified(ctx context.Context, matchID string) (bool, error) {
	if s.markNotifiedFunc == nil {
		panic("stubProcessedMatchRepo: MarkNotified is not used by this test")
	}
	return s.markNotifiedFunc(ctx, matchID)
}

var _ port.ProcessedMatchRepo = (*stubProcessedMatchRepo)(nil)

// --- port.TimerStore ---

type setDisconnectDeadlineCall struct {
	playerID, gameID string
	deadline         time.Time
}

type stubTimerStore struct {
	mu sync.Mutex

	setDisconnectDeadlineFunc   func(ctx context.Context, playerID, gameID string, deadline time.Time) error
	clearDisconnectDeadlineFunc func(ctx context.Context, playerID string) error
	getDisconnectDeadlineFunc   func(ctx context.Context, playerID string) (port.DisconnectDeadline, bool, error)

	setDisconnectDeadlineCalls   []setDisconnectDeadlineCall
	clearDisconnectDeadlineCalls []string
}

func (s *stubTimerStore) SetDisconnectDeadline(ctx context.Context, playerID, gameID string, deadline time.Time) error {
	s.mu.Lock()
	s.setDisconnectDeadlineCalls = append(s.setDisconnectDeadlineCalls, setDisconnectDeadlineCall{playerID, gameID, deadline})
	s.mu.Unlock()
	if s.setDisconnectDeadlineFunc == nil {
		return nil
	}
	return s.setDisconnectDeadlineFunc(ctx, playerID, gameID, deadline)
}

func (s *stubTimerStore) setDisconnectDeadlineCallsSnapshot() []setDisconnectDeadlineCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]setDisconnectDeadlineCall(nil), s.setDisconnectDeadlineCalls...)
}

func (s *stubTimerStore) ClearDisconnectDeadline(ctx context.Context, playerID string) error {
	s.mu.Lock()
	s.clearDisconnectDeadlineCalls = append(s.clearDisconnectDeadlineCalls, playerID)
	s.mu.Unlock()
	if s.clearDisconnectDeadlineFunc == nil {
		return nil
	}
	return s.clearDisconnectDeadlineFunc(ctx, playerID)
}

func (s *stubTimerStore) clearDisconnectDeadlineCallsSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.clearDisconnectDeadlineCalls...)
}

func (s *stubTimerStore) GetDisconnectDeadline(ctx context.Context, playerID string) (port.DisconnectDeadline, bool, error) {
	if s.getDisconnectDeadlineFunc == nil {
		return port.DisconnectDeadline{}, false, nil
	}
	return s.getDisconnectDeadlineFunc(ctx, playerID)
}

var _ port.TimerStore = (*stubTimerStore)(nil)

// --- service.BattleClient ---

type processActionCall struct {
	gameID     string
	playerNum  int
	actionType string
	data       json.RawMessage
}

type createPvPGameCall struct {
	deck1Cards, deck2Cards             []service.BattleDeckCard
	deck1Initiatives, deck2Initiatives service.DeckInitiatives
	player1Summary, player2Summary     service.PlayerSummaryRequest
}

type startNPCBattleCall struct {
	deckCards                      []service.BattleDeckCard
	deckInitiatives                service.DeckInitiatives
	npcModel                       string
	player1Summary, player2Summary service.PlayerSummaryRequest
}

type stubBattleClient struct {
	mu sync.Mutex

	startNPCBattleFunc           func(ctx context.Context, deckCards []service.BattleDeckCard, deckInitiatives service.DeckInitiatives, npcModel string, player1Summary, player2Summary service.PlayerSummaryRequest) (*service.GameCreatedResult, error)
	createPvPGameFunc            func(ctx context.Context, deck1Cards, deck2Cards []service.BattleDeckCard, deck1Initiatives, deck2Initiatives service.DeckInitiatives, player1Summary, player2Summary service.PlayerSummaryRequest) (*service.GameCreatedResult, error)
	processActionFunc            func(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error)
	getGameStateForPlayerFunc    func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error)
	getTurnControlsForPlayerFunc func(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error)
	advanceNpcTurnFunc           func(ctx context.Context, gameID string) (*service.ActionResult, error)
	listNpcModelsFunc            func(ctx context.Context) ([]service.NpcModelEntry, error)

	processActionCalls  []processActionCall
	advanceNpcTurnCalls []string
	createPvPGameCalls  []createPvPGameCall
	startNPCBattleCalls []startNPCBattleCall
}

func (s *stubBattleClient) StartNPCBattle(ctx context.Context, deckCards []service.BattleDeckCard, deckInitiatives service.DeckInitiatives, npcModel string, player1Summary, player2Summary service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
	s.mu.Lock()
	s.startNPCBattleCalls = append(s.startNPCBattleCalls, startNPCBattleCall{deckCards, deckInitiatives, npcModel, player1Summary, player2Summary})
	s.mu.Unlock()
	if s.startNPCBattleFunc == nil {
		panic("stubBattleClient: StartNPCBattle is not used by this test")
	}
	return s.startNPCBattleFunc(ctx, deckCards, deckInitiatives, npcModel, player1Summary, player2Summary)
}

func (s *stubBattleClient) startNPCBattleCallsSnapshot() []startNPCBattleCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]startNPCBattleCall(nil), s.startNPCBattleCalls...)
}

func (s *stubBattleClient) CreatePvPGame(ctx context.Context, deck1Cards, deck2Cards []service.BattleDeckCard, deck1Initiatives, deck2Initiatives service.DeckInitiatives, player1Summary, player2Summary service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
	s.mu.Lock()
	s.createPvPGameCalls = append(s.createPvPGameCalls, createPvPGameCall{deck1Cards, deck2Cards, deck1Initiatives, deck2Initiatives, player1Summary, player2Summary})
	s.mu.Unlock()
	if s.createPvPGameFunc == nil {
		panic("stubBattleClient: CreatePvPGame is not used by this test")
	}
	return s.createPvPGameFunc(ctx, deck1Cards, deck2Cards, deck1Initiatives, deck2Initiatives, player1Summary, player2Summary)
}

func (s *stubBattleClient) createPvPGameCallsSnapshot() []createPvPGameCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]createPvPGameCall(nil), s.createPvPGameCalls...)
}

func (s *stubBattleClient) ProcessAction(ctx context.Context, gameID string, playerNum int, actionType string, data json.RawMessage) (*service.ActionResult, error) {
	s.mu.Lock()
	s.processActionCalls = append(s.processActionCalls, processActionCall{gameID, playerNum, actionType, data})
	s.mu.Unlock()
	if s.processActionFunc == nil {
		panic("stubBattleClient: ProcessAction is not used by this test")
	}
	return s.processActionFunc(ctx, gameID, playerNum, actionType, data)
}

func (s *stubBattleClient) processActionCallsSnapshot() []processActionCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]processActionCall(nil), s.processActionCalls...)
}

func (s *stubBattleClient) GetGameStateForPlayer(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
	if s.getGameStateForPlayerFunc == nil {
		panic("stubBattleClient: GetGameStateForPlayer is not used by this test")
	}
	return s.getGameStateForPlayerFunc(ctx, gameID, playerNum)
}

func (s *stubBattleClient) GetTurnControlsForPlayer(ctx context.Context, gameID string, playerNum int) (json.RawMessage, error) {
	if s.getTurnControlsForPlayerFunc == nil {
		panic("stubBattleClient: GetTurnControlsForPlayer is not used by this test")
	}
	return s.getTurnControlsForPlayerFunc(ctx, gameID, playerNum)
}

func (s *stubBattleClient) AdvanceNpcTurn(ctx context.Context, gameID string) (*service.ActionResult, error) {
	s.mu.Lock()
	s.advanceNpcTurnCalls = append(s.advanceNpcTurnCalls, gameID)
	s.mu.Unlock()
	if s.advanceNpcTurnFunc == nil {
		panic("stubBattleClient: AdvanceNpcTurn is not used by this test")
	}
	return s.advanceNpcTurnFunc(ctx, gameID)
}

func (s *stubBattleClient) advanceNpcTurnCallsSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.advanceNpcTurnCalls...)
}

func (s *stubBattleClient) ListNpcModels(ctx context.Context) ([]service.NpcModelEntry, error) {
	if s.listNpcModelsFunc == nil {
		panic("stubBattleClient: ListNpcModels is not used by this test")
	}
	return s.listNpcModelsFunc(ctx)
}

var _ service.BattleClient = (*stubBattleClient)(nil)

// --- 汎用フィクスチャ ---

func minimalClientGameState() apibattle.ClientGameState {
	return apibattle.ClientGameState{
		ActivePlayer:   1,
		CurrentPhase:   "main",
		CurrentTurn:    1,
		GameID:         "game-1",
		IsMyTurn:       false,
		MyView:         apibattle.PlayerView{PlayerNum: 1},
		OppView:        apibattle.OpponentView{PlayerNum: 2},
		Player1Summary: apibattle.PlayerSummary{Name: "Player One"},
		Player2Summary: apibattle.PlayerSummary{Name: "Player Two"},
		TurnStartedAt:  time.Now(),
	}
}

func newRawGameState(t *testing.T, state apibattle.ClientGameState) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(state)
	require.NoError(t, err)
	return data
}

func newRawTurnControls(t *testing.T, controls apibattle.TurnControlsMessage) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(controls)
	require.NoError(t, err)
	return data
}

// --- Manager 組み立て ---

func newTestInternalSigner(t *testing.T) *internalauth.Signer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return internalauth.NewSigner(internalauth.StaticPrivateKeyResolver(key, internalauth.DefaultKeyID), internalauth.DefaultKeyID)
}

type managerDeps struct {
	battle             *stubBattleClient
	account            *stubAccountClient
	card               *stubCardClient
	matchmaking        *stubMatchmakingClient
	gamePlayers        *stubGamePlayerRepo
	processedMatch     *stubProcessedMatchRepo
	invalidatedGame    *stubInvalidatedGameRepo
	timerStore         *stubTimerStore
	matchmakingTimeout time.Duration
	disconnectTimeout  time.Duration
}

func newTestManager(t *testing.T, d managerDeps) *Manager {
	t.Helper()
	if d.battle == nil {
		d.battle = &stubBattleClient{}
	}
	if d.account == nil {
		d.account = &stubAccountClient{}
	}
	if d.card == nil {
		d.card = &stubCardClient{}
	}
	if d.matchmaking == nil {
		d.matchmaking = &stubMatchmakingClient{}
	}
	if d.gamePlayers == nil {
		d.gamePlayers = &stubGamePlayerRepo{}
	}
	if d.processedMatch == nil {
		d.processedMatch = &stubProcessedMatchRepo{}
	}
	if d.invalidatedGame == nil {
		d.invalidatedGame = &stubInvalidatedGameRepo{}
	}
	if d.timerStore == nil {
		d.timerStore = &stubTimerStore{}
	}
	if d.matchmakingTimeout == 0 {
		d.matchmakingTimeout = time.Minute
	}
	if d.disconnectTimeout == 0 {
		d.disconnectTimeout = DefaultDisconnectTimeout
	}
	return NewManager(d.battle, d.account, d.card, d.matchmaking, d.gamePlayers, d.processedMatch, d.invalidatedGame, d.matchmakingTimeout, newTestInternalSigner(t), d.timerStore, d.disconnectTimeout)
}

// --- ConnectionHub 組み立て ---

type hubDeps struct {
	callbacks         HubCallbacks
	disconnectTimeout time.Duration
	timerStore        *stubTimerStore
	clock             Clock
}

func newTestHub(t *testing.T, d hubDeps) *ConnectionHub {
	t.Helper()
	if d.disconnectTimeout == 0 {
		d.disconnectTimeout = DefaultDisconnectTimeout
	}
	if d.timerStore == nil {
		d.timerStore = &stubTimerStore{}
	}
	var opts []HubOption
	if d.clock != nil {
		opts = append(opts, WithHubClock(d.clock))
	}
	return NewConnectionHub(d.callbacks, d.disconnectTimeout, d.timerStore, opts...)
}

// --- GameRelay 組み立て ---

type relayDeps struct {
	hub          *ConnectionHub
	battleClient *stubBattleClient

	accountClient       *stubAccountClient
	accountUnconfigured bool

	gamePlayerRepo             *stubGamePlayerRepo
	gamePlayerRepoUnconfigured bool

	invalidatedGameRepo             *stubInvalidatedGameRepo
	invalidatedGameRepoUnconfigured bool

	clock Clock
}

func newTestGameRelay(t *testing.T, d relayDeps) *GameRelay {
	t.Helper()
	if d.hub == nil {
		d.hub = newTestHub(t, hubDeps{})
	}
	if d.battleClient == nil {
		d.battleClient = &stubBattleClient{}
	}

	var accountClient port.AccountClient
	if !d.accountUnconfigured {
		if d.accountClient == nil {
			d.accountClient = &stubAccountClient{}
		}
		accountClient = d.accountClient
	}

	var gamePlayerRepo port.GamePlayerRepo
	if !d.gamePlayerRepoUnconfigured {
		if d.gamePlayerRepo == nil {
			d.gamePlayerRepo = &stubGamePlayerRepo{}
		}
		gamePlayerRepo = d.gamePlayerRepo
	}

	var invalidatedGameRepo port.InvalidatedGameRepo
	if !d.invalidatedGameRepoUnconfigured {
		if d.invalidatedGameRepo == nil {
			d.invalidatedGameRepo = &stubInvalidatedGameRepo{}
		}
		invalidatedGameRepo = d.invalidatedGameRepo
	}

	var opts []GameRelayOption
	if d.clock != nil {
		opts = append(opts, WithClock(d.clock))
	}
	return NewGameRelay(d.hub, d.battleClient, accountClient, gamePlayerRepo, invalidatedGameRepo, opts...)
}

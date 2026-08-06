package ws

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	"github.com/kenyamaneko/overload-party-account/packages/api-account/apiaccountserverfake"
	apibattle "github.com/kenyamaneko/overload-party-battle/packages/api-battle-rpc-go"
	"github.com/kenyamaneko/overload-party-battle/packages/api-battle-rpc-go/apibattlerpcserverfake"
	gamelogic "github.com/kenyamaneko/overload-party-battle/packages/game-logic-constants-go"
	"github.com/kenyamaneko/overload-party-card/packages/api-card/apicardserverfake"
	apimatchmaking "github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking"
	"github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking/apimatchmakingserverfake"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/cardclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/matchmakingclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// statefulGamePlayerRepo は port.GamePlayerRepo のインメモリ実装。gameID ごとの
// プレイヤースロットを実際に保持し、InsertGamePlayer で仕込んだ内容を
// LookupPlayerNum / LookupGamePlayers がそのまま読み出せる (DB seed 相当)。
// exp_award_test.go の mockGamePlayerRepo は呼出内容の検証用で状態を持たないため、
// 別名の型として区別する。
type statefulGamePlayerRepo struct {
	mu      sync.Mutex
	entries map[string][]port.GamePlayerEntry // gameID → entries
	awarded map[string]bool                   // gameID → exp_awarded
}

func newStatefulGamePlayerRepo() *statefulGamePlayerRepo {
	return &statefulGamePlayerRepo{
		entries: make(map[string][]port.GamePlayerEntry),
		awarded: make(map[string]bool),
	}
}

func (r *statefulGamePlayerRepo) InsertGamePlayer(_ context.Context, gameID string, playerNum int, playerID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries[gameID] {
		if e.PlayerNum == playerNum {
			return nil // ON CONFLICT DO NOTHING
		}
	}
	r.entries[gameID] = append(r.entries[gameID], port.GamePlayerEntry{PlayerNum: playerNum, PlayerID: playerID, CreatedAt: time.Now()})
	return nil
}

func (r *statefulGamePlayerRepo) LookupPlayerNum(_ context.Context, gameID, playerID string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries[gameID] {
		if e.PlayerID == playerID {
			return e.PlayerNum, nil
		}
	}
	return 0, fmt.Errorf("player %s not found in game %s: %w", playerID, gameID, port.ErrNotFound)
}

func (r *statefulGamePlayerRepo) LookupGamePlayers(_ context.Context, gameID string) ([]port.GamePlayerEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]port.GamePlayerEntry(nil), r.entries[gameID]...), nil
}

func (r *statefulGamePlayerRepo) MarkExpAwarded(_ context.Context, gameID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.awarded[gameID] {
		return false, nil
	}
	r.awarded[gameID] = true
	return true, nil
}

func (r *statefulGamePlayerRepo) CountPlayersByGame(_ context.Context, gameIDs []string) (map[string]int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	counts := make(map[string]int, len(gameIDs))
	for _, gameID := range gameIDs {
		if n := len(r.entries[gameID]); n > 0 {
			counts[gameID] = n
		}
	}
	return counts, nil
}

var _ port.GamePlayerRepo = (*statefulGamePlayerRepo)(nil)

// wsTestOptions は newWSTestServer が組み立てる Manager/Handler の可変設定。
type wsTestOptions struct {
	allowedOrigins     []string
	matchmakingTimeout time.Duration
	disconnectTimeout  time.Duration
}

// wsTestServer は本番同等 (cmd/main と同じ組み立て) の WS サーバを httptest 上で
// 実際に起動し、下流 4 サービスの serverfake と結線した状態で保持する。
type wsTestServer struct {
	httpServer *httptest.Server
	manager    *Manager

	gamePlayerRepo *statefulGamePlayerRepo

	account     *apiaccountserverfake.Server
	seedAccount func(firebaseUID, playerID string)

	card        *apicardserverfake.Server
	matchmaking *apimatchmakingserverfake.Server
	battle      *apibattlerpcserverfake.Server
}

// newWSTestServer は Handler.HandleUpgrade を公開入口とする WS サーバを構築する。
// configure が nil でなければ既定オプションに変更を適用できる。
func newWSTestServer(t *testing.T, configure func(*wsTestOptions)) *wsTestServer {
	t.Helper()

	opts := wsTestOptions{
		matchmakingTimeout: 0,
		disconnectTimeout:  DefaultDisconnectTimeout,
	}
	if configure != nil {
		configure(&opts)
	}

	accountFake := apiaccountserverfake.NewServer()
	cardFake := apicardserverfake.NewServer()
	matchmakingFake := apimatchmakingserverfake.NewServer()
	battleFake := apibattlerpcserverfake.NewServer()
	t.Cleanup(accountFake.Close)
	t.Cleanup(cardFake.Close)
	t.Cleanup(matchmakingFake.Close)
	t.Cleanup(battleFake.Close)

	seedAccount := wireStatefulFindByFirebaseUID(accountFake)

	accountClient := accountclient.New(accountFake.URL(), &http.Client{})
	cardClient := cardclient.New(cardFake.URL(), &http.Client{})
	matchmakingClient := matchmakingclient.New(matchmakingFake.URL(), "test-instance", &http.Client{})
	battleClient := service.NewBattleClient(battleFake.URL(), &http.Client{})

	gamePlayerRepo := newStatefulGamePlayerRepo()
	internalAuthKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	internalSigner := internalauth.NewSigner(
		internalauth.StaticPrivateKeyResolver(internalAuthKey, internalauth.DefaultKeyID),
		internalauth.DefaultKeyID,
	)

	manager := NewManager(
		battleClient, accountClient, cardClient, matchmakingClient,
		gamePlayerRepo, nil, newFakeInvalidatedGameRepo(), opts.matchmakingTimeout, internalSigner, nil,
		opts.disconnectTimeout,
	)
	handler := NewHandler(manager, nil, accountClient, opts.allowedOrigins)

	r := gin.New()
	r.GET("/ws", handler.HandleUpgrade)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &wsTestServer{
		httpServer:     srv,
		manager:        manager,
		gamePlayerRepo: gamePlayerRepo,
		account:        accountFake,
		seedAccount:    seedAccount,
		card:           cardFake,
		matchmaking:    matchmakingFake,
		battle:         battleFake,
	}
}

// wireStatefulFindByFirebaseUID は FindByFirebaseUIDFn を firebaseUID → playerID の
// map に基づかせる。実 account の「未登録なら 404」を再現し、未 seed の uid は
// 401 (未登録) 経路を確実に踏ませる。
func wireStatefulFindByFirebaseUID(server *apiaccountserverfake.Server) func(firebaseUID, playerID string) {
	var mu sync.Mutex
	players := map[string]apiaccount.PlayerResponse{}
	server.FindByFirebaseUIDFn = func(firebaseUID string) (int, any) {
		mu.Lock()
		defer mu.Unlock()
		if p, ok := players[firebaseUID]; ok {
			return http.StatusOK, p
		}
		return http.StatusNotFound, nil
	}
	return func(firebaseUID, playerID string) {
		mu.Lock()
		defer mu.Unlock()
		players[firebaseUID] = apiaccount.PlayerResponse{PlayerID: playerID, FirebaseUID: firebaseUID}
	}
}

// setOnboardedAccount は matchmaking_start の正常系に必要な「オンボーディング完了」
// 「バトル回数上限に達していない」の 2 点を明示設定する。既定 (未設定) 応答は
// 空 PlayerResponse (未完了扱い) / CanBattle=false (上限到達扱い) のため、
// 正常系のテストはこれを呼ばないと先へ進めない。
func setOnboardedAccount(server *apiaccountserverfake.Server, name string, level int64) {
	server.GetPlayerFn = func() (int, any) {
		return http.StatusOK, apiaccount.PlayerResponse{
			PlayerID:         "self",
			OnboardingStatus: apiaccount.OnboardingStatusCompleted,
			Name:             &name,
			Level:            level,
		}
	}
	server.GetBattleLimitFn = func() (int, any) {
		return http.StatusOK, apiaccount.BattleLimitResponse{CanBattle: true}
	}
}

func devToken(uid string) string {
	return middleware.DevTokenPrefix + uid
}

func (s *wsTestServer) wsURL() string {
	return strings.Replace(s.httpServer.URL, "http://", "ws://", 1) + "/ws"
}

// dial は dev-token 経路で uid を認証し、確立した接続を返す。
func (s *wsTestServer) dial(t *testing.T, uid string) *websocket.Conn {
	t.Helper()
	conn, resp, err := websocket.DefaultDialer.Dial(s.wsURL()+"?token="+devToken(uid), nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// dialExpectRejected はハンドシェイクが wantStatus で拒否されることを確認する。
// query は "token=xxx" 形式のクエリ文字列 (空文字なら token パラメータ自体を付けない)。
func (s *wsTestServer) dialExpectRejected(t *testing.T, query string, header http.Header, wantStatus int) {
	t.Helper()
	url := s.wsURL()
	if query != "" {
		url += "?" + query
	}
	_, resp, err := websocket.DefaultDialer.Dial(url, header)
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, wantStatus, resp.StatusCode)
}

const wsReadWait = 5 * time.Second

// readUntilType は wantType のメッセージを受信するまで読み進める。
// ping/pong や本題とは無関係な push が挟まっても目的のメッセージまでスキップする。
func readUntilType(t *testing.T, conn *websocket.Conn, wantType string) WSMessage {
	t.Helper()
	deadline := time.Now().Add(wsReadWait)
	for {
		require.NoError(t, conn.SetReadDeadline(deadline))
		_, data, err := conn.ReadMessage()
		require.NoError(t, err, "waiting for message type %q", wantType)
		var msg WSMessage
		require.NoError(t, json.Unmarshal(data, &msg))
		if msg.Type == wantType {
			return msg
		}
	}
}

// readUntilActionPerformed は action_performed のうち action_type が wantActionType の
// ものを受信するまで読み進める。
func readUntilActionPerformed(t *testing.T, conn *websocket.Conn, wantActionType string) ActionPerformedMessage {
	t.Helper()
	deadline := time.Now().Add(wsReadWait)
	for {
		require.NoError(t, conn.SetReadDeadline(deadline))
		_, data, err := conn.ReadMessage()
		require.NoError(t, err, "waiting for action_performed action_type %q", wantActionType)
		var msg WSMessage
		require.NoError(t, json.Unmarshal(data, &msg))
		if msg.Type != genws.WSServerMsgActionPerformed {
			continue
		}
		var action ActionPerformedMessage
		require.NoError(t, json.Unmarshal(msg.Data, &action))
		if action.ActionType == wantActionType {
			return action
		}
	}
}

func decodeError(t *testing.T, msg WSMessage) ErrorMessage {
	t.Helper()
	require.Equal(t, genws.WSServerMsgError, msg.Type)
	var errBody ErrorMessage
	require.NoError(t, json.Unmarshal(msg.Data, &errBody))
	return errBody
}

// assertNoMessageWithin は within の間、何もメッセージが届かないことを確認する。
func assertNoMessageWithin(t *testing.T, conn *websocket.Conn, within time.Duration) {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(within)))
	_, _, err := conn.ReadMessage()
	require.Error(t, err)
	var netErr net.Error
	require.True(t, errors.As(err, &netErr) && netErr.Timeout(), "want a read timeout (no message), got: %v", err)
}

// fakeGameState は battle の GetGameStateFn に返す最小限の ClientGameState を組み立てる。
// isMyTurn は常に false にしてよい呼び出し元がターンタイマーを起動させない
// (発火すると forfeit 呼び出しが走りテストが不安定になる)。
func fakeGameState(gameID string, myName, oppName string) apibattle.ClientGameState {
	return apibattle.ClientGameState{
		GameID:         gameID,
		CurrentTurn:    1,
		CurrentPhase:   "main",
		ActivePlayer:   1,
		IsMyTurn:       false,
		Player1Summary: apibattle.PlayerSummary{Name: myName},
		Player2Summary: apibattle.PlayerSummary{Name: oppName},
	}
}

// enqueueRecorder は apimatchmakingserverfake.Server.EnqueueFn への呼出を記録する。
type enqueueRecorder struct {
	mu     sync.Mutex
	calls  []apimatchmaking.EnqueueRequest
	status int // 0 の場合は 202 (成功) を返す
}

func (r *enqueueRecorder) fn(req apimatchmaking.EnqueueRequest) (int, any) {
	r.mu.Lock()
	r.calls = append(r.calls, req)
	status := r.status
	r.mu.Unlock()
	if status == 0 {
		status = http.StatusAccepted
	}
	return status, nil
}

func (r *enqueueRecorder) snapshotCalls() []apimatchmaking.EnqueueRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]apimatchmaking.EnqueueRequest(nil), r.calls...)
}

// cancelRecorder は apimatchmakingserverfake.Server.CancelFn への呼出回数を記録する。
type cancelRecorder struct {
	calls  atomic.Int32
	status int // 0 の場合は 200 (成功) を返す
}

func (r *cancelRecorder) fn() (int, any) {
	r.calls.Add(1)
	status := r.status
	if status == 0 {
		status = http.StatusOK
	}
	return status, nil
}

// setupPvPGame は 2 人対戦の game_players 行と account の登録情報を仕込み、
// battle の GetGameStateFn / GetTurnControlsFn に安定した応答を設定する。
// WS 接続そのものは行わない (呼び出し元が enterGame で行う)。
func (s *wsTestServer) setupPvPGame(gameID, p1UID, p1PlayerID, p2UID, p2PlayerID string) {
	s.seedAccount(p1UID, p1PlayerID)
	s.seedAccount(p2UID, p2PlayerID)
	ctx := context.Background()
	_ = s.gamePlayerRepo.InsertGamePlayer(ctx, gameID, 1, p1PlayerID)
	_ = s.gamePlayerRepo.InsertGamePlayer(ctx, gameID, 2, p2PlayerID)

	s.battle.GetGameStateFn = func(gID string, _ int) (int, any) {
		return http.StatusOK, fakeGameState(gID, "TST-P1", "TST-P2")
	}
	s.battle.GetTurnControlsFn = func(string, int) (int, any) {
		return http.StatusOK, apibattle.TurnControlsMessage{}
	}
}

// setupNpcGame は NPC 対戦 (登録スロット 1 件のみ) の game_players 行・account 登録情報・
// battle の既定応答 (AdvanceNpcTurnFn は NpcPending=false) を仕込む。
func (s *wsTestServer) setupNpcGame(gameID, p1UID, p1PlayerID string) {
	s.seedAccount(p1UID, p1PlayerID)
	ctx := context.Background()
	_ = s.gamePlayerRepo.InsertGamePlayer(ctx, gameID, 1, p1PlayerID)

	s.battle.GetGameStateFn = func(gID string, _ int) (int, any) {
		return http.StatusOK, fakeGameState(gID, "TST-P1", "TST-NPC")
	}
	s.battle.GetTurnControlsFn = func(string, int) (int, any) {
		return http.StatusOK, apibattle.TurnControlsMessage{}
	}
	s.battle.AdvanceNpcTurnFn = func(string) (int, any) {
		return http.StatusOK, apibattle.ActionResult{NpcPending: false}
	}
}

// enterGame は game_enter を送信し、入室バーストを読み切った状態の接続を返す。
func (s *wsTestServer) enterGame(t *testing.T, uid, gameID string) *websocket.Conn {
	t.Helper()
	conn := s.dial(t, uid)

	require.NoError(t, conn.WriteJSON(WSMessage{
		Type: genws.WSClientMsgGameEnter,
		Data: mustMarshal(GameEnterMessage{GameID: gameID}),
	}))

	entered := readUntilType(t, conn, genws.WSServerMsgGameEntered)
	assert.Equal(t, genws.WSServerMsgGameEntered, entered.Type)
	readUntilActionPerformed(t, conn, gamelogic.EventTypeBattleStart)
	readUntilActionPerformed(t, conn, gamelogic.EventTypeTurnStart)
	readUntilType(t, conn, genws.WSServerMsgGameState)
	readUntilType(t, conn, genws.WSServerMsgTurnControls)
	return conn
}

func TestWSConnectionAuth(t *testing.T) {
	t.Run("WS接続の認証", func(t *testing.T) {
		t.Run("登録済みプレイヤーのトークンで接続すると、接続が確立しpingにpongが返る", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-ok", "player-ok")

			conn := srv.dial(t, "uid-ok")
			require.NoError(t, conn.WriteJSON(WSMessage{Type: genws.WSClientMsgPing}))

			msg := readUntilType(t, conn, genws.WSServerMsgPong)
			assert.Equal(t, genws.WSServerMsgPong, msg.Type)
		})

		t.Run("トークンなしで接続すると、401で拒否される", func(t *testing.T) {
			srv := newWSTestServer(t, nil)

			srv.dialExpectRejected(t, "", nil, http.StatusUnauthorized)
		})

		t.Run("dev-token形式でないトークンで接続すると、401で拒否される", func(t *testing.T) {
			srv := newWSTestServer(t, nil)

			srv.dialExpectRejected(t, "token=not-a-dev-token", nil, http.StatusUnauthorized)
		})

		t.Run("未登録ユーザーのトークンで接続すると、401で拒否される", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			// uid-unregistered は account fake に seed しない。

			srv.dialExpectRejected(t, "token="+devToken("uid-unregistered"), nil, http.StatusUnauthorized)
		})

		t.Run("許可オリジンを設定しているとき、許可外オリジンからの接続は拒否される", func(t *testing.T) {
			srv := newWSTestServer(t, func(o *wsTestOptions) {
				o.allowedOrigins = []string{"https://allowed.example"}
			})
			srv.seedAccount("uid-origin", "player-origin")
			header := http.Header{"Origin": []string{"https://not-allowed.example"}}

			srv.dialExpectRejected(t, "token="+devToken("uid-origin"), header, http.StatusForbidden)
		})
	})
}

func TestWSMessageRouting(t *testing.T) {
	t.Run("メッセージルーティング", func(t *testing.T) {
		t.Run("未知のメッセージ種別を送っても、接続は維持され続くpingにpongが返る", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-unknown-msg", "player-unknown-msg")
			conn := srv.dial(t, "uid-unknown-msg")

			require.NoError(t, conn.WriteJSON(WSMessage{Type: "not_a_real_message_type"}))
			require.NoError(t, conn.WriteJSON(WSMessage{Type: genws.WSClientMsgPing}))

			msg := readUntilType(t, conn, genws.WSServerMsgPong)
			assert.Equal(t, genws.WSServerMsgPong, msg.Type)
		})

		t.Run("JSONでないフレームを送ると、invalid_messageのエラーが返り接続は維持される", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-bad-frame", "player-bad-frame")
			conn := srv.dial(t, "uid-bad-frame")

			require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte("not json")))

			errMsg := readUntilType(t, conn, genws.WSServerMsgError)
			errBody := decodeError(t, errMsg)
			assert.Equal(t, "invalid_message", errBody.ErrorCode)

			require.NoError(t, conn.WriteJSON(WSMessage{Type: genws.WSClientMsgPing}))
			pong := readUntilType(t, conn, genws.WSServerMsgPong)
			assert.Equal(t, genws.WSServerMsgPong, pong.Type)
		})
	})
}

func TestWSMatchmakingStart(t *testing.T) {
	t.Run("マッチング開始", func(t *testing.T) {
		t.Run("オンボーディング完了済みでデッキ検証を通過すると、matchmaking_startedが返り待ち行列へ登録される", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-mm-ok", "player-mm-ok")
			setOnboardedAccount(srv.account, "TST-PLAYER", 7)
			srv.card.ValidateDeckForBattleFn = func(string) (int, any) { return http.StatusOK, nil }
			rec := &enqueueRecorder{}
			srv.matchmaking.EnqueueFn = rec.fn

			conn := srv.dial(t, "uid-mm-ok")
			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgMatchmakingStart,
				Data: mustMarshal(MatchmakingStartMessage{DeckID: 42}),
			}))

			msg := readUntilType(t, conn, genws.WSServerMsgMatchmakingStarted)
			assert.Equal(t, genws.WSServerMsgMatchmakingStarted, msg.Type)
			calls := rec.snapshotCalls()
			require.Len(t, calls, 1)
			assert.Equal(t, int64(42), calls[0].DeckID)
			assert.Equal(t, "TST-PLAYER", calls[0].Name)
			assert.Equal(t, int64(7), calls[0].Level)
		})

		t.Run("アカウントに表示名が設定されていないとき、空文字でマッチメイキングに登録される", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-mm-noname", "player-mm-noname")
			srv.account.GetPlayerFn = func() (int, any) {
				return http.StatusOK, apiaccount.PlayerResponse{
					PlayerID: "self", OnboardingStatus: apiaccount.OnboardingStatusCompleted, Level: 3,
				}
			}
			srv.account.GetBattleLimitFn = func() (int, any) {
				return http.StatusOK, apiaccount.BattleLimitResponse{CanBattle: true}
			}
			srv.card.ValidateDeckForBattleFn = func(string) (int, any) { return http.StatusOK, nil }
			rec := &enqueueRecorder{}
			srv.matchmaking.EnqueueFn = rec.fn

			conn := srv.dial(t, "uid-mm-noname")
			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgMatchmakingStart,
				Data: mustMarshal(MatchmakingStartMessage{DeckID: 1}),
			}))

			readUntilType(t, conn, genws.WSServerMsgMatchmakingStarted)
			calls := rec.snapshotCalls()
			require.Len(t, calls, 1)
			assert.Equal(t, "", calls[0].Name)
		})

		t.Run("dataがオブジェクトでないmatchmaking_startを送ると、invalid_dataのエラーが返る", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-mm-bad-data", "player-mm-bad-data")
			conn := srv.dial(t, "uid-mm-bad-data")

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgMatchmakingStart,
				Data: json.RawMessage(`"not-an-object"`),
			}))

			errMsg := readUntilType(t, conn, genws.WSServerMsgError)
			errBody := decodeError(t, errMsg)
			assert.Equal(t, "invalid_data", errBody.ErrorCode)
		})

		t.Run("オンボーディング未完了のとき、再試行不可のmatchmaking_errorが返る", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-mm-onboarding", "player-mm-onboarding")
			// GetPlayerFn は未設定のまま (既定応答は onboarding_status 空 = 未完了)。
			conn := srv.dial(t, "uid-mm-onboarding")

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgMatchmakingStart,
				Data: mustMarshal(MatchmakingStartMessage{DeckID: 1}),
			}))

			errMsg := readUntilType(t, conn, genws.WSServerMsgError)
			errBody := decodeError(t, errMsg)
			assert.Equal(t, "matchmaking_error", errBody.ErrorCode)
			assert.False(t, errBody.Retryable)
		})

		t.Run("当日のバトル回数上限に達しているとき、再試行不可のmatchmaking_errorが返る", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-mm-limit", "player-mm-limit")
			name := "TST-PLAYER"
			srv.account.GetPlayerFn = func() (int, any) {
				return http.StatusOK, apiaccount.PlayerResponse{
					PlayerID: "self", OnboardingStatus: apiaccount.OnboardingStatusCompleted, Name: &name,
				}
			}
			// GetBattleLimitFn は未設定のまま (既定応答は CanBattle=false = 上限到達扱い)。
			conn := srv.dial(t, "uid-mm-limit")

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgMatchmakingStart,
				Data: mustMarshal(MatchmakingStartMessage{DeckID: 1}),
			}))

			errMsg := readUntilType(t, conn, genws.WSServerMsgError)
			errBody := decodeError(t, errMsg)
			assert.Equal(t, "matchmaking_error", errBody.ErrorCode)
			assert.False(t, errBody.Retryable)
		})

		t.Run("デッキ検証に失敗したとき、再試行不可のmatchmaking_errorが返る", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-mm-deck", "player-mm-deck")
			setOnboardedAccount(srv.account, "TST-PLAYER", 1)
			srv.card.ValidateDeckForBattleFn = func(string) (int, any) {
				return http.StatusBadRequest, "deck invalid"
			}
			conn := srv.dial(t, "uid-mm-deck")

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgMatchmakingStart,
				Data: mustMarshal(MatchmakingStartMessage{DeckID: 1}),
			}))

			errMsg := readUntilType(t, conn, genws.WSServerMsgError)
			errBody := decodeError(t, errMsg)
			assert.Equal(t, "matchmaking_error", errBody.ErrorCode)
			assert.False(t, errBody.Retryable)
		})

		t.Run("待ち行列への登録が受付停止 (503)のとき、再試行可のmatchmaking_errorが返る", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-mm-503", "player-mm-503")
			setOnboardedAccount(srv.account, "TST-PLAYER", 1)
			srv.card.ValidateDeckForBattleFn = func(string) (int, any) { return http.StatusOK, nil }
			srv.matchmaking.EnqueueFn = func(apimatchmaking.EnqueueRequest) (int, any) {
				return http.StatusServiceUnavailable, "queue closed"
			}
			conn := srv.dial(t, "uid-mm-503")

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgMatchmakingStart,
				Data: mustMarshal(MatchmakingStartMessage{DeckID: 1}),
			}))

			errMsg := readUntilType(t, conn, genws.WSServerMsgError)
			errBody := decodeError(t, errMsg)
			assert.Equal(t, "matchmaking_error", errBody.ErrorCode)
			assert.True(t, errBody.Retryable)
		})

		t.Run("待ち行列への登録が異常応答 (500)のとき、再試行不可のmatchmaking_errorが返る", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-mm-500", "player-mm-500")
			setOnboardedAccount(srv.account, "TST-PLAYER", 1)
			srv.card.ValidateDeckForBattleFn = func(string) (int, any) { return http.StatusOK, nil }
			srv.matchmaking.EnqueueFn = func(apimatchmaking.EnqueueRequest) (int, any) {
				return http.StatusInternalServerError, "boom"
			}
			conn := srv.dial(t, "uid-mm-500")

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgMatchmakingStart,
				Data: mustMarshal(MatchmakingStartMessage{DeckID: 1}),
			}))

			errMsg := readUntilType(t, conn, genws.WSServerMsgError)
			errBody := decodeError(t, errMsg)
			assert.Equal(t, "matchmaking_error", errBody.ErrorCode)
			assert.False(t, errBody.Retryable)
		})

		t.Run("マッチが成立しないまま設定した待機時間を超えると、再試行可のmatchmaking_errorが届く", func(t *testing.T) {
			srv := newWSTestServer(t, func(o *wsTestOptions) {
				o.matchmakingTimeout = 100 * time.Millisecond
			})
			srv.seedAccount("uid-mm-timeout", "player-mm-timeout")
			setOnboardedAccount(srv.account, "TST-PLAYER", 1)
			srv.card.ValidateDeckForBattleFn = func(string) (int, any) { return http.StatusOK, nil }
			srv.matchmaking.EnqueueFn = func(apimatchmaking.EnqueueRequest) (int, any) {
				return http.StatusAccepted, nil
			}
			srv.matchmaking.CancelFn = (&cancelRecorder{}).fn
			conn := srv.dial(t, "uid-mm-timeout")

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgMatchmakingStart,
				Data: mustMarshal(MatchmakingStartMessage{DeckID: 1}),
			}))
			started := readUntilType(t, conn, genws.WSServerMsgMatchmakingStarted)
			assert.Equal(t, genws.WSServerMsgMatchmakingStarted, started.Type)

			errMsg := readUntilType(t, conn, genws.WSServerMsgError)
			errBody := decodeError(t, errMsg)
			assert.Equal(t, "matchmaking_error", errBody.ErrorCode)
			assert.True(t, errBody.Retryable)
		})
	})
}

func TestWSMatchmakingCancel(t *testing.T) {
	t.Run("マッチングキャンセル", func(t *testing.T) {
		t.Run("マッチング待ちをキャンセルすると、matchmaking_cancelledが返り待ち行列から除去される", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-cancel-ok", "player-cancel-ok")
			rec := &cancelRecorder{}
			srv.matchmaking.CancelFn = rec.fn
			conn := srv.dial(t, "uid-cancel-ok")

			require.NoError(t, conn.WriteJSON(WSMessage{Type: genws.WSClientMsgMatchmakingCancel}))

			msg := readUntilType(t, conn, genws.WSServerMsgMatchmakingCancelled)
			assert.Equal(t, genws.WSServerMsgMatchmakingCancelled, msg.Type)
			assert.EqualValues(t, 1, rec.calls.Load())
		})

		t.Run("キャンセルが下流エラーのとき、matchmaking_errorが返る", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-cancel-err", "player-cancel-err")
			srv.matchmaking.CancelFn = func() (int, any) { return http.StatusInternalServerError, "boom" }
			conn := srv.dial(t, "uid-cancel-err")

			require.NoError(t, conn.WriteJSON(WSMessage{Type: genws.WSClientMsgMatchmakingCancel}))

			errMsg := readUntilType(t, conn, genws.WSServerMsgError)
			errBody := decodeError(t, errMsg)
			assert.Equal(t, "matchmaking_error", errBody.ErrorCode)
		})
	})
}

func TestWSGameEnterAndDisconnectGrace(t *testing.T) {
	t.Run("ゲーム入室と切断猶予", func(t *testing.T) {
		t.Run("ゲームの参加者が入室すると、game_enteredに続いて対戦開始イベントと盤面状態が届く", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			const gameID = "TST-GAME-ENTER-OK"
			srv.setupPvPGame(gameID, "uid-enter-p1", "p-enter-1", "uid-enter-p2", "p-enter-2")

			conn := srv.dial(t, "uid-enter-p1")
			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgGameEnter,
				Data: mustMarshal(GameEnterMessage{GameID: gameID}),
			}))

			entered := readUntilType(t, conn, genws.WSServerMsgGameEntered)
			var enteredBody map[string]string
			require.NoError(t, json.Unmarshal(entered.Data, &enteredBody))
			assert.Equal(t, gameID, enteredBody["game_id"])

			readUntilActionPerformed(t, conn, gamelogic.EventTypeBattleStart)
			readUntilActionPerformed(t, conn, gamelogic.EventTypeTurnStart)
			gameState := readUntilType(t, conn, genws.WSServerMsgGameState)
			assert.Equal(t, genws.WSServerMsgGameState, gameState.Type)
			turnControls := readUntilType(t, conn, genws.WSServerMsgTurnControls)
			assert.Equal(t, genws.WSServerMsgTurnControls, turnControls.Type)
		})

		t.Run("ゲームに登録されていないプレイヤーが入室しようとすると、game_errorが返る", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			srv.seedAccount("uid-enter-unregistered", "player-enter-unregistered")
			// gamePlayerRepo に対応する行を投入しない。
			conn := srv.dial(t, "uid-enter-unregistered")

			require.NoError(t, conn.WriteJSON(WSMessage{
				Type: genws.WSClientMsgGameEnter,
				Data: mustMarshal(GameEnterMessage{GameID: "TST-GAME-NO-SUCH"}),
			}))

			errMsg := readUntilType(t, conn, genws.WSServerMsgError)
			errBody := decodeError(t, errMsg)
			assert.Equal(t, "game_error", errBody.ErrorCode)
		})

		t.Run("対戦中に相手が切断すると、opponent_disconnectedが届く", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			const gameID = "TST-GAME-DISCONNECT"
			srv.setupPvPGame(gameID, "uid-dc-p1", "p-dc-1", "uid-dc-p2", "p-dc-2")
			p1Conn := srv.enterGame(t, "uid-dc-p1", gameID)
			p2Conn := srv.enterGame(t, "uid-dc-p2", gameID)

			require.NoError(t, p1Conn.Close())

			msg := readUntilType(t, p2Conn, genws.WSServerMsgOpponentDisconnected)
			assert.Equal(t, genws.WSServerMsgOpponentDisconnected, msg.Type)
		})

		t.Run("切断した相手が猶予内に再接続すると、opponent_reconnectedが届く", func(t *testing.T) {
			srv := newWSTestServer(t, nil)
			const gameID = "TST-GAME-RECONNECT"
			srv.setupPvPGame(gameID, "uid-rc-p1", "p-rc-1", "uid-rc-p2", "p-rc-2")
			p1Conn := srv.enterGame(t, "uid-rc-p1", gameID)
			p2Conn := srv.enterGame(t, "uid-rc-p2", gameID)
			require.NoError(t, p1Conn.Close())
			readUntilType(t, p2Conn, genws.WSServerMsgOpponentDisconnected)

			srv.dial(t, "uid-rc-p1")

			msg := readUntilType(t, p2Conn, genws.WSServerMsgOpponentReconnected)
			assert.Equal(t, genws.WSServerMsgOpponentReconnected, msg.Type)
		})

		t.Run("相手が切断したまま猶予を超えると、切断負けのgame_overが届く", func(t *testing.T) {
			srv := newWSTestServer(t, func(o *wsTestOptions) {
				o.disconnectTimeout = 150 * time.Millisecond
			})
			const gameID = "TST-GAME-FORFEIT"
			srv.setupPvPGame(gameID, "uid-ft-p1", "p-ft-1", "uid-ft-p2", "p-ft-2")
			srv.battle.ProcessActionFn = func(string, apibattle.GameActionRequest) (int, any) {
				return http.StatusOK, apibattle.ActionResult{
					GameOver:         true,
					WinningPlayerNum: 2,
					WinReason:        gamelogic.WinReasonDisconnect,
				}
			}
			p1Conn := srv.enterGame(t, "uid-ft-p1", gameID)
			p2Conn := srv.enterGame(t, "uid-ft-p2", gameID)

			require.NoError(t, p1Conn.Close())

			msg := readUntilType(t, p2Conn, genws.WSServerMsgGameOver)
			var over GameOverMessage
			require.NoError(t, json.Unmarshal(msg.Data, &over))
			assert.Equal(t, gamelogic.WinReasonDisconnect, over.WinReason)
			assert.EqualValues(t, 2, over.WinningPlayerNum)
		})
	})
}

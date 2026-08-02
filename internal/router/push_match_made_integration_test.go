//go:build integration

package router_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	"github.com/kenyamaneko/overload-party-account/packages/api-account/apiaccountserverfake"
	apicard "github.com/kenyamaneko/overload-party-card/packages/api-card"
	apimatchmaking "github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking"
	"github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking/apimatchmakingserverfake"

	pubsubadapter "github.com/kenyamaneko/overload-party-gateway/internal/adapter/pubsub"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/matchmakingclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/handler/rest"
	ws "github.com/kenyamaneko/overload-party-gateway/internal/handler/ws"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository/postgrestest"
	"github.com/kenyamaneko/overload-party-gateway/internal/router"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
	genws "github.com/kenyamaneko/overload-party-gateway/packages/ws-constants"
)

const (
	matchMadePushPath = "/internal/v1/pubsub/match-made"
	wsPath            = "/ws"

	testPlayer1ID  = "11111111-1111-1111-1111-111111111111"
	testPlayer2ID  = "22222222-2222-2222-2222-222222222222"
	testPlayer1UID = "TST-UID-P1"
	testPlayer2UID = "TST-UID-P2"

	matchFoundWait   = 5 * time.Second
	noMatchFoundWait = 500 * time.Millisecond
)

var sharedPG *postgrestest.Postgres

// TestMain は Postgres testcontainer を package scope で 1 回だけ起動する。
// テスト間の分離は Postgres.Truncate で担保する。
func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(postgrestest.RunMain(m, &sharedPG))
}

// fakeBattleClient は service.BattleClient のテスト用実装。この結合テストが
// 経由する CreatePvPGame の呼出回数と結果だけを制御する。
type fakeBattleClient struct {
	mu            sync.Mutex
	calls         int
	failNextCalls int
	gameID        string
}

func (f *fakeBattleClient) StartNPCBattle(context.Context, []service.BattleDeckCard, service.DeckInitiatives, string, service.PlayerSummaryRequest, service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
	return nil, nil
}

func (f *fakeBattleClient) CreatePvPGame(_ context.Context, _, _ []service.BattleDeckCard, _, _ service.DeckInitiatives, _, _ service.PlayerSummaryRequest) (*service.GameCreatedResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.failNextCalls > 0 {
		f.failNextCalls--
		return nil, errors.New("battle unavailable")
	}
	return &service.GameCreatedResult{GameID: f.gameID}, nil
}

func (f *fakeBattleClient) ProcessAction(context.Context, string, int, string, json.RawMessage) (*service.ActionResult, error) {
	return nil, nil
}

func (f *fakeBattleClient) GetGameStateForPlayer(context.Context, string, int) (json.RawMessage, error) {
	return nil, nil
}

func (f *fakeBattleClient) GetTurnControlsForPlayer(context.Context, string, int) (json.RawMessage, error) {
	return nil, nil
}

func (f *fakeBattleClient) AdvanceNpcTurn(context.Context, string) (*service.ActionResult, error) {
	return nil, nil
}

func (f *fakeBattleClient) ListNpcModels(context.Context) ([]service.NpcModelEntry, error) {
	return nil, nil
}

func (f *fakeBattleClient) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

var _ service.BattleClient = (*fakeBattleClient)(nil)

// fakeCardClient は port.CardClient のテスト用実装。デッキ内容は本テストの
// 検証対象ではないため、常に空デッキを返す。
type fakeCardClient struct{}

func (f *fakeCardClient) ListAllCards(context.Context) ([]*apicard.CardDefinition, error) {
	return nil, nil
}

func (f *fakeCardClient) GetDeckCards(context.Context, int64) ([]apicard.DeckCard, port.DeckInitiatives, error) {
	return nil, port.DeckInitiatives{}, nil
}

func (f *fakeCardClient) ValidateDeckForBattle(context.Context, int64) error {
	return nil
}

var _ port.CardClient = (*fakeCardClient)(nil)

// failingGamePlayerRepo は先頭 failNextCalls 回の挿入だけを失敗させ、以降は実 DB の
// リポジトリへ委譲する。対戦の記録が失敗した回とその後の再配信を再現するために使う。
type failingGamePlayerRepo struct {
	*repository.PgGamePlayerRepository
	mu            sync.Mutex
	failNextCalls int
}

func (r *failingGamePlayerRepo) InsertGamePlayer(ctx context.Context, gameID string, playerNum int, playerID string) error {
	r.mu.Lock()
	if r.failNextCalls > 0 {
		r.failNextCalls--
		r.mu.Unlock()
		return errors.New("game_players unavailable")
	}
	r.mu.Unlock()
	return r.PgGamePlayerRepository.InsertGamePlayer(ctx, gameID, playerNum, playerID)
}

var _ port.GamePlayerRepo = (*failingGamePlayerRepo)(nil)

// buildPushEngine は「HTTP push → router → MatchSubscriber → Manager → repo → DB」を
// 実 DB まで直結した engine を、既存の DB 状態を消さずに組み立てる。プロセス再起動を
// 模すテストで、r1 が残した永続状態を r2 が引き継ぐ形を再現するために使う。
func buildPushEngine(
	battleClient service.BattleClient,
	gamePlayerRepo port.GamePlayerRepo,
	accountClient port.AccountClient,
	matchmakingClient port.MatchmakingClient,
) (*gin.Engine, *ws.Manager) {
	processedMatchRepo := repository.NewPgProcessedMatchRepository(sharedPG.Pool)
	wsManager := ws.NewManager(battleClient, accountClient, &fakeCardClient{}, matchmakingClient, gamePlayerRepo, processedMatchRepo, 0, nil, nil, ws.DefaultDisconnectTimeout)
	matchSub, err := pubsubadapter.NewMatchSubscriber(wsManager)
	if err != nil {
		panic(err)
	}
	pushHandler := rest.NewPubSubPushHandler(matchSub)
	handlers := &router.Handlers{PubSub: pushHandler}

	r := gin.New()
	internalGroup := r.Group("/internal/v1")
	router.RegisterPubSubRoutes(internalGroup, handlers)
	return r, wsManager
}

// buildPushRouter は push 受け口だけの router と実 DB のリポジトリを返す。
func buildPushRouter(battleClient service.BattleClient) (*gin.Engine, *repository.PgGamePlayerRepository) {
	gamePlayerRepo := repository.NewPgGamePlayerRepository(sharedPG.Pool)
	r, _ := buildPushEngine(battleClient, gamePlayerRepo, nil, nil)
	return r, gamePlayerRepo
}

// newTestPushRouter は buildPushRouter の前に DB を空にして、テストどうしを分離する。
func newTestPushRouter(t *testing.T, battleClient service.BattleClient) (*gin.Engine, *repository.PgGamePlayerRepository) {
	t.Helper()
	sharedPG.Truncate(t)
	return buildPushRouter(battleClient)
}

// doMatchMadePush は指定 payload を Pub/Sub push envelope に包んで push 受け口へ POST する。
func doMatchMadePush(t *testing.T, r *gin.Engine, payload []byte) *httptest.ResponseRecorder {
	t.Helper()
	encoded := base64.StdEncoding.EncodeToString(payload)
	body := `{"message":{"data":"` + encoded + `","messageId":"m1","attributes":{}},"subscription":"projects/p/subscriptions/match-made-gateway-sub"}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, matchMadePushPath, strings.NewReader(body)))
	return w
}

// findPlayerByFirebaseUID は WS 接続時の firebaseUID → playerID 解決を account の代役として担う。
func findPlayerByFirebaseUID(firebaseUID string) (int, any) {
	switch firebaseUID {
	case testPlayer1UID:
		return http.StatusOK, apiaccount.PlayerResponse{PlayerID: testPlayer1ID, FirebaseUID: firebaseUID}
	case testPlayer2UID:
		return http.StatusOK, apiaccount.PlayerResponse{PlayerID: testPlayer2ID, FirebaseUID: firebaseUID}
	default:
		return http.StatusNotFound, nil
	}
}

// notifyTestEnv は push 受け口と、成立通知の宛先となる 2 人の WS 接続を保持する。
type notifyTestEnv struct {
	router *gin.Engine
	p1Conn *websocket.Conn
	p2Conn *websocket.Conn
}

// newNotifyTestEnv は DB を空にしたうえで push 受け口と WS 接続の入口を同じ Manager に
// 載せ、両プレイヤーが接続済みの環境を返す。
func newNotifyTestEnv(t *testing.T, battleClient service.BattleClient, gamePlayerRepo port.GamePlayerRepo) *notifyTestEnv {
	t.Helper()
	sharedPG.Truncate(t)

	accountFake := apiaccountserverfake.NewServer()
	t.Cleanup(accountFake.Close)
	accountFake.FindByFirebaseUIDFn = findPlayerByFirebaseUID
	matchmakingFake := apimatchmakingserverfake.NewServer()
	t.Cleanup(matchmakingFake.Close)

	accountClient := accountclient.New(accountFake.URL(), &http.Client{})
	matchmakingClient := matchmakingclient.New(matchmakingFake.URL(), "test-instance", &http.Client{})

	r, wsManager := buildPushEngine(battleClient, gamePlayerRepo, accountClient, matchmakingClient)
	r.GET(wsPath, ws.NewHandler(wsManager, nil, accountClient, nil).HandleUpgrade)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &notifyTestEnv{
		router: r,
		p1Conn: dialPlayer(t, srv, testPlayer1UID),
		p2Conn: dialPlayer(t, srv, testPlayer2UID),
	}
}

// dialPlayer は uid のプレイヤーとして WS 接続を確立する。
func dialPlayer(t *testing.T, srv *httptest.Server, uid string) *websocket.Conn {
	t.Helper()
	url := strings.Replace(srv.URL, "http://", "ws://", 1) + wsPath + "?token=" + middleware.DevTokenPrefix + uid
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// readMatchFoundGameID は成立通知を 1 件受信し、通知された gameID を返す。
func readMatchFoundGameID(t *testing.T, conn *websocket.Conn) string {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(matchFoundWait)))
	_, raw, err := conn.ReadMessage()
	require.NoError(t, err)

	var msg ws.WSMessage
	require.NoError(t, json.Unmarshal(raw, &msg))
	require.Equal(t, genws.WSServerMsgMatchFound, msg.Type)
	var body ws.MatchFoundMessage
	require.NoError(t, json.Unmarshal(msg.Data, &body))
	return body.GameID
}

// requireNoNotification は noMatchFoundWait の間 WS に何も届かないことを確かめる。
func requireNoNotification(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(noMatchFoundWait)))
	_, raw, err := conn.ReadMessage()
	require.Error(t, err, "unexpected ws message: %s", raw)
	var netErr net.Error
	require.ErrorAs(t, err, &netErr)
	assert.True(t, netErr.Timeout(), "ws read must end by the read deadline, got %v", err)
}

func matchMadePayload(t *testing.T, matchID string) []byte {
	t.Helper()
	data, err := json.Marshal(apimatchmaking.MatchMadeEvent{
		EventType: apimatchmaking.EventTypeMatchMade,
		MatchID:   matchID,
		Players: []apimatchmaking.MatchedPlayer{
			{PlayerID: testPlayer1ID, DeckID: 1, Name: "p1", Level: 1},
			{PlayerID: testPlayer2ID, DeckID: 2, Name: "p2", Level: 1},
		},
	})
	require.NoError(t, err)
	return data
}

func TestPushMatchMadeE2E(t *testing.T) {
	t.Run("push 受け口経由の match_made 処理", func(t *testing.T) {
		t.Run("有効な push を投げると、200 を返し battle にゲーム作成を1回依頼し両プレイヤーの game_players 行が永続化される", func(t *testing.T) {
			bc := &fakeBattleClient{gameID: "01ARZ3NDEKTSV4RRFFQ69G5FAV"}
			r, gamePlayerRepo := newTestPushRouter(t, bc)
			payload := matchMadePayload(t, "mch_e2e_1")

			w := doMatchMadePush(t, r, payload)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, 1, bc.callCount())
			entries, err := gamePlayerRepo.LookupGamePlayers(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV")
			require.NoError(t, err)
			require.Len(t, entries, 2)
		})

		t.Run("同一 payload の push を2回投げても、battle へのゲーム作成依頼は1回のままで game_players 行は重複しない", func(t *testing.T) {
			bc := &fakeBattleClient{gameID: "01ARZ3NDEKTSV4RRFFQ69G5FA2"}
			r, gamePlayerRepo := newTestPushRouter(t, bc)
			payload := matchMadePayload(t, "mch_e2e_dup")

			w1 := doMatchMadePush(t, r, payload)
			require.Equal(t, http.StatusOK, w1.Code)
			w2 := doMatchMadePush(t, r, payload)

			assert.Equal(t, http.StatusOK, w2.Code)
			assert.Equal(t, 1, bc.callCount(), "duplicate push must not create a second battle game")
			entries, err := gamePlayerRepo.LookupGamePlayers(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FA2")
			require.NoError(t, err)
			assert.Len(t, entries, 2, "game_players rows must not be duplicated by the second push")
		})

		t.Run("プロセスの再起動をまたいでも、同一 matchId の push は battle へのゲーム作成依頼を1回のままに保つ", func(t *testing.T) {
			bc1 := &fakeBattleClient{gameID: "01ARZ3NDEKTSV4RRFFQ69G5FA3"}
			r1, _ := newTestPushRouter(t, bc1)
			payload := matchMadePayload(t, "mch_e2e_restart")
			w1 := doMatchMadePush(t, r1, payload)
			require.Equal(t, http.StatusOK, w1.Code)
			require.Equal(t, 1, bc1.callCount())

			// r1 とプロセス内状態を一切共有しない別の router / Manager / MatchSubscriber を
			// 同じ DB に対して新規に組み立て、プロセス再起動後の再送を再現する。
			bc2 := &fakeBattleClient{gameID: "should-not-be-used"}
			r2, gamePlayerRepo2 := buildPushRouter(bc2)

			w2 := doMatchMadePush(t, r2, payload)

			assert.Equal(t, http.StatusOK, w2.Code)
			assert.Equal(t, 0, bc2.callCount(), "a process-restarted instance must not re-create the battle game for an already-processed matchId")
			entries, err := gamePlayerRepo2.LookupGamePlayers(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FA3")
			require.NoError(t, err)
			assert.Len(t, entries, 2)
		})

		t.Run("battle のゲーム作成が失敗した push を再送すると、200 を返しゲーム作成に成功する", func(t *testing.T) {
			bc := &fakeBattleClient{failNextCalls: 1, gameID: "01ARZ3NDEKTSV4RRFFQ69G5FA5"}
			r, gamePlayerRepo := newTestPushRouter(t, bc)
			payload := matchMadePayload(t, "mch_e2e_recover")

			w1 := doMatchMadePush(t, r, payload)
			require.Equal(t, http.StatusInternalServerError, w1.Code)

			w2 := doMatchMadePush(t, r, payload)

			assert.Equal(t, http.StatusOK, w2.Code)
			assert.Equal(t, 2, bc.callCount(), "the retry after a battle failure must call battle again")
			entries, err := gamePlayerRepo.LookupGamePlayers(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FA5")
			require.NoError(t, err)
			assert.Len(t, entries, 2)
		})
	})
}

func TestPushMatchMadeNotification(t *testing.T) {
	t.Run("push 受け口経由の成立通知", func(t *testing.T) {
		t.Run("同じ成立イベントが二度届いても、成立通知は各プレイヤーに1回だけ送られる", func(t *testing.T) {
			bc := &fakeBattleClient{gameID: "01ARZ3NDEKTSV4RRFFQ69G5FB1"}
			env := newNotifyTestEnv(t, bc, repository.NewPgGamePlayerRepository(sharedPG.Pool))
			payload := matchMadePayload(t, "mch_e2e_notify_dup")

			require.Equal(t, http.StatusOK, doMatchMadePush(t, env.router, payload).Code)
			require.Equal(t, http.StatusOK, doMatchMadePush(t, env.router, payload).Code)

			assert.Equal(t, "01ARZ3NDEKTSV4RRFFQ69G5FB1", readMatchFoundGameID(t, env.p1Conn))
			assert.Equal(t, "01ARZ3NDEKTSV4RRFFQ69G5FB1", readMatchFoundGameID(t, env.p2Conn))
			requireNoNotification(t, env.p1Conn)
			requireNoNotification(t, env.p2Conn)
		})

		t.Run("対戦の記録に失敗したとき、成立通知は送られず push 応答が 500 になる", func(t *testing.T) {
			bc := &fakeBattleClient{gameID: "01ARZ3NDEKTSV4RRFFQ69G5FB2"}
			repo := &failingGamePlayerRepo{
				PgGamePlayerRepository: repository.NewPgGamePlayerRepository(sharedPG.Pool),
				failNextCalls:          1,
			}
			env := newNotifyTestEnv(t, bc, repo)
			payload := matchMadePayload(t, "mch_e2e_notify_fail")

			w := doMatchMadePush(t, env.router, payload)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			requireNoNotification(t, env.p1Conn)
			requireNoNotification(t, env.p2Conn)
		})

		t.Run("対戦の記録に失敗した成立イベントが再配信され記録に成功すると、そこで成立通知が送られる", func(t *testing.T) {
			bc := &fakeBattleClient{gameID: "01ARZ3NDEKTSV4RRFFQ69G5FB3"}
			repo := &failingGamePlayerRepo{
				PgGamePlayerRepository: repository.NewPgGamePlayerRepository(sharedPG.Pool),
				failNextCalls:          1,
			}
			env := newNotifyTestEnv(t, bc, repo)
			payload := matchMadePayload(t, "mch_e2e_notify_retry")
			require.Equal(t, http.StatusInternalServerError, doMatchMadePush(t, env.router, payload).Code)

			require.Equal(t, http.StatusOK, doMatchMadePush(t, env.router, payload).Code)

			assert.Equal(t, "01ARZ3NDEKTSV4RRFFQ69G5FB3", readMatchFoundGameID(t, env.p1Conn))
			assert.Equal(t, "01ARZ3NDEKTSV4RRFFQ69G5FB3", readMatchFoundGameID(t, env.p2Conn))
		})
	})
}

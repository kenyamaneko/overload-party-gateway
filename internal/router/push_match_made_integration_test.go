//go:build integration

package router_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apicard "github.com/kenyamaneko/overload-party-card/packages/api-card"
	apimatchmaking "github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking"

	pubsubadapter "github.com/kenyamaneko/overload-party-gateway/internal/adapter/pubsub"
	"github.com/kenyamaneko/overload-party-gateway/internal/handler/rest"
	ws "github.com/kenyamaneko/overload-party-gateway/internal/handler/ws"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository/postgrestest"
	"github.com/kenyamaneko/overload-party-gateway/internal/router"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

const matchMadePushPath = "/internal/v1/pubsub/match-made"

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

// buildPushRouter は「HTTP push → router → MatchSubscriber → Manager → repo → DB」を
// 実 DB まで直結した router を、既存の DB 状態を消さずに組み立てる。プロセス再起動を
// 模すテストで、r1 が残した永続状態を r2 が引き継ぐ形を再現するために使う。
func buildPushRouter(battleClient service.BattleClient) (*gin.Engine, *repository.PgGamePlayerRepository) {
	gamePlayerRepo := repository.NewPgGamePlayerRepository(sharedPG.Pool)
	processedMatchRepo := repository.NewPgProcessedMatchRepository(sharedPG.Pool)
	wsManager := ws.NewManager(battleClient, nil, &fakeCardClient{}, nil, gamePlayerRepo, processedMatchRepo, 0, nil, nil, ws.DefaultDisconnectTimeout)
	matchSub, err := pubsubadapter.NewMatchSubscriber(wsManager)
	if err != nil {
		panic(err)
	}
	pushHandler := rest.NewPubSubPushHandler(matchSub)
	handlers := &router.Handlers{PubSub: pushHandler}

	r := gin.New()
	internalGroup := r.Group("/internal/v1")
	router.RegisterPubSubRoutes(internalGroup, handlers)
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

func matchMadePayload(t *testing.T, matchID string) []byte {
	t.Helper()
	data, err := json.Marshal(apimatchmaking.MatchMadeEvent{
		EventType: apimatchmaking.EventTypeMatchMade,
		MatchID:   matchID,
		Players: []apimatchmaking.MatchedPlayer{
			{PlayerID: "11111111-1111-1111-1111-111111111111", DeckID: 1, Name: "p1", Level: 1},
			{PlayerID: "22222222-2222-2222-2222-222222222222", DeckID: 2, Name: "p2", Level: 1},
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

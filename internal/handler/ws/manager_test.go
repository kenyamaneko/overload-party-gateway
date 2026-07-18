package ws

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apicard "github.com/kenyamaneko/overload-party-card/packages/api-card"
	apimatchmaking "github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	"github.com/kenyamaneko/overload-party-account/packages/api-account/apiaccountserverfake"

	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

// mockCardClient は port.CardClient のテスト用実装。deckID ごとにデッキ内容/施策 ID/エラーを差し替えられる。
type mockCardClient struct {
	deckCards       map[int64][]apicard.DeckCard
	deckInitiatives map[int64]port.DeckInitiatives
	getDeckCardsErr map[int64]error
}

func (m *mockCardClient) ListAllCards(_ context.Context) ([]*apicard.CardDefinition, error) {
	return nil, nil
}

func (m *mockCardClient) GetDeckCards(_ context.Context, deckID int64) ([]apicard.DeckCard, port.DeckInitiatives, error) {
	if err, ok := m.getDeckCardsErr[deckID]; ok {
		return nil, port.DeckInitiatives{}, err
	}
	return m.deckCards[deckID], m.deckInitiatives[deckID], nil
}

func (m *mockCardClient) ValidateDeckForBattle(_ context.Context, _ int64) error {
	return nil
}

// newTestManager は HandleMatchMade / checkAndIncrementBattleLimit のテスト用に依存を最小構成した Manager を返す。
func newTestManager(bc *mockBattleClient, cardClient port.CardClient, gamePlayerRepo port.GamePlayerRepo) *Manager {
	relay, _ := newTestRelay()
	return &Manager{
		battleClient:   bc,
		cardClient:     cardClient,
		gamePlayerRepo: gamePlayerRepo,
		Relay:          relay,
	}
}

func TestHandleMatchMade(t *testing.T) {
	t.Run("マッチ成立イベントからのゲーム作成", func(t *testing.T) {
		participantCountCases := []struct {
			name    string
			players []apimatchmaking.MatchedPlayer
		}{
			{
				name:    "参加者が 0 人のマッチ成立イベントのとき、ゲームを作成せずエラーを返す",
				players: nil,
			},
			{
				name: "参加者が 1 人のとき、ゲームを作成せずエラーを返す",
				players: []apimatchmaking.MatchedPlayer{
					{PlayerID: "TST-P1", DeckID: 11},
				},
			},
			{
				name: "参加者が 3 人のとき、ゲームを作成せずエラーを返す",
				players: []apimatchmaking.MatchedPlayer{
					{PlayerID: "TST-P1", DeckID: 11},
					{PlayerID: "TST-P2", DeckID: 22},
					{PlayerID: "TST-P3", DeckID: 33},
				},
			},
		}
		for _, tc := range participantCountCases {
			t.Run(tc.name, func(t *testing.T) {
				bc := newMockBattleClient()
				mgr := newTestManager(bc, &mockCardClient{}, repository.NewMockGamePlayerRepository())

				err := mgr.HandleMatchMade(context.Background(), apimatchmaking.MatchMadeEvent{Players: tc.players})

				require.Error(t, err)
				assert.Equal(t, 0, bc.createPvPGameCalls)
			})
		}

		t.Run("参加者が 2 人のとき、battle に PvP ゲーム作成を依頼し、両者のスロットが保存される", func(t *testing.T) {
			bc := newMockBattleClient()
			bc.createPvPGameResult = &service.GameCreatedResult{GameID: "TST-G1"}
			cardClient := &mockCardClient{
				deckCards: map[int64][]apicard.DeckCard{
					11: {{CardID: "TST-0001", ArtNo: 1, Count: 1}},
					22: {{CardID: "TST-0002", ArtNo: 1, Count: 1}},
				},
			}
			repo := repository.NewMockGamePlayerRepository()
			mgr := newTestManager(bc, cardClient, repo)
			event := apimatchmaking.MatchMadeEvent{Players: []apimatchmaking.MatchedPlayer{
				{PlayerID: "TST-P1", DeckID: 11},
				{PlayerID: "TST-P2", DeckID: 22},
			}}

			err := mgr.HandleMatchMade(context.Background(), event)

			require.NoError(t, err)
			assert.Equal(t, 1, bc.createPvPGameCalls)
			assert.Len(t, bc.createPvPGameArgs.deck1Cards, 1)
			assert.Len(t, bc.createPvPGameArgs.deck2Cards, 1)
			num1, err := repo.LookupPlayerNum(context.Background(), "TST-G1", "TST-P1")
			require.NoError(t, err)
			assert.Equal(t, 1, num1)
			num2, err := repo.LookupPlayerNum(context.Background(), "TST-G1", "TST-P2")
			require.NoError(t, err)
			assert.Equal(t, 2, num2)
		})

		t.Run("参加者の名前とレベルが食い違うとき、作成依頼のサマリにそれぞれの値が別々に載る", func(t *testing.T) {
			bc := newMockBattleClient()
			bc.createPvPGameResult = &service.GameCreatedResult{GameID: "TST-G2"}
			cardClient := &mockCardClient{
				deckCards: map[int64][]apicard.DeckCard{11: {}, 22: {}},
			}
			mgr := newTestManager(bc, cardClient, repository.NewMockGamePlayerRepository())
			event := apimatchmaking.MatchMadeEvent{Players: []apimatchmaking.MatchedPlayer{
				{PlayerID: "TST-P1", DeckID: 11, Name: "TST-A", Level: 5},
				{PlayerID: "TST-P2", DeckID: 22, Name: "TST-B", Level: 9},
			}}

			err := mgr.HandleMatchMade(context.Background(), event)

			require.NoError(t, err)
			wantLevel1, wantLevel2 := int64(5), int64(9)
			assert.Equal(t, service.PlayerSummaryRequest{Name: "TST-A", Level: &wantLevel1}, bc.createPvPGameArgs.player1Summary)
			assert.Equal(t, service.PlayerSummaryRequest{Name: "TST-B", Level: &wantLevel2}, bc.createPvPGameArgs.player2Summary)
		})

		t.Run("片方のデッキ解決が失敗するとき、ゲームを作成せずエラーを返す", func(t *testing.T) {
			bc := newMockBattleClient()
			cardClient := &mockCardClient{
				deckCards:       map[int64][]apicard.DeckCard{11: {}},
				getDeckCardsErr: map[int64]error{22: errFake},
			}
			repo := repository.NewMockGamePlayerRepository()
			mgr := newTestManager(bc, cardClient, repo)
			event := apimatchmaking.MatchMadeEvent{Players: []apimatchmaking.MatchedPlayer{
				{PlayerID: "TST-P1", DeckID: 11},
				{PlayerID: "TST-P2", DeckID: 22},
			}}

			err := mgr.HandleMatchMade(context.Background(), event)

			require.Error(t, err)
			assert.Equal(t, 0, bc.createPvPGameCalls)
			entries, err := repo.LookupGamePlayers(context.Background(), "TST-G3")
			require.NoError(t, err)
			assert.Empty(t, entries)
		})

		t.Run("ゲーム作成が失敗するとき、スロットを保存せずエラーを返す", func(t *testing.T) {
			bc := newMockBattleClient()
			bc.createPvPGameErr = errFake
			cardClient := &mockCardClient{
				deckCards: map[int64][]apicard.DeckCard{11: {}, 22: {}},
			}
			repo := repository.NewMockGamePlayerRepository()
			mgr := newTestManager(bc, cardClient, repo)
			event := apimatchmaking.MatchMadeEvent{Players: []apimatchmaking.MatchedPlayer{
				{PlayerID: "TST-P1", DeckID: 11},
				{PlayerID: "TST-P2", DeckID: 22},
			}}

			err := mgr.HandleMatchMade(context.Background(), event)

			require.Error(t, err)
			entries, err := repo.LookupGamePlayers(context.Background(), "TST-G4")
			require.NoError(t, err)
			assert.Empty(t, entries)
		})
	})
}

func TestResolveDeckCards(t *testing.T) {
	t.Run("デッキのバトル転送形式への展開", func(t *testing.T) {
		fixedP2Deck := []apicard.DeckCard{{CardID: "TST-FIXED", ArtNo: 1, Count: 1}}
		tests := []struct {
			name            string
			p1Deck          []apicard.DeckCard
			p1Initiatives   port.DeckInitiatives
			wantCards       []service.BattleDeckCard
			wantInitiatives service.DeckInitiatives
		}{
			{
				name:      "デッキが空のとき、0 枚で依頼される",
				p1Deck:    []apicard.DeckCard{},
				wantCards: []service.BattleDeckCard{},
			},
			{
				name:      "1 種で枚数 1 のとき、その 1 枚だけになる",
				p1Deck:    []apicard.DeckCard{{CardID: "TST-0001", ArtNo: 2, Count: 1}},
				wantCards: []service.BattleDeckCard{{CardID: "TST-0001", ArtNo: 2}},
			},
			{
				name:   "1 種で枚数 3 のとき、同じカードが 3 枚に複製される",
				p1Deck: []apicard.DeckCard{{CardID: "TST-0001", ArtNo: 1, Count: 3}},
				wantCards: []service.BattleDeckCard{
					{CardID: "TST-0001", ArtNo: 1},
					{CardID: "TST-0001", ArtNo: 1},
					{CardID: "TST-0001", ArtNo: 1},
				},
			},
			{
				name: "複数種のとき、宣言順に枚数分ずつ展開される",
				p1Deck: []apicard.DeckCard{
					{CardID: "TST-0001", ArtNo: 1, Count: 2},
					{CardID: "TST-0002", ArtNo: 1, Count: 1},
				},
				wantCards: []service.BattleDeckCard{
					{CardID: "TST-0001", ArtNo: 1},
					{CardID: "TST-0001", ArtNo: 1},
					{CardID: "TST-0002", ArtNo: 1},
				},
			},
			{
				name:            "デッキの施策 ID が依頼に載る",
				p1Deck:          []apicard.DeckCard{},
				p1Initiatives:   port.DeckInitiatives{RoutineID: "RTN-TST01", SpecialID: "SPC-TST01"},
				wantCards:       []service.BattleDeckCard{},
				wantInitiatives: service.DeckInitiatives{RoutineID: "RTN-TST01", SpecialID: "SPC-TST01"},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				bc := newMockBattleClient()
				bc.createPvPGameResult = &service.GameCreatedResult{GameID: "TST-G-DECK"}
				cardClient := &mockCardClient{
					deckCards:       map[int64][]apicard.DeckCard{11: tt.p1Deck, 22: fixedP2Deck},
					deckInitiatives: map[int64]port.DeckInitiatives{11: tt.p1Initiatives},
				}
				mgr := newTestManager(bc, cardClient, repository.NewMockGamePlayerRepository())
				event := apimatchmaking.MatchMadeEvent{Players: []apimatchmaking.MatchedPlayer{
					{PlayerID: "TST-P1", DeckID: 11},
					{PlayerID: "TST-P2", DeckID: 22},
				}}

				err := mgr.HandleMatchMade(context.Background(), event)

				require.NoError(t, err)
				assert.Equal(t, tt.wantCards, bc.createPvPGameArgs.deck1Cards)
				assert.Equal(t, tt.wantInitiatives, bc.createPvPGameArgs.deck1Initiatives)
			})
		}
	})
}

func TestCheckAndIncrementBattleLimit(t *testing.T) {
	t.Run("バトル回数制限の確認と加算", func(t *testing.T) {
		tests := []struct {
			name              string
			limitStatus       int
			canBattle         bool
			incrementStatus   int
			wantIncrementHits int32
			wantMsg           string
			wantErr           bool
		}{
			{
				name:              "残回数がある (CanBattle=true) とき、バトル回数の加算が 1 回だけ行われ、制限メッセージは返らない",
				limitStatus:       http.StatusOK,
				canBattle:         true,
				incrementStatus:   http.StatusNoContent,
				wantIncrementHits: 1,
			},
			{
				name:        "上限到達 (CanBattle=false) のとき、加算されず「daily battle limit reached」を返す",
				limitStatus: http.StatusOK,
				canBattle:   false,
				wantMsg:     "daily battle limit reached",
			},
			{
				name:        "制限情報の取得が 500 のとき、加算されずエラーになる",
				limitStatus: http.StatusInternalServerError,
				wantErr:     true,
			},
			{
				name:              "加算が 500 のとき、エラーになる",
				limitStatus:       http.StatusOK,
				canBattle:         true,
				incrementStatus:   http.StatusInternalServerError,
				wantIncrementHits: 1,
				wantErr:           true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var limitHits, incrementHits atomic.Int32
				fake := apiaccountserverfake.NewServer()
				t.Cleanup(fake.Close)
				fake.GetBattleLimitFn = func() (int, any) {
					limitHits.Add(1)
					return tt.limitStatus, apiaccount.BattleLimitResponse{CanBattle: tt.canBattle}
				}
				fake.IncrementBattleCountFn = func() (int, any) {
					incrementHits.Add(1)
					return tt.incrementStatus, nil
				}
				mgr := &Manager{accountClient: accountclient.New(fake.URL())}

				msg, err := mgr.checkAndIncrementBattleLimit(context.Background())

				assert.Equal(t, int32(1), limitHits.Load())
				assert.Equal(t, tt.wantIncrementHits, incrementHits.Load())
				assert.Equal(t, tt.wantMsg, msg)
				assert.Equal(t, tt.wantErr, err != nil)
			})
		}
	})
}

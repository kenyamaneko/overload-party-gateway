package ws

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	"github.com/kenyamaneko/overload-party-account/packages/api-account/apiaccountserverfake"
	gamelogic "github.com/kenyamaneko/overload-party-battle/packages/game-logic-constants-go"
	apicard "github.com/kenyamaneko/overload-party-card/packages/api-card"
	gamedesign "github.com/kenyamaneko/overload-party-common/packages/game-design-constants"
	apimatchmaking "github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

// fakeCardClient は port.CardClient のテスト用実装。deckID ごとにデッキ内容・施策・
// エラーを差し替えられる。ゼロ値は空デッキを返すだけなので、デッキ内容を問わない
// テストはゼロ値のまま使える。
type fakeCardClient struct {
	deckCards       map[int64][]apicard.DeckCard
	deckInitiatives map[int64]port.DeckInitiatives
	getDeckCardsErr map[int64]error
}

func (f *fakeCardClient) GetDeckCards(_ context.Context, deckID int64) ([]apicard.DeckCard, port.DeckInitiatives, error) {
	if err, ok := f.getDeckCardsErr[deckID]; ok {
		return nil, port.DeckInitiatives{}, err
	}
	return f.deckCards[deckID], f.deckInitiatives[deckID], nil
}

func (f *fakeCardClient) ValidateDeckForBattle(context.Context, int64) error {
	return nil
}

var _ port.CardClient = (*fakeCardClient)(nil)

// recordGameCreatedCall は fakeProcessedMatchRepo.RecordGameCreated への 1 回の呼出を記録する。
type recordGameCreatedCall struct {
	matchID string
	gameID  string
}

// fakeProcessedMatchRepo は gateway.processed_matches の挙動をメモリ上で再現する
// port.ProcessedMatchRepo のテスト用実装。プロセスをまたいだ永続化を、複数の
// Manager インスタンスに同一のインスタンスを共有させることで模倣する。
type fakeProcessedMatchRepo struct {
	mu sync.Mutex

	claimed  map[string]bool
	gameIDs  map[string]string
	notified map[string]bool

	claimCalls   []string
	releaseCalls []string
	recordCalls  []recordGameCreatedCall

	claimErr     error
	releaseErr   error
	recordErr    error
	gameIDForErr error
}

func newFakeProcessedMatchRepo() *fakeProcessedMatchRepo {
	return &fakeProcessedMatchRepo{
		claimed:  make(map[string]bool),
		gameIDs:  make(map[string]string),
		notified: make(map[string]bool),
	}
}

func (f *fakeProcessedMatchRepo) Claim(_ context.Context, matchID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claimCalls = append(f.claimCalls, matchID)
	if f.claimErr != nil {
		return false, f.claimErr
	}
	if f.claimed[matchID] {
		return false, nil
	}
	f.claimed[matchID] = true
	return true, nil
}

func (f *fakeProcessedMatchRepo) Release(_ context.Context, matchID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releaseCalls = append(f.releaseCalls, matchID)
	if f.releaseErr != nil {
		return f.releaseErr
	}
	delete(f.claimed, matchID)
	return nil
}

func (f *fakeProcessedMatchRepo) RecordGameCreated(_ context.Context, matchID, gameID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordCalls = append(f.recordCalls, recordGameCreatedCall{matchID, gameID})
	if f.recordErr != nil {
		return f.recordErr
	}
	f.gameIDs[matchID] = gameID
	return nil
}

func (f *fakeProcessedMatchRepo) GameIDFor(_ context.Context, matchID string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gameIDForErr != nil {
		return "", false, f.gameIDForErr
	}
	gameID, found := f.gameIDs[matchID]
	return gameID, found, nil
}

func (f *fakeProcessedMatchRepo) MarkNotified(_ context.Context, matchID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.claimed[matchID] || f.notified[matchID] {
		return false, nil
	}
	f.notified[matchID] = true
	return true, nil
}

func (f *fakeProcessedMatchRepo) snapshotReleaseCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.releaseCalls...)
}

var _ port.ProcessedMatchRepo = (*fakeProcessedMatchRepo)(nil)

// newTestManagerForMatchMade は HandleMatchMade の永続 dedup を検証するための
// Manager を返す。WS 経路 (accountClient / internalSigner) は HandleMatchMade から
// 参照されないため nil のままにする。
func newTestManagerForMatchMade(bc *mockBattleClient, gamePlayerRepo port.GamePlayerRepo, dedupRepo port.ProcessedMatchRepo) *Manager {
	return newTestManagerWithCards(bc, &fakeCardClient{}, gamePlayerRepo, dedupRepo)
}

// newTestManagerWithCards はデッキ解決の内容までを検証するテスト向けに、card クライアントを
// 差し替えられる Manager を返す。
func newTestManagerWithCards(bc *mockBattleClient, cardClient port.CardClient, gamePlayerRepo port.GamePlayerRepo, dedupRepo port.ProcessedMatchRepo) *Manager {
	return NewManager(bc, nil, cardClient, noopMatchmakingClient{}, gamePlayerRepo, dedupRepo, newFakeInvalidatedGameRepo(), 0, nil, nil, DefaultDisconnectTimeout)
}

func matchMadeEvent(matchID string) apimatchmaking.MatchMadeEvent {
	return apimatchmaking.MatchMadeEvent{
		MatchID: matchID,
		Players: []apimatchmaking.MatchedPlayer{
			{PlayerID: "p1", DeckID: 10},
			{PlayerID: "p2", DeckID: 20},
		},
	}
}

func TestHandleMatchMade(t *testing.T) {
	t.Run("[マッチング]match_madeイベントの永続dedup", func(t *testing.T) {
		t.Run("有効なイベントのとき、battleにゲーム作成を1回依頼し両プレイヤーのgame_players行を挿入する", func(t *testing.T) {
			bc := newMockBattleClient()
			bc.createPvPGameResult = &service.GameCreatedResult{GameID: "g1"}
			gamePlayerRepo := &mockGamePlayerRepo{}
			dedupRepo := newFakeProcessedMatchRepo()
			m := newTestManagerForMatchMade(bc, gamePlayerRepo, dedupRepo)

			err := m.HandleMatchMade(context.Background(), matchMadeEvent("mch_1"))

			require.NoError(t, err)
			require.Len(t, bc.snapshotCreatePvPGameCalls(), 1)
			calls := gamePlayerRepo.snapshotInsertGamePlayerCalls()
			require.Len(t, calls, 2)
			assert.Equal(t, insertGamePlayerCall{gameID: "g1", playerNum: 1, playerID: "p1"}, calls[0])
			assert.Equal(t, insertGamePlayerCall{gameID: "g1", playerNum: 2, playerID: "p2"}, calls[1])
		})

		t.Run("同一matchIdのイベントが新しいManagerインスタンスへ再送されるとき、battleへのゲーム作成依頼は1回のまま増えない", func(t *testing.T) {
			dedupRepo := newFakeProcessedMatchRepo()

			bc1 := newMockBattleClient()
			bc1.createPvPGameResult = &service.GameCreatedResult{GameID: "g1"}
			gamePlayerRepo1 := &mockGamePlayerRepo{}
			m1 := newTestManagerForMatchMade(bc1, gamePlayerRepo1, dedupRepo)
			require.NoError(t, m1.HandleMatchMade(context.Background(), matchMadeEvent("mch_restart")))
			require.Len(t, bc1.snapshotCreatePvPGameCalls(), 1)

			// プロセス再起動を模して、プロセス内状態を持たない別の Manager インスタンスへ
			// 同じ matchId のイベントを渡す。共有するのは永続 dedup リポジトリのみ。
			bc2 := newMockBattleClient()
			gamePlayerRepo2 := &mockGamePlayerRepo{}
			m2 := newTestManagerForMatchMade(bc2, gamePlayerRepo2, dedupRepo)

			err := m2.HandleMatchMade(context.Background(), matchMadeEvent("mch_restart"))

			require.NoError(t, err)
			assert.Empty(t, bc2.snapshotCreatePvPGameCalls(), "battle must not be asked to create a second game for the same matchId")
			calls := gamePlayerRepo2.snapshotInsertGamePlayerCalls()
			require.Len(t, calls, 2, "game_players rows must still be (re)inserted using the previously recorded game")
			assert.Equal(t, "g1", calls[0].gameID)
			assert.Equal(t, "g1", calls[1].gameID)
		})

		t.Run("matchIdがclaim済みでbattle側の結果がまだ記録されていないとき、ゲーム作成を行わず処理をスキップする", func(t *testing.T) {
			dedupRepo := newFakeProcessedMatchRepo()
			claimed, err := dedupRepo.Claim(context.Background(), "mch_inflight")
			require.NoError(t, err)
			require.True(t, claimed)

			bc := newMockBattleClient()
			gamePlayerRepo := &mockGamePlayerRepo{}
			m := newTestManagerForMatchMade(bc, gamePlayerRepo, dedupRepo)

			err = m.HandleMatchMade(context.Background(), matchMadeEvent("mch_inflight"))

			require.NoError(t, err)
			assert.Empty(t, bc.snapshotCreatePvPGameCalls())
			assert.Empty(t, gamePlayerRepo.snapshotInsertGamePlayerCalls())
		})

		t.Run("battleのゲーム作成に失敗したmatchIdが再送されると、claimが解放されており再度ゲーム作成を試みて成功する", func(t *testing.T) {
			dedupRepo := newFakeProcessedMatchRepo()
			bc := newMockBattleClient()
			bc.createPvPGameQueue = []createPvPGameResponse{
				{err: errors.New("battle unavailable")},
				{result: &service.GameCreatedResult{GameID: "g2"}},
			}
			gamePlayerRepo := &mockGamePlayerRepo{}
			m := newTestManagerForMatchMade(bc, gamePlayerRepo, dedupRepo)
			event := matchMadeEvent("mch_retry")

			firstErr := m.HandleMatchMade(context.Background(), event)
			require.Error(t, firstErr)
			secondErr := m.HandleMatchMade(context.Background(), event)
			require.NoError(t, secondErr)

			require.Len(t, bc.snapshotCreatePvPGameCalls(), 2, "the retry after a battle failure must call battle again")
			calls := gamePlayerRepo.snapshotInsertGamePlayerCalls()
			require.Len(t, calls, 2)
			assert.Equal(t, "g2", calls[0].gameID)
		})

		t.Run("battleのゲーム作成成功後にdedup記録が失敗するとき、claimを解放せずエラーを返す", func(t *testing.T) {
			dedupRepo := newFakeProcessedMatchRepo()
			dedupRepo.recordErr = errors.New("db down")
			bc := newMockBattleClient()
			bc.createPvPGameResult = &service.GameCreatedResult{GameID: "g3"}
			gamePlayerRepo := &mockGamePlayerRepo{}
			m := newTestManagerForMatchMade(bc, gamePlayerRepo, dedupRepo)

			err := m.HandleMatchMade(context.Background(), matchMadeEvent("mch_record_fail"))

			require.Error(t, err)
			assert.Empty(t, dedupRepo.snapshotReleaseCalls(), "must not release the claim once battle has already created a game")
			assert.Empty(t, gamePlayerRepo.snapshotInsertGamePlayerCalls())
		})

		t.Run("dedup記録に失敗したmatchIdが再送されると、battleを再度呼び出さず処理をスキップする", func(t *testing.T) {
			dedupRepo := newFakeProcessedMatchRepo()
			dedupRepo.recordErr = errors.New("db down")
			bc := newMockBattleClient()
			bc.createPvPGameResult = &service.GameCreatedResult{GameID: "g4"}
			gamePlayerRepo := &mockGamePlayerRepo{}
			m := newTestManagerForMatchMade(bc, gamePlayerRepo, dedupRepo)
			event := matchMadeEvent("mch_stuck")
			require.Error(t, m.HandleMatchMade(context.Background(), event))

			dedupRepo.recordErr = nil
			err := m.HandleMatchMade(context.Background(), event)

			require.NoError(t, err)
			assert.Len(t, bc.snapshotCreatePvPGameCalls(), 1, "battle must not be called again once a claim exists without a recorded game")
			assert.Empty(t, gamePlayerRepo.snapshotInsertGamePlayerCalls())
		})

		t.Run("claim自体が失敗するとき、エラーを返しbattleを呼び出さない", func(t *testing.T) {
			dedupRepo := newFakeProcessedMatchRepo()
			dedupRepo.claimErr = errors.New("db down")
			bc := newMockBattleClient()
			gamePlayerRepo := &mockGamePlayerRepo{}
			m := newTestManagerForMatchMade(bc, gamePlayerRepo, dedupRepo)

			err := m.HandleMatchMade(context.Background(), matchMadeEvent("mch_claim_fail"))

			require.Error(t, err)
			assert.Empty(t, bc.snapshotCreatePvPGameCalls())
		})

		t.Run("game_players挿入に失敗するとき、エラーを返す", func(t *testing.T) {
			dedupRepo := newFakeProcessedMatchRepo()
			bc := newMockBattleClient()
			bc.createPvPGameResult = &service.GameCreatedResult{GameID: "g5"}
			gamePlayerRepo := &mockGamePlayerRepo{insertErr: errors.New("db down"), insertErrForPlayerNum: 1}
			m := newTestManagerForMatchMade(bc, gamePlayerRepo, dedupRepo)

			err := m.HandleMatchMade(context.Background(), matchMadeEvent("mch_insert_fail"))

			require.Error(t, err)
			require.Len(t, bc.snapshotCreatePvPGameCalls(), 1)
		})

		t.Run("game_players挿入に失敗したmatchIdが再送されると、battleを再度呼び出さず挿入だけがやり直される", func(t *testing.T) {
			dedupRepo := newFakeProcessedMatchRepo()
			bc := newMockBattleClient()
			bc.createPvPGameResult = &service.GameCreatedResult{GameID: "g6"}
			failingRepo := &mockGamePlayerRepo{insertErr: errors.New("db down"), insertErrForPlayerNum: 1}
			m1 := newTestManagerForMatchMade(bc, failingRepo, dedupRepo)
			event := matchMadeEvent("mch_insert_retry")
			require.Error(t, m1.HandleMatchMade(context.Background(), event))

			// 別インスタンスへの再送を模す。今度は挿入が成功する repo を使う。
			bc2 := newMockBattleClient()
			recoveredRepo := &mockGamePlayerRepo{}
			m2 := newTestManagerForMatchMade(bc2, recoveredRepo, dedupRepo)

			err := m2.HandleMatchMade(context.Background(), event)

			require.NoError(t, err)
			assert.Empty(t, bc2.snapshotCreatePvPGameCalls(), "battle must not be re-invoked when only game_players insertion had failed")
			calls := recoveredRepo.snapshotInsertGamePlayerCalls()
			require.Len(t, calls, 2)
			assert.Equal(t, "g6", calls[0].gameID)
			assert.Equal(t, "g6", calls[1].gameID)
		})
	})

	t.Run("[マッチング]マッチ成立イベントからのゲーム作成", func(t *testing.T) {
		participantCountCases := []struct {
			name    string
			players []apimatchmaking.MatchedPlayer
		}{
			{
				name:    "参加者が0人のとき、ゲームを作成せずエラーを返す",
				players: nil,
			},
			{
				name: "参加者が1人のとき、ゲームを作成せずエラーを返す",
				players: []apimatchmaking.MatchedPlayer{
					{PlayerID: "TST-P1", DeckID: 11},
				},
			},
			{
				name: "参加者が3人のとき、ゲームを作成せずエラーを返す",
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
				m := newTestManagerForMatchMade(bc, &mockGamePlayerRepo{}, newFakeProcessedMatchRepo())

				err := m.HandleMatchMade(context.Background(), apimatchmaking.MatchMadeEvent{
					MatchID: "mch_participants", Players: tc.players,
				})

				require.Error(t, err)
				assert.Empty(t, bc.snapshotCreatePvPGameCalls())
			})
		}

		t.Run("両者のデッキが違うとき、それぞれの作成依頼のカード一覧が入れ替わらず届く", func(t *testing.T) {
			bc := newMockBattleClient()
			bc.createPvPGameResult = &service.GameCreatedResult{GameID: "g_decks"}
			cardClient := &fakeCardClient{
				deckCards: map[int64][]apicard.DeckCard{
					10: legalDeck("TST-P1"),
					20: legalDeck("TST-P2"),
				},
			}
			m := newTestManagerWithCards(bc, cardClient, &mockGamePlayerRepo{}, newFakeProcessedMatchRepo())

			err := m.HandleMatchMade(context.Background(), matchMadeEvent("mch_decks"))

			require.NoError(t, err)
			calls := bc.snapshotCreatePvPGameCalls()
			require.Len(t, calls, 1)
			assert.True(t, allCardIDsHavePrefix(calls[0].deck1Cards, "TST-P1"), "p1のデッキ内容がdeck1側に届く")
			assert.True(t, allCardIDsHavePrefix(calls[0].deck2Cards, "TST-P2"), "p2のデッキ内容がdeck2側に届く")
		})

		t.Run("参加者の名前とレベルが食い違うとき、作成依頼のサマリにそれぞれの値が別々に載る", func(t *testing.T) {
			bc := newMockBattleClient()
			bc.createPvPGameResult = &service.GameCreatedResult{GameID: "g_summary"}
			m := newTestManagerForMatchMade(bc, &mockGamePlayerRepo{}, newFakeProcessedMatchRepo())
			event := apimatchmaking.MatchMadeEvent{
				MatchID: "mch_summary",
				Players: []apimatchmaking.MatchedPlayer{
					{PlayerID: "TST-P1", DeckID: 11, Name: "TST-A", Level: 5},
					{PlayerID: "TST-P2", DeckID: 22, Name: "TST-B", Level: 9},
				},
			}

			err := m.HandleMatchMade(context.Background(), event)

			require.NoError(t, err)
			calls := bc.snapshotCreatePvPGameCalls()
			require.Len(t, calls, 1)
			wantLevel1, wantLevel2 := int64(5), int64(9)
			assert.Equal(t, service.PlayerSummaryRequest{Name: "TST-A", Level: &wantLevel1}, calls[0].player1Summary)
			assert.Equal(t, service.PlayerSummaryRequest{Name: "TST-B", Level: &wantLevel2}, calls[0].player2Summary)
		})

		t.Run("片方のデッキ解決が失敗するとき、ゲームを作成せずエラーを返す", func(t *testing.T) {
			bc := newMockBattleClient()
			cardClient := &fakeCardClient{
				getDeckCardsErr: map[int64]error{20: errors.New("card service down")},
			}
			gamePlayerRepo := &mockGamePlayerRepo{}
			m := newTestManagerWithCards(bc, cardClient, gamePlayerRepo, newFakeProcessedMatchRepo())

			err := m.HandleMatchMade(context.Background(), matchMadeEvent("mch_deck_fail"))

			require.Error(t, err)
			assert.Empty(t, bc.snapshotCreatePvPGameCalls())
			assert.Empty(t, gamePlayerRepo.snapshotInsertGamePlayerCalls())
		})

		t.Run("ゲーム作成が失敗するとき、プレイヤーをゲームに登録せずエラーを返す", func(t *testing.T) {
			bc := newMockBattleClient()
			bc.createPvPGameErr = errors.New("battle create failed")
			gamePlayerRepo := &mockGamePlayerRepo{}
			m := newTestManagerForMatchMade(bc, gamePlayerRepo, newFakeProcessedMatchRepo())

			err := m.HandleMatchMade(context.Background(), matchMadeEvent("mch_create_fail"))

			require.Error(t, err)
			require.Len(t, bc.snapshotCreatePvPGameCalls(), 1, "battle must still have been asked to create the game")
			assert.Empty(t, gamePlayerRepo.snapshotInsertGamePlayerCalls())
		})
	})

	t.Run("[マッチング]デッキのバトル転送形式への展開", func(t *testing.T) {
		t.Run("デッキに複数枚積んだカードは、その枚数分だけ作成依頼のカード一覧に並ぶ", func(t *testing.T) {
			bc := newMockBattleClient()
			bc.createPvPGameResult = &service.GameCreatedResult{GameID: "g_expand"}
			cardClient := &fakeCardClient{
				deckCards: map[int64][]apicard.DeckCard{
					10: legalDeck("TST-P1"),
					20: legalDeck("TST-P2"),
				},
			}
			m := newTestManagerWithCards(bc, cardClient, &mockGamePlayerRepo{}, newFakeProcessedMatchRepo())

			err := m.HandleMatchMade(context.Background(), matchMadeEvent("mch_expand"))

			require.NoError(t, err)
			calls := bc.snapshotCreatePvPGameCalls()
			require.Len(t, calls, 1)
			assert.Len(t, calls[0].deck1Cards, gamedesign.DeckSize)
			assert.Equal(t, gamedesign.RestrictionCopyCount[gamedesign.RestrictionUnlimited], countCardID(calls[0].deck1Cards, "TST-P1-0001"))
		})
	})
}

func legalDeck(cardIDPrefix string) []apicard.DeckCard {
	maxCopies := gamedesign.RestrictionCopyCount[gamedesign.RestrictionUnlimited]
	remaining := gamedesign.DeckSize
	var deck []apicard.DeckCard
	for i := 1; remaining > 0; i++ {
		count := maxCopies
		if count > remaining {
			count = remaining
		}
		deck = append(deck, apicard.DeckCard{CardID: fmt.Sprintf("%s-%04d", cardIDPrefix, i), ArtNo: 1, Count: count})
		remaining -= count
	}
	return deck
}

func countCardID(cards []service.BattleDeckCard, cardID string) int {
	n := 0
	for _, c := range cards {
		if c.CardID == cardID {
			n++
		}
	}
	return n
}

func allCardIDsHavePrefix(cards []service.BattleDeckCard, prefix string) bool {
	for _, c := range cards {
		if !strings.HasPrefix(c.CardID, prefix) {
			return false
		}
	}
	return true
}

// noopMatchmakingClient は port.MatchmakingClient のテスト用実装。matchmaking への
// 呼び出し内容を検証しない経路の配線先として使う。
type noopMatchmakingClient struct{}

func (noopMatchmakingClient) Enqueue(_ context.Context, _ int64, _ string, _ int64) error { return nil }
func (noopMatchmakingClient) Cancel(_ context.Context) error                              { return nil }
func (noopMatchmakingClient) ReportMatchAbandoned(_ context.Context, _ string, _ []string) error {
	return nil
}

var _ port.MatchmakingClient = noopMatchmakingClient{}

func TestManagerReconnect(t *testing.T) {
	t.Run("[切断・再接続]切断猶予切れ状態での復帰時の決着評価", func(t *testing.T) {
		t.Run("対戦相手の猶予切れのまま再接続すると、対戦相手のforfeitが実行される", func(t *testing.T) {
			bc := newMockBattleClient()
			bc.processActionResult = &service.ActionResult{}
			repo := &mockGamePlayerRepo{lookupEntries: []port.GamePlayerEntry{
				{PlayerNum: 1, PlayerID: "p1"},
				{PlayerNum: 2, PlayerID: "p2"},
			}}
			// 最初の接続時点では猶予期限のバックアップがまだ無い (found=false)。Unregister で
			// 実際に切断させたあと、対戦相手 (p2) が猶予切れであるという状況を想定して
			// 応答を書き換える。
			store := &fakeTimerStore{getDisconnectFound: false}

			m := NewManager(bc, nil, nil, noopMatchmakingClient{}, repo, nil, newFakeInvalidatedGameRepo(), 0, nil, store, DefaultDisconnectTimeout)
			m.Relay.JoinGame("p1", "g1", 1)
			m.Relay.JoinGame("p2", "g1", 2)

			connP1 := NewConnection(nil, "p1")
			m.Hub.Register(connP1)
			m.Hub.Unregister(connP1)

			store.getDisconnectFound = true
			store.getDisconnectReturn = portDisconnectDeadline("g1", time.Now().Add(-time.Minute))

			m.Hub.Register(NewConnection(nil, "p1"))

			require.Eventually(t, func() bool {
				return len(bc.snapshotProcessActionCalls()) == 1
			}, time.Second, 10*time.Millisecond, "opponent forfeit must fire")
			calls := bc.snapshotProcessActionCalls()
			assert.Equal(t, 2, calls[0].playerNum, "forfeit must be attributed to the still-disconnected opponent")
			assert.Equal(t, gamelogic.ActionTypeForfeit, calls[0].actionType)
		})
	})
}

// newTestManagerForBattleLimit は checkAndIncrementBattleLimit だけを検証するための
// Manager を返す。同メソッドは accountClient しか参照しないため、他の依存は zero value のままでよい。
func newTestManagerForBattleLimit(accountClient port.AccountClient) *Manager {
	return NewManager(nil, accountClient, nil, noopMatchmakingClient{}, nil, nil, newFakeInvalidatedGameRepo(), 0, nil, nil, DefaultDisconnectTimeout)
}

func TestCheckAndIncrementBattleLimit(t *testing.T) {
	t.Run("[マッチング]バトル開始受付時のバトル回数制限確認と加算", func(t *testing.T) {
		t.Run("当日のバトル残回数があるとき、当日のバトル回数を1増やし、呼び出し元への制限メッセージは空になる", func(t *testing.T) {
			fa := apiaccountserverfake.NewServer()
			defer fa.Close()
			fa.GetBattleLimitFn = func() (int, any) {
				return http.StatusOK, apiaccount.BattleLimitResponse{CanBattle: true}
			}
			var incrementCalls atomic.Int32
			fa.IncrementBattleCountFn = func() (int, any) {
				incrementCalls.Add(1)
				return http.StatusNoContent, nil
			}
			m := newTestManagerForBattleLimit(accountclient.New(fa.URL(), &http.Client{}))

			msg, err := m.checkAndIncrementBattleLimit(context.Background())

			require.NoError(t, err)
			assert.Equal(t, "", msg)
			assert.Equal(t, int32(1), incrementCalls.Load())
		})

		t.Run("当日のバトル残回数が無いとき、バトル回数を増やさず、呼び出し元への制限メッセージに拒否理由が載る", func(t *testing.T) {
			fa := apiaccountserverfake.NewServer()
			defer fa.Close()
			fa.GetBattleLimitFn = func() (int, any) {
				return http.StatusOK, apiaccount.BattleLimitResponse{CanBattle: false}
			}
			var incrementCalls atomic.Int32
			fa.IncrementBattleCountFn = func() (int, any) {
				incrementCalls.Add(1)
				return http.StatusNoContent, nil
			}
			m := newTestManagerForBattleLimit(accountclient.New(fa.URL(), &http.Client{}))

			msg, err := m.checkAndIncrementBattleLimit(context.Background())

			require.NoError(t, err)
			assert.Equal(t, "daily battle limit reached", msg)
			assert.Equal(t, int32(0), incrementCalls.Load())
		})

		t.Run("当日のバトル残回数の取得が500エラーのとき、バトル回数を増やさずエラーになる", func(t *testing.T) {
			fa := apiaccountserverfake.NewServer()
			defer fa.Close()
			fa.GetBattleLimitFn = func() (int, any) {
				return http.StatusInternalServerError, nil
			}
			var incrementCalls atomic.Int32
			fa.IncrementBattleCountFn = func() (int, any) {
				incrementCalls.Add(1)
				return http.StatusNoContent, nil
			}
			m := newTestManagerForBattleLimit(accountclient.New(fa.URL(), &http.Client{}))

			_, err := m.checkAndIncrementBattleLimit(context.Background())

			require.Error(t, err)
			assert.Equal(t, int32(0), incrementCalls.Load())
		})

		t.Run("当日のバトル回数を増やすリクエストが500エラーのとき、エラーになる", func(t *testing.T) {
			fa := apiaccountserverfake.NewServer()
			defer fa.Close()
			fa.GetBattleLimitFn = func() (int, any) {
				return http.StatusOK, apiaccount.BattleLimitResponse{CanBattle: true}
			}
			var incrementCalls atomic.Int32
			fa.IncrementBattleCountFn = func() (int, any) {
				incrementCalls.Add(1)
				return http.StatusInternalServerError, nil
			}
			m := newTestManagerForBattleLimit(accountclient.New(fa.URL(), &http.Client{}))

			_, err := m.checkAndIncrementBattleLimit(context.Background())

			require.Error(t, err)
			assert.Equal(t, int32(1), incrementCalls.Load())
		})
	})
}

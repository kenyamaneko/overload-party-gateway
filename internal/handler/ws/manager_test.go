package ws

import (
	"context"
	"errors"
	"sync"
	"testing"

	apicard "github.com/kenyamaneko/overload-party-card/packages/api-card"
	apimatchmaking "github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

// fakeCardClient は port.CardClient のテスト用実装。HandleMatchMade のデッキ解決先
// として使うだけなので、内容の妥当性は問わず空デッキを返す。
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

	claimed map[string]bool
	gameIDs map[string]string

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
		claimed: make(map[string]bool),
		gameIDs: make(map[string]string),
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

func (f *fakeProcessedMatchRepo) snapshotReleaseCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.releaseCalls...)
}

var _ port.ProcessedMatchRepo = (*fakeProcessedMatchRepo)(nil)

// newTestManagerForMatchMade は HandleMatchMade の永続 dedup を検証するための
// Manager を返す。WS 経路 (accountClient / matchmakingClient / internalSigner) は
// HandleMatchMade から参照されないため nil のままにする。
func newTestManagerForMatchMade(bc *mockBattleClient, gamePlayerRepo port.GamePlayerRepo, dedupRepo port.ProcessedMatchRepo) *Manager {
	return NewManager(bc, nil, &fakeCardClient{}, nil, gamePlayerRepo, dedupRepo, 0, nil)
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
	t.Run("match_made イベントの永続 dedup", func(t *testing.T) {
		t.Run("有効なイベントのとき、battle にゲーム作成を1回依頼し両プレイヤーの game_players 行を挿入する", func(t *testing.T) {
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

		t.Run("同一 matchId のイベントが新しい Manager インスタンスへ再送されるとき、battle へのゲーム作成依頼は1回のまま増えない", func(t *testing.T) {
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

		t.Run("matchId が claim 済みで battle 側の結果がまだ記録されていないとき、ゲーム作成を行わず処理をスキップする", func(t *testing.T) {
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

		t.Run("battle のゲーム作成に失敗した matchId が再送されると、claim が解放されており再度ゲーム作成を試みて成功する", func(t *testing.T) {
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

		t.Run("battle のゲーム作成成功後に dedup 記録が失敗するとき、claim を解放せずエラーを返す", func(t *testing.T) {
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

		t.Run("dedup 記録に失敗した matchId が再送されると、battle を再度呼び出さず処理をスキップする", func(t *testing.T) {
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

		t.Run("claim 自体が失敗するとき、エラーを返し battle を呼び出さない", func(t *testing.T) {
			dedupRepo := newFakeProcessedMatchRepo()
			dedupRepo.claimErr = errors.New("db down")
			bc := newMockBattleClient()
			gamePlayerRepo := &mockGamePlayerRepo{}
			m := newTestManagerForMatchMade(bc, gamePlayerRepo, dedupRepo)

			err := m.HandleMatchMade(context.Background(), matchMadeEvent("mch_claim_fail"))

			require.Error(t, err)
			assert.Empty(t, bc.snapshotCreatePvPGameCalls())
		})

		t.Run("game_players 挿入に失敗するとき、エラーを返す", func(t *testing.T) {
			dedupRepo := newFakeProcessedMatchRepo()
			bc := newMockBattleClient()
			bc.createPvPGameResult = &service.GameCreatedResult{GameID: "g5"}
			gamePlayerRepo := &mockGamePlayerRepo{insertErr: errors.New("db down"), insertErrForPlayerNum: 1}
			m := newTestManagerForMatchMade(bc, gamePlayerRepo, dedupRepo)

			err := m.HandleMatchMade(context.Background(), matchMadeEvent("mch_insert_fail"))

			require.Error(t, err)
			require.Len(t, bc.snapshotCreatePvPGameCalls(), 1)
		})

		t.Run("game_players 挿入に失敗した matchId が再送されると、battle を再度呼び出さず挿入だけがやり直される", func(t *testing.T) {
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
}

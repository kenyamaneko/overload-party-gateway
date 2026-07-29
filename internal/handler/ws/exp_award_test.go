package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	gamedesign "github.com/kenyamaneko/overload-party-common/packages/game-design-constants"

	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// mockGamePlayerRepo は port.GamePlayerRepo のテスト用実装。
// awardGameExp の冪等性境界（mark→lookup→award の順序とエラー分岐）を検証する。
type mockGamePlayerRepo struct {
	mu sync.Mutex

	markAwardedReturn bool
	markAwardedErr    error
	markAwardedCalls  int

	lookupEntries []port.GamePlayerEntry
	lookupErr     error
	lookupCalls   int

	lookupPlayerNum    int
	lookupPlayerNumErr error

	// callOrder は MarkExpAwarded と LookupGamePlayers の呼出順を記録する。
	// 「MarkExpAwarded を必ず先に実行」の契約を検証するため。
	callOrder []string

	insertCalls []insertGamePlayerCall
	// insertErrForPlayerNum が非 0 のとき、その playerNum の InsertGamePlayer 呼出のみ
	// insertErr を返す (0 = 全呼出に対して返す)。部分成功後のリトライを再現するため。
	insertErr             error
	insertErrForPlayerNum int
}

// insertGamePlayerCall は mockGamePlayerRepo.InsertGamePlayer への 1 回の呼出を記録する。
type insertGamePlayerCall struct {
	gameID    string
	playerNum int
	playerID  string
}

func (m *mockGamePlayerRepo) InsertGamePlayer(_ context.Context, gameID string, playerNum int, playerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.insertCalls = append(m.insertCalls, insertGamePlayerCall{gameID, playerNum, playerID})
	if m.insertErr != nil && (m.insertErrForPlayerNum == 0 || m.insertErrForPlayerNum == playerNum) {
		return m.insertErr
	}
	return nil
}

// snapshotInsertGamePlayerCalls は insertCalls を排他制御した上で複製して返す。
func (m *mockGamePlayerRepo) snapshotInsertGamePlayerCalls() []insertGamePlayerCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]insertGamePlayerCall(nil), m.insertCalls...)
}

func (m *mockGamePlayerRepo) LookupPlayerNum(_ context.Context, _ string, _ string) (int, error) {
	return m.lookupPlayerNum, m.lookupPlayerNumErr
}

func (m *mockGamePlayerRepo) LookupGamePlayers(_ context.Context, _ string) ([]port.GamePlayerEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lookupCalls++
	m.callOrder = append(m.callOrder, "lookup")
	return m.lookupEntries, m.lookupErr
}

func (m *mockGamePlayerRepo) MarkExpAwarded(_ context.Context, _ string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.markAwardedCalls++
	m.callOrder = append(m.callOrder, "mark")
	return m.markAwardedReturn, m.markAwardedErr
}

var _ port.GamePlayerRepo = (*mockGamePlayerRepo)(nil)

// awardCounter は account サービスの AwardGameExp 呼出回数と受信 body を集計する httptest 用ハンドラ。
type awardCounter struct {
	calls    atomic.Int32
	respCode int // 0 → 200

	mu           sync.Mutex
	capturedBody apiaccount.AwardGameExpRequest
}

func (a *awardCounter) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/v1/players/award-game-exp" {
			var req apiaccount.AwardGameExpRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			a.mu.Lock()
			a.capturedBody = req
			a.mu.Unlock()
			a.calls.Add(1)
			code := a.respCode
			if code == 0 {
				code = http.StatusOK
			}
			w.WriteHeader(code)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
}

// body は直近に受信した AwardGameExp リクエスト body を返す。
func (a *awardCounter) body() apiaccount.AwardGameExpRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.capturedBody
}

// setupAwardRelay は awardGameExp の動線を完全に組み立てた relay を返す。
func setupAwardRelay(t *testing.T, repo port.GamePlayerRepo, account *awardCounter) *GameRelay {
	t.Helper()
	srv := httptest.NewServer(account.handler())
	t.Cleanup(srv.Close)

	relay, _ := newTestRelay()
	relay.gamePlayerRepo = repo
	relay.accountClient = accountclient.New(srv.URL)
	return relay
}

func TestAwardGameExp(t *testing.T) {
	t.Run("EXP 付与", func(t *testing.T) {
		t.Run("記録先も account 連携も無いとき、パニックせず戻る", func(t *testing.T) {
			relay, _ := newTestRelay()
			relay.awardGameExp("g1", 1, "lp_zero")
		})

		t.Run("記録先はあるが account 連携が無いとき、付与済みフラグを立てない", func(t *testing.T) {
			relay, _ := newTestRelay()
			repo := &mockGamePlayerRepo{}
			relay.gamePlayerRepo = repo
			// accountClient nil

			relay.awardGameExp("g1", 1, "lp_zero")

			// accountClient が無いのに MarkExpAwarded を呼ぶと、フラグだけ立って付与されない状態になる。
			// 二重付与は防げるが永久に EXP が付かないゾンビゲームになるため、絶対に呼んではならない。
			assert.Equal(t, 0, repo.markAwardedCalls)
		})

		t.Run("初回付与のとき、フラグ確定→プレイヤー解決の順で実行し account に付与する", func(t *testing.T) {
			repo := &mockGamePlayerRepo{
				markAwardedReturn: true,
				lookupEntries: []port.GamePlayerEntry{
					{PlayerNum: 1, PlayerID: "p1"},
					{PlayerNum: 2, PlayerID: "p2"},
				},
			}
			account := &awardCounter{}
			relay := setupAwardRelay(t, repo, account)

			relay.awardGameExp("g1", 1, "lp_zero")

			assert.Equal(t, 1, repo.markAwardedCalls)
			assert.Equal(t, 1, repo.lookupCalls)
			assert.Equal(t, int32(1), account.calls.Load())
			assert.Equal(t, []string{"mark", "lookup"}, repo.callOrder,
				"MarkExpAwarded MUST be called before LookupGamePlayers (idempotency invariant)")
		})

		t.Run("対戦相手が 1 件 (NPC) のとき、account に付与する", func(t *testing.T) {
			repo := &mockGamePlayerRepo{
				markAwardedReturn: true,
				lookupEntries: []port.GamePlayerEntry{
					{PlayerNum: 1, PlayerID: "p1"},
				},
			}
			account := &awardCounter{}
			relay := setupAwardRelay(t, repo, account)

			relay.awardGameExp("g1", 1, "lp_zero")

			assert.Equal(t, int32(1), account.calls.Load())
		})

		t.Run("既に付与済みのとき、プレイヤー解決も付与もしない", func(t *testing.T) {
			repo := &mockGamePlayerRepo{
				markAwardedReturn: false, // フラグは既に立っていた
			}
			account := &awardCounter{}
			relay := setupAwardRelay(t, repo, account)

			relay.awardGameExp("g1", 1, "lp_zero")

			assert.Equal(t, 1, repo.markAwardedCalls)
			assert.Equal(t, 0, repo.lookupCalls, "must not look up players if already awarded")
			assert.Equal(t, int32(0), account.calls.Load(), "must not award twice")
		})

		t.Run("付与済みフラグの確定が失敗するとき、付与せずプレイヤー解決も呼ばない", func(t *testing.T) {
			// フラグ書き込みに失敗したら付与せず返す。強引に付与すると次回リトライで二重付与に
			// なるため、失敗したら諦めて要監視 (ERROR ログ) が正しい。次回同じ game_id が来たら
			// MarkExpAwarded が再試行され、成功すれば付与される。
			repo := &mockGamePlayerRepo{
				markAwardedErr: errors.New("db down"),
			}
			account := &awardCounter{}
			relay := setupAwardRelay(t, repo, account)

			relay.awardGameExp("g1", 1, "lp_zero")

			assert.Equal(t, 1, repo.markAwardedCalls)
			assert.Equal(t, 0, repo.lookupCalls, "lookup must be skipped when mark fails — otherwise we'd risk double-award on retry")
			assert.Equal(t, int32(0), account.calls.Load())
		})

		t.Run("プレイヤー解決が失敗するとき、付与しない", func(t *testing.T) {
			// マークは成功 (フラグは立った) したがプレイヤー解決で失敗。次回呼んでも MarkExpAwarded が
			// false を返すため二重付与にはならない。このゲームの EXP は失われる (ERROR ログで監視)。
			repo := &mockGamePlayerRepo{
				markAwardedReturn: true,
				lookupErr:         errors.New("db down"),
			}
			account := &awardCounter{}
			relay := setupAwardRelay(t, repo, account)

			relay.awardGameExp("g1", 1, "lp_zero")

			assert.Equal(t, 1, repo.markAwardedCalls)
			assert.Equal(t, 1, repo.lookupCalls)
			assert.Equal(t, int32(0), account.calls.Load())
		})

		t.Run("account が 500 を返すとき、フラグは立ったまま再試行されない", func(t *testing.T) {
			// AwardGameExp の RPC が失敗。フラグは既に立っているので二重付与にはならないが EXP が
			// 失われる。次回 game_id が来ても MarkExpAwarded が false を返すため自動再試行はされない
			// (手動オペレーションが必要)。
			repo := &mockGamePlayerRepo{
				markAwardedReturn: true,
				lookupEntries: []port.GamePlayerEntry{
					{PlayerNum: 1, PlayerID: "p1"},
					{PlayerNum: 2, PlayerID: "p2"},
				},
			}
			account := &awardCounter{respCode: http.StatusInternalServerError}
			relay := setupAwardRelay(t, repo, account)

			relay.awardGameExp("g1", 1, "lp_zero")

			assert.Equal(t, 1, repo.markAwardedCalls, "flag is set first; this is an accepted invariant")
			assert.Equal(t, int32(1), account.calls.Load(), "we attempted award once")

			// 再呼び出しは MarkExpAwarded で false が返るのでスキップされる (リアル DB の挙動だが、
			// ここではモックを再設定して再現)。
			repo.markAwardedReturn = false
			relay.awardGameExp("g1", 1, "lp_zero")
			assert.Equal(t, int32(1), account.calls.Load(), "no retry — EXP is permanently lost without manual intervention")
		})

		t.Run("プレイヤー番号が 1/2 以外のとき、パニックせず付与する", func(t *testing.T) {
			// PlayerNum が 1/2 以外 (不整合データ) でも player1ID/player2ID の組み立てで panic しない。
			repo := &mockGamePlayerRepo{
				markAwardedReturn: true,
				lookupEntries: []port.GamePlayerEntry{
					{PlayerNum: 99, PlayerID: "weird"},
				},
			}
			account := &awardCounter{}
			relay := setupAwardRelay(t, repo, account)

			require.NotPanics(t, func() {
				relay.awardGameExp("g1", 1, "lp_zero")
			})
			// PlayerNum=99 は player1ID/player2ID のどちらにも入らないが AwardGameExp は呼ばれる
			// (空文字 ID をどう処理するかは account の責務)。
			assert.Equal(t, int32(1), account.calls.Load())
		})

		t.Run("2 人のゲームで player 1 が勝ったとき、付与依頼に両プレイヤー ID・勝者 1・理由・pvp が載る", func(t *testing.T) {
			repo := &mockGamePlayerRepo{
				markAwardedReturn: true,
				lookupEntries: []port.GamePlayerEntry{
					{PlayerNum: 1, PlayerID: "TST-P1"},
					{PlayerNum: 2, PlayerID: "TST-P2"},
				},
			}
			account := &awardCounter{}
			relay := setupAwardRelay(t, repo, account)

			relay.awardGameExp("g1", 1, "lp_zero")

			got := account.body()
			assert.Equal(t, "TST-P1", got.Player1ID)
			assert.Equal(t, "TST-P2", got.Player2ID)
			assert.Equal(t, int64(1), got.WinnerNum)
			assert.Equal(t, "lp_zero", got.Reason)
			assert.Equal(t, gamedesign.MatchTypePvp, got.MatchType)
		})

		t.Run("参加者が 1 人だけ (NPC 戦) のとき、付与依頼のマッチ種別が npc になる", func(t *testing.T) {
			repo := &mockGamePlayerRepo{
				markAwardedReturn: true,
				lookupEntries: []port.GamePlayerEntry{
					{PlayerNum: 1, PlayerID: "TST-P1"},
				},
			}
			account := &awardCounter{}
			relay := setupAwardRelay(t, repo, account)

			relay.awardGameExp("g1", 1, "lp_zero")

			got := account.body()
			assert.Equal(t, gamedesign.MatchTypeNpc, got.MatchType)
			assert.Equal(t, "", got.Player2ID)
		})
	})
}

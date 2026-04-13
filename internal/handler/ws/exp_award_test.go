package ws

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
}

func (m *mockGamePlayerRepo) InsertGamePlayer(_ context.Context, _ string, _ int, _ string) error {
	return nil
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

// awardCounter は account サービスの AwardGameExp 呼出回数を集計する httptest 用ハンドラ。
type awardCounter struct {
	calls    atomic.Int32
	respCode int // 0 → 200
}

func (a *awardCounter) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/v1/players/award-game-exp" {
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

// ----------------------------------------------------------------------------
// 早期 return（依存未設定）
// ----------------------------------------------------------------------------

func TestAwardGameExp_NoRepoNoClient_NoOp(t *testing.T) {
	relay, _ := newTestRelay()
	// パニックや副作用が無いことを確認するだけ
	relay.awardGameExp("g1", 1, "lp_zero")
}

func TestAwardGameExp_NoAccountClient_DoesNotMark(t *testing.T) {
	relay, _ := newTestRelay()
	repo := &mockGamePlayerRepo{}
	relay.gamePlayerRepo = repo
	// accountClient nil

	relay.awardGameExp("g1", 1, "lp_zero")

	// accountClient が無いのに MarkExpAwarded が呼ばれると、フラグだけ立って付与されない状態になる。
	// → 二重付与は防げるが、永久に EXP が付かないゾンビゲームになるため、絶対に呼んではならない。
	assert.Equal(t, 0, repo.markAwardedCalls)
}

// ----------------------------------------------------------------------------
// 冪等性: 正常系
// ----------------------------------------------------------------------------

func TestAwardGameExp_FirstCall_AwardsAndMarks(t *testing.T) {
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
}

func TestAwardGameExp_NPC_OneEntry(t *testing.T) {
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
}

// ----------------------------------------------------------------------------
// 冪等性: 既に awarded（重複呼び出し）
// ----------------------------------------------------------------------------

func TestAwardGameExp_AlreadyAwarded_SkipsLookupAndAward(t *testing.T) {
	repo := &mockGamePlayerRepo{
		markAwardedReturn: false, // フラグは既に立っていた
	}
	account := &awardCounter{}
	relay := setupAwardRelay(t, repo, account)

	relay.awardGameExp("g1", 1, "lp_zero")

	assert.Equal(t, 1, repo.markAwardedCalls)
	assert.Equal(t, 0, repo.lookupCalls, "must not look up players if already awarded")
	assert.Equal(t, int32(0), account.calls.Load(), "must not award twice")
}

// ----------------------------------------------------------------------------
// 冪等性: MarkExpAwarded 失敗
// ----------------------------------------------------------------------------

// 最重要ケース: フラグ書き込みに失敗したら、付与せずに返す。
// このとき LookupGamePlayers / AwardGameExp は呼ばれない。
// → 次回同じ game_id が来たら MarkExpAwarded が再試行され、成功すれば付与される。
//
// この方針の意味: マーク失敗時に強引に付与してしまうと、次回リトライで二重付与になる。
// よって「失敗したら諦めて要監視 (ERROR ログ)」が正しい。
func TestAwardGameExp_MarkError_DoesNotAward(t *testing.T) {
	repo := &mockGamePlayerRepo{
		markAwardedErr: errors.New("db down"),
	}
	account := &awardCounter{}
	relay := setupAwardRelay(t, repo, account)

	relay.awardGameExp("g1", 1, "lp_zero")

	assert.Equal(t, 1, repo.markAwardedCalls)
	assert.Equal(t, 0, repo.lookupCalls, "lookup must be skipped when mark fails — otherwise we'd risk double-award on retry")
	assert.Equal(t, int32(0), account.calls.Load())
}

// ----------------------------------------------------------------------------
// 冪等性: LookupGamePlayers 失敗
// ----------------------------------------------------------------------------

// マークは成功（フラグは立った）したが、プレイヤー解決で失敗。
// AwardGameExp は呼ばない。次回呼んでも MarkExpAwarded が false を返すので二重付与にはならない。
// → このゲームの EXP は失われる（ERROR ログで監視）。
func TestAwardGameExp_LookupError_DoesNotAward(t *testing.T) {
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
}

// ----------------------------------------------------------------------------
// account サービス側でのエラー
// ----------------------------------------------------------------------------

// AwardGameExp の RPC が失敗。フラグは既に立っているので二重付与にはならないが、
// EXP が失われる。要監視。次回 game_id が来ても MarkExpAwarded が false を返すため
// 自動再試行はされない（手動オペレーションが必要）。
func TestAwardGameExp_AccountServerError_FlagAlreadyMarked(t *testing.T) {
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

	// 再呼び出しは MarkExpAwarded で false が返るのでスキップされる（リアル DB の挙動だが、
	// ここではモックを再設定して再現）
	repo.markAwardedReturn = false
	relay.awardGameExp("g1", 1, "lp_zero")
	assert.Equal(t, int32(1), account.calls.Load(), "no retry — EXP is permanently lost without manual intervention")
}

// ----------------------------------------------------------------------------
// 入力データ伝播
// ----------------------------------------------------------------------------

// awardGameExp が entries.PlayerNum に基づいて player1ID/player2ID を組み立てることを検証。
// PlayerNum が 1/2 以外の場合（不整合データ）でも panic しないことを担保する。
func TestAwardGameExp_UnknownPlayerNum_DoesNotPanic(t *testing.T) {
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
	// PlayerNum=99 は player1ID/player2ID のどちらにも入らないが、AwardGameExp は呼ばれる
	// （account 側で空文字 ID をどう処理するかは account の責務）
	assert.Equal(t, int32(1), account.calls.Load())
}

package ws

import (
	"context"
	"errors"
	"testing"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/adapter/displaymetacache"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// stubLookuper は固定応答を返す playerLookuper のテストダブル。
type stubLookuper struct {
	response *apiaccount.PlayerResponse
	err      error
}

func (s *stubLookuper) GetPlayer(_ context.Context, _ string) (*apiaccount.PlayerResponse, error) {
	return s.response, s.err
}

// fakeCache は port.ErrNotFound 以外の任意のエラーを Get で返せる cache テストダブル。
// 通常パスは embed した MemoryStore に委譲し、Get エラー注入のみ上書きする。
type fakeCache struct {
	*displaymetacache.MemoryStore
	getErr error
}

func (f *fakeCache) Get(ctx context.Context, gameID string, playerNum int) (port.DisplayMeta, error) {
	if f.getErr != nil {
		return port.DisplayMeta{}, f.getErr
	}
	return f.MemoryStore.Get(ctx, gameID, playerNum)
}

// strPtr は string literal を *string にするテストヘルパ。
func strPtr(s string) *string { return &s }

// TestDisplayResolver_Resolve_ReturnsCacheValueOnHit は cache に snapshot が存在する場合、
// account を叩かずに cache 値が返ることを検証する (高頻度 relay 経路で account 負荷を抑える要件)。
func TestDisplayResolver_Resolve_ReturnsCacheValueOnHit(t *testing.T) {
	cache := displaymetacache.NewMemoryStore()
	ctx := context.Background()
	want := port.DisplayMeta{Name: "alice", Level: 7}
	require.NoError(t, cache.Put(ctx, "g1", 1, want))

	lookuper := &stubLookuper{err: errors.New("must not be called")}
	r := NewDisplayResolver(cache, lookuper)

	got := r.Resolve(ctx, "g1", 1, "p-1")
	assert.Equal(t, want, got)
}

// TestDisplayResolver_Resolve_FallsBackToAccountOnCacheMiss は cache miss 時に
// account 直接 lookup の値が返ることを検証する。
func TestDisplayResolver_Resolve_FallsBackToAccountOnCacheMiss(t *testing.T) {
	cache := displaymetacache.NewMemoryStore()
	lookuper := &stubLookuper{response: &apiaccount.PlayerResponse{
		PlayerID: "p-1",
		Name:     strPtr("alice"),
		Level:    7,
	}}
	r := NewDisplayResolver(cache, lookuper)

	got := r.Resolve(context.Background(), "g1", 1, "p-1")
	assert.Equal(t, port.DisplayMeta{Name: "alice", Level: 7}, got)
}

// TestDisplayResolver_Resolve_PromotesAccountResultToCache は account fallback で得た値が
// cache に書き戻され、次回以降の Get で hit することを検証する (繰り返し account を叩かない)。
func TestDisplayResolver_Resolve_PromotesAccountResultToCache(t *testing.T) {
	cache := displaymetacache.NewMemoryStore()
	lookuper := &stubLookuper{response: &apiaccount.PlayerResponse{
		PlayerID: "p-1",
		Name:     strPtr("alice"),
		Level:    7,
	}}
	r := NewDisplayResolver(cache, lookuper)
	ctx := context.Background()

	_ = r.Resolve(ctx, "g1", 1, "p-1")

	got, err := cache.Get(ctx, "g1", 1)
	require.NoError(t, err)
	assert.Equal(t, port.DisplayMeta{Name: "alice", Level: 7}, got)
}

// TestDisplayResolver_Resolve_FallsBackToAccountOnCacheReadError は cache 自体の read 失敗
// (NotFound 以外) でも account 直接 lookup へフォールバックして表示可能な値を返すことを検証する。
func TestDisplayResolver_Resolve_FallsBackToAccountOnCacheReadError(t *testing.T) {
	cache := &fakeCache{
		MemoryStore: displaymetacache.NewMemoryStore(),
		getErr:      errors.New("cache backend offline"),
	}
	lookuper := &stubLookuper{response: &apiaccount.PlayerResponse{
		PlayerID: "p-1",
		Name:     strPtr("alice"),
		Level:    7,
	}}
	r := NewDisplayResolver(cache, lookuper)

	got := r.Resolve(context.Background(), "g1", 1, "p-1")
	assert.Equal(t, port.DisplayMeta{Name: "alice", Level: 7}, got)
}

// TestDisplayResolver_Resolve_WritesFallbackValueWhenAccountFails は account 直接 lookup
// も失敗した場合に「Player {playerID 短縮}」形式のフォールバック表示値を返すことを検証する
// (空文字での silent fallback を避け、UI 上で失敗を識別可能にする要件)。
func TestDisplayResolver_Resolve_WritesFallbackValueWhenAccountFails(t *testing.T) {
	cache := displaymetacache.NewMemoryStore()
	lookuper := &stubLookuper{err: errors.New("account offline")}
	r := NewDisplayResolver(cache, lookuper)

	got := r.Resolve(context.Background(), "g1", 1, "abc123def456")
	assert.Equal(t, port.DisplayMeta{Name: "Player abc123", Level: 0}, got)
}

// TestDisplayResolver_Resolve_PersistsFallbackValueToCache は account 失敗時に書き込まれた
// フォールバック表示値が cache に残り、後続呼び出しで account を叩かないことを検証する。
func TestDisplayResolver_Resolve_PersistsFallbackValueToCache(t *testing.T) {
	cache := displaymetacache.NewMemoryStore()
	lookuper := &stubLookuper{err: errors.New("account offline")}
	r := NewDisplayResolver(cache, lookuper)
	ctx := context.Background()

	_ = r.Resolve(ctx, "g1", 1, "abc123def456")

	got, err := cache.Get(ctx, "g1", 1)
	require.NoError(t, err)
	assert.Equal(t, port.DisplayMeta{Name: "Player abc123", Level: 0}, got)
}

// TestDisplayResolver_Resolve_TreatsNilNameAsAccountFailure は account 応答に name が
// 含まれない (onboarding 未完了等の異常) 場合もフォールバック表示値を返すことを検証する
// (Name nil を silent に空文字に倒さない — gateway#47 で報告された問題への直接の修正)。
func TestDisplayResolver_Resolve_TreatsNilNameAsAccountFailure(t *testing.T) {
	cache := displaymetacache.NewMemoryStore()
	lookuper := &stubLookuper{response: &apiaccount.PlayerResponse{
		PlayerID: "p-1",
		Name:     nil,
		Level:    7,
	}}
	r := NewDisplayResolver(cache, lookuper)

	got := r.Resolve(context.Background(), "g1", 1, "abc123def456")
	assert.Equal(t, port.DisplayMeta{Name: "Player abc123", Level: 0}, got)
}

// TestFallbackDisplayMeta_TruncatesLongPlayerID は playerID の長さに応じた短縮挙動を
// 検証する (UI 上で目視判別する prefix 長の定義)。
func TestFallbackDisplayMeta_TruncatesLongPlayerID(t *testing.T) {
	cases := []struct {
		name     string
		playerID string
		want     string
	}{
		{
			name:     "prefix 長より短い playerID はそのまま使う",
			playerID: "abc",
			want:     "Player abc",
		},
		{
			name:     "prefix 長と同じ playerID はそのまま使う",
			playerID: "abc123",
			want:     "Player abc123",
		},
		{
			name:     "prefix 長を超える playerID は先頭 6 文字に短縮する",
			playerID: "abc123def456",
			want:     "Player abc123",
		},
		{
			name:     "空 playerID はそのまま使う",
			playerID: "",
			want:     "Player ",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fallbackDisplayMeta(tc.playerID)
			assert.Equal(t, port.DisplayMeta{Name: tc.want, Level: 0}, got)
		})
	}
}

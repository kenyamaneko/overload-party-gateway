package ws

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/adapter/displaymetacache"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// stubGetter は固定応答を返す port.PlayerProfileGetter のテストダブル。
type stubGetter struct {
	profile port.PlayerProfile
	err     error
}

func (s *stubGetter) GetPlayerProfile(_ context.Context, _ string) (port.PlayerProfile, error) {
	return s.profile, s.err
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

// testGameID / testPlayerNum は cache key を構成する固定値。cache の key 解決ロジックは
// displaymetacache 側で検証済みのため、resolver test では (gameID, playerNum) の組み合わせ
// 自体を変えるのは観点外。
const (
	testGameID    = "g1"
	testPlayerNum = 1
)

// 短縮対象の長い playerID と、それを fallback したときの期待表示値。
const (
	longPlayerID    = "abc123def456"
	fallbackForLong = "Player abc123"
)

// TestDisplayResolver_Resolve_ReturnsExpectedMeta は (cache 状態, getter 状態) の各組み合わせで
// Resolve が常に表示可能な DisplayMeta を返す契約を検証する。各ケースは「どこで失敗を吸収する
// 経路に切り替わるか」のシナリオを表す。
func TestDisplayResolver_Resolve_ReturnsExpectedMeta(t *testing.T) {
	cachedAlice := port.DisplayMeta{Name: "alice-from-cache", Level: 1}
	accountAlice := port.PlayerProfile{Name: "alice-from-account", Level: 7}
	fallback := port.DisplayMeta{Name: fallbackForLong, Level: 0}

	cases := []struct {
		name     string
		setup    func() (displayCache, port.PlayerProfileGetter)
		playerID string
		want     port.DisplayMeta
	}{
		{
			name: "cache hit では cache の値を返し account を呼ばない",
			setup: func() (displayCache, port.PlayerProfileGetter) {
				cache := displaymetacache.NewMemoryStore()
				_ = cache.Put(context.Background(), testGameID, testPlayerNum, cachedAlice)
				return cache, &stubGetter{err: errors.New("must not be called")}
			},
			playerID: "any-player-id-cache-hit",
			want:     cachedAlice,
		},
		{
			name: "cache miss では account の値を返す",
			setup: func() (displayCache, port.PlayerProfileGetter) {
				return displaymetacache.NewMemoryStore(), &stubGetter{profile: accountAlice}
			},
			playerID: "any-player-id-cache-miss",
			want:     port.DisplayMeta(accountAlice),
		},
		{
			name: "cache read エラーでも account の値を返す",
			setup: func() (displayCache, port.PlayerProfileGetter) {
				cache := &fakeCache{
					MemoryStore: displaymetacache.NewMemoryStore(),
					getErr:      errors.New("cache backend offline"),
				}
				return cache, &stubGetter{profile: accountAlice}
			},
			playerID: "any-player-id-cache-read-error",
			want:     port.DisplayMeta(accountAlice),
		},
		{
			name: "account 失敗時はフォールバック表示値を返す",
			setup: func() (displayCache, port.PlayerProfileGetter) {
				return displaymetacache.NewMemoryStore(), &stubGetter{err: errors.New("account offline")}
			},
			playerID: longPlayerID,
			want:     fallback,
		},
		{
			name: "account 応答に name 無し (Name=\"\") はフォールバック表示値を返す",
			setup: func() (displayCache, port.PlayerProfileGetter) {
				return displaymetacache.NewMemoryStore(), &stubGetter{profile: port.PlayerProfile{Name: "", Level: 7}}
			},
			playerID: longPlayerID,
			want:     fallback,
		},
		{
			name: "getter 未注入でもフォールバック表示値を返す",
			setup: func() (displayCache, port.PlayerProfileGetter) {
				return displaymetacache.NewMemoryStore(), nil
			},
			playerID: longPlayerID,
			want:     fallback,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cache, getter := tc.setup()
			r := NewDisplayResolver(cache, getter)
			got := r.Resolve(context.Background(), testGameID, testPlayerNum, tc.playerID)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestDisplayResolver_Resolve_PersistsResolvedMetaToCache は Resolve が account にフォールバック
// したケースで結果値を cache に書き戻すことを検証する (相手 player の毎 relay 都度 account 直接
// lookup を抑える要件)。
func TestDisplayResolver_Resolve_PersistsResolvedMetaToCache(t *testing.T) {
	accountAlice := port.PlayerProfile{Name: "alice", Level: 7}

	cases := []struct {
		name       string
		getter     port.PlayerProfileGetter
		playerID   string
		wantCached port.DisplayMeta
	}{
		{
			name:       "account fallback で得た値を cache に書き戻す",
			getter:     &stubGetter{profile: accountAlice},
			playerID:   "any-player-id-account-success",
			wantCached: port.DisplayMeta(accountAlice),
		},
		{
			name:       "account 失敗時にフォールバック表示値を cache に書き込む",
			getter:     &stubGetter{err: errors.New("account offline")},
			playerID:   longPlayerID,
			wantCached: port.DisplayMeta{Name: fallbackForLong, Level: 0},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cache := displaymetacache.NewMemoryStore()
			r := NewDisplayResolver(cache, tc.getter)
			ctx := context.Background()
			_ = r.Resolve(ctx, testGameID, testPlayerNum, tc.playerID)

			got, err := cache.Get(ctx, testGameID, testPlayerNum)
			require.NoError(t, err)
			assert.Equal(t, tc.wantCached, got)
		})
	}
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
			playerID: longPlayerID,
			want:     fallbackForLong,
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

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

// failingPutCache は Put が常に error を返す cache。書き込み失敗時の handler 挙動を
// 検証するために使う。Get は MemoryStore に委譲する。
type failingPutCache struct {
	*displaymetacache.MemoryStore
}

func (f *failingPutCache) Put(_ context.Context, _ string, _ int, _ port.DisplayMeta) error {
	return errors.New("cache backend offline")
}

// TestManager_writeDisplayMetaSnapshot_WritesAccountValue は account 応答 (name 確定) を
// そのまま cache に snapshot することを検証する (正常系)。
func TestManager_writeDisplayMetaSnapshot_WritesAccountValue(t *testing.T) {
	cache := displaymetacache.NewMemoryStore()
	getter := &stubGetter{profile: port.PlayerProfile{Name: "alice", Level: 7}}
	m := &Manager{displayCache: cache, playerProfileGetter: getter}
	ctx := context.Background()

	require.NoError(t, m.writeDisplayMetaSnapshot(ctx, "g1", 1, "p-1"))

	got, err := cache.Get(ctx, "g1", 1)
	require.NoError(t, err)
	assert.Equal(t, port.DisplayMeta{Name: "alice", Level: 7}, got)
}

// TestManager_writeDisplayMetaSnapshot_ReturnsErrorWhenAccountFails は account 呼び出しが
// 失敗した場合に error を伝播することを検証する (Pub/Sub に再配信させる経路)。
func TestManager_writeDisplayMetaSnapshot_ReturnsErrorWhenAccountFails(t *testing.T) {
	cache := displaymetacache.NewMemoryStore()
	getter := &stubGetter{err: errors.New("account offline")}
	m := &Manager{displayCache: cache, playerProfileGetter: getter}

	err := m.writeDisplayMetaSnapshot(context.Background(), "g1", 1, "p-1")
	assert.Error(t, err)
}

// TestManager_writeDisplayMetaSnapshot_SkipsWriteWhenNameEmpty は account 応答に name が
// 含まれない (Name="") 場合に snapshot 書き込みをスキップし、handler は継続 (nil 返却) する
// ことを検証する。フォールバック表示値の書き込みは relay 経路の最終行に集約する設計のため、
// ここでは何も書かない。
func TestManager_writeDisplayMetaSnapshot_SkipsWriteWhenNameEmpty(t *testing.T) {
	cache := displaymetacache.NewMemoryStore()
	getter := &stubGetter{profile: port.PlayerProfile{Name: "", Level: 7}}
	m := &Manager{displayCache: cache, playerProfileGetter: getter}
	ctx := context.Background()

	require.NoError(t, m.writeDisplayMetaSnapshot(ctx, "g1", 1, "abc123def456"))

	_, err := cache.Get(ctx, "g1", 1)
	require.ErrorIs(t, err, port.ErrNotFound)
}

// TestManager_writeDisplayMetaSnapshot_ContinuesWhenCachePutFails は cache 書き込み失敗が
// handler を停止させないことを検証する (試合継続のため、relay 経路で必要時に再 lookup する設計)。
func TestManager_writeDisplayMetaSnapshot_ContinuesWhenCachePutFails(t *testing.T) {
	cache := &failingPutCache{MemoryStore: displaymetacache.NewMemoryStore()}
	getter := &stubGetter{profile: port.PlayerProfile{Name: "alice", Level: 7}}
	m := &Manager{displayCache: cache, playerProfileGetter: getter}

	require.NoError(t, m.writeDisplayMetaSnapshot(context.Background(), "g1", 1, "p-1"))
}

// TestManager_writeDisplayMetaSnapshot_SkipsWhenCacheNil は cache 未注入 (test / mock モード) で
// no-op として成功することを検証する。
func TestManager_writeDisplayMetaSnapshot_SkipsWhenCacheNil(t *testing.T) {
	getter := &stubGetter{profile: port.PlayerProfile{Name: "alice", Level: 7}}
	m := &Manager{playerProfileGetter: getter}

	require.NoError(t, m.writeDisplayMetaSnapshot(context.Background(), "g1", 1, "p-1"))
}

// TestManager_writeDisplayMetaSnapshot_SkipsWhenGetterNil は getter 未注入 (test / mock モード) で
// no-op として成功することを検証する。
func TestManager_writeDisplayMetaSnapshot_SkipsWhenGetterNil(t *testing.T) {
	cache := displaymetacache.NewMemoryStore()
	m := &Manager{displayCache: cache}

	require.NoError(t, m.writeDisplayMetaSnapshot(context.Background(), "g1", 1, "p-1"))
}

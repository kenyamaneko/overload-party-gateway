package ws

import (
	"context"
	"errors"
	"net/http"
	"testing"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	"github.com/kenyamaneko/overload-party-account/packages/api-account/apiaccountserverfake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/adapter/displaymetacache"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
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
	srv := apiaccountserverfake.NewServer()
	defer srv.Close()
	srv.GetPlayerByIDFn = func(_ string) (int, any) {
		name := "alice"
		return http.StatusOK, apiaccount.PlayerResponse{PlayerID: "p-1", Name: &name, Level: 7}
	}

	cache := displaymetacache.NewMemoryStore()
	m := &Manager{accountClient: accountclient.New(srv.URL()), displayCache: cache}
	ctx := context.Background()

	require.NoError(t, m.writeDisplayMetaSnapshot(ctx, "g1", 1, "p-1"))

	got, err := cache.Get(ctx, "g1", 1)
	require.NoError(t, err)
	assert.Equal(t, port.DisplayMeta{Name: "alice", Level: 7}, got)
}

// TestManager_writeDisplayMetaSnapshot_ReturnsErrorWhenAccountFails は account 呼び出しが
// 失敗した場合に error を伝播することを検証する (Pub/Sub に再配信させる経路)。
func TestManager_writeDisplayMetaSnapshot_ReturnsErrorWhenAccountFails(t *testing.T) {
	srv := apiaccountserverfake.NewServer()
	defer srv.Close()
	srv.GetPlayerByIDFn = func(_ string) (int, any) {
		return http.StatusInternalServerError, nil
	}

	cache := displaymetacache.NewMemoryStore()
	m := &Manager{accountClient: accountclient.New(srv.URL()), displayCache: cache}

	err := m.writeDisplayMetaSnapshot(context.Background(), "g1", 1, "p-1")
	assert.Error(t, err)
}

// TestManager_writeDisplayMetaSnapshot_SkipsWriteWhenNameNil は account 応答に name が
// 含まれない場合 (onboarding 未完了等の異常) に snapshot 書き込みをスキップし、handler は
// 継続 (nil 返却) することを検証する。フォールバック表示値の書き込みは relay 経路の
// 最終行に集約する設計のため、ここでは何も書かない。
func TestManager_writeDisplayMetaSnapshot_SkipsWriteWhenNameNil(t *testing.T) {
	srv := apiaccountserverfake.NewServer()
	defer srv.Close()
	srv.GetPlayerByIDFn = func(_ string) (int, any) {
		return http.StatusOK, apiaccount.PlayerResponse{PlayerID: "p-1", Name: nil, Level: 7}
	}

	cache := displaymetacache.NewMemoryStore()
	m := &Manager{accountClient: accountclient.New(srv.URL()), displayCache: cache}
	ctx := context.Background()

	require.NoError(t, m.writeDisplayMetaSnapshot(ctx, "g1", 1, "abc123def456"))

	_, err := cache.Get(ctx, "g1", 1)
	require.ErrorIs(t, err, port.ErrNotFound)
}

// TestManager_writeDisplayMetaSnapshot_ContinuesWhenCachePutFails は cache 書き込み失敗が
// handler を停止させないことを検証する (試合継続のため、relay 経路で必要時に再 lookup する設計)。
func TestManager_writeDisplayMetaSnapshot_ContinuesWhenCachePutFails(t *testing.T) {
	srv := apiaccountserverfake.NewServer()
	defer srv.Close()
	srv.GetPlayerByIDFn = func(_ string) (int, any) {
		name := "alice"
		return http.StatusOK, apiaccount.PlayerResponse{PlayerID: "p-1", Name: &name, Level: 7}
	}

	cache := &failingPutCache{MemoryStore: displaymetacache.NewMemoryStore()}
	m := &Manager{accountClient: accountclient.New(srv.URL()), displayCache: cache}

	require.NoError(t, m.writeDisplayMetaSnapshot(context.Background(), "g1", 1, "p-1"))
}

// TestManager_writeDisplayMetaSnapshot_SkipsWhenCacheNil は cache 未注入 (test / mock モード) で
// no-op として成功することを検証する。
func TestManager_writeDisplayMetaSnapshot_SkipsWhenCacheNil(t *testing.T) {
	m := &Manager{accountClient: accountclient.New("http://invalid.local")}

	require.NoError(t, m.writeDisplayMetaSnapshot(context.Background(), "g1", 1, "p-1"))
}

package displaymetacache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// newTestRedisStore は miniredis backed の RedisStore を構築する。
// FastForward で TTL 検証を行うためテスト側で miniredis 本体も返す。
func newTestRedisStore(t *testing.T) (*RedisStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedisStore(client), mr
}

// TestRedisStore_Put_StoresHashWithTTL は「Hash key =
// game:{game_id}:player:{player_num}, fields = {name, level}, TTL = 1h」を
// Put が満たすことを固定する。
func TestRedisStore_Put_StoresHashWithTTL(t *testing.T) {
	store, mr := newTestRedisStore(t)
	ctx := context.Background()

	require.NoError(t, store.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 7}))

	key := "game:g1:player:1"
	assert.True(t, mr.Exists(key))
	assert.Equal(t, "alice", mr.HGet(key, "name"))
	assert.Equal(t, "7", mr.HGet(key, "level"))
	assert.Equal(t, time.Hour, mr.TTL(key))
}

// TestRedisStore_Get_ReturnsStoredMeta は Put 直後に Get が同じ
// name / level を返すことを固定する (正常系の往復)。
func TestRedisStore_Get_ReturnsStoredMeta(t *testing.T) {
	store, _ := newTestRedisStore(t)
	ctx := context.Background()

	require.NoError(t, store.Put(ctx, "g1", 2, port.DisplayMeta{Name: "bob", Level: 12}))

	got, err := store.Get(ctx, "g1", 2)
	require.NoError(t, err)
	assert.Equal(t, port.DisplayMeta{Name: "bob", Level: 12}, got)
}

// TestRedisStore_Get_MissReturnsNotFound は key 未書き込み時に
// port.ErrNotFound が返り、空文字や level=0 などの silent fallback を
// 行わないことを固定する。
func TestRedisStore_Get_MissReturnsNotFound(t *testing.T) {
	store, _ := newTestRedisStore(t)
	ctx := context.Background()

	_, err := store.Get(ctx, "g-missing", 1)
	assert.ErrorIs(t, err, port.ErrNotFound)
}

// TestRedisStore_Get_ExpiredKeyReturnsNotFound は TTL 経過後の key も
// not found 相当として扱われることを固定する (1h TTL が機能している)。
func TestRedisStore_Get_ExpiredKeyReturnsNotFound(t *testing.T) {
	store, mr := newTestRedisStore(t)
	ctx := context.Background()

	require.NoError(t, store.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 7}))
	mr.FastForward(snapshotTTL + time.Second)

	_, err := store.Get(ctx, "g1", 1)
	assert.ErrorIs(t, err, port.ErrNotFound)
}

// TestRedisStore_KeySeparatesGameAndPlayer は (gameID, playerNum) の組が
// 別 key にマッピングされ互いに干渉しないことを固定する (key 設計の事故防止)。
func TestRedisStore_KeySeparatesGameAndPlayer(t *testing.T) {
	store, _ := newTestRedisStore(t)
	ctx := context.Background()

	require.NoError(t, store.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 1}))
	require.NoError(t, store.Put(ctx, "g1", 2, port.DisplayMeta{Name: "bob", Level: 2}))
	require.NoError(t, store.Put(ctx, "g2", 1, port.DisplayMeta{Name: "carol", Level: 3}))

	cases := []struct {
		gameID    string
		playerNum int
		want      port.DisplayMeta
	}{
		{"g1", 1, port.DisplayMeta{Name: "alice", Level: 1}},
		{"g1", 2, port.DisplayMeta{Name: "bob", Level: 2}},
		{"g2", 1, port.DisplayMeta{Name: "carol", Level: 3}},
	}
	for _, c := range cases {
		got, err := store.Get(ctx, c.gameID, c.playerNum)
		require.NoError(t, err)
		assert.Equal(t, c.want, got)
	}
}

// TestRedisStore_InvalidKeyParts は空 gameID / 非正 playerNum が
// fail-fast で error になり Redis を呼ばないことを固定する。
func TestRedisStore_InvalidKeyParts(t *testing.T) {
	store, _ := newTestRedisStore(t)
	ctx := context.Background()
	meta := port.DisplayMeta{Name: "x", Level: 1}

	cases := []struct {
		name      string
		gameID    string
		playerNum int
	}{
		{"empty gameID", "", 1},
		{"zero playerNum", "g1", 0},
		{"negative playerNum", "g1", -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Error(t, store.Put(ctx, c.gameID, c.playerNum, meta))
			_, err := store.Get(ctx, c.gameID, c.playerNum)
			assert.Error(t, err)
			// not-found ではなく入力 validation エラーであること
			assert.False(t, errors.Is(err, port.ErrNotFound))
		})
	}
}

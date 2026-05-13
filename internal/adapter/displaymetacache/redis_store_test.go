package displaymetacache

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// newTestRedisStore は miniredis backed の RedisStore を構築する。
// TTL 検証のため miniredis 本体も返す。
func newTestRedisStore(t *testing.T) (*RedisStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedisStore(client), mr
}

func TestRedisStore_Put_WritesHashFieldsWithTTL(t *testing.T) {
	cases := []struct {
		name      string
		gameID    string
		playerNum int
		meta      port.DisplayMeta
	}{
		{
			name:      "single digit level",
			gameID:    "g1",
			playerNum: 1,
			meta:      port.DisplayMeta{Name: "alice", Level: 7},
		},
		{
			name:      "double digit level",
			gameID:    "g1",
			playerNum: 2,
			meta:      port.DisplayMeta{Name: "bob", Level: 42},
		},
		{
			name:      "japanese name",
			gameID:    "g-jp",
			playerNum: 1,
			meta:      port.DisplayMeta{Name: "山田太郎", Level: 1},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store, mr := newTestRedisStore(t)
			ctx := context.Background()

			require.NoError(t, store.Put(ctx, c.gameID, c.playerNum, c.meta))

			key := fmt.Sprintf("game:%s:player:%d", c.gameID, c.playerNum)
			assert.True(t, mr.Exists(key))
			assert.Equal(t, c.meta.Name, mr.HGet(key, "name"))
			assert.Equal(t, strconv.Itoa(c.meta.Level), mr.HGet(key, "level"))
			assert.Equal(t, time.Hour, mr.TTL(key))
		})
	}
}

func TestRedisStore_Get_Success(t *testing.T) {
	cases := []struct {
		name      string
		gameID    string
		playerNum int
		meta      port.DisplayMeta
	}{
		{
			name:      "small level",
			gameID:    "g1",
			playerNum: 2,
			meta:      port.DisplayMeta{Name: "bob", Level: 12},
		},
		{
			name:      "zero level",
			gameID:    "g2",
			playerNum: 1,
			meta:      port.DisplayMeta{Name: "carol", Level: 0},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store, _ := newTestRedisStore(t)
			ctx := context.Background()
			require.NoError(t, store.Put(ctx, c.gameID, c.playerNum, c.meta))

			got, err := store.Get(ctx, c.gameID, c.playerNum)
			require.NoError(t, err)
			assert.Equal(t, c.meta, got)
		})
	}
}

func TestRedisStore_Get_NotFound(t *testing.T) {
	cases := []struct {
		name      string
		seed      func(t *testing.T, store *RedisStore, mr *miniredis.Miniredis, ctx context.Context)
		gameID    string
		playerNum int
	}{
		{
			name:      "key never written",
			seed:      func(t *testing.T, store *RedisStore, mr *miniredis.Miniredis, ctx context.Context) {},
			gameID:    "g-missing",
			playerNum: 1,
		},
		{
			name: "key expired after TTL",
			seed: func(t *testing.T, store *RedisStore, mr *miniredis.Miniredis, ctx context.Context) {
				require.NoError(t, store.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 7}))
				mr.FastForward(snapshotTTL + time.Second)
			},
			gameID:    "g1",
			playerNum: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store, mr := newTestRedisStore(t)
			ctx := context.Background()
			c.seed(t, store, mr, ctx)

			_, err := store.Get(ctx, c.gameID, c.playerNum)
			assert.ErrorIs(t, err, port.ErrNotFound)
		})
	}
}

func TestRedisStore_Get_KeysSeparatedByGameAndPlayer(t *testing.T) {
	store, _ := newTestRedisStore(t)
	ctx := context.Background()

	seeds := []struct {
		gameID    string
		playerNum int
		meta      port.DisplayMeta
	}{
		{
			gameID:    "g1",
			playerNum: 1,
			meta:      port.DisplayMeta{Name: "alice", Level: 1},
		},
		{
			gameID:    "g1",
			playerNum: 2,
			meta:      port.DisplayMeta{Name: "bob", Level: 2},
		},
		{
			gameID:    "g2",
			playerNum: 1,
			meta:      port.DisplayMeta{Name: "carol", Level: 3},
		},
	}
	for _, s := range seeds {
		require.NoError(t, store.Put(ctx, s.gameID, s.playerNum, s.meta))
	}

	cases := []struct {
		name      string
		gameID    string
		playerNum int
		want      port.DisplayMeta
	}{
		{
			name:      "g1/p1 retrieves alice",
			gameID:    "g1",
			playerNum: 1,
			want:      port.DisplayMeta{Name: "alice", Level: 1},
		},
		{
			name:      "g1/p2 retrieves bob",
			gameID:    "g1",
			playerNum: 2,
			want:      port.DisplayMeta{Name: "bob", Level: 2},
		},
		{
			name:      "g2/p1 retrieves carol",
			gameID:    "g2",
			playerNum: 1,
			want:      port.DisplayMeta{Name: "carol", Level: 3},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := store.Get(ctx, c.gameID, c.playerNum)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestRedisStore_Put_InvalidKeyParts(t *testing.T) {
	cases := []struct {
		name      string
		gameID    string
		playerNum int
	}{
		{
			name:      "empty gameID",
			gameID:    "",
			playerNum: 1,
		},
		{
			name:      "zero playerNum",
			gameID:    "g1",
			playerNum: 0,
		},
		{
			name:      "negative playerNum",
			gameID:    "g1",
			playerNum: -1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store, _ := newTestRedisStore(t)
			ctx := context.Background()
			meta := port.DisplayMeta{Name: "x", Level: 1}

			assert.Error(t, store.Put(ctx, c.gameID, c.playerNum, meta))
		})
	}
}

func TestRedisStore_Get_InvalidKeyParts(t *testing.T) {
	cases := []struct {
		name      string
		gameID    string
		playerNum int
	}{
		{
			name:      "empty gameID",
			gameID:    "",
			playerNum: 1,
		},
		{
			name:      "zero playerNum",
			gameID:    "g1",
			playerNum: 0,
		},
		{
			name:      "negative playerNum",
			gameID:    "g1",
			playerNum: -1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store, _ := newTestRedisStore(t)
			ctx := context.Background()

			_, err := store.Get(ctx, c.gameID, c.playerNum)
			require.Error(t, err)
			assert.False(t, errors.Is(err, port.ErrNotFound))
		})
	}
}

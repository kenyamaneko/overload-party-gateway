package displaymetacache

import (
	"context"
	"errors"
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
func newTestRedisStore(t *testing.T) (*RedisStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedisStore(client), mr
}

func TestRedisStore_Put_Hash項目とTTLを書き込む(t *testing.T) {
	cases := []struct {
		name string
		meta port.DisplayMeta
	}{
		{
			name: "通常レベル",
			meta: port.DisplayMeta{Name: "alice", Level: 7},
		},
		{
			name: "レベル0",
			meta: port.DisplayMeta{Name: "bob", Level: 0},
		},
		{
			name: "日本語名",
			meta: port.DisplayMeta{Name: "山田太郎", Level: 99},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store, mr := newTestRedisStore(t)
			ctx := context.Background()
			require.NoError(t, store.Put(ctx, "g1", 1, c.meta))

			assert.Equal(t, c.meta.Name, mr.HGet("game:g1:player:1", "name"))
			assert.Equal(t, strconv.Itoa(c.meta.Level), mr.HGet("game:g1:player:1", "level"))
			assert.Equal(t, time.Hour, mr.TTL("game:g1:player:1"))
		})
	}
}

func TestRedisStore_Put_既存keyを上書きする(t *testing.T) {
	cases := []struct {
		name      string
		first     port.DisplayMeta
		overwrite port.DisplayMeta
	}{
		{
			name:      "nameとlevelの両方を変更",
			first:     port.DisplayMeta{Name: "alice", Level: 1},
			overwrite: port.DisplayMeta{Name: "alice2", Level: 9},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store, _ := newTestRedisStore(t)
			ctx := context.Background()
			require.NoError(t, store.Put(ctx, "g1", 1, c.first))
			require.NoError(t, store.Put(ctx, "g1", 1, c.overwrite))

			got, err := store.Get(ctx, "g1", 1)
			require.NoError(t, err)
			assert.Equal(t, c.overwrite, got)
		})
	}
}

func TestRedisStore_Get_書き込んだメタを返す(t *testing.T) {
	cases := []struct {
		name string
		meta port.DisplayMeta
	}{
		{
			name: "通常レベル",
			meta: port.DisplayMeta{Name: "alice", Level: 7},
		},
		{
			name: "レベル0",
			meta: port.DisplayMeta{Name: "bob", Level: 0},
		},
		{
			name: "日本語名",
			meta: port.DisplayMeta{Name: "山田太郎", Level: 99},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store, _ := newTestRedisStore(t)
			ctx := context.Background()
			require.NoError(t, store.Put(ctx, "g1", 1, c.meta))

			got, err := store.Get(ctx, "g1", 1)
			require.NoError(t, err)
			assert.Equal(t, c.meta, got)
		})
	}
}

func TestRedisStore_Get_書き込みがない場合はErrNotFoundを返す(t *testing.T) {
	cases := []struct {
		name      string
		gameID    string
		playerNum int
	}{
		{
			name:      "playerNum1",
			gameID:    "g1",
			playerNum: 1,
		},
		{
			name:      "playerNum2",
			gameID:    "g1",
			playerNum: 2,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store, _ := newTestRedisStore(t)
			_, err := store.Get(context.Background(), c.gameID, c.playerNum)
			assert.ErrorIs(t, err, port.ErrNotFound)
		})
	}
}

func TestRedisStore_Get_TTL経過後はErrNotFoundを返す(t *testing.T) {
	cases := []struct {
		name    string
		advance time.Duration
	}{
		{
			name:    "TTLの1秒後",
			advance: snapshotTTL + time.Second,
		},
		{
			name:    "TTLの1時間後",
			advance: snapshotTTL + time.Hour,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store, mr := newTestRedisStore(t)
			ctx := context.Background()
			require.NoError(t, store.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 7}))
			mr.FastForward(c.advance)

			_, err := store.Get(ctx, "g1", 1)
			assert.ErrorIs(t, err, port.ErrNotFound)
		})
	}
}

func TestRedisStore_Get_gameIDとplayerNumで別keyに分離される(t *testing.T) {
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
			name:      "g1のplayer1はaliceを返す",
			gameID:    "g1",
			playerNum: 1,
			want:      port.DisplayMeta{Name: "alice", Level: 1},
		},
		{
			name:      "g1のplayer2はbobを返す",
			gameID:    "g1",
			playerNum: 2,
			want:      port.DisplayMeta{Name: "bob", Level: 2},
		},
		{
			name:      "g2のplayer1はcarolを返す",
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

func TestRedisStore_Put_入力検証エラーを返す(t *testing.T) {
	cases := []struct {
		name      string
		gameID    string
		playerNum int
	}{
		{
			name:      "空のgameID",
			gameID:    "",
			playerNum: 1,
		},
		{
			name:      "playerNumが0",
			gameID:    "g1",
			playerNum: 0,
		},
		{
			name:      "playerNumが負",
			gameID:    "g1",
			playerNum: -1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store, _ := newTestRedisStore(t)
			err := store.Put(context.Background(), c.gameID, c.playerNum, port.DisplayMeta{Name: "x", Level: 1})
			assert.Error(t, err)
		})
	}
}

func TestRedisStore_Get_入力検証エラーを返す(t *testing.T) {
	cases := []struct {
		name      string
		gameID    string
		playerNum int
	}{
		{
			name:      "空のgameID",
			gameID:    "",
			playerNum: 1,
		},
		{
			name:      "playerNumが0",
			gameID:    "g1",
			playerNum: 0,
		},
		{
			name:      "playerNumが負",
			gameID:    "g1",
			playerNum: -1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store, _ := newTestRedisStore(t)
			_, err := store.Get(context.Background(), c.gameID, c.playerNum)
			require.Error(t, err)
			assert.False(t, errors.Is(err, port.ErrNotFound))
		})
	}
}

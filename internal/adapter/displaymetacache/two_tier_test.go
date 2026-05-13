package displaymetacache

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// newTestTwoTier は MemoryStore (L1) + miniredis backed RedisStore (L2) を合成した TwoTier を構築する。
func newTestTwoTier(t *testing.T) (*TwoTier, *MemoryStore, *RedisStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	l1 := NewMemoryStore()
	l2 := NewRedisStore(client)
	return New(l1, l2), l1, l2, mr
}

func TestTwoTier_Put_L1とL2の両方に書き込む(t *testing.T) {
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
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tier, l1, l2, _ := newTestTwoTier(t)
			ctx := context.Background()
			require.NoError(t, tier.Put(ctx, "g1", 1, c.meta))

			gotL1, err := l1.Get(ctx, "g1", 1)
			require.NoError(t, err)
			assert.Equal(t, c.meta, gotL1)

			gotL2, err := l2.Get(ctx, "g1", 1)
			require.NoError(t, err)
			assert.Equal(t, c.meta, gotL2)
		})
	}
}

func TestTwoTier_Put_L2失敗時にL1を巻き戻す(t *testing.T) {
	cases := []struct {
		name string
		meta port.DisplayMeta
	}{
		{
			name: "通常レベル",
			meta: port.DisplayMeta{Name: "alice", Level: 7},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tier, l1, _, mr := newTestTwoTier(t)
			ctx := context.Background()
			mr.Close()

			err := tier.Put(ctx, "g1", 1, c.meta)
			require.Error(t, err)

			_, getErr := l1.Get(ctx, "g1", 1)
			assert.ErrorIs(t, getErr, port.ErrNotFound)
		})
	}
}

func TestTwoTier_Get_L1にあればL2を見ずに返す(t *testing.T) {
	cases := []struct {
		name string
		meta port.DisplayMeta
	}{
		{
			name: "通常レベル",
			meta: port.DisplayMeta{Name: "alice", Level: 7},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tier, l1, _, mr := newTestTwoTier(t)
			ctx := context.Background()
			require.NoError(t, l1.Put(ctx, "g1", 1, c.meta))
			mr.Close() // L2 が停止しても影響を受けないこと

			got, err := tier.Get(ctx, "g1", 1)
			require.NoError(t, err)
			assert.Equal(t, c.meta, got)
		})
	}
}

func TestTwoTier_Get_L1になくL2にあればL1に昇格して返す(t *testing.T) {
	cases := []struct {
		name string
		meta port.DisplayMeta
	}{
		{
			name: "通常レベル",
			meta: port.DisplayMeta{Name: "alice", Level: 7},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tier, l1, l2, _ := newTestTwoTier(t)
			ctx := context.Background()
			require.NoError(t, l2.Put(ctx, "g1", 1, c.meta))

			got, err := tier.Get(ctx, "g1", 1)
			require.NoError(t, err)
			assert.Equal(t, c.meta, got)

			gotL1, err := l1.Get(ctx, "g1", 1)
			require.NoError(t, err)
			assert.Equal(t, c.meta, gotL1)
		})
	}
}

func TestTwoTier_Get_両方になければErrNotFoundを返す(t *testing.T) {
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
			tier, _, _, _ := newTestTwoTier(t)
			_, err := tier.Get(context.Background(), c.gameID, c.playerNum)
			assert.ErrorIs(t, err, port.ErrNotFound)
		})
	}
}

func TestTwoTier_Get_L1のNotFound以外のエラーをそのまま伝播する(t *testing.T) {
	cases := []struct {
		name    string
		l1Error error
	}{
		{
			name:    "汎用エラー",
			l1Error: errors.New("l1 boom"),
		},
		{
			name:    "接続エラー",
			l1Error: errors.New("l1 connection lost"),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mr := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			tier := New(&stubL1{getErr: c.l1Error}, NewRedisStore(client))

			_, err := tier.Get(context.Background(), "g1", 1)
			require.Error(t, err)
			assert.NotErrorIs(t, err, port.ErrNotFound)
		})
	}
}

func TestTwoTier_Get_L1昇格失敗をそのまま伝播する(t *testing.T) {
	cases := []struct {
		name       string
		promoteErr error
	}{
		{
			name:       "昇格時にエラー",
			promoteErr: errors.New("l1 promote boom"),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mr := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			l2 := NewRedisStore(client)
			ctx := context.Background()
			require.NoError(t, l2.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 7}))
			tier := New(&stubL1{getErr: port.ErrNotFound, putErr: c.promoteErr}, l2)

			_, err := tier.Get(ctx, "g1", 1)
			require.Error(t, err)
			assert.NotErrorIs(t, err, port.ErrNotFound)
		})
	}
}

// stubL1 は l1Tier interface を満たすテスト用 store。
type stubL1 struct {
	getErr error
	putErr error
}

func (s *stubL1) Put(_ context.Context, _ string, _ int, _ port.DisplayMeta) error {
	return s.putErr
}

func (s *stubL1) Get(_ context.Context, _ string, _ int) (port.DisplayMeta, error) {
	return port.DisplayMeta{}, s.getErr
}

func (s *stubL1) Evict(_ context.Context, _ string, _ int) error {
	return nil
}

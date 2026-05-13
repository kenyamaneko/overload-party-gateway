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

// newTestTwoTier は MemoryStore (L1) + miniredis backed RedisStore (L2) を
// 合成した TwoTier を構築する。
func newTestTwoTier(t *testing.T) (*TwoTier, *MemoryStore, *RedisStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	l1 := NewMemoryStore()
	l2 := NewRedisStore(client)
	return New(l1, l2), l1, l2, mr
}

func TestTwoTier_Put_WritesToBothLayers(t *testing.T) {
	cases := []struct {
		name string
		meta port.DisplayMeta
	}{
		{
			name: "non-zero level",
			meta: port.DisplayMeta{Name: "alice", Level: 7},
		},
		{
			name: "zero level",
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

func TestTwoTier_Put_RollsBackL1OnL2Failure(t *testing.T) {
	cases := []struct {
		name      string
		breakL2   func(mr *miniredis.Miniredis)
		gameID    string
		playerNum int
		meta      port.DisplayMeta
	}{
		{
			name:      "L2 closed before Put",
			breakL2:   func(mr *miniredis.Miniredis) { mr.Close() },
			gameID:    "g1",
			playerNum: 1,
			meta:      port.DisplayMeta{Name: "alice", Level: 7},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tier, l1, _, mr := newTestTwoTier(t)
			ctx := context.Background()
			c.breakL2(mr)

			err := tier.Put(ctx, c.gameID, c.playerNum, c.meta)
			require.Error(t, err)

			_, getErr := l1.Get(ctx, c.gameID, c.playerNum)
			assert.ErrorIs(t, getErr, port.ErrNotFound)
		})
	}
}

func TestTwoTier_Get_Success(t *testing.T) {
	cases := []struct {
		name           string
		seed           func(t *testing.T, l1 *MemoryStore, l2 *RedisStore, mr *miniredis.Miniredis, ctx context.Context)
		want           port.DisplayMeta
		verifyL1State  func(t *testing.T, l1 *MemoryStore, ctx context.Context, want port.DisplayMeta)
	}{
		{
			name: "L1 hit returns L1 value without touching L2",
			seed: func(t *testing.T, l1 *MemoryStore, l2 *RedisStore, mr *miniredis.Miniredis, ctx context.Context) {
				require.NoError(t, l1.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 7}))
				mr.Close()
			},
			want: port.DisplayMeta{Name: "alice", Level: 7},
			verifyL1State: func(t *testing.T, l1 *MemoryStore, ctx context.Context, want port.DisplayMeta) {
				got, err := l1.Get(ctx, "g1", 1)
				require.NoError(t, err)
				assert.Equal(t, want, got)
			},
		},
		{
			name: "L1 miss with L2 hit promotes value to L1",
			seed: func(t *testing.T, l1 *MemoryStore, l2 *RedisStore, mr *miniredis.Miniredis, ctx context.Context) {
				require.NoError(t, l2.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 7}))
			},
			want: port.DisplayMeta{Name: "alice", Level: 7},
			verifyL1State: func(t *testing.T, l1 *MemoryStore, ctx context.Context, want port.DisplayMeta) {
				got, err := l1.Get(ctx, "g1", 1)
				require.NoError(t, err)
				assert.Equal(t, want, got)
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tier, l1, l2, mr := newTestTwoTier(t)
			ctx := context.Background()
			c.seed(t, l1, l2, mr, ctx)

			got, err := tier.Get(ctx, "g1", 1)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)

			c.verifyL1State(t, l1, ctx, c.want)
		})
	}
}

func TestTwoTier_Get_NotFoundWhenBothLayersMiss(t *testing.T) {
	cases := []struct {
		name      string
		gameID    string
		playerNum int
	}{
		{
			name:      "player 1 missing",
			gameID:    "g-missing",
			playerNum: 1,
		},
		{
			name:      "player 2 missing",
			gameID:    "g-missing",
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

func TestTwoTier_Get_PropagatesL1Errors(t *testing.T) {
	cases := []struct {
		name string
		l1   *stubL1
	}{
		{
			name: "L1 Get returns non-not-found error",
			l1:   &stubL1{getErr: errors.New("l1 boom")},
		},
		{
			name: "L1 promote after L2 hit fails",
			l1: &stubL1{
				getErr: port.ErrNotFound,
				putErr: errors.New("l1 promote boom"),
			},
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

			tier := New(c.l1, l2)

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

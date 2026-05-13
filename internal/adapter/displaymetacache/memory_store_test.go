package displaymetacache

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

func TestMemoryStore_Get_Success(t *testing.T) {
	cases := []struct {
		name      string
		seed      func(t *testing.T, s *MemoryStore, ctx context.Context)
		gameID    string
		playerNum int
		want      port.DisplayMeta
	}{
		{
			name: "Put then Get returns stored snapshot",
			seed: func(t *testing.T, s *MemoryStore, ctx context.Context) {
				require.NoError(t, s.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 7}))
			},
			gameID:    "g1",
			playerNum: 1,
			want:      port.DisplayMeta{Name: "alice", Level: 7},
		},
		{
			name: "second Put overwrites previous snapshot",
			seed: func(t *testing.T, s *MemoryStore, ctx context.Context) {
				require.NoError(t, s.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 1}))
				require.NoError(t, s.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice2", Level: 9}))
			},
			gameID:    "g1",
			playerNum: 1,
			want:      port.DisplayMeta{Name: "alice2", Level: 9},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := NewMemoryStore()
			ctx := context.Background()
			c.seed(t, s, ctx)

			got, err := s.Get(ctx, c.gameID, c.playerNum)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestMemoryStore_Get_NotFound(t *testing.T) {
	cases := []struct {
		name      string
		seed      func(t *testing.T, s *MemoryStore, ctx context.Context)
		gameID    string
		playerNum int
	}{
		{
			name:      "without any Put",
			seed:      func(t *testing.T, s *MemoryStore, ctx context.Context) {},
			gameID:    "g-missing",
			playerNum: 1,
		},
		{
			name: "after Evict of the same key",
			seed: func(t *testing.T, s *MemoryStore, ctx context.Context) {
				require.NoError(t, s.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 7}))
				require.NoError(t, s.Evict(ctx, "g1", 1))
			},
			gameID:    "g1",
			playerNum: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := NewMemoryStore()
			ctx := context.Background()
			c.seed(t, s, ctx)

			_, err := s.Get(ctx, c.gameID, c.playerNum)
			assert.ErrorIs(t, err, port.ErrNotFound)
		})
	}
}

func TestMemoryStore_Evict_Idempotent(t *testing.T) {
	cases := []struct {
		name string
		seed func(t *testing.T, s *MemoryStore, ctx context.Context)
	}{
		{
			name: "evict without prior Put",
			seed: func(t *testing.T, s *MemoryStore, ctx context.Context) {},
		},
		{
			name: "second evict after a successful one",
			seed: func(t *testing.T, s *MemoryStore, ctx context.Context) {
				require.NoError(t, s.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 7}))
				require.NoError(t, s.Evict(ctx, "g1", 1))
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := NewMemoryStore()
			ctx := context.Background()
			c.seed(t, s, ctx)

			assert.NoError(t, s.Evict(ctx, "g1", 1))
		})
	}
}

func TestMemoryStore_Put_InvalidKeyParts(t *testing.T) {
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
			s := NewMemoryStore()
			ctx := context.Background()
			meta := port.DisplayMeta{Name: "x", Level: 1}

			assert.Error(t, s.Put(ctx, c.gameID, c.playerNum, meta))
		})
	}
}

func TestMemoryStore_Get_InvalidKeyParts(t *testing.T) {
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
			s := NewMemoryStore()
			ctx := context.Background()

			_, err := s.Get(ctx, c.gameID, c.playerNum)
			assert.Error(t, err)
		})
	}
}

func TestMemoryStore_Evict_InvalidKeyParts(t *testing.T) {
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
			s := NewMemoryStore()
			ctx := context.Background()

			assert.Error(t, s.Evict(ctx, c.gameID, c.playerNum))
		})
	}
}

package displaymetacache

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// TestMemoryStore_PutGetRoundTrip は Put した snapshot を直後の Get で
// 同値として取得できることを検証する。
func TestMemoryStore_PutGetRoundTrip(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	meta := port.DisplayMeta{Name: "alice", Level: 7}

	require.NoError(t, s.Put(ctx, "g1", 1, meta))

	got, err := s.Get(ctx, "g1", 1)
	require.NoError(t, err)
	require.Equal(t, meta, got)
}

// TestMemoryStore_PutOverwrites は同一 key への 2 回目の Put が後勝ちで
// 上書きされることを検証する (match_made の再配信で同 game に再書き込みされ得るため)。
func TestMemoryStore_PutOverwrites(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	require.NoError(t, s.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 1}))
	require.NoError(t, s.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 9}))

	got, err := s.Get(ctx, "g1", 1)
	require.NoError(t, err)
	require.Equal(t, port.DisplayMeta{Name: "alice", Level: 9}, got)
}

// TestMemoryStore_GetReturnsNotFoundWhenAbsent は未書き込み key への Get が
// port.ErrNotFound を返すことを検証する (空文字 / level=0 等の silent fallback 禁止)。
func TestMemoryStore_GetReturnsNotFoundWhenAbsent(t *testing.T) {
	s := NewMemoryStore()

	_, err := s.Get(context.Background(), "g1", 1)
	require.ErrorIs(t, err, port.ErrNotFound)
}

// TestMemoryStore_EvictRemovesStoredKey は Put 後に Evict すると以降の Get が
// port.ErrNotFound を返すことを検証する (TwoTier の L2 失敗時 rollback の支え)。
func TestMemoryStore_EvictRemovesStoredKey(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	require.NoError(t, s.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 7}))
	require.NoError(t, s.Evict(ctx, "g1", 1))

	_, err := s.Get(ctx, "g1", 1)
	require.ErrorIs(t, err, port.ErrNotFound)
}

// TestMemoryStore_EvictIsIdempotent は未書き込み key への Evict が error に
// ならないことを検証する (rollback 経路が同 key を再 Evict しても安全であるため)。
func TestMemoryStore_EvictIsIdempotent(t *testing.T) {
	s := NewMemoryStore()

	require.NoError(t, s.Evict(context.Background(), "g1", 1))
}

// TestMemoryStore_PutRejectsInvalidKeyParts は空 gameID / 非正 playerNum で
// Put が fail-fast に error を返すことを検証する。
func TestMemoryStore_PutRejectsInvalidKeyParts(t *testing.T) {
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
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			err := s.Put(context.Background(), tc.gameID, tc.playerNum, port.DisplayMeta{Name: "x", Level: 1})
			assert.Error(t, err)
		})
	}
}

// TestMemoryStore_GetRejectsInvalidKeyParts は空 gameID / 非正 playerNum で
// Get が fail-fast に error を返すことを検証する。
func TestMemoryStore_GetRejectsInvalidKeyParts(t *testing.T) {
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
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			_, err := s.Get(context.Background(), tc.gameID, tc.playerNum)
			assert.Error(t, err)
		})
	}
}

// TestMemoryStore_EvictRejectsInvalidKeyParts は空 gameID / 非正 playerNum で
// Evict が fail-fast に error を返すことを検証する。
func TestMemoryStore_EvictRejectsInvalidKeyParts(t *testing.T) {
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
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			err := s.Evict(context.Background(), tc.gameID, tc.playerNum)
			assert.Error(t, err)
		})
	}
}

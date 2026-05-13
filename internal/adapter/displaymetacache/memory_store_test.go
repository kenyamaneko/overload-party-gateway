package displaymetacache

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// TestMemoryStore_PutGetRoundTrip は Put 直後の Get が同 snapshot を返す
// 正常往復を固定する。
func TestMemoryStore_PutGetRoundTrip(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	require.NoError(t, s.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 7}))

	got, err := s.Get(ctx, "g1", 1)
	require.NoError(t, err)
	assert.Equal(t, port.DisplayMeta{Name: "alice", Level: 7}, got)
}

// TestMemoryStore_GetMissReturnsNotFound は未書き込み key で
// port.ErrNotFound を返すことを固定する (silent fallback 禁止)。
func TestMemoryStore_GetMissReturnsNotFound(t *testing.T) {
	s := NewMemoryStore()
	_, err := s.Get(context.Background(), "g-missing", 1)
	assert.ErrorIs(t, err, port.ErrNotFound)
}

// TestMemoryStore_PutOverwrite は同 key への Put が後勝ちで上書きされる
// ことを固定する (match_made を試合またぎで再書き込みする運用に対応)。
func TestMemoryStore_PutOverwrite(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	require.NoError(t, s.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 1}))
	require.NoError(t, s.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice2", Level: 9}))

	got, err := s.Get(ctx, "g1", 1)
	require.NoError(t, err)
	assert.Equal(t, port.DisplayMeta{Name: "alice2", Level: 9}, got)
}

// TestMemoryStore_Evict は Evict が L1 を巻き戻し、以降の Get が
// not found を返すことを固定する (TwoTier の L2 失敗時 rollback の支え)。
func TestMemoryStore_Evict(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()

	require.NoError(t, s.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 7}))
	require.NoError(t, s.Evict(ctx, "g1", 1))

	_, err := s.Get(ctx, "g1", 1)
	assert.ErrorIs(t, err, port.ErrNotFound)
}

// TestMemoryStore_EvictMissingKeyIsIdempotent は未書き込み key への Evict が
// error にならないことを固定する (再 Evict / 既に消えた key への呼び出しが
// no-op に倒れる)。
func TestMemoryStore_EvictMissingKeyIsIdempotent(t *testing.T) {
	s := NewMemoryStore()
	assert.NoError(t, s.Evict(context.Background(), "g-missing", 1))
}

// TestMemoryStore_InvalidKeyParts は空 gameID / 非正 playerNum で
// fail-fast する仕様を固定する。
func TestMemoryStore_InvalidKeyParts(t *testing.T) {
	s := NewMemoryStore()
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
			assert.Error(t, s.Put(ctx, c.gameID, c.playerNum, meta))
			_, err := s.Get(ctx, c.gameID, c.playerNum)
			assert.Error(t, err)
		})
	}
}

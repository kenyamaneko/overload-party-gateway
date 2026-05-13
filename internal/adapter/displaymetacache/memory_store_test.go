package displaymetacache

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// TestMemoryStore_Put_StoresLatestValue は Put 後の Get が最後に Put した値を
// 返すことを検証する。複数回 Put した場合の後勝ち挙動は match_made の再配信で
// 同 game に再書き込みされ得るため要件である。
func TestMemoryStore_Put_StoresLatestValue(t *testing.T) {
	cases := []struct {
		name string
		puts []port.DisplayMeta
		want port.DisplayMeta
	}{
		{
			name: "1 回 Put すると書き込んだ値が取得できる",
			puts: []port.DisplayMeta{{Name: "alice", Level: 7}},
			want: port.DisplayMeta{Name: "alice", Level: 7},
		},
		{
			name: "複数回 Put すると最後の値が取得できる",
			puts: []port.DisplayMeta{
				{Name: "alice", Level: 1},
				{Name: "alice", Level: 9},
			},
			want: port.DisplayMeta{Name: "alice", Level: 9},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			ctx := context.Background()
			for _, m := range tc.puts {
				require.NoError(t, s.Put(ctx, "g1", 1, m))
			}
			got, err := s.Get(ctx, "g1", 1)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// TestMemoryStore_Get_ReturnsNotFoundWhenAbsent は未書き込み key への Get が
// port.ErrNotFound を返すことを検証する (空文字 / level=0 等の silent fallback 禁止)。
func TestMemoryStore_Get_ReturnsNotFoundWhenAbsent(t *testing.T) {
	s := NewMemoryStore()

	_, err := s.Get(context.Background(), "g1", 1)
	require.ErrorIs(t, err, port.ErrNotFound)
}

// TestMemoryStore_Evict_LeavesKeyAbsent は事前状態を問わず Evict が error を
// 返さず、以降の Get が port.ErrNotFound を返すことを検証する。未書き込み key
// への idempotent 性は TwoTier の L2 失敗時 rollback が同 key を再 Evict しても
// 安全である要件のために必要である。
func TestMemoryStore_Evict_LeavesKeyAbsent(t *testing.T) {
	cases := []struct {
		name string
		puts []port.DisplayMeta
	}{
		{
			name: "書き込み済み key を Evict すると以降の Get が NotFound を返す",
			puts: []port.DisplayMeta{{Name: "alice", Level: 7}},
		},
		{
			name: "未書き込み key への Evict は idempotent に成功し以降の Get も NotFound を返す",
			puts: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMemoryStore()
			ctx := context.Background()
			for _, m := range tc.puts {
				require.NoError(t, s.Put(ctx, "g1", 1, m))
			}
			require.NoError(t, s.Evict(ctx, "g1", 1))
			_, err := s.Get(ctx, "g1", 1)
			require.ErrorIs(t, err, port.ErrNotFound)
		})
	}
}

// TestMemoryStore_Put_RejectsInvalidKeyParts は空 gameID / 非正 playerNum で
// Put が fail-fast に error を返すことを検証する。
func TestMemoryStore_Put_RejectsInvalidKeyParts(t *testing.T) {
	cases := []struct {
		name      string
		gameID    string
		playerNum int
	}{
		{
			name:      "空の gameID では error を返す",
			gameID:    "",
			playerNum: 1,
		},
		{
			name:      "playerNum が 0 では error を返す",
			gameID:    "g1",
			playerNum: 0,
		},
		{
			name:      "playerNum が負の値では error を返す",
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

// TestMemoryStore_Get_RejectsInvalidKeyParts は空 gameID / 非正 playerNum で
// Get が fail-fast に error を返すことを検証する。
func TestMemoryStore_Get_RejectsInvalidKeyParts(t *testing.T) {
	cases := []struct {
		name      string
		gameID    string
		playerNum int
	}{
		{
			name:      "空の gameID では error を返す",
			gameID:    "",
			playerNum: 1,
		},
		{
			name:      "playerNum が 0 では error を返す",
			gameID:    "g1",
			playerNum: 0,
		},
		{
			name:      "playerNum が負の値では error を返す",
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

// TestMemoryStore_Evict_RejectsInvalidKeyParts は空 gameID / 非正 playerNum で
// Evict が fail-fast に error を返すことを検証する。
func TestMemoryStore_Evict_RejectsInvalidKeyParts(t *testing.T) {
	cases := []struct {
		name      string
		gameID    string
		playerNum int
	}{
		{
			name:      "空の gameID では error を返す",
			gameID:    "",
			playerNum: 1,
		},
		{
			name:      "playerNum が 0 では error を返す",
			gameID:    "g1",
			playerNum: 0,
		},
		{
			name:      "playerNum が負の値では error を返す",
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

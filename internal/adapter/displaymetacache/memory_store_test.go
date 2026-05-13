package displaymetacache

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

func TestMemoryStore_Put_書き込んだメタをGetで取得できる(t *testing.T) {
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
			s := NewMemoryStore()
			ctx := context.Background()
			require.NoError(t, s.Put(ctx, "g1", 1, c.meta))

			got, err := s.Get(ctx, "g1", 1)
			require.NoError(t, err)
			assert.Equal(t, c.meta, got)
		})
	}
}

func TestMemoryStore_Put_既存keyを上書きする(t *testing.T) {
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
		{
			name:      "levelのみ変更",
			first:     port.DisplayMeta{Name: "alice", Level: 1},
			overwrite: port.DisplayMeta{Name: "alice", Level: 9},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := NewMemoryStore()
			ctx := context.Background()
			require.NoError(t, s.Put(ctx, "g1", 1, c.first))
			require.NoError(t, s.Put(ctx, "g1", 1, c.overwrite))

			got, err := s.Get(ctx, "g1", 1)
			require.NoError(t, err)
			assert.Equal(t, c.overwrite, got)
		})
	}
}

func TestMemoryStore_Get_書き込みがない場合はErrNotFoundを返す(t *testing.T) {
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
			s := NewMemoryStore()
			_, err := s.Get(context.Background(), c.gameID, c.playerNum)
			assert.ErrorIs(t, err, port.ErrNotFound)
		})
	}
}

func TestMemoryStore_Evict_書き込み済みkeyを削除する(t *testing.T) {
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
			s := NewMemoryStore()
			ctx := context.Background()
			require.NoError(t, s.Put(ctx, "g1", 1, c.meta))
			require.NoError(t, s.Evict(ctx, "g1", 1))

			_, err := s.Get(ctx, "g1", 1)
			assert.ErrorIs(t, err, port.ErrNotFound)
		})
	}
}

func TestMemoryStore_Evict_未書き込みkeyに対しても成功する(t *testing.T) {
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
			s := NewMemoryStore()
			assert.NoError(t, s.Evict(context.Background(), c.gameID, c.playerNum))
		})
	}
}

func TestMemoryStore_Put_入力検証エラーを返す(t *testing.T) {
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
			s := NewMemoryStore()
			err := s.Put(context.Background(), c.gameID, c.playerNum, port.DisplayMeta{Name: "x", Level: 1})
			assert.Error(t, err)
		})
	}
}

func TestMemoryStore_Get_入力検証エラーを返す(t *testing.T) {
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
			s := NewMemoryStore()
			_, err := s.Get(context.Background(), c.gameID, c.playerNum)
			assert.Error(t, err)
		})
	}
}

func TestMemoryStore_Evict_入力検証エラーを返す(t *testing.T) {
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
			s := NewMemoryStore()
			err := s.Evict(context.Background(), c.gameID, c.playerNum)
			assert.Error(t, err)
		})
	}
}

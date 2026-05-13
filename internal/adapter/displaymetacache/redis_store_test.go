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

// newTestRedisStore は miniredis backed の RedisStore を構築する。TTL 検証の
// ためテスト側で miniredis 本体も返す。MaxRetries=-1 は障害注入 (miniredis.Close 後)
// で retry のログを抑制するため。
func newTestRedisStore(t *testing.T) (*RedisStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr:       mr.Addr(),
		MaxRetries: -1,
	})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedisStore(client), mr
}

// TestRedisStore_Put_WritesHashAndTTL は Put が指定 key に name / level の
// Hash field を書き、TTL = 1h を設定することを検証する (key 規約 + 試合終了で
// 自然消滅させる TTL 設計の担保)。
func TestRedisStore_Put_WritesHashAndTTL(t *testing.T) {
	store, mr := newTestRedisStore(t)
	ctx := context.Background()

	require.NoError(t, store.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 7}))

	key := "game:g1:player:1"
	require.True(t, mr.Exists(key))
	require.Equal(t, "alice", mr.HGet(key, "name"))
	require.Equal(t, "7", mr.HGet(key, "level"))
	require.Equal(t, time.Hour, mr.TTL(key))
}

// TestRedisStore_Put_Overwrites は同一 key への 2 回目の Put が後勝ちで
// 上書きされ、Get で最新値が返ることを検証する。
func TestRedisStore_Put_Overwrites(t *testing.T) {
	store, _ := newTestRedisStore(t)
	ctx := context.Background()

	require.NoError(t, store.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 1}))
	require.NoError(t, store.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 9}))

	got, err := store.Get(ctx, "g1", 1)
	require.NoError(t, err)
	require.Equal(t, port.DisplayMeta{Name: "alice", Level: 9}, got)
}

// TestRedisStore_Get_ReturnsStoredMeta は Put 直後の Get が同じ name / level を
// 返すことを検証する (HSet と HGetAll の往復が parse 含めて整合していること)。
func TestRedisStore_Get_ReturnsStoredMeta(t *testing.T) {
	store, _ := newTestRedisStore(t)
	ctx := context.Background()
	meta := port.DisplayMeta{Name: "bob", Level: 12}

	require.NoError(t, store.Put(ctx, "g1", 2, meta))

	got, err := store.Get(ctx, "g1", 2)
	require.NoError(t, err)
	require.Equal(t, meta, got)
}

// TestRedisStore_Get_ReturnsNotFoundWhenAbsent は未書き込み key への Get が
// port.ErrNotFound を返すことを検証する (空文字 / level=0 等の silent fallback 禁止)。
func TestRedisStore_Get_ReturnsNotFoundWhenAbsent(t *testing.T) {
	store, _ := newTestRedisStore(t)

	_, err := store.Get(context.Background(), "g1", 1)
	require.ErrorIs(t, err, port.ErrNotFound)
}

// TestRedisStore_Get_ReturnsNotFoundAfterTTLExpired は TTL 経過後の Get が
// port.ErrNotFound を返すことを検証する (1h TTL が機能し、試合終了後に
// 自然消滅する設計の担保)。
func TestRedisStore_Get_ReturnsNotFoundAfterTTLExpired(t *testing.T) {
	store, mr := newTestRedisStore(t)
	ctx := context.Background()

	require.NoError(t, store.Put(ctx, "g1", 1, port.DisplayMeta{Name: "alice", Level: 7}))
	mr.FastForward(snapshotTTL + time.Second)

	_, err := store.Get(ctx, "g1", 1)
	require.ErrorIs(t, err, port.ErrNotFound)
}

// TestRedisStore_Get_SeparatesKeysByGameAndPlayer は (gameID, playerNum) の
// 各次元で key が分離され、別 (gameID, playerNum) に書き込んだ distractor が
// target の Get に干渉しないことを検証する (同時進行ゲームや同 game 内の
// player1 / player2 が混ざらないこと)。
func TestRedisStore_Get_SeparatesKeysByGameAndPlayer(t *testing.T) {
	target := port.DisplayMeta{Name: "alice", Level: 1}
	cases := []struct {
		name                string
		distractorGameID    string
		distractorPlayerNum int
		distractor          port.DisplayMeta
	}{
		{
			name:                "playerNum 違いは別 key に分離される",
			distractorGameID:    "g1",
			distractorPlayerNum: 2,
			distractor:          port.DisplayMeta{Name: "bob", Level: 2},
		},
		{
			name:                "gameID 違いは別 key に分離される",
			distractorGameID:    "g2",
			distractorPlayerNum: 1,
			distractor:          port.DisplayMeta{Name: "carol", Level: 3},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := newTestRedisStore(t)
			ctx := context.Background()
			require.NoError(t, store.Put(ctx, "g1", 1, target))
			require.NoError(t, store.Put(ctx, tc.distractorGameID, tc.distractorPlayerNum, tc.distractor))

			got, err := store.Get(ctx, "g1", 1)
			require.NoError(t, err)
			require.Equal(t, target, got)
		})
	}
}

// TestRedisStore_Put_RejectsInvalidKeyParts は空 gameID / 非正 playerNum で
// Put が fail-fast に error を返し Redis を呼ばないことを検証する。
func TestRedisStore_Put_RejectsInvalidKeyParts(t *testing.T) {
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
			store, _ := newTestRedisStore(t)
			err := store.Put(context.Background(), tc.gameID, tc.playerNum, port.DisplayMeta{Name: "x", Level: 1})
			assert.Error(t, err)
		})
	}
}

// TestRedisStore_Get_RejectsInvalidKeyParts は空 gameID / 非正 playerNum で
// Get が fail-fast に error を返し、入力検証 error と not-found を区別することを
// 検証する (silent fallback と取り違えないため)。
func TestRedisStore_Get_RejectsInvalidKeyParts(t *testing.T) {
	cases := []struct {
		name      string
		gameID    string
		playerNum int
	}{
		{
			name:      "空の gameID では not-found ではない error を返す",
			gameID:    "",
			playerNum: 1,
		},
		{
			name:      "playerNum が 0 では not-found ではない error を返す",
			gameID:    "g1",
			playerNum: 0,
		},
		{
			name:      "playerNum が負の値では not-found ではない error を返す",
			gameID:    "g1",
			playerNum: -1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := newTestRedisStore(t)
			_, err := store.Get(context.Background(), tc.gameID, tc.playerNum)
			require.Error(t, err)
			assert.False(t, errors.Is(err, port.ErrNotFound))
		})
	}
}

//go:build integration

package redistimer

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/valkeytest"
)

// testRedisURL は TestMain が起動した Valkey container の接続 URL。
var testRedisURL string

// TestMain はパッケージ内の結合テスト全体で共有する Valkey container を起動する。
func TestMain(m *testing.M) {
	ctx := context.Background()
	vk, err := valkeytest.New(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "valkeytest start: %v\n", err)
		os.Exit(1)
	}
	testRedisURL = vk.URL()
	code := m.Run()
	if err := vk.Terminate(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "valkeytest terminate: %v\n", err)
	}
	os.Exit(code)
}

// newTestStore は container に接続した新しい *Store を、DB を空にした上で返す。
// 各テストケースの分離に使う。
func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, client := newFreshStore(t)
	require.NoError(t, client.FlushDB(context.Background()).Err())
	return store
}

// newFreshStore は container に接続した、DB の状態を引き継がない新しい *Store を返す。
// 別プロセスから読み出す状況 (インメモリ状態を共有しない書き込み側・読み出し側の分離) を
// 再現するために使う。DB は空にしないため、既存データを検証する側で使う。
func newFreshStore(t *testing.T) (*Store, *redis.Client) {
	t.Helper()
	opt, err := redis.ParseURL(testRedisURL)
	require.NoError(t, err)
	client := redis.NewClient(opt)
	require.NoError(t, client.Ping(context.Background()).Err())
	t.Cleanup(func() { _ = client.Close() })
	return NewStore(client), client
}

func TestDisconnectDeadline(t *testing.T) {
	t.Run("切断猶予期限の読み書き", func(t *testing.T) {
		t.Run("書き込んだ期限を新しい Store から読み出すと、同じ gameID と期限が返る", func(t *testing.T) {
			writer := newTestStore(t)
			ctx := context.Background()
			deadline := time.Now().Add(2 * time.Minute).Truncate(time.Millisecond)

			require.NoError(t, writer.SetDisconnectDeadline(ctx, "p1", "game_1", deadline))

			reader, _ := newFreshStore(t)
			got, found, err := reader.GetDisconnectDeadline(ctx, "p1")
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, "game_1", got.GameID)
			assert.True(t, deadline.Equal(got.Deadline))
		})

		t.Run("一度も書き込んでいないプレイヤーを読み出すと、見つからない", func(t *testing.T) {
			store := newTestStore(t)
			ctx := context.Background()

			_, found, err := store.GetDisconnectDeadline(ctx, "unknown")
			require.NoError(t, err)
			assert.False(t, found)
		})

		t.Run("削除した後に読み出すと、見つからない", func(t *testing.T) {
			store := newTestStore(t)
			ctx := context.Background()
			require.NoError(t, store.SetDisconnectDeadline(ctx, "p1", "game_1", time.Now().Add(time.Minute)))

			require.NoError(t, store.ClearDisconnectDeadline(ctx, "p1"))

			_, found, err := store.GetDisconnectDeadline(ctx, "p1")
			require.NoError(t, err)
			assert.False(t, found)
		})

		t.Run("同じプレイヤーで再度書き込むと、最新の値に置き換わる", func(t *testing.T) {
			store := newTestStore(t)
			ctx := context.Background()
			require.NoError(t, store.SetDisconnectDeadline(ctx, "p1", "game_1", time.Now().Add(time.Minute)))

			newDeadline := time.Now().Add(5 * time.Minute).Truncate(time.Millisecond)
			require.NoError(t, store.SetDisconnectDeadline(ctx, "p1", "game_2", newDeadline))

			got, found, err := store.GetDisconnectDeadline(ctx, "p1")
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, "game_2", got.GameID)
			assert.True(t, newDeadline.Equal(got.Deadline))
		})
	})
}

func TestTurnDeadline(t *testing.T) {
	t.Run("ターン期限の読み書き", func(t *testing.T) {
		t.Run("書き込んだ期限を新しい Store から読み出すと、同じ activePlayerID と期限が返る", func(t *testing.T) {
			writer := newTestStore(t)
			ctx := context.Background()
			deadline := time.Now().Add(30 * time.Second).Truncate(time.Millisecond)

			require.NoError(t, writer.SetTurnDeadline(ctx, "game_1", "p1", deadline))

			reader, _ := newFreshStore(t)
			got, found, err := reader.GetTurnDeadline(ctx, "game_1")
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, "p1", got.ActivePlayerID)
			assert.True(t, deadline.Equal(got.Deadline))
		})

		t.Run("一度も書き込んでいないゲームを読み出すと、見つからない", func(t *testing.T) {
			store := newTestStore(t)
			ctx := context.Background()

			_, found, err := store.GetTurnDeadline(ctx, "unknown_game")
			require.NoError(t, err)
			assert.False(t, found)
		})

		t.Run("削除した後に読み出すと、見つからない", func(t *testing.T) {
			store := newTestStore(t)
			ctx := context.Background()
			require.NoError(t, store.SetTurnDeadline(ctx, "game_1", "p1", time.Now().Add(time.Minute)))

			require.NoError(t, store.ClearTurnDeadline(ctx, "game_1"))

			_, found, err := store.GetTurnDeadline(ctx, "game_1")
			require.NoError(t, err)
			assert.False(t, found)
		})

		t.Run("手番交代で再度書き込むと、activePlayerID が置き換わる", func(t *testing.T) {
			store := newTestStore(t)
			ctx := context.Background()
			require.NoError(t, store.SetTurnDeadline(ctx, "game_1", "p1", time.Now().Add(time.Minute)))

			newDeadline := time.Now().Add(90 * time.Second).Truncate(time.Millisecond)
			require.NoError(t, store.SetTurnDeadline(ctx, "game_1", "p2", newDeadline))

			got, found, err := store.GetTurnDeadline(ctx, "game_1")
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, "p2", got.ActivePlayerID)
			assert.True(t, newDeadline.Equal(got.Deadline))
		})
	})
}

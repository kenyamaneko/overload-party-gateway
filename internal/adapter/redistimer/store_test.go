//go:build integration

package redistimer

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/valkeytest"
)

var testRedisURL string

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

func newTestClient(t *testing.T) *redis.Client {
	t.Helper()
	opt, err := redis.ParseURL(testRedisURL)
	require.NoError(t, err)
	client := redis.NewClient(opt)
	ctx := context.Background()
	require.NoError(t, client.Ping(ctx).Err())
	require.NoError(t, client.FlushDB(ctx).Err())
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(newTestClient(t))
}

func TestDisconnectDeadline(t *testing.T) {
	t.Run("[切断復帰]プレイヤーの切断猶予期限の書き込みと読み出し", func(t *testing.T) {
		t.Run("あるプレイヤーの切断猶予期限を一度も書き込んでいないとき、読み出すと「見つからない」になる", func(t *testing.T) {
			store := newTestStore(t)
			ctx := context.Background()

			_, found, err := store.GetDisconnectDeadline(ctx, "player-1")
			require.NoError(t, err)
			require.False(t, found)
		})

		t.Run("あるプレイヤーの切断猶予期限を書き込んだあと、同じプレイヤーについて読み出すと「見つかった」になり、書き込んだ対戦の識別子と期限が得られる", func(t *testing.T) {
			store := newTestStore(t)
			ctx := context.Background()
			deadline := time.Date(2030, time.January, 1, 12, 0, 0, 123_000_000, time.UTC)

			require.NoError(t, store.SetDisconnectDeadline(ctx, "player-1", "game-1", deadline))

			got, found, err := store.GetDisconnectDeadline(ctx, "player-1")
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, "game-1", got.GameID)
			assert.Equal(t, deadline.UnixNano(), got.Deadline.UnixNano())
		})

		t.Run("あるプレイヤーについて期限を書き込んだあと、別の期限で再度書き込む (上書きする) と、その後の読み出し結果は後から書き込んだ値になる", func(t *testing.T) {
			store := newTestStore(t)
			ctx := context.Background()
			first := time.Date(2030, time.January, 1, 12, 0, 0, 123_000_000, time.UTC)
			second := first.Add(1 * time.Hour)

			require.NoError(t, store.SetDisconnectDeadline(ctx, "player-1", "game-1", first))
			require.NoError(t, store.SetDisconnectDeadline(ctx, "player-1", "game-2", second))

			got, found, err := store.GetDisconnectDeadline(ctx, "player-1")
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, "game-2", got.GameID)
			assert.Equal(t, second.UnixNano(), got.Deadline.UnixNano())
		})

		t.Run("書き込む期限がミリ秒未満の精度を含むとき、読み出した値はミリ秒単位に丸められる", func(t *testing.T) {
			store := newTestStore(t)
			ctx := context.Background()
			base := time.Date(2030, time.January, 1, 12, 0, 0, 123_000_000, time.UTC)
			deadline := base.Add(200 * time.Microsecond)

			require.NoError(t, store.SetDisconnectDeadline(ctx, "player-1", "game-1", deadline))

			got, found, err := store.GetDisconnectDeadline(ctx, "player-1")
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, base.UnixNano(), got.Deadline.UnixNano())
		})

		t.Run("期限を書き込んだあとに削除すると、その後の読み出しは「見つからない」になる", func(t *testing.T) {
			store := newTestStore(t)
			ctx := context.Background()
			deadline := time.Date(2030, time.January, 1, 12, 0, 0, 0, time.UTC)

			require.NoError(t, store.SetDisconnectDeadline(ctx, "player-1", "game-1", deadline))
			require.NoError(t, store.ClearDisconnectDeadline(ctx, "player-1"))

			_, found, err := store.GetDisconnectDeadline(ctx, "player-1")
			require.NoError(t, err)
			require.False(t, found)
		})

		t.Run("あるプレイヤーの切断猶予期限を一度も書き込んでいない状態で削除操作を行っても、エラーにならない", func(t *testing.T) {
			store := newTestStore(t)
			ctx := context.Background()

			require.NoError(t, store.ClearDisconnectDeadline(ctx, "player-1"))
		})
	})
}

func TestGetDisconnectDeadline(t *testing.T) {
	t.Run("[切断復帰]保存されている期限データの解釈", func(t *testing.T) {
		t.Run("保存されている期限の値が数値として解釈できない形式であるとき、読み出しはエラーになる", func(t *testing.T) {
			client := newTestClient(t)
			store := NewStore(client)
			ctx := context.Background()
			playerID := "player-1"

			require.NoError(t, client.HSet(ctx, disconnectKeyPrefix+playerID,
				fieldGameID, "game-1",
				fieldDeadlineMillis, "not-a-number",
			).Err())

			_, _, err := store.GetDisconnectDeadline(ctx, playerID)
			require.ErrorIs(t, err, strconv.ErrSyntax)
		})
	})
}

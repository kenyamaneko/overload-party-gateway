//go:build integration

package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

const (
	testPlayer1ID = "11111111-1111-4111-8111-111111111111"
	testPlayer2ID = "22222222-2222-4222-8222-222222222222"
)

func TestPgGamePlayerRepository(t *testing.T) {
	t.Run("対戦へのプレイヤー登録の冪等性", func(t *testing.T) {
		t.Run("同一対戦・同一スロットへ重ねて登録したとき、エラーにならず先に登録した内容を保持する", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgGamePlayerRepository(sharedPG.Pool)
			ctx := context.Background()
			require.NoError(t, repo.InsertGamePlayer(ctx, "g1", 1, testPlayer1ID))

			err := repo.InsertGamePlayer(ctx, "g1", 1, testPlayer2ID)

			require.NoError(t, err)
			entries, lookupErr := repo.LookupGamePlayers(ctx, "g1")
			require.NoError(t, lookupErr)
			require.Len(t, entries, 1)
			assert.Equal(t, testPlayer1ID, entries[0].PlayerID)
		})
	})

	t.Run("プレイヤーIDからのスロット番号の照会", func(t *testing.T) {
		t.Run("game_idとplayer_idに一致する行があるとき、player_numを返す", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgGamePlayerRepository(sharedPG.Pool)
			ctx := context.Background()
			require.NoError(t, repo.InsertGamePlayer(ctx, "g1", 2, testPlayer1ID))

			num, err := repo.LookupPlayerNum(ctx, "g1", testPlayer1ID)

			require.NoError(t, err)
			assert.Equal(t, 2, num)
		})

		t.Run("game_idとplayer_idに一致する行が無いとき、port.ErrNotFoundを返す", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgGamePlayerRepository(sharedPG.Pool)
			ctx := context.Background()

			_, err := repo.LookupPlayerNum(ctx, "g1", testPlayer1ID)

			assert.ErrorIs(t, err, port.ErrNotFound)
		})
	})

	t.Run("対戦ごとの人間プレイヤー数の集計", func(t *testing.T) {
		t.Run("人間2人の対戦のとき、2を返す", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgGamePlayerRepository(sharedPG.Pool)
			ctx := context.Background()
			require.NoError(t, repo.InsertGamePlayer(ctx, "g1", 1, testPlayer1ID))
			require.NoError(t, repo.InsertGamePlayer(ctx, "g1", 2, testPlayer2ID))

			counts, err := repo.CountPlayersByGame(ctx, []string{"g1"})

			require.NoError(t, err)
			assert.Equal(t, map[string]int{"g1": 2}, counts)
		})

		t.Run("人間1人の対戦のとき、1を返す", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgGamePlayerRepository(sharedPG.Pool)
			ctx := context.Background()
			require.NoError(t, repo.InsertGamePlayer(ctx, "g1", 1, testPlayer1ID))

			counts, err := repo.CountPlayersByGame(ctx, []string{"g1"})

			require.NoError(t, err)
			assert.Equal(t, map[string]int{"g1": 1}, counts)
		})

		t.Run("行が無い対戦のとき、その対戦を含まない", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgGamePlayerRepository(sharedPG.Pool)
			ctx := context.Background()
			require.NoError(t, repo.InsertGamePlayer(ctx, "g1", 1, testPlayer1ID))

			counts, err := repo.CountPlayersByGame(ctx, []string{"g1", "g_no_rows"})

			require.NoError(t, err)
			assert.Equal(t, map[string]int{"g1": 1}, counts)
		})

		t.Run("複数の対戦を指定するとき、対戦ごとに数を返す", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgGamePlayerRepository(sharedPG.Pool)
			ctx := context.Background()
			require.NoError(t, repo.InsertGamePlayer(ctx, "g1", 1, testPlayer1ID))
			require.NoError(t, repo.InsertGamePlayer(ctx, "g1", 2, testPlayer2ID))
			require.NoError(t, repo.InsertGamePlayer(ctx, "g2", 1, testPlayer1ID))

			counts, err := repo.CountPlayersByGame(ctx, []string{"g1", "g2"})

			require.NoError(t, err)
			assert.Equal(t, map[string]int{"g1": 2, "g2": 1}, counts)
		})

		t.Run("指定した対戦が無いとき、空を返す", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgGamePlayerRepository(sharedPG.Pool)
			ctx := context.Background()
			require.NoError(t, repo.InsertGamePlayer(ctx, "g1", 1, testPlayer1ID))

			counts, err := repo.CountPlayersByGame(ctx, nil)

			require.NoError(t, err)
			assert.Empty(t, counts)
		})
	})

	t.Run("対戦のプレイヤースロットの読み出し", func(t *testing.T) {
		t.Run("人間が0人の対戦のとき、空の一覧を返す", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgGamePlayerRepository(sharedPG.Pool)
			ctx := context.Background()

			entries, err := repo.LookupGamePlayers(ctx, "g1")

			require.NoError(t, err)
			assert.Empty(t, entries)
		})

		t.Run("人間が1人の対戦のとき、そのプレイヤーのみを返す", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgGamePlayerRepository(sharedPG.Pool)
			ctx := context.Background()
			require.NoError(t, repo.InsertGamePlayer(ctx, "g1", 1, testPlayer1ID))

			entries, err := repo.LookupGamePlayers(ctx, "g1")

			require.NoError(t, err)
			require.Len(t, entries, 1)
			assert.Equal(t, 1, entries[0].PlayerNum)
			assert.Equal(t, testPlayer1ID, entries[0].PlayerID)
		})

		t.Run("人間が2人の対戦のとき、スロットごとのプレイヤーと行の作成時刻を返す", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgGamePlayerRepository(sharedPG.Pool)
			ctx := context.Background()
			insertedAt := time.Now()
			require.NoError(t, repo.InsertGamePlayer(ctx, "g1", 1, testPlayer1ID))
			require.NoError(t, repo.InsertGamePlayer(ctx, "g1", 2, testPlayer2ID))

			entries, err := repo.LookupGamePlayers(ctx, "g1")

			require.NoError(t, err)
			require.Len(t, entries, 2)
			assert.Equal(t, 1, entries[0].PlayerNum)
			assert.Equal(t, testPlayer1ID, entries[0].PlayerID)
			assert.Equal(t, 2, entries[1].PlayerNum)
			assert.Equal(t, testPlayer2ID, entries[1].PlayerID)
			assert.WithinDuration(t, insertedAt, entries[0].CreatedAt, time.Minute)
			assert.WithinDuration(t, insertedAt, entries[1].CreatedAt, time.Minute)
		})
	})

	t.Run("経験値付与済みフラグの冪等な設定", func(t *testing.T) {
		t.Run("game_idに一致しplayer_num=1の行でexp_awardedがfalseのとき、trueに更新してtrueを返す", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgGamePlayerRepository(sharedPG.Pool)
			ctx := context.Background()
			require.NoError(t, repo.InsertGamePlayer(ctx, "g1", 1, testPlayer1ID))

			awarded, err := repo.MarkExpAwarded(ctx, "g1")

			require.NoError(t, err)
			assert.True(t, awarded)
		})

		t.Run("game_idに一致しplayer_num=1の行でexp_awardedが既にtrueのとき、更新されずfalseを返す", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgGamePlayerRepository(sharedPG.Pool)
			ctx := context.Background()
			require.NoError(t, repo.InsertGamePlayer(ctx, "g1", 1, testPlayer1ID))
			_, err := repo.MarkExpAwarded(ctx, "g1")
			require.NoError(t, err)

			awarded, err := repo.MarkExpAwarded(ctx, "g1")

			require.NoError(t, err)
			assert.False(t, awarded)
		})
	})
}

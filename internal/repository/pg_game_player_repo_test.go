//go:build integration

package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

const (
	testPlayer1ID = "11111111-1111-4111-8111-111111111111"
	testPlayer2ID = "22222222-2222-4222-8222-222222222222"
)

func TestPgGamePlayerRepository(t *testing.T) {
	t.Run("対戦ごとの人間プレイヤー数の集計", func(t *testing.T) {
		t.Run("人間 2 人の対戦のとき、2 を返す", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgGamePlayerRepository(sharedPG.Pool)
			ctx := context.Background()
			require.NoError(t, repo.InsertGamePlayer(ctx, "g1", 1, testPlayer1ID))
			require.NoError(t, repo.InsertGamePlayer(ctx, "g1", 2, testPlayer2ID))

			counts, err := repo.CountPlayersByGame(ctx, []string{"g1"})

			require.NoError(t, err)
			assert.Equal(t, map[string]int{"g1": 2}, counts)
		})

		t.Run("人間 1 人の対戦のとき、1 を返す", func(t *testing.T) {
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
}

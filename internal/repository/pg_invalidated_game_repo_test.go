//go:build integration

package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

func TestPgInvalidatedGameRepository(t *testing.T) {
	t.Run("無効になった対戦の記録", func(t *testing.T) {
		t.Run("記録した対戦は、決着していない対戦として読み出せる", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgInvalidatedGameRepository(sharedPG.Pool)
			ctx := context.Background()
			require.NoError(t, repo.MarkInvalidated(ctx, []string{"g1", "g2"}))

			unfinished, err := repo.ListUnfinished(ctx)

			require.NoError(t, err)
			assert.ElementsMatch(t, []string{"g1", "g2"}, unfinished)
		})

		t.Run("同じ対戦を重ねて記録しても、決着していない対戦として一度だけ読み出せる", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgInvalidatedGameRepository(sharedPG.Pool)
			ctx := context.Background()
			require.NoError(t, repo.MarkInvalidated(ctx, []string{"g1"}))
			require.NoError(t, repo.MarkInvalidated(ctx, []string{"g1"}))

			unfinished, err := repo.ListUnfinished(ctx)

			require.NoError(t, err)
			assert.Equal(t, []string{"g1"}, unfinished)
		})

		t.Run("決着済みにした対戦は、決着していない対戦として読み出されない", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgInvalidatedGameRepository(sharedPG.Pool)
			ctx := context.Background()
			require.NoError(t, repo.MarkInvalidated(ctx, []string{"g1", "g2"}))
			require.NoError(t, repo.MarkFinished(ctx, "g1"))

			unfinished, err := repo.ListUnfinished(ctx)

			require.NoError(t, err)
			assert.Equal(t, []string{"g2"}, unfinished)
		})

		t.Run("決着済みにした対戦を重ねて記録しても、決着していない対戦に戻らない", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgInvalidatedGameRepository(sharedPG.Pool)
			ctx := context.Background()
			require.NoError(t, repo.MarkInvalidated(ctx, []string{"g1"}))
			require.NoError(t, repo.MarkFinished(ctx, "g1"))
			require.NoError(t, repo.MarkInvalidated(ctx, []string{"g1"}))

			unfinished, err := repo.ListUnfinished(ctx)

			require.NoError(t, err)
			assert.Empty(t, unfinished)
		})

		t.Run("記録する対戦が空のとき、決着していない対戦は無い", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgInvalidatedGameRepository(sharedPG.Pool)
			ctx := context.Background()
			require.NoError(t, repo.MarkInvalidated(ctx, nil))

			unfinished, err := repo.ListUnfinished(ctx)

			require.NoError(t, err)
			assert.Empty(t, unfinished)
		})

		t.Run("記録した対戦は、無効として扱われる", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgInvalidatedGameRepository(sharedPG.Pool)
			ctx := context.Background()
			require.NoError(t, repo.MarkInvalidated(ctx, []string{"g1"}))

			invalidated, err := repo.IsInvalidated(ctx, "g1")

			require.NoError(t, err)
			assert.True(t, invalidated)
		})

		t.Run("決着済みにした対戦も、無効として扱われる", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgInvalidatedGameRepository(sharedPG.Pool)
			ctx := context.Background()
			require.NoError(t, repo.MarkInvalidated(ctx, []string{"g1"}))
			require.NoError(t, repo.MarkFinished(ctx, "g1"))

			invalidated, err := repo.IsInvalidated(ctx, "g1")

			require.NoError(t, err)
			assert.True(t, invalidated)
		})

		t.Run("記録していない対戦は、無効として扱われない", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgInvalidatedGameRepository(sharedPG.Pool)
			ctx := context.Background()

			invalidated, err := repo.IsInvalidated(ctx, "g_never_invalidated")

			require.NoError(t, err)
			assert.False(t, invalidated)
		})
	})
}

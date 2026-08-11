//go:build integration

package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

const (
	igGameID1 = "TST-GAME-1"
	igGameID2 = "TST-GAME-2"
	igGameID3 = "TST-GAME-3"
)

func queryInvalidatedAt(t *testing.T, gameID string) time.Time {
	t.Helper()
	var invalidatedAt time.Time
	err := sharedPg.Pool.QueryRow(context.Background(),
		`SELECT invalidated_at FROM gateway.invalidated_games WHERE game_id = $1`,
		gameID,
	).Scan(&invalidatedAt)
	require.NoError(t, err)
	return invalidatedAt
}

func queryFinishedAt(t *testing.T, gameID string) time.Time {
	t.Helper()
	var finishedAt time.Time
	err := sharedPg.Pool.QueryRow(context.Background(),
		`SELECT finished_at FROM gateway.invalidated_games WHERE game_id = $1`,
		gameID,
	).Scan(&finishedAt)
	require.NoError(t, err)
	return finishedAt
}

func queryRevertedAt(t *testing.T, gameID string) time.Time {
	t.Helper()
	var revertedAt time.Time
	err := sharedPg.Pool.QueryRow(context.Background(),
		`SELECT reverted_at FROM gateway.invalidated_games WHERE game_id = $1`,
		gameID,
	).Scan(&revertedAt)
	require.NoError(t, err)
	return revertedAt
}

func TestMarkInvalidated(t *testing.T) {
	repo := repository.NewPgInvalidatedGameRepository(sharedPg.Pool)
	ctx := context.Background()

	t.Run("[起動時復旧]対戦の無効化記録", func(t *testing.T) {
		t.Run("対戦IDを1件も指定しなかったとき、記録処理を呼んでも何も起きず、エラーにもならない", func(t *testing.T) {
			sharedPg.Truncate(t)

			err := repo.MarkInvalidated(ctx, []string{})
			require.NoError(t, err)

			got, err := repo.IsInvalidated(ctx, igGameID1)
			require.NoError(t, err)
			assert.False(t, got)
		})

		t.Run("未記録の対戦IDを1件指定したとき、記録すると、その対戦が無効な対戦として記録される", func(t *testing.T) {
			sharedPg.Truncate(t)

			err := repo.MarkInvalidated(ctx, []string{igGameID1})
			require.NoError(t, err)

			got, err := repo.IsInvalidated(ctx, igGameID1)
			require.NoError(t, err)
			assert.True(t, got)
		})

		t.Run("複数の対戦IDをまとめて指定したとき、記録すると、それぞれの対戦が無効な対戦として記録される", func(t *testing.T) {
			sharedPg.Truncate(t)

			err := repo.MarkInvalidated(ctx, []string{igGameID1, igGameID2})
			require.NoError(t, err)

			got1, err := repo.IsInvalidated(ctx, igGameID1)
			require.NoError(t, err)
			assert.True(t, got1)

			got2, err := repo.IsInvalidated(ctx, igGameID2)
			require.NoError(t, err)
			assert.True(t, got2)
		})

		t.Run("同じ呼び出しの中で同じ対戦IDを重複して指定しても、エラーにならず、その対戦が無効な対戦として記録される", func(t *testing.T) {
			sharedPg.Truncate(t)

			err := repo.MarkInvalidated(ctx, []string{igGameID1, igGameID1})
			require.NoError(t, err)

			got, err := repo.IsInvalidated(ctx, igGameID1)
			require.NoError(t, err)
			assert.True(t, got)
		})

		t.Run("既に無効として記録済みの対戦IDを指定したとき、再度記録してもエラーにならず、記録内容は変わらない", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.MarkInvalidated(ctx, []string{igGameID1}))
			require.NoError(t, repo.MarkFinished(ctx, igGameID1))
			invalidatedAtBefore := queryInvalidatedAt(t, igGameID1)

			err := repo.MarkInvalidated(ctx, []string{igGameID1})
			require.NoError(t, err)

			unfinished, err := repo.ListUnfinished(ctx)
			require.NoError(t, err)
			assert.NotContains(t, unfinished, igGameID1)
			assert.Equal(t, invalidatedAtBefore, queryInvalidatedAt(t, igGameID1))
		})
	})
}

func TestListUnfinished(t *testing.T) {
	repo := repository.NewPgInvalidatedGameRepository(sharedPg.Pool)
	ctx := context.Background()

	t.Run("[起動時復旧]未決着の無効な対戦一覧の取得", func(t *testing.T) {
		t.Run("無効な対戦が1件も記録されていないとき、一覧を取得すると、要素を1件も含まない一覧が返る", func(t *testing.T) {
			sharedPg.Truncate(t)

			got, err := repo.ListUnfinished(ctx)
			require.NoError(t, err)
			assert.Empty(t, got)
		})

		t.Run("無効な対戦が決着していない状態で1件記録されているとき、一覧を取得すると、その対戦1件を含む一覧が返る", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.MarkInvalidated(ctx, []string{igGameID1}))

			got, err := repo.ListUnfinished(ctx)
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{igGameID1}, got)
		})

		t.Run("無効な対戦のうち一部が既に決着済みのとき、一覧を取得すると、決着していない対戦だけが含まれる", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.MarkInvalidated(ctx, []string{igGameID1, igGameID2}))
			require.NoError(t, repo.MarkFinished(ctx, igGameID1))

			got, err := repo.ListUnfinished(ctx)
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{igGameID2}, got)
		})

		t.Run("未決着の対戦が複数件あるとき、一覧を取得すると、それら全件を含む一覧が返る", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.MarkInvalidated(ctx, []string{igGameID1, igGameID2, igGameID3}))

			got, err := repo.ListUnfinished(ctx)
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{igGameID1, igGameID2, igGameID3}, got)
		})
	})
}

func TestMarkFinished(t *testing.T) {
	repo := repository.NewPgInvalidatedGameRepository(sharedPg.Pool)
	ctx := context.Background()

	t.Run("[起動時復旧]対戦の決着記録", func(t *testing.T) {
		t.Run("未決着の無効な対戦を決着済みとして記録すると、以降その対戦は未決着の一覧に含まれなくなる", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.MarkInvalidated(ctx, []string{igGameID1}))

			require.NoError(t, repo.MarkFinished(ctx, igGameID1))

			got, err := repo.ListUnfinished(ctx)
			require.NoError(t, err)
			assert.NotContains(t, got, igGameID1)
		})

		t.Run("未決着の無効な対戦を決着済みとして記録すると、以降その対戦は消費回数の返却待ち一覧に含まれるようになる", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.MarkInvalidated(ctx, []string{igGameID1}))

			require.NoError(t, repo.MarkFinished(ctx, igGameID1))

			got, err := repo.ListRevertPending(ctx)
			require.NoError(t, err)
			assert.Contains(t, got, igGameID1)
		})

		t.Run("未決着の無効な対戦が複数あるとき、そのうち1件だけを決着済みとして記録すると、残りは未決着の一覧に含まれたままになる", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.MarkInvalidated(ctx, []string{igGameID1, igGameID2}))

			require.NoError(t, repo.MarkFinished(ctx, igGameID1))

			got, err := repo.ListUnfinished(ctx)
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{igGameID2}, got)
		})

		t.Run("既に決着済みの対戦を指定したとき、再度決着済みとして記録してもエラーにならず、決着した時刻は変わらない", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.MarkInvalidated(ctx, []string{igGameID1}))
			require.NoError(t, repo.MarkFinished(ctx, igGameID1))
			finishedAtBefore := queryFinishedAt(t, igGameID1)

			err := repo.MarkFinished(ctx, igGameID1)
			require.NoError(t, err)

			assert.Equal(t, finishedAtBefore, queryFinishedAt(t, igGameID1))
		})
	})
}

func TestListRevertPending(t *testing.T) {
	repo := repository.NewPgInvalidatedGameRepository(sharedPg.Pool)
	ctx := context.Background()

	t.Run("[起動時復旧]消費バトル回数の返却待ち一覧の取得", func(t *testing.T) {
		t.Run("決着済みで返却待ちの無効な対戦が1件も無いとき、一覧を取得すると、要素を1件も含まない一覧が返る", func(t *testing.T) {
			sharedPg.Truncate(t)

			got, err := repo.ListRevertPending(ctx)
			require.NoError(t, err)
			assert.Empty(t, got)
		})

		t.Run("決着済みで返却待ちの無効な対戦が1件あるとき、一覧を取得すると、その対戦1件を含む一覧が返る", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.MarkInvalidated(ctx, []string{igGameID1}))
			require.NoError(t, repo.MarkFinished(ctx, igGameID1))

			got, err := repo.ListRevertPending(ctx)
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{igGameID1}, got)
		})

		t.Run("決着していない無効な対戦があるとき、一覧を取得すると、その対戦は含まれない", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.MarkInvalidated(ctx, []string{igGameID1}))

			got, err := repo.ListRevertPending(ctx)
			require.NoError(t, err)
			assert.NotContains(t, got, igGameID1)
		})

		t.Run("返却待ちの対戦が複数件あるとき、一覧を取得すると、それら全件を含む一覧が返る", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.MarkInvalidated(ctx, []string{igGameID1, igGameID2}))
			require.NoError(t, repo.MarkFinished(ctx, igGameID1))
			require.NoError(t, repo.MarkFinished(ctx, igGameID2))

			got, err := repo.ListRevertPending(ctx)
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{igGameID1, igGameID2}, got)
		})
	})
}

func TestMarkReverted(t *testing.T) {
	repo := repository.NewPgInvalidatedGameRepository(sharedPg.Pool)
	ctx := context.Background()

	t.Run("[起動時復旧]消費バトル回数の返却記録", func(t *testing.T) {
		t.Run("返却待ちの対戦を返却済みとして記録すると、以降その対戦は返却待ちの一覧に含まれなくなる", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.MarkInvalidated(ctx, []string{igGameID1}))
			require.NoError(t, repo.MarkFinished(ctx, igGameID1))

			require.NoError(t, repo.MarkReverted(ctx, igGameID1))

			got, err := repo.ListRevertPending(ctx)
			require.NoError(t, err)
			assert.NotContains(t, got, igGameID1)
		})

		t.Run("返却待ちの対戦が複数あるとき、そのうち1件だけを返却済みとして記録すると、残りは返却待ちの一覧に含まれたままになる", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.MarkInvalidated(ctx, []string{igGameID1, igGameID2}))
			require.NoError(t, repo.MarkFinished(ctx, igGameID1))
			require.NoError(t, repo.MarkFinished(ctx, igGameID2))

			require.NoError(t, repo.MarkReverted(ctx, igGameID1))

			got, err := repo.ListRevertPending(ctx)
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{igGameID2}, got)
		})

		t.Run("既に返却済みの対戦を指定したとき、再度返却済みとして記録してもエラーにならず、返却した時刻は変わらない", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.MarkInvalidated(ctx, []string{igGameID1}))
			require.NoError(t, repo.MarkFinished(ctx, igGameID1))
			require.NoError(t, repo.MarkReverted(ctx, igGameID1))
			revertedAtBefore := queryRevertedAt(t, igGameID1)

			err := repo.MarkReverted(ctx, igGameID1)
			require.NoError(t, err)

			assert.Equal(t, revertedAtBefore, queryRevertedAt(t, igGameID1))
		})
	})
}

func TestIsInvalidated(t *testing.T) {
	repo := repository.NewPgInvalidatedGameRepository(sharedPg.Pool)
	ctx := context.Background()

	t.Run("[起動時復旧]対戦の無効化有無の判定", func(t *testing.T) {
		t.Run("対戦が無効として記録されていないとき、判定すると、無効でないことを示す結果が返る", func(t *testing.T) {
			sharedPg.Truncate(t)

			got, err := repo.IsInvalidated(ctx, igGameID1)
			require.NoError(t, err)
			assert.False(t, got)
		})

		t.Run("対戦が無効として記録されているとき、判定すると、無効であることを示す結果が返る", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.MarkInvalidated(ctx, []string{igGameID1}))

			got, err := repo.IsInvalidated(ctx, igGameID1)
			require.NoError(t, err)
			assert.True(t, got)
		})

		t.Run("対戦が決着済み・返却済みまで進んでいても無効として記録されている限り、判定すると、無効であることを示す結果が返る", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.MarkInvalidated(ctx, []string{igGameID1}))
			require.NoError(t, repo.MarkFinished(ctx, igGameID1))
			require.NoError(t, repo.MarkReverted(ctx, igGameID1))

			got, err := repo.IsInvalidated(ctx, igGameID1)
			require.NoError(t, err)
			assert.True(t, got)
		})
	})
}

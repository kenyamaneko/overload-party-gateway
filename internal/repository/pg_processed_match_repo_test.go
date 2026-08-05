//go:build integration

package repository_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository/postgrestest"
)

var sharedPG *postgrestest.Postgres

func TestMain(m *testing.M) {
	os.Exit(postgrestest.RunMain(m, &sharedPG))
}

func TestPgProcessedMatchRepository(t *testing.T) {
	t.Run("matchIdの永続dedup", func(t *testing.T) {
		t.Run("未処理のmatchIdのとき、処理を開始できる", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgProcessedMatchRepository(sharedPG.Pool)
			ctx := context.Background()

			started, err := repo.Claim(ctx, "mch_1")

			require.NoError(t, err)
			assert.True(t, started)
		})

		t.Run("既に処理を開始済みのmatchIdのとき、重ねて処理を開始できない", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgProcessedMatchRepository(sharedPG.Pool)
			ctx := context.Background()
			_, err := repo.Claim(ctx, "mch_2")
			require.NoError(t, err)

			started, err := repo.Claim(ctx, "mch_2")

			require.NoError(t, err)
			assert.False(t, started)
		})

		t.Run("処理の開始を取り消したmatchIdのとき、再び処理を開始できる", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgProcessedMatchRepository(sharedPG.Pool)
			ctx := context.Background()
			_, err := repo.Claim(ctx, "mch_3")
			require.NoError(t, err)
			require.NoError(t, repo.Release(ctx, "mch_3"))

			started, err := repo.Claim(ctx, "mch_3")

			require.NoError(t, err)
			assert.True(t, started)
		})

		t.Run("ゲーム作成がまだ記録されていないmatchIdのとき、記録は見つからない", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgProcessedMatchRepository(sharedPG.Pool)
			ctx := context.Background()
			_, err := repo.Claim(ctx, "mch_4")
			require.NoError(t, err)

			_, found, err := repo.GameIDFor(ctx, "mch_4")

			require.NoError(t, err)
			assert.False(t, found)
		})

		t.Run("ゲーム作成を記録したmatchIdのとき、記録した内容がそのまま読み出せる", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgProcessedMatchRepository(sharedPG.Pool)
			ctx := context.Background()
			_, err := repo.Claim(ctx, "mch_5")
			require.NoError(t, err)
			require.NoError(t, repo.RecordGameCreated(ctx, "mch_5", "g1"))

			gameID, found, err := repo.GameIDFor(ctx, "mch_5")

			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, "g1", gameID)
		})

		t.Run("処理を開始したことが無いmatchIdのとき、記録は見つからない", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgProcessedMatchRepository(sharedPG.Pool)
			ctx := context.Background()

			_, found, err := repo.GameIDFor(ctx, "mch_never_claimed")

			require.NoError(t, err)
			assert.False(t, found)
		})

		t.Run("まだ通知していないmatchIdのとき、成立通知の送信権を取得できる", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgProcessedMatchRepository(sharedPG.Pool)
			ctx := context.Background()
			_, err := repo.Claim(ctx, "mch_6")
			require.NoError(t, err)

			marked, err := repo.MarkNotified(ctx, "mch_6")

			require.NoError(t, err)
			assert.True(t, marked)
		})

		t.Run("既に通知したmatchIdのとき、成立通知の送信権を取得できない", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgProcessedMatchRepository(sharedPG.Pool)
			ctx := context.Background()
			_, err := repo.Claim(ctx, "mch_7")
			require.NoError(t, err)
			_, err = repo.MarkNotified(ctx, "mch_7")
			require.NoError(t, err)

			marked, err := repo.MarkNotified(ctx, "mch_7")

			require.NoError(t, err)
			assert.False(t, marked)
		})

		t.Run("処理を開始したことが無いmatchIdのとき、成立通知の送信権を取得できない", func(t *testing.T) {
			sharedPG.Truncate(t)
			repo := repository.NewPgProcessedMatchRepository(sharedPG.Pool)
			ctx := context.Background()

			marked, err := repo.MarkNotified(ctx, "mch_never_claimed")

			require.NoError(t, err)
			assert.False(t, marked)
		})
	})
}

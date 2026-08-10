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
	pmMatchID1 = "TST-MATCH-1"
	pmGameID1  = "TST-GAME-1"
)

func TestClaim(t *testing.T) {
	repo := repository.NewPgProcessedMatchRepository(sharedPg.Pool)
	ctx := context.Background()

	t.Run("マッチの処理開始権の取得", func(t *testing.T) {
		noPreClaim := func(t *testing.T) {}
		preClaimed := func(t *testing.T) {
			_, err := repo.Claim(ctx, pmMatchID1)
			require.NoError(t, err)
		}

		tests := []struct {
			name        string
			preClaim    func(t *testing.T)
			wantClaimed bool
		}{
			{
				name:        "対象のマッチがまだ処理を開始されていないとき、開始を試みると、開始できたことを示す結果が返る",
				preClaim:    noPreClaim,
				wantClaimed: true,
			},
			{
				name:        "対象のマッチが既に処理を開始済みのとき、再度開始を試みると、開始できなかったことを示す結果が返り、エラーにはならない",
				preClaim:    preClaimed,
				wantClaimed: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				sharedPg.Truncate(t)
				tt.preClaim(t)

				claimed, err := repo.Claim(ctx, pmMatchID1)
				require.NoError(t, err)
				assert.Equal(t, tt.wantClaimed, claimed)
			})
		}
	})
}

func TestRelease(t *testing.T) {
	repo := repository.NewPgProcessedMatchRepository(sharedPg.Pool)
	ctx := context.Background()

	t.Run("処理開始の取り消し", func(t *testing.T) {
		noPreClaim := func(t *testing.T) {}
		preClaimed := func(t *testing.T) {
			_, err := repo.Claim(ctx, pmMatchID1)
			require.NoError(t, err)
		}

		tests := []struct {
			name     string
			preClaim func(t *testing.T)
		}{
			{
				name:     "処理を開始済みのマッチを取り消すと、以降そのマッチは再度処理を開始できるようになる",
				preClaim: preClaimed,
			},
			{
				name:     "処理を開始していないマッチを取り消しても、エラーにならず、そのマッチは処理を開始できる状態のままになる",
				preClaim: noPreClaim,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				sharedPg.Truncate(t)
				tt.preClaim(t)

				require.NoError(t, repo.Release(ctx, pmMatchID1))

				claimed, err := repo.Claim(ctx, pmMatchID1)
				require.NoError(t, err)
				assert.True(t, claimed)
			})
		}
	})
}

func TestRecordGameCreated(t *testing.T) {
	repo := repository.NewPgProcessedMatchRepository(sharedPg.Pool)
	ctx := context.Background()

	t.Run("対戦作成結果の記録", func(t *testing.T) {
		t.Run("処理を開始済みのマッチに対して対戦のIDを記録すると、以降そのマッチについて記録済みの対戦IDを取得できるようになる", func(t *testing.T) {
			sharedPg.Truncate(t)
			_, err := repo.Claim(ctx, pmMatchID1)
			require.NoError(t, err)

			require.NoError(t, repo.RecordGameCreated(ctx, pmMatchID1, pmGameID1))

			gameID, found, err := repo.GameIDFor(ctx, pmMatchID1)
			require.NoError(t, err)
			assert.True(t, found)
			assert.Equal(t, pmGameID1, gameID)
		})

		t.Run("処理を開始していないマッチに対して対戦のIDを記録しても、エラーにならず、対戦IDは記録されない (該当するマッチについて取得すると見つからなかったことを示す結果が返ったままになる)", func(t *testing.T) {
			sharedPg.Truncate(t)

			require.NoError(t, repo.RecordGameCreated(ctx, pmMatchID1, pmGameID1))

			_, found, err := repo.GameIDFor(ctx, pmMatchID1)
			require.NoError(t, err)
			assert.False(t, found)
		})
	})
}

func TestMarkNotified(t *testing.T) {
	repo := repository.NewPgProcessedMatchRepository(sharedPg.Pool)
	ctx := context.Background()

	t.Run("成立通知の送信権の取得", func(t *testing.T) {
		noPreClaim := func(t *testing.T) {}
		claimedNotYetNotified := func(t *testing.T) {
			_, err := repo.Claim(ctx, pmMatchID1)
			require.NoError(t, err)
		}
		claimedAlreadyNotified := func(t *testing.T) {
			_, err := repo.Claim(ctx, pmMatchID1)
			require.NoError(t, err)
			_, err = repo.MarkNotified(ctx, pmMatchID1)
			require.NoError(t, err)
		}

		tests := []struct {
			name       string
			setup      func(t *testing.T)
			wantMarked bool
		}{
			{
				name:       "対象のマッチについてまだ送信済みでないとき、送信権を取得すると、取得できたことを示す結果が返る",
				setup:      claimedNotYetNotified,
				wantMarked: true,
			},
			{
				name:       "対象のマッチについて既に送信済みのとき、再度取得を試みると、取得できなかったことを示す結果が返り、エラーにはならない",
				setup:      claimedAlreadyNotified,
				wantMarked: false,
			},
			{
				name:       "処理を開始していないマッチについて送信権を取得しようとしても、取得できなかったことを示す結果が返り、エラーにはならない",
				setup:      noPreClaim,
				wantMarked: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				sharedPg.Truncate(t)
				tt.setup(t)

				marked, err := repo.MarkNotified(ctx, pmMatchID1)
				require.NoError(t, err)
				assert.Equal(t, tt.wantMarked, marked)
			})
		}
	})
}

func TestGameIDFor(t *testing.T) {
	repo := repository.NewPgProcessedMatchRepository(sharedPg.Pool)
	ctx := context.Background()

	t.Run("マッチに対応する対戦IDの取得", func(t *testing.T) {
		t.Run("対象のマッチについて処理が開始されていないとき、取得すると、見つからなかったことを示す結果が返る", func(t *testing.T) {
			sharedPg.Truncate(t)

			_, found, err := repo.GameIDFor(ctx, pmMatchID1)
			require.NoError(t, err)
			assert.False(t, found)
		})

		t.Run("対象のマッチについて処理は開始されているが対戦IDがまだ記録されていないとき、取得すると、見つからなかったことを示す結果が返る", func(t *testing.T) {
			sharedPg.Truncate(t)
			_, err := repo.Claim(ctx, pmMatchID1)
			require.NoError(t, err)

			_, found, err := repo.GameIDFor(ctx, pmMatchID1)
			require.NoError(t, err)
			assert.False(t, found)
		})

		t.Run("対象のマッチについて対戦IDが記録済みのとき、取得すると、記録されている対戦IDと、見つかったことを示す結果が返る", func(t *testing.T) {
			sharedPg.Truncate(t)
			_, err := repo.Claim(ctx, pmMatchID1)
			require.NoError(t, err)
			require.NoError(t, repo.RecordGameCreated(ctx, pmMatchID1, pmGameID1))

			gameID, found, err := repo.GameIDFor(ctx, pmMatchID1)
			require.NoError(t, err)
			assert.True(t, found)
			assert.Equal(t, pmGameID1, gameID)
		})
	})
}

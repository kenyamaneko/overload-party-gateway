//go:build integration

package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

const (
	gpGameID1 = "TST-GAME-1"
	gpGameID2 = "TST-GAME-2"

	gpPlayerID1 = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	gpPlayerID2 = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
)

func queryExpAwarded(t *testing.T, gameID string, playerNum int) bool {
	t.Helper()
	var awarded bool
	err := sharedPg.Pool.QueryRow(context.Background(),
		`SELECT exp_awarded FROM gateway.game_players WHERE game_id = $1 AND player_num = $2`,
		gameID, playerNum,
	).Scan(&awarded)
	require.NoError(t, err)
	return awarded
}

func TestInsertGamePlayer(t *testing.T) {
	repo := repository.NewPgGamePlayerRepository(sharedPg.Pool)
	ctx := context.Background()

	t.Run("スロットへのプレイヤー登録", func(t *testing.T) {
		t.Run("指定した対戦の指定したスロット番号にまだ誰も登録されていないとき、登録すると、そのプレイヤーがそのスロットに記録される", func(t *testing.T) {
			sharedPg.Truncate(t)

			err := repo.InsertGamePlayer(ctx, gpGameID1, 1, gpPlayerID1)
			require.NoError(t, err)

			got, err := repo.LookupGamePlayers(ctx, gpGameID1)
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, 1, got[0].PlayerNum)
			assert.Equal(t, gpPlayerID1, got[0].PlayerID)
		})

		t.Run("指定した対戦の指定したスロット番号に既に別のプレイヤーが登録されているとき、同じスロット番号へ別のプレイヤーを登録しても、エラーにならず先に登録されていたプレイヤーのまま変わらない", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.InsertGamePlayer(ctx, gpGameID1, 1, gpPlayerID1))

			err := repo.InsertGamePlayer(ctx, gpGameID1, 1, gpPlayerID2)
			require.NoError(t, err)

			got, err := repo.LookupGamePlayers(ctx, gpGameID1)
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, gpPlayerID1, got[0].PlayerID)
		})
	})
}

func TestLookupPlayerNum(t *testing.T) {
	repo := repository.NewPgGamePlayerRepository(sharedPg.Pool)
	ctx := context.Background()

	t.Run("プレイヤーのスロット番号の取得", func(t *testing.T) {
		t.Run("指定した対戦に指定したプレイヤーが登録されているとき、取得すると、登録したスロット番号が返る", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.InsertGamePlayer(ctx, gpGameID1, 2, gpPlayerID1))

			got, err := repo.LookupPlayerNum(ctx, gpGameID1, gpPlayerID1)
			require.NoError(t, err)
			assert.Equal(t, 2, got)
		})

		t.Run("指定した対戦に指定したプレイヤーが登録されていないとき、取得すると、見つからないことを表すエラーになる", func(t *testing.T) {
			sharedPg.Truncate(t)

			_, err := repo.LookupPlayerNum(ctx, gpGameID1, gpPlayerID1)
			require.ErrorIs(t, err, port.ErrNotFound)
		})

		t.Run("指定したプレイヤーが別の対戦には登録されているが、問い合わせた対戦には登録されていないとき、取得すると、見つからないことを表すエラーになる", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.InsertGamePlayer(ctx, gpGameID2, 1, gpPlayerID1))

			_, err := repo.LookupPlayerNum(ctx, gpGameID1, gpPlayerID1)
			require.ErrorIs(t, err, port.ErrNotFound)
		})
	})
}

func TestLookupGamePlayers(t *testing.T) {
	repo := repository.NewPgGamePlayerRepository(sharedPg.Pool)
	ctx := context.Background()

	t.Run("ゲーム内プレイヤー一覧の取得", func(t *testing.T) {
		t.Run("指定した対戦にプレイヤーが1人も登録されていないとき、一覧を取得すると、要素を1件も含まない一覧が返る", func(t *testing.T) {
			sharedPg.Truncate(t)

			got, err := repo.LookupGamePlayers(ctx, gpGameID1)
			require.NoError(t, err)
			assert.Empty(t, got)
		})

		t.Run("指定した対戦にプレイヤーが1人登録されているとき、一覧を取得すると、そのプレイヤー1件だけを含む一覧が返る", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.InsertGamePlayer(ctx, gpGameID1, 1, gpPlayerID1))

			got, err := repo.LookupGamePlayers(ctx, gpGameID1)
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, 1, got[0].PlayerNum)
			assert.Equal(t, gpPlayerID1, got[0].PlayerID)
		})

		t.Run("指定した対戦に複数のプレイヤーが登録されているとき、一覧を取得すると、スロット番号の昇順に並んだ一覧が返る", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.InsertGamePlayer(ctx, gpGameID1, 2, gpPlayerID2))
			require.NoError(t, repo.InsertGamePlayer(ctx, gpGameID1, 1, gpPlayerID1))

			got, err := repo.LookupGamePlayers(ctx, gpGameID1)
			require.NoError(t, err)
			require.Len(t, got, 2)
			assert.Equal(t, 1, got[0].PlayerNum)
			assert.Equal(t, 2, got[1].PlayerNum)
		})

		t.Run("別の対戦に登録されたプレイヤーがいるとき、一覧を取得すると、指定した対戦に属するプレイヤーだけが返る", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.InsertGamePlayer(ctx, gpGameID1, 1, gpPlayerID1))
			require.NoError(t, repo.InsertGamePlayer(ctx, gpGameID2, 1, gpPlayerID2))

			got, err := repo.LookupGamePlayers(ctx, gpGameID1)
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, gpPlayerID1, got[0].PlayerID)
		})
	})
}

func TestCountPlayersByGame(t *testing.T) {
	repo := repository.NewPgGamePlayerRepository(sharedPg.Pool)
	ctx := context.Background()

	t.Run("ゲームごとのプレイヤー人数の集計", func(t *testing.T) {
		t.Run("問い合わせる対戦IDを1件も指定しなかったとき、集計すると、要素を1件も含まない集計結果が返る", func(t *testing.T) {
			sharedPg.Truncate(t)

			got, err := repo.CountPlayersByGame(ctx, []string{})
			require.NoError(t, err)
			assert.Empty(t, got)
		})

		t.Run("問い合わせた対戦のうち、プレイヤーが1人も登録されていない対戦があるとき、集計結果にその対戦のIDは含まれない", func(t *testing.T) {
			sharedPg.Truncate(t)

			got, err := repo.CountPlayersByGame(ctx, []string{gpGameID1})
			require.NoError(t, err)
			assert.NotContains(t, got, gpGameID1)
		})

		t.Run("問い合わせた対戦に1人だけ登録されているとき、集計結果はその対戦について1件と数える", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.InsertGamePlayer(ctx, gpGameID1, 1, gpPlayerID1))

			got, err := repo.CountPlayersByGame(ctx, []string{gpGameID1})
			require.NoError(t, err)
			assert.Equal(t, 1, got[gpGameID1])
		})

		t.Run("問い合わせた対戦に複数人登録されているとき、集計結果はその対戦について登録人数分と数える", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.InsertGamePlayer(ctx, gpGameID1, 1, gpPlayerID1))
			require.NoError(t, repo.InsertGamePlayer(ctx, gpGameID1, 2, gpPlayerID2))

			got, err := repo.CountPlayersByGame(ctx, []string{gpGameID1})
			require.NoError(t, err)
			assert.Equal(t, 2, got[gpGameID1])
		})

		t.Run("複数の対戦IDをまとめて問い合わせたとき、集計結果は対戦IDごとに個別の件数を返す", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.InsertGamePlayer(ctx, gpGameID1, 1, gpPlayerID1))
			require.NoError(t, repo.InsertGamePlayer(ctx, gpGameID2, 1, gpPlayerID1))
			require.NoError(t, repo.InsertGamePlayer(ctx, gpGameID2, 2, gpPlayerID2))

			got, err := repo.CountPlayersByGame(ctx, []string{gpGameID1, gpGameID2})
			require.NoError(t, err)
			assert.Equal(t, 1, got[gpGameID1])
			assert.Equal(t, 2, got[gpGameID2])
		})

		t.Run("問い合わせの対象に含めなかった対戦に登録されているプレイヤーがいても、集計結果に影響しない", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.InsertGamePlayer(ctx, gpGameID1, 1, gpPlayerID1))
			require.NoError(t, repo.InsertGamePlayer(ctx, gpGameID2, 1, gpPlayerID2))

			got, err := repo.CountPlayersByGame(ctx, []string{gpGameID1})
			require.NoError(t, err)
			assert.Equal(t, 1, got[gpGameID1])
			assert.NotContains(t, got, gpGameID2)
		})
	})
}

func TestMarkExpAwarded(t *testing.T) {
	repo := repository.NewPgGamePlayerRepository(sharedPg.Pool)
	ctx := context.Background()

	t.Run("経験値付与済みの記録", func(t *testing.T) {
		noPlayers := func(t *testing.T) {}
		notYetAwarded := func(t *testing.T) {
			require.NoError(t, repo.InsertGamePlayer(ctx, gpGameID1, 1, gpPlayerID1))
		}
		alreadyAwarded := func(t *testing.T) {
			require.NoError(t, repo.InsertGamePlayer(ctx, gpGameID1, 1, gpPlayerID1))
			_, err := repo.MarkExpAwarded(ctx, gpGameID1)
			require.NoError(t, err)
		}

		tests := []struct {
			name        string
			setup       func(t *testing.T)
			wantAwarded bool
		}{
			{
				name:        "対象の対戦にプレイヤーが1人も登録されていないとき、記録しても、付与済みにできなかったことを示す結果が返り、エラーにはならない",
				setup:       noPlayers,
				wantAwarded: false,
			},
			{
				name:        "対象の対戦のスロット番号1のプレイヤーについて経験値がまだ付与されていないとき、記録すると、付与済みとして記録できたことを示す結果が返る",
				setup:       notYetAwarded,
				wantAwarded: true,
			},
			{
				name:        "対象の対戦のスロット番号1のプレイヤーについて経験値が既に付与済みのとき、再度記録しても、付与済みにできなかったことを示す結果が返り、エラーにはならない",
				setup:       alreadyAwarded,
				wantAwarded: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				sharedPg.Truncate(t)
				tt.setup(t)

				awarded, err := repo.MarkExpAwarded(ctx, gpGameID1)
				require.NoError(t, err)
				assert.Equal(t, tt.wantAwarded, awarded)
			})
		}

		t.Run("対象の対戦にスロット番号1とスロット番号2の両方のプレイヤーが登録されているとき、記録すると、スロット番号1のプレイヤーだけが付与済みになる", func(t *testing.T) {
			sharedPg.Truncate(t)
			require.NoError(t, repo.InsertGamePlayer(ctx, gpGameID1, 1, gpPlayerID1))
			require.NoError(t, repo.InsertGamePlayer(ctx, gpGameID1, 2, gpPlayerID2))

			_, err := repo.MarkExpAwarded(ctx, gpGameID1)
			require.NoError(t, err)

			assert.True(t, queryExpAwarded(t, gpGameID1, 1))
			assert.False(t, queryExpAwarded(t, gpGameID1, 2))
		})
	})
}

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

func TestGetInt64(t *testing.T) {
	repo := repository.NewFirestoreGameConfigRepository(sharedFirestoreClient)
	ctx := context.Background()

	t.Run("設定値の取得", func(t *testing.T) {
		valueCases := []struct {
			name  string
			key   string
			value int64
		}{
			{
				name:  "指定したキーの設定が保存されているとき、取得すると、保存されている数値が返る",
				key:   "TST-game-config-present",
				value: 42,
			},
			{
				name:  "保存されている数値が0のとき、取得すると、0が返る (見つからない場合の結果とは区別される)",
				key:   "TST-game-config-zero",
				value: 0,
			},
		}

		for _, tt := range valueCases {
			t.Run(tt.name, func(t *testing.T) {
				_, err := sharedFirestoreClient.Collection("game_config").Doc(tt.key).Set(ctx, map[string]any{"value": tt.value})
				require.NoError(t, err)

				got, err := repo.GetInt64(ctx, tt.key)
				require.NoError(t, err)
				assert.Equal(t, tt.value, got)
			})
		}

		t.Run("指定したキーの設定が保存されていないとき、取得すると、見つからないことを表すエラーになる", func(t *testing.T) {
			key := "TST-game-config-missing"
			_, err := sharedFirestoreClient.Collection("game_config").Doc(key).Delete(ctx)
			require.NoError(t, err)

			_, err = repo.GetInt64(ctx, key)
			require.ErrorIs(t, err, port.ErrNotFound)
		})

		t.Run("指定したキーのドキュメントは存在するが、値のフィールド自体が設定されていないとき、取得すると、値のフィールドが欠落していることを表すエラーになり、戻り値の数値は0のままになる", func(t *testing.T) {
			key := "TST-game-config-no-value-field"
			_, err := sharedFirestoreClient.Collection("game_config").Doc(key).Set(ctx, map[string]any{"unrelated": "x"})
			require.NoError(t, err)

			got, err := repo.GetInt64(ctx, key)
			require.Error(t, err)
			require.NotErrorIs(t, err, port.ErrNotFound)
			assert.ErrorContains(t, err, "value field missing")
			assert.Zero(t, got)
		})

		t.Run("指定したキーの設定は存在するが、数値として読み取れない形式で保存されているとき、取得すると、値のフィールドが欠落している場合とは異なる、解析に失敗したことを表すエラーになる", func(t *testing.T) {
			key := "TST-game-config-non-numeric"
			_, err := sharedFirestoreClient.Collection("game_config").Doc(key).Set(ctx, map[string]any{"value": "not-a-number"})
			require.NoError(t, err)

			_, err = repo.GetInt64(ctx, key)
			require.Error(t, err)
			require.NotErrorIs(t, err, port.ErrNotFound)
			assert.ErrorContains(t, err, "parse game config")
		})
	})
}

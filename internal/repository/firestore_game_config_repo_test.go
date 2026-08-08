//go:build integration

package repository_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"

	"cloud.google.com/go/firestore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

func TestFirestoreGameConfigRepository(t *testing.T) {
	// Firestore emulator を前提とする integration test。FIRESTORE_EMULATOR_HOST が未設定なら
	// スキップ。CI では .github/workflows/ci.yaml がエミュレーターを起動する。
	host := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if host == "" {
		t.Skip("FIRESTORE_EMULATOR_HOST not set; skipping Firestore integration test")
	}

	const projectID = "overload-party-test"
	ctx := context.Background()

	resetEmulator(t, host, projectID)

	client, err := firestore.NewClient(ctx, projectID)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	_, err = client.Collection("game_config").Doc("exp_win").Set(ctx, map[string]any{"value": int64(40)})
	require.NoError(t, err)

	repo := repository.NewFirestoreGameConfigRepository(client)

	t.Run("[対戦連携]game_configの取得", func(t *testing.T) {
		t.Run("存在するキーexp_winのとき、値40を返す", func(t *testing.T) {
			got, err := repo.GetInt64(ctx, "exp_win")
			require.NoError(t, err)
			assert.Equal(t, int64(40), got)
		})

		t.Run("存在しないキーのとき、値が見つからないことを示すエラーになる", func(t *testing.T) {
			_, err := repo.GetInt64(ctx, "does_not_exist")
			require.Error(t, err)
			assert.True(t, errors.Is(err, port.ErrNotFound), "expected ErrNotFound, got: %v", err)
		})
	})
}

func resetEmulator(t *testing.T, host, projectID string) {
	t.Helper()
	url := fmt.Sprintf("http://%s/emulator/v1/projects/%s/databases/(default)/documents", host, projectID)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("firestore emulator reset failed: status=%d body=%s", resp.StatusCode, string(body))
	}
}

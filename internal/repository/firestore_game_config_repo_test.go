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

// Firestore emulator を前提とする integration test。
// FIRESTORE_EMULATOR_HOST が未設定の場合はスキップ。
// CI では .github/workflows/ci.yaml がエミュレーターを起動する。
func TestFirestoreGameConfigRepository(t *testing.T) {
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

	t.Run("Get existing key", func(t *testing.T) {
		got, err := repo.GetInt64(ctx, "exp_win")
		require.NoError(t, err)
		assert.Equal(t, int64(40), got)
	})

	t.Run("Missing key returns ErrNotFound", func(t *testing.T) {
		_, err := repo.GetInt64(ctx, "does_not_exist")
		require.Error(t, err)
		assert.True(t, errors.Is(err, port.ErrNotFound), "expected ErrNotFound, got: %v", err)
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

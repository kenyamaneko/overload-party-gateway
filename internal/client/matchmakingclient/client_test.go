package matchmakingclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking/apimatchmakingclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/client/matchmakingclient"
)

func TestCancel(t *testing.T) {
	t.Run("[下流サービス連携]マッチメイキングキューからの除去", func(t *testing.T) {
		t.Run("対象プレイヤーがキューに存在するとき、除去に成功する", func(t *testing.T) {
			var gotMethod, gotPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.Path
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(server.Close)

			client := matchmakingclient.New(server.URL, "test-instance", server.Client())

			err := client.Cancel(context.Background())

			require.NoError(t, err)
			assert.Equal(t, http.MethodPost, gotMethod)
			assert.Equal(t, "/internal/v1/cancel", gotPath)
		})

		t.Run("対象プレイヤーがキューに存在しない(404)とき、エラーにならず除去が成功したものとして扱われる", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			}))
			t.Cleanup(server.Close)

			client := matchmakingclient.New(server.URL, "test-instance", server.Client())

			err := client.Cancel(context.Background())

			require.NoError(t, err)
		})

		t.Run("matchmakingサービスが認証失敗(401)を返したとき、そのままエラーになる", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
			}))
			t.Cleanup(server.Close)

			client := matchmakingclient.New(server.URL, "test-instance", server.Client())

			err := client.Cancel(context.Background())

			require.Error(t, err)
			assert.ErrorIs(t, err, apimatchmakingclient.ErrUnauthorized)
		})

		t.Run("matchmakingサービスがRedis接続エラー(503)を返したとき、そのままエラーになる", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			t.Cleanup(server.Close)

			client := matchmakingclient.New(server.URL, "test-instance", server.Client())

			err := client.Cancel(context.Background())

			require.Error(t, err)
			assert.ErrorIs(t, err, apimatchmakingclient.ErrServiceUnavailable)
		})
	})
}

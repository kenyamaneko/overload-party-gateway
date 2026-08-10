package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
)

func TestNewForwarder(t *testing.T) {
	t.Run("転送先設定の検証", func(t *testing.T) {
		t.Run("転送先として妥当な形式のURL文字列を指定したとき、以降のリクエストを転送できる状態が構築され、エラーは返らない", func(t *testing.T) {
			handler, err := NewForwarder("http://example.com")

			assert.NoError(t, err)
			assert.NotNil(t, handler)
		})

		t.Run("転送先として妥当な形式ではないURL文字列を指定したとき、転送できる状態は構築されず、エラーが返る", func(t *testing.T) {
			handler, err := NewForwarder("://invalid-target-url")

			assert.Error(t, err)
			assert.Nil(t, handler)
		})
	})

	t.Run("下流サービスへの転送リクエストの構成", func(t *testing.T) {
		t.Run("リクエストを下流サービスへ転送するとき、転送先へ送られるリクエストには、サービス間の内部認証を示すヘッダーが付与された状態になる", func(t *testing.T) {
			var gotHeader string
			downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotHeader = r.Header.Get(internalauth.HeaderName)
				w.WriteHeader(http.StatusOK)
			}))
			defer downstream.Close()

			handler, err := NewForwarder(downstream.URL)
			require.NoError(t, err)

			r := gin.New()
			r.Any("/proxy", handler)

			// httptest.ResponseRecorder は http.CloseNotifier を実装しないため、キャンセル可能な context を渡す。
			ctx, cancel := context.WithCancel(internalauth.WithToken(context.Background(), "internal-auth-test-token"))
			defer cancel()
			req := httptest.NewRequest(http.MethodGet, "/proxy", nil).WithContext(ctx)
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			assert.Equal(t, "internal-auth-test-token", gotHeader)
		})
	})
}

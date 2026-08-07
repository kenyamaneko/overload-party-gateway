package cardclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apicard "github.com/kenyamaneko/overload-party-card/packages/api-card"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
)

func TestClient_InjectsInternalAuthHeader(t *testing.T) {
	t.Run("[内部認証]card宛リクエストへのX-Internal-Authヘッダーの注入", func(t *testing.T) {
		t.Run("トークンを格納したコンテキストのとき、X-Internal-Authヘッダーとして送信される", func(t *testing.T) {
			const wantToken = "test.jwt.token"
			var got string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Get(internalauth.HeaderName)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(apicard.DeckDetailResponse{})
			}))
			defer srv.Close()

			c := New(srv.URL, &http.Client{})
			ctx := internalauth.WithToken(context.Background(), wantToken)
			_, _, err := c.GetDeckCards(ctx, 1)
			require.NoError(t, err)
			assert.Equal(t, wantToken, got)
		})
	})
}

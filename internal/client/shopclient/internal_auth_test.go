package shopclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
)

func TestClient_InjectsInternalAuthHeader(t *testing.T) {
	cases := []struct {
		name       string
		ctx        context.Context
		wantHeader string
	}{
		{
			name:       "ctx に token があるとき X-Internal-Auth を付与する",
			ctx:        internalauth.WithToken(context.Background(), "test.jwt.token"),
			wantHeader: "test.jwt.token",
		},
		{
			name:       "ctx に token がないとき header を付けない",
			ctx:        context.Background(),
			wantHeader: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Get(internalauth.HeaderName)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"products":[]}`))
			}))
			defer srv.Close()

			c := New(srv.URL)
			_, err := c.GetProducts(tc.ctx, "player-123")
			require.NoError(t, err)
			assert.Equal(t, tc.wantHeader, got)
		})
	}
}

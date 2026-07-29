package cardclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kenyamaneko/overload-party-card/packages/api-card/apicardclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
)

func TestClient_InjectsInternalAuthHeader(t *testing.T) {
	t.Run("X-Internal-Auth header の注入", func(t *testing.T) {
		t.Run("ctx に格納した token が X-Internal-Auth header として送られる", func(t *testing.T) {
			const wantToken = "test.jwt.token"
			var got string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Get(internalauth.HeaderName)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`[]`))
			}))
			defer srv.Close()

			c := New(srv.URL)
			ctx := internalauth.WithToken(context.Background(), wantToken)
			_, err := c.ListAllCards(ctx)
			require.NoError(t, err)
			assert.Equal(t, wantToken, got)
		})
	})
}

func TestClient_ValidateDeckForBattle_MapsStatus(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		wantErr error
	}{
		{
			name:    "検証成功の 200 は nil を返す",
			status:  http.StatusOK,
			wantErr: nil,
		},
		{
			name:    "デッキ不正の 400 は ErrDeckInvalid に写像する",
			status:  http.StatusBadRequest,
			wantErr: apicardclient.ErrDeckInvalid,
		},
		{
			name:    "デッキ不在の 404 は not found を伝播する",
			status:  http.StatusNotFound,
			wantErr: apicardclient.ErrNotFound,
		},
		{
			name:    "5xx は internal server error を伝播する",
			status:  http.StatusInternalServerError,
			wantErr: apicardclient.ErrInternalServer,
		},
	}

	t.Run("対戦用デッキ検証の応答の変換", func(t *testing.T) {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(tc.status)
				}))
				defer srv.Close()

				c := New(srv.URL)
				ctx := internalauth.WithToken(context.Background(), "test.jwt.token")
				err := c.ValidateDeckForBattle(ctx, 1)

				require.ErrorIs(t, err, tc.wantErr)
			})
		}
	})
}

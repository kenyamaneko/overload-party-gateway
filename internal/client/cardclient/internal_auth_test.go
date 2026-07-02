package cardclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kenyamaneko/overload-party-card/packages/api-card/apicardclient"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
)

// TestClient_InjectsInternalAuthHeader は呼び出しに内部認証トークンが X-Internal-Auth として乗ることを検証する。
func TestClient_InjectsInternalAuthHeader(t *testing.T) {
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
	if _, err := c.ListAllCards(ctx); err != nil {
		t.Fatalf("ListAllCards: %v", err)
	}
	if got != wantToken {
		t.Errorf("X-Internal-Auth = %q, want %q", got, wantToken)
	}
}

// TestClient_ValidateDeckForBattle_MapsStatus は検証結果のステータスが呼び出し元へ SDK sentinel として伝播することを検証する。
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
}

package accountclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
)

func TestClient_InjectsInternalAuthHeader(t *testing.T) {
	const wantToken = "test.jwt.token"
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(internalauth.HeaderName)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New(srv.URL)
	ctx := internalauth.WithToken(context.Background(), wantToken)
	if err := c.IncrementBattleCount(ctx); err != nil {
		t.Fatalf("IncrementBattleCount: %v", err)
	}
	if got != wantToken {
		t.Errorf("X-Internal-Auth = %q, want %q", got, wantToken)
	}
}

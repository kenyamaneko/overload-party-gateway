package matchmakingclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
)

// TestClient_InjectsInternalAuthHeader は ADR-037 Phase 1 の検収項目:
// ctx に token があれば outbound に X-Internal-Auth が付与されることを保証する。
func TestClient_InjectsInternalAuthHeader(t *testing.T) {
	const wantToken = "test.jwt.token"
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(internalauth.HeaderName)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	c := New(srv.URL)
	ctx := internalauth.WithToken(context.Background(), wantToken)
	if err := c.Enqueue(ctx, "player-123", 42); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if got != wantToken {
		t.Errorf("X-Internal-Auth = %q, want %q", got, wantToken)
	}
}

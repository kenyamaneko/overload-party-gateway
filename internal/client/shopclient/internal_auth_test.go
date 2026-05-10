package shopclient

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
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"products":[]}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	ctx := internalauth.WithToken(context.Background(), wantToken)
	if _, err := c.GetProducts(ctx, "player-123"); err != nil {
		t.Fatalf("GetProducts: %v", err)
	}
	if got != wantToken {
		t.Errorf("X-Internal-Auth = %q, want %q", got, wantToken)
	}
}

func TestClient_NoHeaderWhenCtxAbsent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(internalauth.HeaderName)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"products":[]}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	if _, err := c.GetProducts(context.Background(), "player-123"); err != nil {
		t.Fatalf("GetProducts: %v", err)
	}
	if got != "" {
		t.Errorf("X-Internal-Auth = %q, want empty", got)
	}
}

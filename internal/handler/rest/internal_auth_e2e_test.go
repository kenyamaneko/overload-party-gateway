package rest

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/shopclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
)

// TestInternalAuth_E2E は ADR-037 Phase 1 の検収シナリオ:
// gin engine の middleware チェーン (withPlayerID → IssueInternalAuth) を組み、
// shop handler から呼び出した outbound HTTP に X-Internal-Auth が乗り、
// claims (sub / iss / kid / alg / exp-iat) が ADR-037 §1 の仕様と一致することを検証する。
func TestInternalAuth_E2E(t *testing.T) {
	const (
		secret    = "e2e-test-secret-32-bytes-or-longer"
		playerID  = "player-e2e-123"
	)

	var got string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(internalauth.HeaderName)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"products":[]}`))
	}))
	defer upstream.Close()

	signer := internalauth.NewSigner(
		internalauth.StaticHS256Resolver([]byte(secret), internalauth.DefaultKeyID),
		internalauth.DefaultKeyID,
	)
	shopHandler := NewShopHandler(shopclient.New(upstream.URL))

	r := gin.New()
	r.Use(withPlayerID(playerID))
	r.Use(middleware.IssueInternalAuth(signer))
	r.GET("/products", shopHandler.GetProducts)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/products", nil)
	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, "handler returned non-200; body=%s", rr.Body.String())
	require.NotEmpty(t, got, "X-Internal-Auth header was not received upstream")

	claims := &jwt.RegisteredClaims{}
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	parsed, err := parser.ParseWithClaims(got, claims, func(*jwt.Token) (any, error) {
		return []byte(secret), nil
	})
	require.NoError(t, err, "failed to parse JWT")
	require.True(t, parsed.Valid, "JWT signature invalid")

	assert.Equal(t, "HS256", parsed.Method.Alg(), "alg")
	assert.Equal(t, string(internalauth.DefaultKeyID), parsed.Header["kid"], "kid")
	assert.Equal(t, playerID, claims.Subject, "sub should equal player_id")
	assert.Equal(t, internalauth.Issuer, claims.Issuer, "iss")
	require.NotNil(t, claims.IssuedAt, "iat present")
	require.NotNil(t, claims.ExpiresAt, "exp present")
	assert.Equal(t, internalauth.DefaultTTL, claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time), "exp - iat == TTL")
	assert.WithinDuration(t, time.Now(), claims.IssuedAt.Time, 5*time.Second, "iat near now")
}

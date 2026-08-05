package rest

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/cardclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
)

func TestInternalAuth_E2E(t *testing.T) {
	t.Run("内部認証JWTのend-to-end伝搬", func(t *testing.T) {
		t.Run("player_idを設定して下流を呼ぶと、署名済みJWTがX-Internal-Authとして転送される", func(t *testing.T) {
			const playerID = "player-e2e-123"

			signingKey, err := rsa.GenerateKey(rand.Reader, 2048)
			require.NoError(t, err)

			var got string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.Header.Get(internalauth.HeaderName)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`[]`))
			}))
			defer upstream.Close()

			signer := internalauth.NewSigner(
				internalauth.StaticPrivateKeyResolver(signingKey, internalauth.DefaultKeyID),
				internalauth.DefaultKeyID,
			)
			cc := cardclient.New(upstream.URL, &http.Client{})

			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("player_id", playerID)
				c.Next()
			})
			r.Use(middleware.IssueInternalAuth(signer))
			r.GET("/cards", func(c *gin.Context) {
				if _, err := cc.ListAllCards(c.Request.Context()); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{})
			})

			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/cards", nil)
			r.ServeHTTP(rr, req)

			require.Equal(t, http.StatusOK, rr.Code, "handler returned non-200; body=%s", rr.Body.String())
			require.NotEmpty(t, got, "X-Internal-Auth header was not received upstream")

			claims := &jwt.RegisteredClaims{}
			parser := jwt.NewParser(jwt.WithoutClaimsValidation())
			parsed, err := parser.ParseWithClaims(got, claims, func(*jwt.Token) (any, error) {
				return &signingKey.PublicKey, nil
			})
			require.NoError(t, err, "failed to parse JWT")
			require.True(t, parsed.Valid, "JWT signature invalid")

			assert.Equal(t, "RS256", parsed.Method.Alg(), "alg")
			assert.Equal(t, string(internalauth.DefaultKeyID), parsed.Header["kid"], "kid")
			assert.Equal(t, playerID, claims.Subject, "sub should equal player_id")
			assert.Equal(t, internalauth.Issuer, claims.Issuer, "iss")
			require.NotNil(t, claims.IssuedAt, "iat present")
			require.NotNil(t, claims.ExpiresAt, "exp present")
			assert.Equal(t, internalauth.DefaultTTL, claims.ExpiresAt.Sub(claims.IssuedAt.Time), "exp - iat == TTL")
			assert.WithinDuration(t, time.Now(), claims.IssuedAt.Time, 5*time.Second, "iat near now")
		})
	})
}

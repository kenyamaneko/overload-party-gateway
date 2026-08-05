package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
)

func newTestSigner() *internalauth.Signer {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return internalauth.NewSigner(
		internalauth.StaticPrivateKeyResolver(key, internalauth.DefaultKeyID),
		internalauth.DefaultKeyID,
	)
}

func newErrorSigner() *internalauth.Signer {
	return internalauth.NewSigner(
		func(internalauth.KeyID) (*rsa.PrivateKey, error) { return nil, errors.New("boom") },
		internalauth.DefaultKeyID,
	)
}

func TestIssueInternalAuth(t *testing.T) {
	t.Run("内部認証tokenの発行と注入", func(t *testing.T) {
		t.Run("player_idがあるとき、tokenを発行しRequest.Contextに注入する", func(t *testing.T) {
			engine := gin.New()
			var observedToken string
			var observedOK bool
			engine.GET("/test",
				func(c *gin.Context) {
					c.Set(string(playerIDKey), "player-123")
					c.Next()
				},
				IssueInternalAuth(newTestSigner()),
				func(c *gin.Context) {
					observedToken, observedOK = internalauth.TokenFrom(c.Request.Context())
					c.Status(http.StatusOK)
				},
			)

			rr := httptest.NewRecorder()
			engine.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/test", nil))

			require.Equal(t, http.StatusOK, rr.Code)
			assert.True(t, observedOK)
			assert.NotEmpty(t, observedToken)
		})

		cases := []struct {
			name        string
			setupPlayer gin.HandlerFunc
			signer      *internalauth.Signer
			wantStatus  int
		}{
			{
				name:        "player_idが未設定のとき、401になり下流に到達しない",
				setupPlayer: func(c *gin.Context) { c.Next() },
				signer:      newTestSigner(),
				wantStatus:  http.StatusUnauthorized,
			},
			{
				name: "signerがエラーのとき、500になり下流に到達しない",
				setupPlayer: func(c *gin.Context) {
					c.Set(string(playerIDKey), "player-123")
					c.Next()
				},
				signer:     newErrorSigner(),
				wantStatus: http.StatusInternalServerError,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				engine := gin.New()
				var sawDownstream bool
				engine.GET("/test",
					tc.setupPlayer,
					IssueInternalAuth(tc.signer),
					func(c *gin.Context) {
						sawDownstream = true
						c.Status(http.StatusOK)
					},
				)

				rr := httptest.NewRecorder()
				engine.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/test", nil))

				require.Equal(t, tc.wantStatus, rr.Code)
				assert.False(t, sawDownstream)
			})
		}
	})
}

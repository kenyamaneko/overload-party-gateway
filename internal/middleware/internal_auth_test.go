package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
)

const testSecret = "test-secret-32-bytes-long-xxxxxxxx"

func newTestSigner() *internalauth.Signer {
	return internalauth.NewSigner(
		internalauth.StaticHS256Resolver([]byte(testSecret), internalauth.DefaultKeyID),
		internalauth.DefaultKeyID,
	)
}

func newErrorSigner() *internalauth.Signer {
	return internalauth.NewSigner(
		func(internalauth.KeyID) ([]byte, error) { return nil, errors.New("boom") },
		internalauth.DefaultKeyID,
	)
}

func TestIssueInternalAuth_TokenInjected(t *testing.T) {
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
}

func TestIssueInternalAuth_Failure(t *testing.T) {
	cases := []struct {
		name        string
		setupPlayer gin.HandlerFunc
		signer      *internalauth.Signer
		wantStatus  int
	}{
		{
			name:        "player_id 未設定で 401 を返す",
			setupPlayer: func(c *gin.Context) { c.Next() },
			signer:      newTestSigner(),
			wantStatus:  http.StatusUnauthorized,
		},
		{
			name: "signer のエラーで 500 を返す",
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
}

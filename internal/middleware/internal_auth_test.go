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

func TestIssueInternalAuth(t *testing.T) {
	const testSecret = "test-secret-32-bytes-long-xxxxxxxx"
	signer := internalauth.NewSigner(
		internalauth.StaticHS256Resolver([]byte(testSecret), internalauth.DefaultKeyID),
		internalauth.DefaultKeyID,
	)

	cases := []struct {
		name           string
		setPlayerID    string
		signer         *internalauth.Signer
		wantStatus     int
		wantTokenInCtx bool
	}{
		{
			name:           "player_id present issues token into request context",
			setPlayerID:    "player-123",
			signer:         signer,
			wantStatus:     http.StatusOK,
			wantTokenInCtx: true,
		},
		{
			name:        "missing player_id returns 401",
			setPlayerID: "",
			signer:      signer,
			wantStatus:  http.StatusUnauthorized,
		},
		{
			name:        "signer error returns 500",
			setPlayerID: "player-123",
			signer: internalauth.NewSigner(
				func(internalauth.KeyID) ([]byte, error) { return nil, errors.New("boom") },
				internalauth.DefaultKeyID,
			),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			var observedToken string
			var observedOK bool
			engine.GET("/test",
				func(c *gin.Context) {
					if tc.setPlayerID != "" {
						c.Set(string(playerIDKey), tc.setPlayerID)
					}
					c.Next()
				},
				IssueInternalAuth(tc.signer),
				func(c *gin.Context) {
					observedToken, observedOK = internalauth.TokenFrom(c.Request.Context())
					c.Status(http.StatusOK)
				},
			)

			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			engine.ServeHTTP(rr, req)

			require.Equal(t, tc.wantStatus, rr.Code)
			if tc.wantTokenInCtx {
				assert.True(t, observedOK, "expected token in request context")
				assert.NotEmpty(t, observedToken)
			} else {
				assert.False(t, observedOK, "expected no token in request context")
			}
		})
	}
}

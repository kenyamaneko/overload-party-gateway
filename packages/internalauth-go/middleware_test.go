package internalauth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeVerifier は Verifier の最小 fake 実装。
type fakeVerifier struct {
	playerID string
	err      error
}

func (f *fakeVerifier) Verify(string) (string, error) {
	return f.playerID, f.err
}

var _ Verifier = (*fakeVerifier)(nil)

func newAuthTestEngine(verifier Verifier) (*gin.Engine, *string) {
	r := gin.New()
	var observed string
	r.GET("/probe", VerifyInternalAuth(verifier), func(c *gin.Context) {
		observed = c.GetString(PlayerIDContextKey)
		c.Status(http.StatusOK)
	})
	return r, &observed
}

func TestVerifyInternalAuth_Success(t *testing.T) {
	verifier := &fakeVerifier{playerID: "player-123"}
	engine, observed := newAuthTestEngine(verifier)

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set(HeaderName, "any.signed.token")

	rr := httptest.NewRecorder()
	engine.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "player-123", *observed)
}

func TestVerifyInternalAuth_Unauthorized(t *testing.T) {
	cases := []struct {
		name     string
		verifier Verifier
		setupReq func(*http.Request)
	}{
		{
			name:     "X-Internal-Auth が欠落していれば 401",
			verifier: &fakeVerifier{playerID: "irrelevant"},
			setupReq: func(*http.Request) {},
		},
		{
			name:     "verifier が error を返すなら 401",
			verifier: &fakeVerifier{err: errors.New("invalid token")},
			setupReq: func(r *http.Request) { r.Header.Set(HeaderName, "any.signed.token") },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine, observed := newAuthTestEngine(tc.verifier)
			req := httptest.NewRequest(http.MethodGet, "/probe", nil)
			tc.setupReq(req)

			rr := httptest.NewRecorder()
			engine.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusUnauthorized, rr.Code)
			assert.Empty(t, *observed)
		})
	}
}

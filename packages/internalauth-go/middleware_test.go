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
	verifier := &MockVerifier{VerifyFn: func(string) (string, error) { return "player-123", nil }}
	engine, observed := newAuthTestEngine(verifier)

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set(HeaderName, "any.signed.token")

	rr := httptest.NewRecorder()
	engine.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "player-123", *observed)
}

func TestVerifyInternalAuth_PreservesToken(t *testing.T) {
	const sentToken = "any.signed.token"
	verifier := &MockVerifier{VerifyFn: func(string) (string, error) { return "player-123", nil }}

	r := gin.New()
	var ginCtxToken string
	var requestCtxToken string
	var requestCtxOK bool
	r.GET("/probe", VerifyInternalAuth(verifier), func(c *gin.Context) {
		ginCtxToken = c.GetString(TokenContextKey)
		requestCtxToken, requestCtxOK = TokenFrom(c.Request.Context())
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.Header.Set(HeaderName, sentToken)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, sentToken, ginCtxToken, "gin context に raw token が保存される")
	assert.True(t, requestCtxOK, "Request.Context に token が伝搬する")
	assert.Equal(t, sentToken, requestCtxToken)
}

func TestVerifyInternalAuth_Unauthorized(t *testing.T) {
	cases := []struct {
		name     string
		verifier Verifier
		setupReq func(*http.Request)
	}{
		{
			// VerifyFn 未設定の MockVerifier は呼ばれると panic するため、header 欠落時に
			// verifier へ到達しないことも同時に確かめている
			name:     "X-Internal-Auth が欠落していれば 401",
			verifier: &MockVerifier{},
			setupReq: func(*http.Request) {},
		},
		{
			name:     "verifier が error を返すなら 401",
			verifier: &MockVerifier{VerifyFn: func(string) (string, error) { return "", errors.New("invalid token") }},
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

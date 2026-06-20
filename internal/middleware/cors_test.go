package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupRouter(allowedOrigins ...string) *gin.Engine {
	r := gin.New()
	r.Use(UseCORS(allowedOrigins...))
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	r.OPTIONS("/test", func(c *gin.Context) {
		// preflight では到達しない。middleware が先に abort する。
		c.Status(http.StatusOK)
	})
	return r
}

// TestCORS は許可判定と preflight に応じた CORS ヘッダ付与を検証する。
func TestCORS(t *testing.T) {
	const (
		allowMethods = "GET, POST, PUT, DELETE, OPTIONS"
		allowHeaders = "Authorization, Content-Type"
	)
	tests := []struct {
		name           string
		allowedOrigins []string
		method         string
		reqHeaders     map[string]string
		wantCode       int
		wantOrigin     string
		wantMethods    string
		wantHeaders    string
		wantMaxAge     string
	}{
		{
			name:           "no origin header passes through without CORS headers",
			allowedOrigins: []string{"http://example.com"},
			method:         http.MethodGet,
			reqHeaders:     nil,
			wantCode:       http.StatusOK,
		},
		{
			name:           "allowed origin receives full CORS headers",
			allowedOrigins: []string{"http://example.com", "http://other.com"},
			method:         http.MethodGet,
			reqHeaders:     map[string]string{"Origin": "http://example.com"},
			wantCode:       http.StatusOK,
			wantOrigin:     "http://example.com",
			wantMethods:    allowMethods,
			wantHeaders:    allowHeaders,
			wantMaxAge:     corsMaxAge,
		},
		{
			name:           "denied origin passes through without CORS headers",
			allowedOrigins: []string{"http://example.com"},
			method:         http.MethodGet,
			reqHeaders:     map[string]string{"Origin": "http://evil.com"},
			wantCode:       http.StatusOK,
		},
		{
			name:           "empty allowlist allows any origin",
			allowedOrigins: nil,
			method:         http.MethodGet,
			reqHeaders:     map[string]string{"Origin": "http://anything.com"},
			wantCode:       http.StatusOK,
			wantOrigin:     "http://anything.com",
			wantMethods:    allowMethods,
			wantHeaders:    allowHeaders,
			wantMaxAge:     corsMaxAge,
		},
		{
			name:           "preflight on allowed origin returns 204 with CORS headers",
			allowedOrigins: []string{"http://example.com"},
			method:         http.MethodOptions,
			reqHeaders:     map[string]string{"Origin": "http://example.com"},
			wantCode:       http.StatusNoContent,
			wantOrigin:     "http://example.com",
			wantMethods:    allowMethods,
			wantHeaders:    allowHeaders,
			wantMaxAge:     corsMaxAge,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := setupRouter(tt.allowedOrigins...)
			req := httptest.NewRequest(tt.method, "/test", nil)
			for k, v := range tt.reqHeaders {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()

			r.ServeHTTP(w, req)

			assert.Equal(t, tt.wantCode, w.Code)
			assert.Equal(t, tt.wantOrigin, w.Header().Get("Access-Control-Allow-Origin"))
			assert.Equal(t, tt.wantMethods, w.Header().Get("Access-Control-Allow-Methods"))
			assert.Equal(t, tt.wantHeaders, w.Header().Get("Access-Control-Allow-Headers"))
			assert.Equal(t, tt.wantMaxAge, w.Header().Get("Access-Control-Max-Age"))
		})
	}
}

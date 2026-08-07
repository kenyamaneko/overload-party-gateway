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

func TestCORS(t *testing.T) {
	const (
		allowMethods = "GET, POST, PUT, DELETE, OPTIONS"
		allowHeaders = "Authorization, Content-Type"
	)
	t.Run("[HTTPミドルウェア]CORSヘッダーの付与", func(t *testing.T) {
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
				name:           "Originヘッダーが無いとき、CORSヘッダーなしで通過する",
				allowedOrigins: []string{"http://example.com"},
				method:         http.MethodGet,
				reqHeaders:     nil,
				wantCode:       http.StatusOK,
			},
			{
				name:           "許可されたOriginのとき、全CORSヘッダーが付く",
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
				name:           "許可されないOriginのとき、CORSヘッダーなしで通過する",
				allowedOrigins: []string{"http://example.com"},
				method:         http.MethodGet,
				reqHeaders:     map[string]string{"Origin": "http://evil.com"},
				wantCode:       http.StatusOK,
			},
			{
				name:           "許可リストが空のとき、任意のOriginを許可する",
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
				name:           "許可されたOriginへのpreflightのとき、204とCORSヘッダーを返す",
				allowedOrigins: []string{"http://example.com"},
				method:         http.MethodOptions,
				reqHeaders:     map[string]string{"Origin": "http://example.com"},
				wantCode:       http.StatusNoContent,
				wantOrigin:     "http://example.com",
				wantMethods:    allowMethods,
				wantHeaders:    allowHeaders,
				wantMaxAge:     corsMaxAge,
			},
			{
				name:           "許可されないOriginからのpreflightのとき、204で応答せずCORSヘッダーなしで後続処理に渡る",
				allowedOrigins: []string{"http://example.com"},
				method:         http.MethodOptions,
				reqHeaders:     map[string]string{"Origin": "http://evil.com"},
				wantCode:       http.StatusOK,
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
	})
}

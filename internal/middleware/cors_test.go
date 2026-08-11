package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestUseCORS(t *testing.T) {
	t.Run("[接続管理]オリジン許可判定とプリフライト応答", func(t *testing.T) {
		t.Run("リクエストにOriginヘッダーが無いとき、CORSヘッダーは付与されない", func(t *testing.T) {
			reached := false
			r := gin.New()
			r.GET("/test", UseCORS("https://allowed.example"), func(c *gin.Context) {
				reached = true
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, "", w.Header().Get("Access-Control-Allow-Origin"))
			assert.True(t, reached)
		})

		cases := []struct {
			name             string
			allowedOrigins   []string
			origin           string
			method           string
			wantStatus       int
			wantOriginHeader string
			wantReached      bool
		}{
			{
				name:             "許可オリジンが1つも設定されていないとき(制限なしモード)、どのOriginでもCORSヘッダーが付与される",
				allowedOrigins:   nil,
				origin:           "https://random-origin.example",
				method:           http.MethodGet,
				wantStatus:       http.StatusOK,
				wantOriginHeader: "https://random-origin.example",
				wantReached:      true,
			},
			{
				name:             "許可オリジンが1件以上設定されており、リクエストのOriginがそのいずれかと一致するとき、CORSヘッダーが付与される",
				allowedOrigins:   []string{"https://allowed.example", "https://also-allowed.example"},
				origin:           "https://allowed.example",
				method:           http.MethodGet,
				wantStatus:       http.StatusOK,
				wantOriginHeader: "https://allowed.example",
				wantReached:      true,
			},
			{
				name:             "許可オリジンが設定されており、リクエストのOriginがどれとも一致しないとき、CORSヘッダーは付与されない。ただしリクエスト自体は拒否されず後続の処理に進む",
				allowedOrigins:   []string{"https://allowed.example"},
				origin:           "https://not-allowed.example",
				method:           http.MethodGet,
				wantStatus:       http.StatusOK,
				wantOriginHeader: "",
				wantReached:      true,
			},
			{
				name:             "Originが許可されており、かつリクエストメソッドがOPTIONS(プリフライト)のとき、204(No Content)を返してそこで処理を終える(後続のハンドラは実行されない)",
				allowedOrigins:   []string{"https://allowed.example"},
				origin:           "https://allowed.example",
				method:           http.MethodOptions,
				wantStatus:       http.StatusNoContent,
				wantOriginHeader: "https://allowed.example",
				wantReached:      false,
			},
			{
				name:             "Originが許可されており、かつリクエストメソッドがOPTIONS以外のとき、CORSヘッダーを付与した上で後続の処理に進む",
				allowedOrigins:   []string{"https://allowed.example"},
				origin:           "https://allowed.example",
				method:           http.MethodPost,
				wantStatus:       http.StatusOK,
				wantOriginHeader: "https://allowed.example",
				wantReached:      true,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				reached := false
				r := gin.New()
				r.Handle(tc.method, "/test", UseCORS(tc.allowedOrigins...), func(c *gin.Context) {
					reached = true
					c.Status(http.StatusOK)
				})

				req := httptest.NewRequest(tc.method, "/test", nil)
				req.Header.Set("Origin", tc.origin)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)

				assert.Equal(t, tc.wantStatus, w.Code)
				assert.Equal(t, tc.wantOriginHeader, w.Header().Get("Access-Control-Allow-Origin"))
				assert.Equal(t, tc.wantReached, reached)
			})
		}

		t.Run("Originが許可されてCORSヘッダーが付与されるとき、許可するHTTPメソッド群・許可するリクエストヘッダー群・プリフライト結果のキャッシュ有効期間(24時間)が併せて通知される", func(t *testing.T) {
			r := gin.New()
			r.GET("/test", UseCORS("https://allowed.example"), func(c *gin.Context) {
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Origin", "https://allowed.example")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.NotEmpty(t, w.Header().Get("Access-Control-Allow-Methods"))
			assert.NotEmpty(t, w.Header().Get("Access-Control-Allow-Headers"))
			assert.Equal(t, "86400", w.Header().Get("Access-Control-Max-Age"))
		})
	})
}

package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORS プリフライトのキャッシュ有効期間（秒）。24 時間。
const corsMaxAge = "86400"

// CORS は CORS ヘッダーを設定する Gin middleware を返します。
// 本番では allowedOrigins で許可オリジンを制限する。
func CORS(allowedOrigins ...string) gin.HandlerFunc {
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		origins[o] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}

		allowed := len(origins) == 0
		if !allowed {
			_, allowed = origins[origin]
		}
		if !allowed {
			c.Next()
			return
		}

		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
		c.Header("Access-Control-Max-Age", corsMaxAge)

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

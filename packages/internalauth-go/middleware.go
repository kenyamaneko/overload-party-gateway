package internalauth

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// VerifyInternalAuth は X-Internal-Auth (HMAC JWT) の検証を行う Gin middleware を返す。
// header 欠落 / 検証失敗時はいずれも 401 を返す。
func VerifyInternalAuth(verifier Verifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader(HeaderName)
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": HeaderName + " header is required"})
			return
		}
		playerID, err := verifier.Verify(token)
		if err != nil {
			slog.WarnContext(c.Request.Context(), "internal auth verify failed",
				slog.String("error", err.Error()),
			)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid internal auth token"})
			return
		}
		c.Set(PlayerIDContextKey, playerID)
		c.Next()
	}
}

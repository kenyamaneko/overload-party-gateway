package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
)

// IssueInternalAuth は context の player_id から HMAC JWT を発行し、Request.Context に格納する
// Gin middleware を返す。
func IssueInternalAuth(signer *internalauth.Signer) gin.HandlerFunc {
	return func(c *gin.Context) {
		playerID := GetPlayerID(c)
		if playerID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing player id"})
			return
		}

		token, err := signer.Issue(playerID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to issue internal auth token"})
			return
		}

		ctx := internalauth.WithToken(c.Request.Context(), token)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

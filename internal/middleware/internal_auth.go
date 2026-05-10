package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
)

// IssueInternalAuth は player_id 解決済みの request に対し、ADR-037 の HMAC JWT を発行して
// Request.Context に格納する Gin middleware を返す。
//
// PlayerResolve の後段にチェインする前提。各 client は Request.Context から
// internalauth.InjectHeader(ctx, h) で X-Internal-Auth ヘッダを 1 行で取り出す。
//
// player_id 未設定時は 401、JWT 発行失敗時は 500 を返して fail-fast する
// (発行失敗は鍵設定ミス等の決定論的エラーで、握りつぶすとデバッグ不能になるため)。
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

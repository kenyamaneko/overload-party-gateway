package middleware

import (
	"context"
	"net/http"
	"strings"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

type contextKey string

const firebaseUIDKey contextKey = "firebase_uid"
const playerIDKey contextKey = "player_id"

// DevTokenPrefix は dev/local モードの認証トークンプレフィックスです。
// トークン形式: "dev-token-{uid}"。
const DevTokenPrefix = "dev-token-"

// FirebaseAuth は Firebase ID トークンを検証する Gin middleware を返します
func FirebaseAuth(authClient *auth.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing authorization header"})
			return
		}

		idToken := strings.TrimPrefix(authHeader, "Bearer ")
		if idToken == authHeader {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid authorization format"})
			return
		}

		token, err := authClient.VerifyIDToken(c.Request.Context(), idToken)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		c.Set(string(firebaseUIDKey), token.UID)
		c.Next()
	}
}

// GetFirebaseUID は context から Firebase UID を取得します
func GetFirebaseUID(c *gin.Context) string {
	uid, _ := c.Get(string(firebaseUIDKey))
	if s, ok := uid.(string); ok {
		return s
	}
	return ""
}

// GetPlayerID は context からプレイヤー ID を取得します
func GetPlayerID(c *gin.Context) string {
	id, _ := c.Get(string(playerIDKey))
	if s, ok := id.(string); ok {
		return s
	}
	return ""
}

// PlayerResolve は認証済み Firebase UID を account サービス経由でプレイヤー UUID に解決する Gin middleware を返します。
// FirebaseAuth の後にチェインする必要がある。
func PlayerResolve(accountClient port.AccountClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := GetFirebaseUID(c)
		if uid == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing firebase uid"})
			return
		}

		player, err := accountClient.FindByFirebaseUID(c.Request.Context(), uid)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve player"})
			return
		}
		if player == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "player not registered"})
			return
		}

		c.Set(string(playerIDKey), player.PlayerID)
		c.Next()
	}
}

// NewFirebaseAuthClient は Firebase Auth クライアントを生成します
func NewFirebaseAuthClient(ctx context.Context) (*auth.Client, error) {
	app, err := firebase.NewApp(ctx, nil)
	if err != nil {
		return nil, err
	}
	return app.Auth(ctx)
}

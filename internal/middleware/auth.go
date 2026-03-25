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

// DevTokenPrefix is the prefix for dev/local mode authentication tokens.
// Token format: "dev-token-{uid}".
const DevTokenPrefix = "dev-token-"

// FirebaseAuth returns a Gin middleware that verifies Firebase ID tokens.
// Every REST request must include a valid Bearer token.
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

// GetFirebaseUID extracts the Firebase UID set by the auth middleware.
func GetFirebaseUID(c *gin.Context) string {
	uid, _ := c.Get(string(firebaseUIDKey))
	if s, ok := uid.(string); ok {
		return s
	}
	return ""
}

// GetPlayerID extracts the player ID set by the auth middleware.
func GetPlayerID(c *gin.Context) string {
	id, _ := c.Get(string(playerIDKey))
	if s, ok := id.(string); ok {
		return s
	}
	return ""
}

// PlayerResolve returns a Gin middleware that resolves the authenticated
// Firebase UID to a player UUID. Must be chained AFTER FirebaseAuth.
// Sets "player_id" in the Gin context. Returns 401 if the player is not registered.
func PlayerResolve(playerRepo port.PlayerRepo) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := GetFirebaseUID(c)
		if uid == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing firebase uid"})
			return
		}

		player, err := playerRepo.FindByFirebaseUID(c.Request.Context(), uid)
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

// NewFirebaseAuthClient initializes a Firebase Auth client using Application Default Credentials.
func NewFirebaseAuthClient(ctx context.Context) (*auth.Client, error) {
	app, err := firebase.NewApp(ctx, nil)
	if err != nil {
		return nil, err
	}
	return app.Auth(ctx)
}

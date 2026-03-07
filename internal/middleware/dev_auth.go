package middleware

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

// DevAuth returns a Gin middleware that accepts dev tokens without Firebase.
// Token format: "dev-token-{uid}" → sets firebase_uid context key to "{uid}".
func DevAuth() gin.HandlerFunc {
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

		if !strings.HasPrefix(idToken, "dev-token-") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid dev token format"})
			return
		}

		uid := strings.TrimPrefix(idToken, "dev-token-")
		c.Set(string(firebaseUIDKey), uid)
		c.Next()
	}
}

// DevPlayerSetup is called after a new dev player is auto-created.
// It receives the context and the new player's ID for additional setup (e.g. starter deck).
type DevPlayerSetup func(ctx context.Context, playerID string) error

// DevRegisterFunc abstracts player registration so the middleware doesn't
// depend on model/service details. It should register the player and return
// the resulting player ID. It may return ErrPlayerAlreadyRegistered if the
// player already exists, which is not an error in this context.
type DevRegisterFunc func(ctx context.Context, firebaseUID, username string) (playerID string, err error)

// DevAuthWithPlayerResolve returns a DevAuth middleware that also resolves
// firebase_uid → playerID, auto-creating the player if needed.
// Sets both "firebase_uid" and "player_id" context keys.
// Optional onCreated callbacks run once after a new player is created.
func DevAuthWithPlayerResolve(playerRepo repository.PlayerRepo, registerFn DevRegisterFunc, onCreated ...DevPlayerSetup) gin.HandlerFunc {
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

		if !strings.HasPrefix(idToken, "dev-token-") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid dev token format"})
			return
		}

		uid := strings.TrimPrefix(idToken, "dev-token-")
		c.Set(string(firebaseUIDKey), uid)

		playerID, created, err := resolveOrCreateDevPlayer(c, playerRepo, registerFn, uid)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("resolve player: %v", err)})
			return
		}
		c.Set(string(playerIDKey), playerID)

		if created {
			for _, fn := range onCreated {
				if err := fn(c.Request.Context(), playerID); err != nil {
					log.Printf("dev player setup failed: %v", err)
				}
			}
		}

		c.Next()
	}
}

func resolveOrCreateDevPlayer(c *gin.Context, playerRepo repository.PlayerRepo, registerFn DevRegisterFunc, firebaseUID string) (string, bool, error) {
	ctx := c.Request.Context()

	player, err := playerRepo.FindByFirebaseUID(ctx, firebaseUID)
	if err != nil {
		return "", false, fmt.Errorf("find by firebase uid: %w", err)
	}
	if player != nil {
		return player.PlayerID, false, nil
	}

	username := "Dev_" + firebaseUID
	playerID, err := registerFn(ctx, firebaseUID, username)
	if err != nil {
		return "", false, fmt.Errorf("register dev player: %w", err)
	}
	log.Printf("auto-created dev player: uid=%s playerID=%s username=%s", firebaseUID, playerID, username)

	return playerID, true, nil
}

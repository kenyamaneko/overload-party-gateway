package middleware

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/civil"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/kenyamaneko/overload-party-common/model"
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

// DevAuthWithPlayerResolve returns a DevAuth middleware that also resolves
// firebase_uid → playerID, auto-creating the player if needed.
// Sets both "firebase_uid" and "player_id" context keys.
// Optional onCreated callbacks run once after a new player is created.
func DevAuthWithPlayerResolve(playerRepo repository.PlayerRepo, onCreated ...DevPlayerSetup) gin.HandlerFunc {
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

		playerID, created, err := resolveOrCreateDevPlayer(c, playerRepo, uid)
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

func resolveOrCreateDevPlayer(c *gin.Context, playerRepo repository.PlayerRepo, firebaseUID string) (string, bool, error) {
	player, err := playerRepo.FindByFirebaseUID(c.Request.Context(), firebaseUID)
	if err != nil {
		return "", false, fmt.Errorf("find by firebase uid: %w", err)
	}
	if player != nil {
		return player.PlayerID, false, nil
	}

	now := time.Now()
	newPlayer := &model.Player{
		PlayerID:    uuid.New().String(),
		FirebaseUID: firebaseUID,
		Username:    "Dev_" + firebaseUID,
		Level:       1,
		Exp:         0,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	dailyBattle := &model.PlayerDailyBattle{
		PlayerID:         newPlayer.PlayerID,
		DailyBattleCount: 0,
		LastResetDate:    civil.DateOf(now.UTC()),
	}

	if err := playerRepo.Create(c.Request.Context(), newPlayer, dailyBattle); err != nil {
		return "", false, fmt.Errorf("auto-create player: %w", err)
	}
	log.Printf("auto-created dev player: uid=%s playerID=%s username=%s", firebaseUID, newPlayer.PlayerID, newPlayer.Username)

	return newPlayer.PlayerID, true, nil
}

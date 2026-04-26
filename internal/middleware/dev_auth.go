package middleware

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
)

// DevAuth は Firebase なしで dev トークンを受け付ける Gin middleware を返します。
// トークン形式: "dev-token-{uid}" → firebase_uid context key に "{uid}" を設定。
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

		if !strings.HasPrefix(idToken, DevTokenPrefix) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid dev token format"})
			return
		}

		uid := strings.TrimPrefix(idToken, DevTokenPrefix)
		c.Set(string(firebaseUIDKey), uid)
		c.Next()
	}
}

// DevPlayerSetup は dev プレイヤーが自動作成された後に呼ばれるコールバックです
type DevPlayerSetup func(ctx context.Context, playerID string) error

// DevAuthWithPlayerResolve は dev トークン認証と firebase_uid → playerID 解決を行う Gin middleware を返します。
// プレイヤーが未登録の場合は account サービス経由で自動作成する。
func DevAuthWithPlayerResolve(accountClient *accountclient.Client, onCreated ...DevPlayerSetup) gin.HandlerFunc {
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

		if !strings.HasPrefix(idToken, DevTokenPrefix) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid dev token format"})
			return
		}

		uid := strings.TrimPrefix(idToken, DevTokenPrefix)
		c.Set(string(firebaseUIDKey), uid)

		playerID, created, err := resolveOrCreateDevPlayer(c.Request.Context(), accountClient, uid)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("resolve player: %v", err)})
			return
		}
		c.Set(string(playerIDKey), playerID)

		if created {
			for _, fn := range onCreated {
				if err := fn(c.Request.Context(), playerID); err != nil {
					c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("dev player setup: %v", err)})
					return
				}
			}
		}

		c.Next()
	}
}

func resolveOrCreateDevPlayer(ctx context.Context, accountClient *accountclient.Client, firebaseUID string) (string, bool, error) {
	player, err := accountClient.FindByFirebaseUID(ctx, firebaseUID)
	if err != nil {
		return "", false, fmt.Errorf("find by firebase uid: %w", err)
	}
	if player != nil {
		return player.PlayerID, false, nil
	}

	newPlayer, err := accountClient.Register(ctx, firebaseUID)
	if err != nil {
		// 並行リクエストとの競合時は FindByFirebaseUID にフォールバック
		if errors.Is(err, accountclient.ErrPlayerAlreadyRegistered) {
			existing, lerr := accountClient.FindByFirebaseUID(ctx, firebaseUID)
			if lerr != nil {
				return "", false, fmt.Errorf("recover from race: %w", lerr)
			}
			if existing == nil {
				return "", false, errors.New("register returned already-registered but player not found")
			}
			return existing.PlayerID, false, nil
		}
		return "", false, fmt.Errorf("register dev player: %w", err)
	}
	log.Printf("auto-created dev player: uid=%s playerID=%s (name unset until onboarding)", firebaseUID, newPlayer.PlayerID)

	return newPlayer.PlayerID, true, nil
}

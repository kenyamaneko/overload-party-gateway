package ws

import (
	"log"
	"net/http"
	"strings"

	"firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// Handler upgrades HTTP connections to WebSocket and hands off to Manager.
type Handler struct {
	manager    *Manager
	authClient *auth.Client // nil in local/dev mode
	playerRepo port.PlayerRepo
	upgrader   websocket.Upgrader
}

func NewHandler(manager *Manager, authClient *auth.Client, playerRepo port.PlayerRepo, allowedOrigins []string) *Handler {
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		origins[o] = struct{}{}
	}

	return &Handler{
		manager:    manager,
		authClient: authClient,
		playerRepo: playerRepo,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  wsReadBufferSize,
			WriteBufferSize: wsWriteBufferSize,
			CheckOrigin: func(r *http.Request) bool {
				if len(origins) == 0 {
					return true // dev/local: no restriction
				}
				_, ok := origins[r.Header.Get("Origin")]
				return ok
			},
		},
	}
}

// HandleUpgrade handles GET /ws?token=<firebase_id_token>
func (h *Handler) HandleUpgrade(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
		return
	}

	var playerID string

	if h.authClient != nil {
		// Production: verify Firebase token
		decoded, err := h.authClient.VerifyIDToken(c.Request.Context(), token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		player, err := h.playerRepo.FindByFirebaseUID(c.Request.Context(), decoded.UID)
		if err != nil || player == nil {
			log.Printf("ws handler: find player by firebase uid %s: %v", decoded.UID, err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "player not registered"})
			return
		}
		playerID = player.PlayerID
	} else {
		// Local/dev: extract UID from dev token and resolve player
		if !strings.HasPrefix(token, middleware.DevTokenPrefix) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid dev token format"})
			return
		}
		uid := strings.TrimPrefix(token, middleware.DevTokenPrefix)
		player, err := h.playerRepo.FindByFirebaseUID(c.Request.Context(), uid)
		if err != nil || player == nil {
			log.Printf("ws handler (dev): player not found for uid=%s: %v", uid, err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "player not registered"})
			return
		}
		playerID = player.PlayerID
	}

	wsConn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}

	conn := NewConnection(wsConn, playerID)
	h.manager.Hub.Register(conn)

	go conn.WritePump()
	go conn.ReadPump(h.manager.Hub, h.manager)
}

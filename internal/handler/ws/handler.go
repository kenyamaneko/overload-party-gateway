package ws

import (
	"log"
	"net/http"
	"strings"

	"firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
)

// Handler は HTTP 接続を WebSocket にアップグレードし Manager に引き渡します
type Handler struct {
	manager       *Manager
	authClient    *auth.Client // nil in local/dev mode
	accountClient *accountclient.Client
	upgrader      websocket.Upgrader
}

// NewHandler は WebSocket Handler を生成します
func NewHandler(manager *Manager, authClient *auth.Client, accountClient *accountclient.Client, allowedOrigins []string) *Handler {
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		origins[o] = struct{}{}
	}

	return &Handler{
		manager:       manager,
		authClient:    authClient,
		accountClient: accountClient,
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

// HandleUpgrade は GET /ws?token=<firebase_id_token> の WebSocket アップグレードを処理します
func (h *Handler) HandleUpgrade(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing token"})
		return
	}

	var playerID string

	if h.authClient != nil {
		decoded, err := h.authClient.VerifyIDToken(c.Request.Context(), token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}
		player, err := h.accountClient.FindByFirebaseUID(c.Request.Context(), decoded.UID)
		if err != nil || player == nil {
			log.Printf("ws handler: find player by firebase uid %s: %v", decoded.UID, err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "player not registered"})
			return
		}
		playerID = player.PlayerID
	} else {
		if !strings.HasPrefix(token, middleware.DevTokenPrefix) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid dev token format"})
			return
		}
		uid := strings.TrimPrefix(token, middleware.DevTokenPrefix)
		player, err := h.accountClient.FindByFirebaseUID(c.Request.Context(), uid)
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

package router

import (
	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/handler/rest"
)

// Handlers は全 REST ハンドラーをグループ化し、cmd/main と cmd/local でルート定義を共有します
type Handlers struct {
	Auth     *rest.AuthHandler
	GameLog  *rest.GameLogHandler
	NPC      *rest.NPCHandler
	Spectate *rest.SpectateHandler
}

// RegisterAuthRoutes は認証エンドポイント（register / login）を登録します
func RegisterAuthRoutes(group *gin.RouterGroup, h *Handlers) {
	group.POST("/auth/register", h.Auth.Register)
	group.POST("/auth/login", h.Auth.Login)
}

// RegisterAPIRoutes は認証済み + プレイヤー解決済みの全エンドポイントを登録します
func RegisterAPIRoutes(api *gin.RouterGroup, h *Handlers) {
	api.GET("/games/:gameId/log", h.GameLog.GetGameLog)
	api.GET("/games/:gameId/log/text", h.GameLog.GetGameLogText)

	api.GET("/npc/models", h.NPC.GetNPCModels)

	api.GET("/spectate/games", h.Spectate.GetActiveGames)
}

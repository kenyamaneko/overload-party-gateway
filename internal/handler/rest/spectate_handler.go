package rest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	ws "github.com/kenyamaneko/overload-party-gateway/internal/handler/ws"
)

// SpectateHandler は観戦関連の REST エンドポイントを処理します
type SpectateHandler struct {
	wsManager *ws.Manager
}

// NewSpectateHandler は SpectateHandler を生成します
func NewSpectateHandler(wsManager *ws.Manager) *SpectateHandler {
	return &SpectateHandler{wsManager: wsManager}
}

// GetActiveGames は現在観戦可能なゲーム一覧を返します
func (h *SpectateHandler) GetActiveGames(c *gin.Context) {
	games := h.wsManager.ActiveSpectateGames(c.Request.Context())
	c.JSON(http.StatusOK, games)
}

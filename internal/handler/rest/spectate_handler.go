package rest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	ws "github.com/kenyamaneko/overload-party-gateway/internal/handler/ws"
)

type SpectateHandler struct {
	wsManager *ws.Manager
}

func NewSpectateHandler(wsManager *ws.Manager) *SpectateHandler {
	return &SpectateHandler{wsManager: wsManager}
}

func (h *SpectateHandler) GetActiveGames(c *gin.Context) {
	games := h.wsManager.ActiveSpectateGames()
	c.JSON(http.StatusOK, games)
}

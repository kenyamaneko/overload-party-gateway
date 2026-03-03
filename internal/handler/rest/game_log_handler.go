package rest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

// GameLogHandler handles game log / replay endpoints.
type GameLogHandler struct {
	gameLogService *service.GameLogService
}

func NewGameLogHandler(gameLogService *service.GameLogService) *GameLogHandler {
	return &GameLogHandler{gameLogService: gameLogService}
}

// GetGameLog returns a JSON game log.
// GET /api/v1/games/:gameId/log
func (h *GameLogHandler) GetGameLog(c *gin.Context) {
	gameID := c.Param("gameId")

	log, err := h.gameLogService.GetGameLog(c.Request.Context(), gameID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, log)
}

// GetGameLogText returns a plain-text game log.
// GET /api/v1/games/:gameId/log/text
func (h *GameLogHandler) GetGameLogText(c *gin.Context) {
	gameID := c.Param("gameId")

	text, err := h.gameLogService.GetGameLogText(c.Request.Context(), gameID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(text))
}

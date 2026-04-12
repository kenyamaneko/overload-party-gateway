package rest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

// GameLogHandler はゲームログの REST エンドポイントを処理します
type GameLogHandler struct {
	battleClient service.BattleClient
}

// NewGameLogHandler は GameLogHandler を生成します
func NewGameLogHandler(battleClient service.BattleClient) *GameLogHandler {
	return &GameLogHandler{battleClient: battleClient}
}

// GetGameLog はゲームログを JSON で返します
func (h *GameLogHandler) GetGameLog(c *gin.Context) {
	gameID := c.Param("gameId")

	log, err := h.battleClient.GetGameLog(c.Request.Context(), gameID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if log == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "game not found"})
		return
	}

	c.Data(http.StatusOK, "application/json; charset=utf-8", log)
}

// GetGameLogText はゲームログをテキスト形式で返します
func (h *GameLogHandler) GetGameLogText(c *gin.Context) {
	gameID := c.Param("gameId")

	text, err := h.battleClient.GetGameLogText(c.Request.Context(), gameID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if text == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "game not found"})
		return
	}

	c.Data(http.StatusOK, "text/plain; charset=utf-8", text)
}

package rest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

type PlayerHandler struct {
	playerService *service.PlayerService
}

func NewPlayerHandler(playerService *service.PlayerService) *PlayerHandler {
	return &PlayerHandler{playerService: playerService}
}

// playerResponse wraps the generated Player model with computed level progress fields.
type playerResponse struct {
	*model.Player
	LevelExpCurrent  int64 `json:"level_exp_current"`
	LevelExpRequired int64 `json:"level_exp_required"`
}

func (h *PlayerHandler) GetPlayer(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	ctx := c.Request.Context()

	player, err := h.playerService.GetPlayer(ctx, playerID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	progress, err := h.playerService.GetLevelProgress(ctx, player.Level, player.Exp)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, playerResponse{
		Player:           player,
		LevelExpCurrent:  progress.LevelExpCurrent,
		LevelExpRequired: progress.LevelExpRequired,
	})
}

func (h *PlayerHandler) UpdateName(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)

	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	player, err := h.playerService.UpdateUsername(c.Request.Context(), playerID, req.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, player)
}

func (h *PlayerHandler) GetBattleLimit(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)

	result, err := h.playerService.GetBattleLimit(c.Request.Context(), playerID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

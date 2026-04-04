package rest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

type UserSettingsHandler struct {
	svc *service.UserSettingsService
}

func NewUserSettingsHandler(svc *service.UserSettingsService) *UserSettingsHandler {
	return &UserSettingsHandler{svc: svc}
}

func (h *UserSettingsHandler) GetSettings(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)

	s, err := h.svc.Get(c.Request.Context(), playerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, s)
}

func (h *UserSettingsHandler) UpdateSettings(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)

	var req model.UpdateSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Language == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "language is required"})
		return
	}

	s := &model.UserSettings{
		PlayerID:    playerID,
		Language:    req.Language,
		BgmVolume:   req.BgmVolume,
		SeVolume:    req.SeVolume,
		PushEnabled: req.PushEnabled,
	}

	if err := h.svc.Update(c.Request.Context(), s); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, s)
}

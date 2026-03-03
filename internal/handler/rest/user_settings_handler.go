package rest

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
)

type UserSettingsHandler struct {
	repo repository.UserSettingsRepo
}

func NewUserSettingsHandler(repo repository.UserSettingsRepo) *UserSettingsHandler {
	return &UserSettingsHandler{repo: repo}
}

func (h *UserSettingsHandler) GetSettings(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)

	s, err := h.repo.Get(c.Request.Context(), playerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if s == nil {
		s = &model.UserSettings{
			PlayerID:    playerID,
			Language:    "ja",
			BgmVolume:   50,
			SeVolume:    50,
			PushEnabled: true,
			UpdatedAt:   time.Now(),
		}
	}

	c.JSON(http.StatusOK, s)
}

func (h *UserSettingsHandler) UpdateSettings(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)

	var req struct {
		Language    string `json:"language" binding:"required"`
		BgmVolume   int64  `json:"bgm_volume"`
		SeVolume    int64  `json:"se_volume"`
		PushEnabled bool   `json:"push_enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	s := &model.UserSettings{
		PlayerID:    playerID,
		Language:    req.Language,
		BgmVolume:   req.BgmVolume,
		SeVolume:    req.SeVolume,
		PushEnabled: req.PushEnabled,
	}

	if err := h.repo.Upsert(c.Request.Context(), s); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, s)
}

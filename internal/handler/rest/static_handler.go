package rest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/config"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

type StaticHandler struct {
	cfg       *config.Config
	staticSvc *service.StaticService
}

func NewStaticHandler(cfg *config.Config, staticSvc *service.StaticService) *StaticHandler {
	return &StaticHandler{cfg: cfg, staticSvc: staticSvc}
}

func (h *StaticHandler) GetVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"minimumVersion": h.cfg.AppMinVersion,
		"latestVersion":  h.cfg.AppLatestVersion,
		"forceUpdate":    h.cfg.AppForceUpdate,
	})
}

func (h *StaticHandler) GetAnnouncements(c *gin.Context) {
	c.JSON(http.StatusOK, h.staticSvc.ActiveAnnouncements())
}

func (h *StaticHandler) GetDaily(c *gin.Context) {
	tip := h.staticSvc.TodaysTip()
	if tip == nil {
		c.JSON(http.StatusOK, gin.H{"id": "", "text": ""})
		return
	}
	c.JSON(http.StatusOK, tip)
}

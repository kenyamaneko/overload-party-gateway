package rest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/config"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

// StaticHandler はバージョン情報やお知らせなど静的コンテンツの REST エンドポイントを処理します
type StaticHandler struct {
	cfg       *config.Config
	staticSvc *service.StaticService
}

// NewStaticHandler は StaticHandler を生成します
func NewStaticHandler(cfg *config.Config, staticSvc *service.StaticService) *StaticHandler {
	return &StaticHandler{cfg: cfg, staticSvc: staticSvc}
}

// GetVersion はアプリバージョン情報を返します
func (h *StaticHandler) GetVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"minimumVersion": h.cfg.AppMinVersion,
		"latestVersion":  h.cfg.AppLatestVersion,
		"forceUpdate":    h.cfg.AppForceUpdate,
	})
}

// GetAnnouncements は現在有効なお知らせ一覧を返します
func (h *StaticHandler) GetAnnouncements(c *gin.Context) {
	c.JSON(http.StatusOK, h.staticSvc.ActiveAnnouncements())
}

// GetDaily は日替わり Tips を返します
func (h *StaticHandler) GetDaily(c *gin.Context) {
	tip := h.staticSvc.TodaysTip()
	if tip == nil {
		c.JSON(http.StatusOK, gin.H{"id": "", "text": ""})
		return
	}
	c.JSON(http.StatusOK, tip)
}

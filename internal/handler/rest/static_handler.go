package rest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/config"
	apigateway "github.com/kenyamaneko/overload-party-gateway/packages/api-gateway"
)

// StaticHandler は version など gateway 自身が保持する静的情報の REST エンドポイントを処理します
type StaticHandler struct {
	cfg *config.Config
}

// NewStaticHandler は StaticHandler を生成します
func NewStaticHandler(cfg *config.Config) *StaticHandler {
	return &StaticHandler{cfg: cfg}
}

// GetVersion はアプリバージョン情報を返します
func (h *StaticHandler) GetVersion(c *gin.Context) {
	c.JSON(http.StatusOK, apigateway.VersionResponse{
		MinimumVersion: h.cfg.AppMinVersion,
		LatestVersion:  h.cfg.AppLatestVersion,
		ForceUpdate:    h.cfg.AppForceUpdate,
		StoreURL:       h.cfg.AppStoreURL,
	})
}

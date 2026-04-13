package rest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	apigateway "github.com/kenyamaneko/overload-party-gateway/packages/api-gateway"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
)

// UserSettingsHandler はユーザー設定の REST エンドポイントを処理します
type UserSettingsHandler struct {
	account *accountclient.Client
}

// NewUserSettingsHandler は UserSettingsHandler を生成します
func NewUserSettingsHandler(account *accountclient.Client) *UserSettingsHandler {
	return &UserSettingsHandler{account: account}
}

// GetSettings はユーザー設定を返します
func (h *UserSettingsHandler) GetSettings(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	settings, err := h.account.GetSettings(c.Request.Context(), playerID)
	if err != nil {
		respondAccountErr(c, err)
		return
	}
	c.JSON(http.StatusOK, settings)
}

// UpdateSettings はユーザー設定を更新します
func (h *UserSettingsHandler) UpdateSettings(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	var req apigateway.UpdateSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Language == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "language is required"})
		return
	}
	settings, err := h.account.UpdateSettings(c.Request.Context(), playerID, toAccountUpdateSettingsRequest(req))
	if err != nil {
		respondAccountErr(c, err)
		return
	}
	c.JSON(http.StatusOK, settings)
}

// toAccountUpdateSettingsRequest は client 契約を内部 RPC 契約に変換します。
func toAccountUpdateSettingsRequest(req apigateway.UpdateSettingsRequest) apiaccount.UpdateSettingsRequest {
	return apiaccount.UpdateSettingsRequest{
		Language:    req.Language,
		BgmVolume:   req.BgmVolume,
		SeVolume:    req.SeVolume,
		PushEnabled: req.PushEnabled,
	}
}

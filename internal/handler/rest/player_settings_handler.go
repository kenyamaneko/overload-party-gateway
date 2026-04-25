package rest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	apigateway "github.com/kenyamaneko/overload-party-gateway/packages/api-gateway"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
)

// PlayerSettingsHandler はプレイヤー設定の REST エンドポイントを処理します
type PlayerSettingsHandler struct {
	account *accountclient.Client
}

// NewPlayerSettingsHandler は PlayerSettingsHandler を生成します
func NewPlayerSettingsHandler(account *accountclient.Client) *PlayerSettingsHandler {
	return &PlayerSettingsHandler{account: account}
}

// GetSettings はプレイヤー設定を返します
func (h *PlayerSettingsHandler) GetSettings(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	settings, err := h.account.GetSettings(c.Request.Context(), playerID)
	if err != nil {
		respondAccountErr(c, err)
		return
	}
	c.JSON(http.StatusOK, settings)
}

// UpdateSettings はプレイヤー設定を部分更新します。
// nil フィールドは現状維持。空リクエストは account 側で 400 を返す。
func (h *PlayerSettingsHandler) UpdateSettings(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	var req apigateway.UpdateSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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

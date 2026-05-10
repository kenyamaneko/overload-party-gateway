package rest

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/presenter"
)

// PlayerHandler はプレイヤー情報の REST エンドポイントを処理します
type PlayerHandler struct {
	account *accountclient.Client
}

// NewPlayerHandler は PlayerHandler を生成します
func NewPlayerHandler(account *accountclient.Client) *PlayerHandler {
	return &PlayerHandler{account: account}
}

// GetPlayer はプレイヤー情報を返します
func (h *PlayerHandler) GetPlayer(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	player, err := h.account.GetPlayer(c.Request.Context(), playerID)
	if err != nil {
		respondAccountErr(c, err)
		return
	}
	c.JSON(http.StatusOK, presenter.PlayerToWire(player))
}

// UpdateName はプレイヤー名を変更します。
// 表示名の業務バリデーション (空・空白のみ・制御文字・上限超) は
// account 側を SSoT とし、gateway はリクエストを中継するだけです。
func (h *PlayerHandler) UpdateName(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	player, err := h.account.UpdateName(c.Request.Context(), playerID, req.Name)
	if err != nil {
		respondAccountErr(c, err)
		return
	}
	c.JSON(http.StatusOK, player)
}

// GetBattleLimit はバトル回数制限情報を返します
func (h *PlayerHandler) GetBattleLimit(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	limit, err := h.account.GetBattleLimit(c.Request.Context(), playerID)
	if err != nil {
		respondAccountErr(c, err)
		return
	}
	c.JSON(http.StatusOK, presenter.BattleLimitToWire(limit))
}

func respondAccountErr(c *gin.Context, err error) {
	if errors.Is(err, accountclient.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, accountclient.ErrPlayerAlreadyRegistered) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, accountclient.ErrInvalidRequest) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}

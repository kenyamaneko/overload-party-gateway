package rest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/client/cardclient"
)

// PlayerCardHandler はプレイヤー所持カードの REST エンドポイントを処理します
type PlayerCardHandler struct {
	card *cardclient.Client
}

// NewPlayerCardHandler は PlayerCardHandler を生成します
func NewPlayerCardHandler(card *cardclient.Client) *PlayerCardHandler {
	return &PlayerCardHandler{card: card}
}

// GetPlayerCards はプレイヤーの所持カード一覧を返します
func (h *PlayerCardHandler) GetPlayerCards(c *gin.Context) {
	cards, err := h.card.ListPlayerCards(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cards)
}

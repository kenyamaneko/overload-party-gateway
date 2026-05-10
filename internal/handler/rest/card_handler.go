package rest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/client/cardclient"
)

// CardHandler はカード情報の REST エンドポイントを処理します
type CardHandler struct {
	card *cardclient.Client
}

// NewCardHandler は CardHandler を生成します
func NewCardHandler(card *cardclient.Client) *CardHandler {
	return &CardHandler{card: card}
}

// GetAllCards は所有状態付きの全カード一覧を返します
func (h *CardHandler) GetAllCards(c *gin.Context) {
	cards, err := h.card.ListCardsWithOwnership(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, cards)
}

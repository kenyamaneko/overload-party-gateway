package rest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

type CardHandler struct {
	cardService *service.CardService
}

func NewCardHandler(cardService *service.CardService) *CardHandler {
	return &CardHandler{cardService: cardService}
}

func (h *CardHandler) GetAllCards(c *gin.Context) {
	cards, err := h.cardService.GetAllCards(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, cards)
}

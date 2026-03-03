package rest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

type PlayerCardHandler struct {
	deckService *service.DeckService
}

func NewPlayerCardHandler(deckService *service.DeckService) *PlayerCardHandler {
	return &PlayerCardHandler{deckService: deckService}
}

func (h *PlayerCardHandler) GetPlayerCards(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)

	cards, err := h.deckService.GetPlayerCards(c.Request.Context(), playerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, cards)
}

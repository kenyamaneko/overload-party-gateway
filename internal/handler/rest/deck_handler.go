package rest

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

type DeckHandler struct {
	deckService *service.DeckService
}

func NewDeckHandler(deckService *service.DeckService) *DeckHandler {
	return &DeckHandler{deckService: deckService}
}

func (h *DeckHandler) GetDecks(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)

	decks, err := h.deckService.GetDecks(c.Request.Context(), playerID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, decks)
}

func (h *DeckHandler) GetDeck(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	deckID, err := strconv.ParseInt(c.Param("deckId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid deck_id"})
		return
	}

	deck, cards, err := h.deckService.GetDeck(c.Request.Context(), playerID, deckID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"deck": deck, "cards": cards})
}

func (h *DeckHandler) CreateDeck(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)

	var req service.CreateDeckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	deck, err := h.deckService.CreateDeck(c.Request.Context(), playerID, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, deck)
}

func (h *DeckHandler) UpdateDeck(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	deckID, err := strconv.ParseInt(c.Param("deckId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid deck_id"})
		return
	}

	var req service.UpdateDeckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	deck, err := h.deckService.UpdateDeck(c.Request.Context(), playerID, deckID, req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, deck)
}

func (h *DeckHandler) DeleteDeck(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	deckID, err := strconv.ParseInt(c.Param("deckId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid deck_id"})
		return
	}

	if err := h.deckService.DeleteDeck(c.Request.Context(), playerID, deckID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

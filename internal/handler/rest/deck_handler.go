package rest

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	apicard "github.com/kenyamaneko/overload-party-card/packages/api-card"
	apigateway "github.com/kenyamaneko/overload-party-gateway/packages/api-gateway"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/cardclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
)

// DeckHandler はデッキ CRUD の REST エンドポイントを処理します
type DeckHandler struct {
	card *cardclient.Client
}

// NewDeckHandler は DeckHandler を生成します
func NewDeckHandler(card *cardclient.Client) *DeckHandler {
	return &DeckHandler{card: card}
}

// GetDecks はプレイヤーのデッキ一覧を返します
func (h *DeckHandler) GetDecks(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	decks, err := h.card.ListDecks(c.Request.Context(), playerID)
	if err != nil {
		respondCardErr(c, err)
		return
	}
	c.JSON(http.StatusOK, decks)
}

// GetDeck は指定デッキの詳細を返します
func (h *DeckHandler) GetDeck(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	deckID, err := strconv.ParseInt(c.Param("deckId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid deck_id"})
		return
	}

	deck, cards, err := h.card.GetDeck(c.Request.Context(), playerID, deckID)
	if err != nil {
		respondCardErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deck": deck, "cards": cards})
}

// CreateDeck は新しいデッキを作成します
func (h *DeckHandler) CreateDeck(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)

	var req apigateway.DeckCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	deck, err := h.card.CreateDeck(c.Request.Context(), playerID, toCardCreateRequest(req))
	if err != nil {
		respondCardErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, deck)
}

// UpdateDeck はデッキを更新します
func (h *DeckHandler) UpdateDeck(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	deckID, err := strconv.ParseInt(c.Param("deckId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid deck_id"})
		return
	}

	var req apigateway.DeckUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	deck, err := h.card.UpdateDeck(c.Request.Context(), playerID, deckID, toCardUpdateRequest(req))
	if err != nil {
		respondCardErr(c, err)
		return
	}
	c.JSON(http.StatusOK, deck)
}

// DeleteDeck はデッキを削除します
func (h *DeckHandler) DeleteDeck(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	deckID, err := strconv.ParseInt(c.Param("deckId"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid deck_id"})
		return
	}

	if err := h.card.DeleteDeck(c.Request.Context(), playerID, deckID); err != nil {
		respondCardErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// toCardCreateRequest は client 契約を内部 RPC 契約に変換します。
func toCardCreateRequest(req apigateway.DeckCreateRequest) apicard.DeckCreateRequest {
	cards := make([]apicard.DeckCardEntry, len(req.Cards))
	for i, c := range req.Cards {
		cards[i] = apicard.DeckCardEntry{CardID: c.CardID, ArtNo: c.ArtNo, Count: int(c.Count)}
	}
	return apicard.DeckCreateRequest{
		DeckName:  req.DeckName,
		Cards:     cards,
		PlaymatNo: req.PlaymatNo,
		SleeveNo:  req.SleeveNo,
	}
}

// toCardUpdateRequest は client 契約を内部 RPC 契約に変換します。
func toCardUpdateRequest(req apigateway.DeckUpdateRequest) apicard.DeckUpdateRequest {
	cards := make([]apicard.DeckCardEntry, len(req.Cards))
	for i, c := range req.Cards {
		cards[i] = apicard.DeckCardEntry{CardID: c.CardID, ArtNo: c.ArtNo, Count: int(c.Count)}
	}
	return apicard.DeckUpdateRequest{
		DeckName:  req.DeckName,
		Cards:     cards,
		PlaymatNo: req.PlaymatNo,
		SleeveNo:  req.SleeveNo,
	}
}

func respondCardErr(c *gin.Context, err error) {
	if errors.Is(err, cardclient.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, cardclient.ErrDeckInvalid) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}

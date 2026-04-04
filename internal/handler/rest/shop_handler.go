package rest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

type ShopHandler struct {
	shopService *service.ShopService
}

func NewShopHandler(shopService *service.ShopService) *ShopHandler {
	return &ShopHandler{shopService: shopService}
}

type selectFactionRequest struct {
	Faction string `json:"faction" binding:"required"`
}

func (h *ShopHandler) SelectFaction(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)

	var req selectFactionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	count, err := h.shopService.SelectFaction(c.Request.Context(), playerID, req.Faction)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":       "faction selected",
		"faction":       req.Faction,
		"cards_granted": count,
	})
}

func (h *ShopHandler) GetProducts(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)

	products, err := h.shopService.GetProducts(c.Request.Context(), playerID)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"products": products})
}

func (h *ShopHandler) Purchase(c *gin.Context) {
	var req model.PurchaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	playerID := middleware.GetPlayerID(c)

	if err := h.shopService.Purchase(c.Request.Context(), playerID, req.ProductID, req.Platform, req.PurchaseToken); err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "purchase completed",
		"product_id": req.ProductID,
	})
}

func (h *ShopHandler) Subscribe(c *gin.Context) {
	var req model.PurchaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	playerID := middleware.GetPlayerID(c)

	expiresAt, err := h.shopService.Subscribe(c.Request.Context(), playerID, req.ProductID, req.Platform, req.PurchaseToken)
	if err != nil {
		respondError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "subscription activated",
		"expires_at": expiresAt,
	})
}

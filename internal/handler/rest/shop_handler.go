package rest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
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
		if err.Error() == "faction already selected" {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if err.Error() == "invalid faction: "+req.Faction {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"products": products})
}

type purchaseRequest struct {
	ProductID     string `json:"product_id" binding:"required"`
	Platform      string `json:"platform" binding:"required"`
	PurchaseToken string `json:"purchase_token" binding:"required"`
}

func (h *ShopHandler) Purchase(c *gin.Context) {
	var req purchaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	playerID := middleware.GetPlayerID(c)

	if err := h.shopService.Purchase(c.Request.Context(), playerID, req.ProductID, req.Platform, req.PurchaseToken); err != nil {
		if err.Error() == "receipt verification failed" {
			c.JSON(http.StatusPaymentRequired, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "purchase completed",
		"product_id": req.ProductID,
	})
}

type subscribeRequest struct {
	ProductID     string `json:"product_id" binding:"required"`
	Platform      string `json:"platform" binding:"required"`
	PurchaseToken string `json:"purchase_token" binding:"required"`
}

func (h *ShopHandler) Subscribe(c *gin.Context) {
	var req subscribeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	playerID := middleware.GetPlayerID(c)

	expiresAt, err := h.shopService.Subscribe(c.Request.Context(), playerID, req.ProductID, req.Platform, req.PurchaseToken)
	if err != nil {
		if err.Error() == "subscription verification failed" {
			c.JSON(http.StatusPaymentRequired, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "subscription activated",
		"expires_at": expiresAt,
	})
}

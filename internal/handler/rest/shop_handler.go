package rest

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	apishop "github.com/kenyamaneko/overload-party-shop/packages/api-shop"
	apigateway "github.com/kenyamaneko/overload-party-gateway/packages/api-gateway"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/shopclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
)

// validatePurchaseRequest は gateway 段階で弾ける明らかな不正を検出する。
// 商品 ID の存在やレシート検証は shop サービス側で実施する。
func validatePurchaseRequest(req apigateway.PurchaseRequest) error {
	if req.ProductID == "" {
		return errors.New("product_id is required")
	}
	if req.Platform == "" {
		return errors.New("platform is required")
	}
	if req.PurchaseToken == "" {
		return errors.New("purchase_token is required")
	}
	return nil
}

// ShopHandler はショップ関連の REST エンドポイントを処理します
type ShopHandler struct {
	shop *shopclient.Client
}

// NewShopHandler は ShopHandler を生成します
func NewShopHandler(shop *shopclient.Client) *ShopHandler {
	return &ShopHandler{shop: shop}
}

// SelectFaction はファクション選択を処理します
func (h *ShopHandler) SelectFaction(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	var req apigateway.SelectFactionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Faction == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "faction is required"})
		return
	}
	resp, err := h.shop.SelectFaction(c.Request.Context(), playerID, req.Faction)
	if err != nil {
		respondShopErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message":       resp.Message,
		"faction":       resp.Faction,
		"cards_granted": resp.CardsGranted,
	})
}

// GetProducts は商品一覧を返します
func (h *ShopHandler) GetProducts(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	products, err := h.shop.GetProducts(c.Request.Context(), playerID)
	if err != nil {
		respondShopErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"products": products})
}

// Purchase は商品購入を処理します
func (h *ShopHandler) Purchase(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	var req apigateway.PurchaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validatePurchaseRequest(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.shop.Purchase(c.Request.Context(), playerID, toShopPurchaseRequest(req)); err != nil {
		respondShopErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "purchase completed", "product_id": req.ProductID})
}

// Subscribe はサブスクリプション登録を処理します
func (h *ShopHandler) Subscribe(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	var req apigateway.PurchaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validatePurchaseRequest(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	expiresAt, err := h.shop.Subscribe(c.Request.Context(), playerID, toShopPurchaseRequest(req))
	if err != nil {
		respondShopErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "subscription activated", "expires_at": expiresAt})
}

// toShopPurchaseRequest は client 契約を内部 RPC 契約に変換します。
func toShopPurchaseRequest(req apigateway.PurchaseRequest) apishop.PurchaseRequest {
	return apishop.PurchaseRequest{
		ProductID:     req.ProductID,
		Platform:      apishop.Platform(req.Platform),
		PurchaseToken: req.PurchaseToken,
	}
}

func respondShopErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, shopclient.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, shopclient.ErrAlreadyOwned), errors.Is(err, shopclient.ErrFactionAlreadySelected):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, shopclient.ErrInvalidFaction):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, shopclient.ErrReceiptInvalid):
		c.JSON(http.StatusPaymentRequired, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

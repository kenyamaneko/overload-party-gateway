package rest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

type WebhookHandler struct {
	subscriptionService *service.SubscriptionService
}

func NewWebhookHandler(subscriptionService *service.SubscriptionService) *WebhookHandler {
	return &WebhookHandler{subscriptionService: subscriptionService}
}

func (h *WebhookHandler) HandleAppleWebhook(c *gin.Context) {
	var payload service.AppleNotificationPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.subscriptionService.HandleAppleNotification(c.Request.Context(), payload.SignedPayload); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusOK)
}

func (h *WebhookHandler) HandleGoogleWebhook(c *gin.Context) {
	var msg service.GoogleRTDNMessage
	if err := c.ShouldBindJSON(&msg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.subscriptionService.HandleGoogleNotification(c.Request.Context(), msg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusOK)
}

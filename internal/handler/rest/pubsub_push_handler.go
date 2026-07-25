package rest

import (
	"encoding/base64"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// pushEnvelope は Cloud Pub/Sub push 配信のリクエストボディの形。
type pushEnvelope struct {
	Message struct {
		Data       string            `json:"data" binding:"required"`
		MessageID  string            `json:"messageId" binding:"required"`
		Attributes map[string]string `json:"attributes"`
	} `json:"message" binding:"required"`
	Subscription string `json:"subscription"`
}

// PubSubPushHandler は Pub/Sub push 配信の HTTP エンドポイントを処理します。
// 到達制御は Cloud Run の呼び出し IAM が担うため、本 handler はアプリ層の
// 認証を行いません。
type PubSubPushHandler struct {
	matchMade port.PushMessageProcessor
}

// NewPubSubPushHandler は PubSubPushHandler を生成します。
func NewPubSubPushHandler(matchMade port.PushMessageProcessor) *PubSubPushHandler {
	return &PubSubPushHandler{matchMade: matchMade}
}

// HandleMatchMade は match_made イベントの push 配信を処理します。
func (h *PubSubPushHandler) HandleMatchMade(c *gin.Context) {
	var envelope pushEnvelope
	if err := c.ShouldBindJSON(&envelope); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid push envelope"})
		return
	}

	data, err := base64.StdEncoding.DecodeString(envelope.Message.Data)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid base64 data in message.data"})
		return
	}

	if err := h.matchMade.ProcessMessage(c.Request.Context(), data); err != nil {
		slog.Error("pubsub push: process match_made failed", "message_id", envelope.Message.MessageID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to process message"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

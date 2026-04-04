package rest

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

type NPCHandler struct {
	battleClient service.BattleClient
}

func NewNPCHandler(battleClient service.BattleClient) *NPCHandler {
	return &NPCHandler{battleClient: battleClient}
}

func (h *NPCHandler) GetNPCModels(c *gin.Context) {
	models, err := h.battleClient.GetNPCModels(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if models == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "npc models not found"})
		return
	}

	c.Data(http.StatusOK, "application/json; charset=utf-8", models)
}

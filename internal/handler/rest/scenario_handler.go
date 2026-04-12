package rest

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/client/scenarioclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
)

// ScenarioHandler はストーリーシナリオの REST エンドポイントを処理します
type ScenarioHandler struct {
	client *scenarioclient.Client
}

// NewScenarioHandler は ScenarioHandler を生成します
func NewScenarioHandler(client *scenarioclient.Client) *ScenarioHandler {
	return &ScenarioHandler{client: client}
}

// ListEpisodes はエピソード一覧を返します
func (h *ScenarioHandler) ListEpisodes(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	lang := c.DefaultQuery("lang", "ja")
	episodes, err := h.client.ListEpisodes(c.Request.Context(), playerID, lang)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"episodes": episodes})
}

// GetScript はエピソードのスクリプトを返します
func (h *ScenarioHandler) GetScript(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	episodeID := c.Param("episodeId")
	lang := c.DefaultQuery("lang", "ja")
	script, err := h.client.GetScript(c.Request.Context(), playerID, episodeID, lang)
	if err != nil {
		respondScenarioErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"episode_id": episodeID, "script": script})
}

// CompleteEpisode はエピソード完了を記録します
func (h *ScenarioHandler) CompleteEpisode(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	episodeID := c.Param("episodeId")
	if err := h.client.CompleteEpisode(c.Request.Context(), playerID, episodeID); err != nil {
		respondScenarioErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "episode completed", "episode_id": episodeID})
}

func respondScenarioErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, scenarioclient.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, scenarioclient.ErrLocked):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

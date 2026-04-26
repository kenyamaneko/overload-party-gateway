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

// GetOnboardingStatus はオンボーディング完了状態を返します。
// GetOnboardingScript はオンボーディングシナリオ本文を返します。
func (h *ScenarioHandler) GetOnboardingScript(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	lang := c.DefaultQuery("lang", "ja")
	script, err := h.client.GetOnboardingScript(c.Request.Context(), playerID, lang)
	if err != nil {
		respondScenarioErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"script": script})
}

// UpdateOnboardingName はオンボード内 name 入力で受け取った表示名を scenario へ伝えます。
// scenario が account の validate REST を中継するため、400 はそのままユーザーへ返します。
func (h *ScenarioHandler) UpdateOnboardingName(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.client.UpdateOnboardingName(c.Request.Context(), playerID, req.Name); err != nil {
		respondScenarioErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// SelectOnboardingFaction はオンボード内 faction 選択ステップを scenario へ伝えます。
func (h *ScenarioHandler) SelectOnboardingFaction(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	var req struct {
		InitialFactionID string `json:"initial_faction_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.client.SelectOnboardingFaction(c.Request.Context(), playerID, req.InitialFactionID); err != nil {
		respondScenarioErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// CompleteOnboarding はオンボーディング完了を記録し player-onboarded を発行します。
// faction は事前の SelectOnboardingFaction で account に永続化済みのため body は受け取りません。
func (h *ScenarioHandler) CompleteOnboarding(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	resp, err := h.client.CompleteOnboarding(c.Request.Context(), playerID)
	if err != nil {
		respondScenarioErr(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func respondScenarioErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, scenarioclient.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, scenarioclient.ErrLocked):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, scenarioclient.ErrInvalidRequest):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, scenarioclient.ErrAlreadyOnboarded):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}

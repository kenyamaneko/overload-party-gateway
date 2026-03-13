package rest

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

type StoryHandler struct {
	storyService *service.StoryService
}

func NewStoryHandler(storyService *service.StoryService) *StoryHandler {
	return &StoryHandler{storyService: storyService}
}

func (h *StoryHandler) ListEpisodes(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	lang := c.DefaultQuery("lang", "ja")

	episodes, err := h.storyService.ListEpisodes(c.Request.Context(), playerID, lang)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"episodes": episodes})
}

func (h *StoryHandler) GetScript(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	episodeID := c.Param("episodeId")
	lang := c.DefaultQuery("lang", "ja")

	script, err := h.storyService.GetScript(c.Request.Context(), playerID, episodeID, lang)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrEpisodeNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case errors.Is(err, service.ErrEpisodeLocked):
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"episode_id": episodeID,
		"script":     script,
	})
}

func (h *StoryHandler) CompleteEpisode(c *gin.Context) {
	playerID := middleware.GetPlayerID(c)
	episodeID := c.Param("episodeId")

	err := h.storyService.CompleteEpisode(c.Request.Context(), playerID, episodeID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrEpisodeNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case errors.Is(err, service.ErrEpisodeLocked):
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":    "episode completed",
		"episode_id": episodeID,
	})
}

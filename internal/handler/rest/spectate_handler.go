package rest

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	ws "github.com/kenyamaneko/overload-party-gateway/internal/handler/ws"
)

// SpectateGameInfo is the JSON response for a single spectatable game.
type SpectateGameInfo struct {
	GameID    string    `json:"game_id"`
	Player1ID string    `json:"player1_id"`
	Player2ID string    `json:"player2_id"`
	StartedAt time.Time `json:"started_at"`
}

// SpectateHandler exposes the list of spectatable games.
type SpectateHandler struct {
	wsManager *ws.Manager
}

func NewSpectateHandler(wsManager *ws.Manager) *SpectateHandler {
	return &SpectateHandler{wsManager: wsManager}
}

// GetActiveGames handles GET /api/v1/spectate/games.
func (h *SpectateHandler) GetActiveGames(c *gin.Context) {
	games := h.wsManager.ActiveSpectateGames()
	c.JSON(http.StatusOK, games)
}

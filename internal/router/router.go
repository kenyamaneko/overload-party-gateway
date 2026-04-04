package router

import (
	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/handler/rest"
)

// Handlers groups all REST handlers so route registration can be shared
// between cmd/main and cmd/local without duplicating route definitions.
type Handlers struct {
	Auth         *rest.AuthHandler
	Player       *rest.PlayerHandler
	Card         *rest.CardHandler
	Deck         *rest.DeckHandler
	PlayerCard   *rest.PlayerCardHandler
	GameLog      *rest.GameLogHandler
	NPC          *rest.NPCHandler
	Shop         *rest.ShopHandler
	Story        *rest.StoryHandler
	Spectate     *rest.SpectateHandler
	UserSettings *rest.UserSettingsHandler
	Webhook      *rest.WebhookHandler
	News         *rest.NewsHandler
}

// RegisterAuthRoutes registers auth endpoints (register / login).
// In production these go on a group with Firebase auth only (no PlayerResolve).
// In local mode they share the DevAuth group with the API routes.
func RegisterAuthRoutes(group *gin.RouterGroup, h *Handlers) {
	group.POST("/auth/register", h.Auth.Register)
	group.POST("/auth/login", h.Auth.Login)
}

// RegisterAPIRoutes registers all authenticated+player-resolved endpoints.
func RegisterAPIRoutes(api *gin.RouterGroup, h *Handlers) {
	// Player
	api.GET("/player", h.Player.GetPlayer)
	api.PUT("/player/name", h.Player.UpdateName)
	api.GET("/player/battle-limit", h.Player.GetBattleLimit)
	api.GET("/player/cards", h.PlayerCard.GetPlayerCards)

	// Decks
	api.GET("/player/decks", h.Deck.GetDecks)
	api.GET("/player/decks/:deckId", h.Deck.GetDeck)
	api.POST("/player/decks", h.Deck.CreateDeck)
	api.PUT("/player/decks/:deckId", h.Deck.UpdateDeck)
	api.DELETE("/player/decks/:deckId", h.Deck.DeleteDeck)

	// User settings
	api.GET("/player/settings", h.UserSettings.GetSettings)
	api.PUT("/player/settings", h.UserSettings.UpdateSettings)

	// Cards
	api.GET("/cards", h.Card.GetAllCards)

	// Game log (proxied to battle server)
	api.GET("/games/:gameId/log", h.GameLog.GetGameLog)
	api.GET("/games/:gameId/log/text", h.GameLog.GetGameLogText)

	// NPC models (proxied to battle server)
	api.GET("/npc/models", h.NPC.GetNPCModels)

	// Spectate
	api.GET("/spectate/games", h.Spectate.GetActiveGames)

	// Shop
	api.POST("/player/select-faction", h.Shop.SelectFaction)
	api.GET("/shop/products", h.Shop.GetProducts)
	api.POST("/shop/purchase", h.Shop.Purchase)
	api.POST("/shop/subscribe", h.Shop.Subscribe)

	// Story scenarios
	api.GET("/scenarios", h.Story.ListEpisodes)
	api.GET("/scenarios/:episodeId/script", h.Story.GetScript)
	api.POST("/scenarios/:episodeId/complete", h.Story.CompleteEpisode)
}

// RegisterWebhookRoutes registers webhook endpoints (no Firebase auth).
func RegisterWebhookRoutes(webhooks *gin.RouterGroup, h *Handlers) {
	webhooks.POST("/apple", h.Webhook.HandleAppleWebhook)
	webhooks.POST("/google", h.Webhook.HandleGoogleWebhook)
}

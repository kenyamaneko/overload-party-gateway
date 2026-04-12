package router

import (
	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/handler/rest"
)

// Handlers は全 REST ハンドラーをグループ化し、cmd/main と cmd/local でルート定義を共有します
type Handlers struct {
	Auth         *rest.AuthHandler
	Player       *rest.PlayerHandler
	Card         *rest.CardHandler
	Deck         *rest.DeckHandler
	PlayerCard   *rest.PlayerCardHandler
	GameLog      *rest.GameLogHandler
	NPC          *rest.NPCHandler
	Shop         *rest.ShopHandler
	Scenario     *rest.ScenarioHandler
	Spectate     *rest.SpectateHandler
	UserSettings *rest.UserSettingsHandler
	News         *rest.NewsHandler
}

// RegisterAuthRoutes は認証エンドポイント（register / login）を登録します
func RegisterAuthRoutes(group *gin.RouterGroup, h *Handlers) {
	group.POST("/auth/register", h.Auth.Register)
	group.POST("/auth/login", h.Auth.Login)
}

// RegisterAPIRoutes は認証済み + プレイヤー解決済みの全エンドポイントを登録します
func RegisterAPIRoutes(api *gin.RouterGroup, h *Handlers) {
	api.GET("/player", h.Player.GetPlayer)
	api.PUT("/player/name", h.Player.UpdateName)
	api.GET("/player/battle-limit", h.Player.GetBattleLimit)
	api.GET("/player/cards", h.PlayerCard.GetPlayerCards)

	// デッキ
	api.GET("/player/decks", h.Deck.GetDecks)
	api.GET("/player/decks/:deckId", h.Deck.GetDeck)
	api.POST("/player/decks", h.Deck.CreateDeck)
	api.PUT("/player/decks/:deckId", h.Deck.UpdateDeck)
	api.DELETE("/player/decks/:deckId", h.Deck.DeleteDeck)

	// ユーザー設定
	api.GET("/player/settings", h.UserSettings.GetSettings)
	api.PUT("/player/settings", h.UserSettings.UpdateSettings)

	// カード
	api.GET("/cards", h.Card.GetAllCards)

	api.GET("/games/:gameId/log", h.GameLog.GetGameLog)
	api.GET("/games/:gameId/log/text", h.GameLog.GetGameLogText)

	api.GET("/npc/models", h.NPC.GetNPCModels)

	// 観戦
	api.GET("/spectate/games", h.Spectate.GetActiveGames)

	// ショップ
	api.POST("/player/select-faction", h.Shop.SelectFaction)
	api.GET("/shop/products", h.Shop.GetProducts)
	api.POST("/shop/purchase", h.Shop.Purchase)
	api.POST("/shop/subscribe", h.Shop.Subscribe)

	// ストーリーシナリオ
	api.GET("/scenarios", h.Scenario.ListEpisodes)
	api.GET("/scenarios/:episodeId/script", h.Scenario.GetScript)
	api.POST("/scenarios/:episodeId/complete", h.Scenario.CompleteEpisode)
}

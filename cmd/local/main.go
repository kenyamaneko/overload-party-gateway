package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/cache"
	"github.com/kenyamaneko/overload-party-gateway/internal/constants"
	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/handler/rest"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

func main() {
	log.Println("=== Overload Party Gateway (LOCAL MODE) ===")

	// 1. Card cache from JSON
	cardCache := cache.NewCardCache()
	if err := cardCache.LoadFromJSON("internal/cache/cards_gen.json"); err != nil {
		log.Fatalf("failed to load cards from JSON: %v", err)
	}
	log.Printf("loaded %d cards from internal/cache/cards_gen.json", cardCache.Count())

	// 2. Mock repositories
	playerRepo := repository.NewMockPlayerRepository()
	deckRepo := repository.NewMockDeckRepository()
	cardRepo := repository.NewMockCardRepository(cardCache.All())
	shopRepo := repository.NewMockShopRepository()
	userSettingsRepo := repository.NewMockUserSettingsRepository()
	gameConfigRepo := repository.NewMockGameConfigRepository()

	// 3. Services
	authService := service.NewAuthService(playerRepo, shopRepo, userSettingsRepo)
	playerService := service.NewPlayerService(playerRepo, gameConfigRepo)
	cardService := service.NewCardService(cardRepo)
	deckService := service.NewDeckService(deckRepo, cardCache)
	shopService := service.NewShopService(shopRepo, playerRepo, cardCache, nil, nil)
	subscriptionService := service.NewSubscriptionService(shopRepo)
	// 4. Handlers
	authHandler := rest.NewAuthHandler(authService)
	playerHandler := rest.NewPlayerHandler(playerService)
	cardHandler := rest.NewCardHandler(cardService)
	deckHandler := rest.NewDeckHandler(deckService)
	playerCardHandler := rest.NewPlayerCardHandler(deckService)
	shopHandler := rest.NewShopHandler(shopService)
	webhookHandler := rest.NewWebhookHandler(subscriptionService)
	userSettingsHandler := rest.NewUserSettingsHandler(userSettingsRepo)

	// 5. Dev player setup: give all active cards + starter deck on first request
	devPlayerSetup := func(ctx context.Context, playerID string) error {
		var playerCards []*model.PlayerCard
		for _, card := range cardCache.All() {
			if !card.IsActive {
				continue
			}
				copies := 3 // default
			if card.Restriction == "semi_limited" {
				copies = 2
			} else if card.Restriction == "limited" {
				copies = 1
			}
			playerCards = append(playerCards, &model.PlayerCard{
				PlayerID:            playerID,
				CardNo:              card.CardNo,
				IllustrationVariant: 0,
				Count:               copies,
			})
		}
		deckRepo.SeedPlayerCards(playerID, playerCards)

		// Create a starter deck (first 30 cards by expanding counts)
		var deckCards []model.DeckCard
		remaining := constants.DeckSize
		for _, pc := range playerCards {
			if remaining <= 0 {
				break
			}
			use := pc.Count
			if use > remaining {
				use = remaining
			}
			deckCards = append(deckCards, model.DeckCard{
				PlayerID:            playerID,
				CardNo:              pc.CardNo,
				IllustrationVariant: pc.IllustrationVariant,
				Count:               use,
			})
			remaining -= use
		}
		totalCards := constants.DeckSize - remaining
		deck := &model.Deck{
			PlayerID:  playerID,
			DeckName:  "Starter Deck",
			IsValid:   totalCards == constants.DeckSize,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := deckRepo.Create(ctx, deck, deckCards); err != nil {
			return err
		}
		log.Printf("auto-created starter deck for %s: %d cards, deckID=%d", playerID, totalCards, deck.DeckID)
		return nil
	}

	// 6. Bridge: when ShopService inserts player cards (e.g. select-faction),
	// also add them to MockDeckRepository so DeckService.GetPlayerCards can find them.
	shopRepo.SetOnCardsInserted(func(playerID string, cards []*model.PlayerCard) {
		deckRepo.SeedPlayerCards(playerID, cards)
		log.Printf("synced %d player cards for %s (shop → deck)", len(cards), playerID)
	})

	// 7. Router (DevAuth instead of FirebaseAuth)
	r := gin.Default()
	r.Use(middleware.CORS()) // allow all origins in local mode

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "mode": "local"})
	})

	// Public API endpoints (no auth required, used by client splash screen)
	pub := r.Group("/api/v1")
	{
		pub.GET("/health", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})
		pub.GET("/version", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"minimumVersion": "0.0.0",
				"latestVersion":  "0.0.0",
				"forceUpdate":    false,
			})
		})
		pub.GET("/announcements", func(c *gin.Context) {
			c.JSON(http.StatusOK, []gin.H{})
		})
		pub.GET("/daily", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"id":   "local-tip-1",
				"text": "ローカルモードで開発中です。",
			})
		})
		pub.GET("/cloud-news", func(c *gin.Context) {
			c.JSON(http.StatusOK, []gin.H{
				{"id": "1", "tag": "aws", "headline": "Lambda が ARM64 対応を拡大、コスト最大34%削減", "meta": "2時間前"},
				{"id": "2", "tag": "gcp", "headline": "Cloud Run に GPU サポートが GA、ML推論ワークロードに対応", "meta": "5時間前"},
				{"id": "3", "tag": "azure", "headline": "Cosmos DB の新プライシングモデルが発表", "meta": "8時間前"},
				{"id": "4", "tag": "topic", "headline": "マルチクラウド戦略の落とし穴: 3つの失敗パターン", "meta": "1日前"},
			})
		})
	}

	api := r.Group("/api/v1")
	api.Use(middleware.DevAuthWithPlayerResolve(playerRepo, middleware.DevPlayerSetup(devPlayerSetup)))
	{
		api.POST("/auth/register", authHandler.Register)
		api.POST("/auth/login", authHandler.Login)

		api.GET("/player", playerHandler.GetPlayer)
		api.PUT("/player/name", playerHandler.UpdateName)
		api.GET("/player/battle-limit", playerHandler.GetBattleLimit)
		api.GET("/player/cards", playerCardHandler.GetPlayerCards)

		api.GET("/player/decks", deckHandler.GetDecks)
		api.GET("/player/decks/:deckId", deckHandler.GetDeck)
		api.POST("/player/decks", deckHandler.CreateDeck)
		api.PUT("/player/decks/:deckId", deckHandler.UpdateDeck)
		api.DELETE("/player/decks/:deckId", deckHandler.DeleteDeck)

		api.GET("/player/settings", userSettingsHandler.GetSettings)
		api.PUT("/player/settings", userSettingsHandler.UpdateSettings)

		api.GET("/cards", cardHandler.GetAllCards)

		// Shop
		api.POST("/player/select-faction", shopHandler.SelectFaction)
		api.GET("/shop/products", shopHandler.GetProducts)
		api.POST("/shop/purchase", shopHandler.Purchase)
		api.POST("/shop/subscribe", shopHandler.Subscribe)
	}

	// Webhooks (no auth)
	webhooks := r.Group("/api/v1/shop/webhook")
	{
		webhooks.POST("/apple", webhookHandler.HandleAppleWebhook)
		webhooks.POST("/google", webhookHandler.HandleGoogleWebhook)
	}

	srv := &http.Server{
		Addr:    ":9001",
		Handler: r,
	}

	srvCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Println("gateway local server starting on :9001")
		log.Println("  REST: http://localhost:9001/api/v1/")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-srvCtx.Done()
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server forced to shutdown: %v", err)
	}
	log.Println("gateway local server exited")
}

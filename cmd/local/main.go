package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/cache"
	"github.com/kenyamaneko/overload-party-gateway/internal/config"
	"github.com/kenyamaneko/overload-party-gateway/internal/constants"
	"github.com/kenyamaneko/overload-party-gateway/internal/handler/rest"
	ws "github.com/kenyamaneko/overload-party-gateway/internal/handler/ws"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

func main() {
	log.Println("=== Overload Party Gateway (LOCAL MODE) ===")

	cfg := config.Load()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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
	factionRepo := repository.NewMockFactionRepository()
	storyRepo := repository.NewMockStoryRepository()
	userSettingsRepo := repository.NewMockUserSettingsRepository()
	gameConfigRepo := repository.NewMockGameConfigRepository()

	// 3. Services
	authService := service.NewAuthService(playerRepo, shopRepo, userSettingsRepo)
	playerService := service.NewPlayerService(playerRepo, gameConfigRepo)
	cardService := service.NewCardService(cardRepo, deckRepo)
	deckService := service.NewDeckService(deckRepo, cardCache)
	shopService := service.NewShopService(shopRepo, playerRepo, factionRepo, cardCache, nil, nil)
	storyService := service.NewStoryService(storyRepo, factionRepo, playerRepo, nil, "")
	subscriptionService := service.NewSubscriptionService(shopRepo, playerRepo)
	// 4. Battle client (real if BATTLE_SERVER_URL is set, mock otherwise)
	var battleClient service.BattleClient
	if battleURL := os.Getenv("BATTLE_SERVER_URL"); battleURL != "" {
		log.Printf("using real battle client: %s", battleURL)
		battleClient = service.NewBattleClient(battleURL)
	} else {
		log.Println("using mock battle client")
		battleClient = service.NewMockBattleClient()
	}

	// 5. Handlers
	wsManager := ws.NewManager(battleClient, playerService, deckRepo)
	go wsManager.StartMatchmaking(ctx)
	wsHandler := ws.NewHandler(wsManager, nil, playerRepo, nil)
	authHandler := rest.NewAuthHandler(authService)
	playerHandler := rest.NewPlayerHandler(playerService)
	spectateHandler := rest.NewSpectateHandler(wsManager)
	cardHandler := rest.NewCardHandler(cardService)
	deckHandler := rest.NewDeckHandler(deckService)
	playerCardHandler := rest.NewPlayerCardHandler(deckService)
	gameLogHandler := rest.NewGameLogHandler(battleClient)
	shopHandler := rest.NewShopHandler(shopService)
	webhookHandler := rest.NewWebhookHandler(subscriptionService)
	storyHandler := rest.NewStoryHandler(storyService)
	userSettingsHandler := rest.NewUserSettingsHandler(userSettingsRepo)

	// 5. Dev player setup: give all active cards + starter decks on first request
	devPlayerSetup := func(ctx context.Context, playerID string) error {
		var playerCards []*model.PlayerCard
		for _, card := range cardCache.All() {
			if !card.IsActive {
				continue
			}
			playerCards = append(playerCards, &model.PlayerCard{
				PlayerID:            playerID,
				CardNo:              card.CardNo,
				ArtNo: 0,
				Count:               3, // 保持できるカードは制限しない
			})
		}
		deckRepo.SeedPlayerCards(playerID, playerCards)

		// Create starter decks
		starterDecks := map[string][]int64{
			"SD Standard":     {1, 1, 1, 3, 6, 6, 7, 7, 7, 8, 8, 8, 9, 9, 13, 13, 15, 17, 20, 20, 20, 98, 99, 100, 101, 101, 115, 118, 119, 121},
			"Tenki Standard":  {23, 23, 26, 26, 26, 27, 27, 29, 29, 29, 31, 32, 32, 32, 35, 35, 37, 38, 38, 38, 41, 46, 46, 94, 98, 99, 100, 101, 101, 117},
			"Sugar Standard":  {47, 47, 47, 48, 48, 48, 49, 49, 50, 50, 51, 51, 52, 55, 56, 58, 58, 60, 60, 61, 62, 62, 66, 98, 99, 100, 104, 104, 104, 106},
			"Tuners Standard": {70, 70, 70, 72, 74, 74, 74, 76, 76, 77, 77, 77, 78, 79, 79, 81, 82, 84, 85, 86, 86, 86, 89, 89, 90, 90, 100, 101, 101, 101},
		}

		for deckName, cardNos := range starterDecks {
			cardCounts := make(map[int64]int)
			for _, cardNo := range cardNos {
				cardCounts[cardNo]++
			}

			var deckCards []model.DeckCard
			for cardNo, count := range cardCounts {
				deckCards = append(deckCards, model.DeckCard{
					PlayerID:            playerID,
					CardNo:              cardNo,
					ArtNo: 0,
					Count:               count,
				})
			}

			deck := &model.Deck{
				PlayerID:  playerID,
				DeckName:  deckName,
				IsValid:   len(cardNos) == constants.DeckSize,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}
			if err := deckRepo.Create(ctx, deck, deckCards); err != nil {
				return err
			}
			log.Printf("auto-created starter deck %s for %s: %d cards, deckID=%d", deckName, playerID, len(cardNos), deck.DeckID)
		}

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

	// WebSocket (no REST auth middleware; auth is handled inside HandleUpgrade)
	r.GET("/ws", wsHandler.HandleUpgrade)

	// Public API endpoints (no auth required, used by client splash screen)
	pub := r.Group("/api/v1")
	{
		pub.GET("/health", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})
		staticHandler := rest.NewStaticHandler(cfg, "data")
		pub.GET("/version", staticHandler.GetVersion)
		pub.GET("/announcements", staticHandler.GetAnnouncements)
		pub.GET("/daily", staticHandler.GetDaily)
		pub.GET("/cloud-news", func(c *gin.Context) {
			c.JSON(http.StatusOK, []gin.H{
				{"id": "1", "tag": "aws", "headline": "Lambda が ARM64 対応を拡大、コスト最大34%削減", "meta": "2時間前"},
				{"id": "2", "tag": "gcp", "headline": "Cloud Run に GPU サポートが GA、ML推論ワークロードに対応", "meta": "5時間前"},
				{"id": "3", "tag": "azure", "headline": "Cosmos DB の新プライシングモデルが発表", "meta": "8時間前"},
				{"id": "4", "tag": "topic", "headline": "マルチクラウド戦略の落とし穴: 3つの失敗パターン", "meta": "1日前"},
			})
		})
	}

	devRegister := func(ctx context.Context, firebaseUID, username string) (string, error) {
		p, err := authService.Register(ctx, firebaseUID, username)
		if err != nil {
			return "", err
		}
		return p.PlayerID, nil
	}

	api := r.Group("/api/v1")
	api.Use(middleware.DevAuthWithPlayerResolve(playerRepo, devRegister, middleware.DevPlayerSetup(devPlayerSetup)))
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

		// Game log (proxied to battle server)
		api.GET("/games/:gameId/log", gameLogHandler.GetGameLog)
		api.GET("/games/:gameId/log/text", gameLogHandler.GetGameLogText)

		// Spectate
		api.GET("/spectate/games", spectateHandler.GetActiveGames)

		// Shop
		api.POST("/player/select-faction", shopHandler.SelectFaction)
		api.GET("/shop/products", shopHandler.GetProducts)
		api.POST("/shop/purchase", shopHandler.Purchase)
		api.POST("/shop/subscribe", shopHandler.Subscribe)

		// Story scenarios
		api.GET("/scenarios", storyHandler.ListEpisodes)
		api.GET("/scenarios/:episodeId/script", storyHandler.GetScript)
		api.POST("/scenarios/:episodeId/complete", storyHandler.CompleteEpisode)
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

	srvCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
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

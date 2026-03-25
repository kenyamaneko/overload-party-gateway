package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	gencache "github.com/kenyamaneko/overload-party-common/packages/gamedata/cache"
	"github.com/kenyamaneko/overload-party-gateway/internal/cache"
	"github.com/kenyamaneko/overload-party-gateway/internal/config"
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

	// 1. Card cache from embedded JSON
	cardCache := cache.NewCardCache()
	if err := cardCache.LoadFromBytes(gencache.CardsJSON); err != nil {
		log.Fatalf("failed to load embedded cards: %v", err)
	}
	log.Printf("loaded %d cards from embedded cards_gen.json", cardCache.Count())

	// 2. Mock repositories
	playerRepo := repository.NewMockPlayerRepository()
	deckRepo := repository.NewMockDeckRepository()
	playerCardRepo := repository.NewMockPlayerCardRepository()
	cardRepo := repository.NewMockCardRepository(cardCache.All())
	shopRepo := repository.NewMockShopRepository()
	factionRepo := repository.NewMockFactionRepository()
	storyRepo := repository.NewMockStoryRepository()
	userSettingsRepo := repository.NewMockUserSettingsRepository()
	gameConfigRepo := repository.NewMockGameConfigRepository()

	// 3. Services
	authService := service.NewAuthService(playerRepo, shopRepo, userSettingsRepo, &repository.MockTxRunner{})
	playerService := service.NewPlayerService(playerRepo, gameConfigRepo)
	cardService := service.NewCardService(cardRepo, playerCardRepo)
	deckService := service.NewDeckService(deckRepo, playerCardRepo, cardCache)
	shopService := service.NewShopService(shopRepo, playerRepo, factionRepo, cardCache, nil, nil)
	storyService := service.NewStoryService(storyRepo, nil, "")
	subscriptionService := service.NewSubscriptionService(shopRepo, playerRepo)
	// 4. Battle client (uses cfg.BattleServerURL, default http://localhost:9002)
	log.Printf("battle client: %s", cfg.BattleServerURL)
	battleClient := service.NewBattleClient(cfg.BattleServerURL)

	// 5. Handlers
	wsManager := ws.NewManager(battleClient, playerService, deckService, deckRepo)
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
				PlayerID: playerID,
				CardID:   card.CardID,
				ArtNo:    0,
				Count:    3, // 保持できるカードは制限しない
			})
		}
		playerCardRepo.SeedPlayerCards(playerID, playerCards)

		// Create starter decks
		starterDecks := map[string][]string{
			"SHE Standard":    {"SH-0001", "SH-0001", "SH-0001", "SH-0002", "SH-0005", "SH-0005", "SH-0006", "SH-0006", "SH-0006", "SH-0007", "SH-0007", "SH-0007", "SH-0008", "SH-0008", "SH-0012", "SH-0012", "SH-0014", "SH-0016", "SH-0019", "SH-0019", "SH-0019", "NT-0007", "NT-0008", "NT-0009", "NT-0010", "NT-0010", "NT-0023", "SH-0022", "NT-0025", "SH-0023"},
			"Tenki Standard":  {"TK-0001", "TK-0001", "TK-0004", "TK-0004", "TK-0004", "TK-0005", "TK-0005", "TK-0007", "TK-0007", "TK-0007", "TK-0009", "TK-0010", "TK-0010", "TK-0010", "TK-0013", "TK-0013", "TK-0014", "TK-0015", "TK-0015", "TK-0015", "TK-0018", "TK-0024", "TK-0024", "NT-0004", "NT-0007", "NT-0008", "NT-0009", "NT-0010", "NT-0010", "NT-0024"},
			"Sugar Standard":  {"SL-0001", "SL-0001", "SL-0001", "SL-0002", "SL-0002", "SL-0002", "SL-0003", "SL-0003", "SL-0004", "SL-0004", "SL-0006", "SL-0006", "SL-0007", "SL-0009", "SL-0013", "SL-0011", "SL-0011", "SL-0015", "SL-0015", "SL-0016", "SL-0017", "SL-0017", "SL-0022", "NT-0007", "NT-0008", "NT-0009", "NT-0013", "NT-0013", "NT-0013", "NT-0015"},
			"Tuners Standard": {"TN-0001", "TN-0001", "TN-0001", "TN-0002", "TN-0004", "TN-0004", "TN-0004", "TN-0006", "TN-0006", "TN-0007", "TN-0007", "TN-0007", "TN-0008", "TN-0009", "TN-0009", "TN-0010", "TN-0011", "TN-0013", "TN-0014", "TN-0015", "TN-0015", "TN-0015", "TN-0017", "TN-0017", "TN-0018", "TN-0018", "NT-0009", "NT-0010", "NT-0010", "NT-0010"},
		}

		for deckName, cardIDs := range starterDecks {
			cardCounts := make(map[string]int)
			for _, cardID := range cardIDs {
				cardCounts[cardID]++
			}

			var deckCards []model.DeckCard
			for cardID, count := range cardCounts {
				deckCards = append(deckCards, model.DeckCard{
					PlayerID: playerID,
					CardID:   cardID,
					ArtNo:    0,
					Count:    count,
				})
			}

			deck := &model.Deck{
				PlayerID:  playerID,
				DeckName:  deckName,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}
			if err := deckRepo.Create(ctx, deck, deckCards); err != nil {
				return err
			}
			log.Printf("auto-created starter deck %s for %s: %d cards, deckID=%d", deckName, playerID, len(cardIDs), deck.DeckID)
		}

		return nil
	}

	// 6. Bridge: when ShopService inserts player cards (e.g. select-faction),
	// also add them to MockPlayerCardRepository so services can read them.
	shopRepo.SetOnCardsInserted(func(playerID string, cards []*model.PlayerCard) {
		playerCardRepo.SeedPlayerCards(playerID, cards)
		log.Printf("synced %d player cards for %s (shop → playerCard)", len(cards), playerID)
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
			now := time.Now()
			c.JSON(http.StatusOK, []model.NewsArticle{
				{ArticleID: "dev-1", Source: "aws", Title: "Lambda が ARM64 対応を拡大、コスト最大34%削減", Tags: []string{"aws", "serverless"}, FetchedAt: now},
				{ArticleID: "dev-2", Source: "gcp", Title: "Cloud Run に GPU サポートが GA、ML推論ワークロードに対応", Tags: []string{"gcp", "container"}, FetchedAt: now},
				{ArticleID: "dev-3", Source: "azure", Title: "Cosmos DB の新プライシングモデルが発表", Tags: []string{"azure", "database"}, FetchedAt: now},
				{ArticleID: "dev-4", Source: "oci", Title: "マルチクラウド戦略の落とし穴: 3つの失敗パターン", Tags: []string{"multi-cloud"}, FetchedAt: now},
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

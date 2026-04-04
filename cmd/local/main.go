package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	gencache "github.com/kenyamaneko/overload-party-common/packages/devdata/cache"
	"github.com/kenyamaneko/overload-party-gateway/internal/cache"
	"github.com/kenyamaneko/overload-party-gateway/internal/config"
	"github.com/kenyamaneko/overload-party-gateway/internal/handler/rest"
	ws "github.com/kenyamaneko/overload-party-gateway/internal/handler/ws"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/router"
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
	subRepo := repository.NewMockSubscriptionRepository()
	factionRepo := repository.NewMockFactionRepository()
	storyRepo := repository.NewMockStoryRepository()
	userSettingsRepo := repository.NewMockUserSettingsRepository()
	gameConfigRepo := repository.NewMockGameConfigRepository()

	// 2b. Seed shop products from embedded JSON
	seedShopProductsFromJSON(shopRepo)

	// 3. Services
	authService := service.NewAuthService(playerRepo, shopRepo, userSettingsRepo, &repository.MockTxRunner{})
	playerService := service.NewPlayerService(playerRepo, gameConfigRepo)
	cardService := service.NewCardService(cardRepo, playerCardRepo)
	deckService := service.NewDeckService(deckRepo, playerCardRepo, cardCache)
	shopService := service.NewShopService(shopRepo, subRepo, playerRepo, factionRepo, &repository.MockTxRunner{}, cardCache, nil, nil)
	storyService := service.NewStoryService(storyRepo, nil, "")
	subscriptionService := service.NewSubscriptionService(subRepo, playerRepo, &repository.MockTxRunner{}, nil)
	newsRepo := repository.NewMockNewsRepository()
	seedNewsMock(newsRepo)
	newsService := service.NewNewsService(newsRepo)
	userSettingsService := service.NewUserSettingsService(userSettingsRepo)
	// 4. Battle client (uses cfg.BattleServerURL, default http://localhost:9002)
	log.Printf("battle client: %s", cfg.BattleServerURL)
	battleClient := service.NewBattleClient(cfg.BattleServerURL)

	// 5. Handlers
	wsManager := ws.NewManager(battleClient, playerService, deckService, deckRepo, gameConfigRepo)
	go wsManager.StartMatchmaking(ctx)
	wsHandler := ws.NewHandler(wsManager, nil, playerRepo, nil)
	handlers := &router.Handlers{
		Auth:         rest.NewAuthHandler(authService),
		Player:       rest.NewPlayerHandler(playerService),
		Spectate:     rest.NewSpectateHandler(wsManager),
		Card:         rest.NewCardHandler(cardService),
		Deck:         rest.NewDeckHandler(deckService),
		PlayerCard:   rest.NewPlayerCardHandler(deckService),
		GameLog:      rest.NewGameLogHandler(battleClient),
		NPC:          rest.NewNPCHandler(battleClient),
		Shop:         rest.NewShopHandler(shopService),
		Webhook:      rest.NewWebhookHandler(subscriptionService),
		Story:        rest.NewStoryHandler(storyService),
		UserSettings: rest.NewUserSettingsHandler(userSettingsService),
		News:         rest.NewNewsHandler(newsService),
	}

	// 5. Dev player setup: give all active cards + starter decks on first request
	devPlayerSetup := newDevPlayerSetup(cardCache, playerCardRepo, deckRepo)

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
		staticSvc, err := service.NewStaticService("data")
		if err != nil {
			log.Fatalf("failed to create static service: %v", err)
		}
		staticHandler := rest.NewStaticHandler(cfg, staticSvc)
		pub.GET("/version", staticHandler.GetVersion)
		pub.GET("/announcements", staticHandler.GetAnnouncements)
		pub.GET("/daily", staticHandler.GetDaily)
		pub.GET("/cloud-news", handlers.News.GetCloudNews)
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
	router.RegisterAuthRoutes(api, handlers)
	router.RegisterAPIRoutes(api, handlers)

	// Webhooks (no auth)
	router.RegisterWebhookRoutes(r.Group("/api/v1/shop/webhook"), handlers)

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

func seedShopProductsFromJSON(repo *repository.MockShopRepository) {
	var products []*model.Product
	if err := json.Unmarshal(gencache.ProductsJSON, &products); err != nil {
		log.Fatalf("failed to unmarshal products_gen.json: %v", err)
	}
	for _, p := range products {
		repo.AddProduct(p)
	}
	log.Printf("seeded %d shop products from products_gen.json", len(products))
}

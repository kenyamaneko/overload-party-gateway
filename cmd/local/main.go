package main

import (
	"context"
	"encoding/json"
	"fmt"
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
	subscriptionService := service.NewSubscriptionService(subRepo, playerRepo, &repository.MockTxRunner{})
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
		UserSettings: rest.NewUserSettingsHandler(userSettingsRepo),
	}

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

		// Create starter decks from embedded JSON
		var starterDecks []struct {
			DeckName string   `json:"deck_name"`
			Cards    []string `json:"cards"`
		}
		if err := json.Unmarshal(gencache.StarterDecksJSON, &starterDecks); err != nil {
			return fmt.Errorf("unmarshal starter_decks_gen.json: %w", err)
		}

		for _, sd := range starterDecks {
			cardCounts := make(map[string]int)
			for _, cardID := range sd.Cards {
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
				DeckName:  sd.DeckName,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}
			if err := deckRepo.Create(ctx, deck, deckCards); err != nil {
				return err
			}
			log.Printf("auto-created starter deck %s for %s: %d cards, deckID=%d", sd.DeckName, playerID, len(sd.Cards), deck.DeckID)
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
			var articles []model.NewsArticle
			if err := json.Unmarshal(gencache.NewsMockJSON, &articles); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load news mock"})
				return
			}
			now := time.Now()
			for i := range articles {
				articles[i].FetchedAt = now
			}
			c.JSON(http.StatusOK, articles)
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

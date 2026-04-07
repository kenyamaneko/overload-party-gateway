package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	gcsstorage "cloud.google.com/go/storage"

	"github.com/kenyamaneko/overload-party-gateway/internal/cache"
	"github.com/kenyamaneko/overload-party-gateway/internal/config"
	"github.com/kenyamaneko/overload-party-gateway/internal/handler/rest"
	ws "github.com/kenyamaneko/overload-party-gateway/internal/handler/ws"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/router"
	"github.com/kenyamaneko/overload-party-gateway/internal/platform"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

const (
	serverShutdownTimeout    = 10 * time.Second
	cardCacheRefreshInterval = 5 * time.Minute
)

func main() {
	ctx := context.Background()
	cfg := config.Load()

	if cfg.Env == "prod" && len(cfg.AllowedOrigins) == 0 {
		log.Fatal("ALLOWED_ORIGINS must be set in production")
	}
	if cfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL must be set")
	}

	if cfg.Env == "prod" {
		gin.SetMode(gin.ReleaseMode)
	}

	// PostgreSQL connection pool
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to create pg pool: %v", err)
	}
	defer pool.Close()

	// Firebase Auth client
	authClient, err := middleware.NewFirebaseAuthClient(ctx)
	if err != nil {
		log.Fatalf("failed to create firebase auth client: %v", err)
	}

	// Repositories (PostgreSQL)
	playerRepo := repository.NewPgPlayerRepository(pool)
	cardRepo := repository.NewPgCardRepository(pool)
	deckRepo := repository.NewPgDeckRepository(pool)
	playerCardRepo := repository.NewPgPlayerCardRepository(pool)
	shopRepo := repository.NewPgShopRepository(pool)
	subRepo := repository.NewPgSubscriptionRepository(pool)
	factionRepo := repository.NewPgFactionRepository(pool)
	storyRepo := repository.NewPgStoryRepository(pool)
	userSettingsRepo := repository.NewPgUserSettingsRepository(pool)
	gameConfigRepo := repository.NewPgGameConfigRepository(pool)
	newsRepo := repository.NewPgNewsRepository(pool)

	// Card cache (load at startup)
	cardCache := cache.NewCardCache()
	if err := cardCache.Load(ctx, cardRepo); err != nil {
		log.Fatalf("failed to load card cache: %v", err)
	}
	log.Printf("loaded %d cards into cache", cardCache.Count())

	// Receipt verifiers
	var appleVerifier platform.ReceiptVerifier
	var googleVerifier platform.ReceiptVerifier
	if cfg.ApplePrivateKeyPath != "" {
		av, err := platform.NewAppleReceiptVerifier(
			cfg.AppleKeyID, cfg.AppleIssuerID, cfg.AppleBundleID,
			cfg.ApplePrivateKeyPath, cfg.AppleEnvironment,
		)
		if err != nil {
			log.Fatalf("failed to create apple verifier: %v", err)
		}
		appleVerifier = av
	}
	if cfg.GooglePackageName != "" {
		gv, err := platform.NewGoogleReceiptVerifier(ctx, cfg.GooglePackageName)
		if err != nil {
			log.Fatalf("failed to create google verifier: %v", err)
		}
		googleVerifier = gv
	}

	// GCS client for story scripts
	var gcsClient *gcsstorage.Client
	if cfg.StoryBucket != "" {
		gc, err := gcsstorage.NewClient(ctx)
		if err != nil {
			log.Fatalf("failed to create gcs client: %v", err)
		}
		defer gc.Close()
		gcsClient = gc
	}

	// Services
	txManager := repository.NewTxManager(pool)
	authService := service.NewAuthService(playerRepo, shopRepo, userSettingsRepo, txManager)
	playerService := service.NewPlayerService(playerRepo, gameConfigRepo)
	cardService := service.NewCardService(cardRepo, playerCardRepo)
	deckService := service.NewDeckService(deckRepo, playerCardRepo, cardCache)
	shopService := service.NewShopService(shopRepo, subRepo, playerRepo, factionRepo, txManager, cardCache, appleVerifier, googleVerifier)
	storyService := service.NewStoryService(storyRepo, gcsClient, cfg.StoryBucket)
	var googleSubVerifier service.GoogleSubVerifier
	if cfg.GooglePackageName != "" {
		gv, err := platform.NewGooglePlaySubVerifier(ctx, cfg.GooglePackageName)
		if err != nil {
			log.Fatalf("failed to create google sub verifier: %v", err)
		}
		googleSubVerifier = gv
	}
	subscriptionService := service.NewSubscriptionService(subRepo, playerRepo, txManager, googleSubVerifier)
	newsService := service.NewNewsService(newsRepo)
	userSettingsService := service.NewUserSettingsService(userSettingsRepo)

	// Game player repository (gateway-owned game_players table)
	gamePlayerRepo := repository.NewPgGamePlayerRepository(pool)

	// Battle client (HTTP → battle server)
	battleClient := service.NewBattleClient(cfg.BattleServerURL)
	wsManager := ws.NewManager(battleClient, playerService, deckService, deckRepo, gameConfigRepo, gamePlayerRepo)
	go wsManager.StartMatchmaking(ctx)
	wsHandler := ws.NewHandler(wsManager, authClient, playerRepo, cfg.AllowedOrigins)
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
		Story:        rest.NewStoryHandler(storyService),
		Webhook:      rest.NewWebhookHandler(subscriptionService),
		UserSettings: rest.NewUserSettingsHandler(userSettingsService),
		News:         rest.NewNewsHandler(newsService),
	}

	// Router
	r := gin.Default()
	r.Use(middleware.CORS(cfg.AllowedOrigins...))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
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

	v1 := r.Group("/api/v1")
	v1.Use(middleware.FirebaseAuth(authClient))

	// Auth endpoints: only need firebase_uid (player may not exist yet)
	router.RegisterAuthRoutes(v1, handlers)

	// All other endpoints: need player_id resolved from firebase_uid
	api := v1.Group("")
	api.Use(middleware.PlayerResolve(playerRepo))
	router.RegisterAPIRoutes(api, handlers)

	// Webhooks (no Firebase auth -- authenticated by Apple/Google)
	router.RegisterWebhookRoutes(r.Group("/api/v1/shop/webhook"), handlers)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	srvCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Start background goroutine for card cache refresh
	go refreshCardCache(srvCtx, cardCache, cardRepo)

	go func() {
		log.Printf("gateway server starting on :%s (env=%s)", cfg.Port, cfg.Env)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-srvCtx.Done()
	log.Println("shutting down gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server forced to shutdown: %v", err)
	}

	log.Println("gateway server exited")
}

// refreshCardCache periodically refreshes the card definition cache.
func refreshCardCache(ctx context.Context, cc *cache.CardCache, repo port.CardRepo) {
	ticker := time.NewTicker(cardCacheRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := cc.Refresh(ctx, repo); err != nil {
				log.Printf("card cache refresh error: %v", err)
			} else {
				log.Printf("card cache refreshed: %d cards", cc.Count())
			}
		}
	}
}

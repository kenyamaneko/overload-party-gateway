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

	"github.com/kenyamaneko/overload-party-gateway/internal/cache"
	"github.com/kenyamaneko/overload-party-gateway/internal/config"
	"github.com/kenyamaneko/overload-party-gateway/internal/handler/rest"
	ws "github.com/kenyamaneko/overload-party-gateway/internal/handler/ws"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/platform"
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
	shopRepo := repository.NewPgShopRepository(pool)
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

	// Services
	authService := service.NewAuthService(playerRepo, shopRepo, userSettingsRepo)
	playerService := service.NewPlayerService(playerRepo, gameConfigRepo)
	cardService := service.NewCardService(cardRepo, deckRepo)
	deckService := service.NewDeckService(deckRepo, cardCache)
	shopService := service.NewShopService(shopRepo, playerRepo, cardCache, appleVerifier, googleVerifier)
	subscriptionService := service.NewSubscriptionService(shopRepo, playerRepo)

	// Battle client (HTTP → battle server)
	battleClient := service.NewBattleClient(cfg.BattleServerURL)
	wsManager := ws.NewManager(battleClient, playerService, deckRepo)
	go wsManager.StartMatchmaking(ctx)
	wsHandler := ws.NewHandler(wsManager, authClient, playerRepo, cfg.AllowedOrigins)
	authHandler := rest.NewAuthHandler(authService)
	playerHandler := rest.NewPlayerHandler(playerService)
	cardHandler := rest.NewCardHandler(cardService)
	deckHandler := rest.NewDeckHandler(deckService)
	playerCardHandler := rest.NewPlayerCardHandler(deckService)
	gameLogHandler := rest.NewGameLogHandler(battleClient)
	shopHandler := rest.NewShopHandler(shopService)
	webhookHandler := rest.NewWebhookHandler(subscriptionService)
	userSettingsHandler := rest.NewUserSettingsHandler(userSettingsRepo)
	newsHandler := rest.NewNewsHandler(newsRepo)

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
				"id":   "daily-tip-1",
				"text": "FEリソースの可用性が0になると相手のDVが加算されます。サポートカードで守りを固めましょう！",
			})
		})
		pub.GET("/cloud-news", newsHandler.GetCloudNews)
	}

	v1 := r.Group("/api/v1")
	v1.Use(middleware.FirebaseAuth(authClient))

	// Auth endpoints: only need firebase_uid (player may not exist yet)
	{
		v1.POST("/auth/register", authHandler.Register)
		v1.POST("/auth/login", authHandler.Login)
	}

	// All other endpoints: need player_id resolved from firebase_uid
	api := v1.Group("")
	api.Use(middleware.PlayerResolve(playerRepo))
	{
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

		// Shop
		api.POST("/player/select-faction", shopHandler.SelectFaction)
		api.GET("/shop/products", shopHandler.GetProducts)
		api.POST("/shop/purchase", shopHandler.Purchase)
		api.POST("/shop/subscribe", shopHandler.Subscribe)
	}

	// Webhooks (no Firebase auth -- authenticated by Apple/Google)
	webhooks := r.Group("/api/v1/shop/webhook")
	{
		webhooks.POST("/apple", webhookHandler.HandleAppleWebhook)
		webhooks.POST("/google", webhookHandler.HandleGoogleWebhook)
	}

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
func refreshCardCache(ctx context.Context, cc *cache.CardCache, repo repository.CardRepo) {
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

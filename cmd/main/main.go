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

	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/cardclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/matchmakingclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/scenarioclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/shopclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/config"
	pubsubadapter "github.com/kenyamaneko/overload-party-gateway/internal/adapter/pubsub"
	"github.com/kenyamaneko/overload-party-gateway/internal/handler/rest"
	ws "github.com/kenyamaneko/overload-party-gateway/internal/handler/ws"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
	"github.com/kenyamaneko/overload-party-gateway/internal/router"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

const serverShutdownTimeout = 10 * time.Second

func main() {
	ctx := context.Background()
	cfg := config.Load()

	if cfg.Env == "prod" && len(cfg.AllowedOrigins) == 0 {
		log.Fatal("ALLOWED_ORIGINS must be set in production")
	}
	if cfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL must be set")
	}
	if cfg.PubsubProjectID == "" {
		log.Fatal("PUBSUB_PROJECT_ID must be set")
	}

	if cfg.Env == "prod" {
		gin.SetMode(gin.ReleaseMode)
	}

	// PostgreSQL 接続プール: gateway.game_players（gateway 所有）と
	// newsfeed.news_articles（read-only クロススキーマプロキシ）に使用
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to create pg pool: %v", err)
	}
	defer pool.Close()

	// Firebase Auth クライアント
	authClient, err := middleware.NewFirebaseAuthClient(ctx)
	if err != nil {
		log.Fatalf("failed to create firebase auth client: %v", err)
	}

	// gateway 所有の game_players リポジトリ + read-only newsfeed プロキシ
	newsRepo := repository.NewPgNewsRepository(pool)
	gamePlayerRepo := repository.NewPgGamePlayerRepository(pool)

	// 外部サービスクライアント
	cardClient := cardclient.New(cfg.CardServiceURL)
	matchmakingClient := matchmakingclient.New(cfg.MatchmakingServiceURL)
	accountClient := accountclient.New(cfg.AccountServiceURL)
	shopClient := shopclient.New(cfg.ShopServiceURL)
	scenarioClient := scenarioclient.New(cfg.ScenarioServiceURL)

	newsService := service.NewNewsService(newsRepo)

	// Battle クライアント（HTTP → battle server）
	battleClient := service.NewBattleClient(cfg.BattleServerURL)
	matchmakingTimeout := time.Duration(cfg.MatchmakingTimeoutSec) * time.Second
	wsManager := ws.NewManager(battleClient, accountClient, cardClient, matchmakingClient, gamePlayerRepo, matchmakingTimeout)
	wsHandler := ws.NewHandler(wsManager, authClient, accountClient, cfg.AllowedOrigins)

	handlers := &router.Handlers{
		Auth:         rest.NewAuthHandler(accountClient),
		Player:       rest.NewPlayerHandler(accountClient),
		UserSettings: rest.NewUserSettingsHandler(accountClient),
		Spectate:     rest.NewSpectateHandler(wsManager),
		Card:         rest.NewCardHandler(cardClient),
		Deck:         rest.NewDeckHandler(cardClient),
		PlayerCard:   rest.NewPlayerCardHandler(cardClient),
		GameLog:      rest.NewGameLogHandler(battleClient),
		NPC:          rest.NewNPCHandler(battleClient),
		Shop:         rest.NewShopHandler(shopClient),
		Scenario:     rest.NewScenarioHandler(scenarioClient),
		News:         rest.NewNewsHandler(newsService),
	}

	// ルーター
	r := gin.Default()
	r.Use(middleware.CORS(cfg.AllowedOrigins...))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// WebSocket（REST auth middleware なし。認証は HandleUpgrade 内で処理）
	r.GET("/ws", wsHandler.HandleUpgrade)

	// 公開 API エンドポイント（認証不要、クライアントのスプラッシュ画面で使用）
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

	router.RegisterAuthRoutes(v1, handlers)

	api := v1.Group("")
	api.Use(middleware.PlayerResolve(accountClient))
	router.RegisterAPIRoutes(api, handlers)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	srvCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// マッチメイキング Pub/Sub subscriber を開始
	subscriber, err := pubsubadapter.NewMatchSubscriber(srvCtx, cfg.PubsubProjectID, cfg.MatchmakingSubscription, wsManager)
	if err != nil {
		log.Fatalf("failed to create match subscriber: %v", err)
	}
	defer func() { _ = subscriber.Close() }()

	go func() {
		if err := subscriber.Run(srvCtx); err != nil && srvCtx.Err() == nil {
			log.Fatalf("match subscriber error: %v", err)
		}
	}()

	// クロスサービスイベント subscriber（faction-selected, premium-updated）。
	// 接続中プレイヤーに WS 完了メッセージを push する。
	wsPusher := &pubsubadapter.HubWSPusher{Hub: wsManager.Hub}

	factionEventSub, err := pubsubadapter.NewEventSubscriber(
		srvCtx, cfg.PubsubProjectID, cfg.FactionSelectedSubscription, "faction-selected", wsPusher,
	)
	if err != nil {
		log.Fatalf("failed to create faction-selected event subscriber: %v", err)
	}
	defer func() { _ = factionEventSub.Close() }()

	go func() {
		if err := factionEventSub.Run(srvCtx); err != nil && srvCtx.Err() == nil {
			log.Fatalf("faction-selected event subscriber error: %v", err)
		}
	}()

	premiumEventSub, err := pubsubadapter.NewEventSubscriber(
		srvCtx, cfg.PubsubProjectID, cfg.PremiumUpdatedSubscription, "premium-updated", wsPusher,
	)
	if err != nil {
		log.Fatalf("failed to create premium-updated event subscriber: %v", err)
	}
	defer func() { _ = premiumEventSub.Close() }()

	go func() {
		if err := premiumEventSub.Run(srvCtx); err != nil && srvCtx.Err() == nil {
			log.Fatalf("premium-updated event subscriber error: %v", err)
		}
	}()

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

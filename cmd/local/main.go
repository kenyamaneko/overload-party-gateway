// Package main はローカルモード gateway のエントリポイントです。
//
// *_SERVICE_URL 環境変数で指定した URL の全下流サービスが起動している必要がある。
// cmd/main との違いは認証 middleware: UseDevAuth により Firebase ID トークンの代わりに
// `dev-token-{uid}` を使用でき、CORS は全オリジン許可となる。
package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	pubsubadapter "github.com/kenyamaneko/overload-party-gateway/internal/adapter/pubsub"
	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/cardclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/matchmakingclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/config"
	"github.com/kenyamaneko/overload-party-gateway/internal/handler/rest"
	ws "github.com/kenyamaneko/overload-party-gateway/internal/handler/ws"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
	"github.com/kenyamaneko/overload-party-gateway/internal/router"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

func main() {
	log.Println("=== Overload Party Gateway (LOCAL MODE) ===")

	cfg := config.Load()
	if cfg.DatabaseConn == "" {
		log.Fatal("DATABASE_CONN must be set (gateway owns gateway.game_players and reads newsfeed.news_articles)")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DatabaseConn)
	if err != nil {
		log.Fatalf("failed to create pg pool: %v", err)
	}
	defer pool.Close()

	gamePlayerRepo := repository.NewPgGamePlayerRepository(pool)
	processedMatchRepo := repository.NewPgProcessedMatchRepository(pool)

	// ローカルモードでは Firestore (game_config) は optional。GOOGLE_CLOUD_PROJECT_ID が
	// 未設定ならスキップする (NPC バトルがメインワークフロー)。FIRESTORE_EMULATOR_HOST が
	// 設定されていれば公式クライアントが自動的にエミュレーターへルーティングする。
	if cfg.GoogleCloudProjectID != "" {
		fsClient, err := firestore.NewClient(ctx, cfg.GoogleCloudProjectID)
		if err != nil {
			log.Fatalf("failed to create firestore client: %v", err)
		}
		defer func() { _ = fsClient.Close() }()
		_ = repository.NewFirestoreGameConfigRepository(fsClient)
	} else {
		log.Println("GOOGLE_CLOUD_PROJECT_ID is unset; skipping Firestore client")
	}

	cardClient := cardclient.New(cfg.CardServiceURL)
	matchmakingClient := matchmakingclient.New(cfg.MatchmakingServiceURL)
	accountClient := accountclient.New(cfg.AccountServiceURL)

	if cfg.InternalAuthSecret == "" {
		log.Fatal("INTERNAL_AUTH_SECRET must be set")
	}
	internalSigner := internalauth.NewSigner(
		internalauth.StaticHS256Resolver([]byte(cfg.InternalAuthSecret), internalauth.DefaultKeyID),
		internalauth.DefaultKeyID,
	)

	battleClient := service.NewBattleClient(cfg.BattleServerURL)
	matchmakingTimeout := time.Duration(cfg.MatchmakingTimeoutSec) * time.Second
	wsManager := ws.NewManager(battleClient, accountClient, cardClient, matchmakingClient, gamePlayerRepo, processedMatchRepo, matchmakingTimeout, internalSigner)
	wsHandler := ws.NewHandler(wsManager, nil, accountClient, nil)

	matchSub, err := pubsubadapter.NewMatchSubscriber(wsManager)
	if err != nil {
		log.Fatalf("failed to create match subscriber: %v", err)
	}

	handlers := &router.Handlers{
		Auth:   rest.NewAuthHandler(accountClient),
		PubSub: rest.NewPubSubPushHandler(matchSub),
	}

	r := gin.Default()
	r.Use(middleware.UseCORS())

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "mode": "local"})
	})

	r.GET("/ws", wsHandler.HandleUpgrade)

	pub := r.Group("/api/v1")
	{
		pub.GET("/health", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})
		staticHandler := rest.NewStaticHandler(cfg)
		pub.GET("/version", staticHandler.GetVersion)
	}

	// Pub/Sub push 配信の内部エンドポイント。ローカルモードは Pub/Sub エミュレーターを使うため
	// 本物の Google 署名 OIDC トークンを用意できず、cmd/main のような push 認証は行わない。
	internalGroup := r.Group("/internal/v1")
	router.RegisterPubSubRoutes(internalGroup, handlers)

	// ローカルモードは Firebase Auth エミュレーターを持たない (この compose スタックは
	// Pub/Sub と Firestore のエミュレーターのみ提供する) ため、Firebase ID トークン検証の
	// 代わりに dev-token を使い、その代償として prod (cmd/main) との認証非対称を許容する。
	// auth (register/login) は dev-token を検証するだけにし、プレイヤー生成は handler に委ねる。
	// 自動生成 middleware の配下に置くと register が二重生成になり 409 を返すため分離する。
	auth := r.Group("/api/v1")
	auth.Use(middleware.UseDevAuth())
	router.RegisterAuthRoutes(auth, handlers)

	// forward は dev-token からプレイヤーを自動生成する。これは register を呼ばない dev-token
	// クライアント (同梱 UI) を成立させるためのローカル限定の非対称で、prod は明示 register を要する。
	api := r.Group("/api/v1")
	api.Use(middleware.UseDevAuthWithPlayerResolve(accountClient))
	api.Use(middleware.IssueInternalAuth(internalSigner))
	if err := router.RegisterForwardRoutes(api, cfg); err != nil {
		log.Fatalf("failed to register forward routes: %v", err)
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

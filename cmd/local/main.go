// Package main はローカルモード gateway のエントリポイントです。
//
// *_SERVICE_URL 環境変数で指定した URL の全下流サービスが起動している必要がある。
// cmd/main との違いは認証 middleware: DevAuth により Firebase ID トークンの代わりに
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
	"github.com/redis/go-redis/v9"

	"github.com/kenyamaneko/overload-party-gateway/internal/adapter/displaymetacache"
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

	// display meta cache (L1 in-memory + L2 Upstash Redis)。
	// ローカルモードでは UPSTASH_REDIS_URL_GATEWAY 未設定で L2 を skip し L1 のみで
	// 動かす (NPC バトル中心の local 開発を Redis 必須にしないため)。本 PR は基盤
	// 整備のみで wsManager / handler への配線はしないため、初期化結果は破棄して
	// 起動が壊れないことだけ担保する。
	if cfg.RedisURL == "" {
		log.Println("UPSTASH_REDIS_URL_GATEWAY is unset; display meta cache runs with in-memory L1 only (L2 Redis skipped)")
		_ = displaymetacache.NewMemoryStore()
	} else {
		opt, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			log.Fatalf("failed to parse UPSTASH_REDIS_URL_GATEWAY: %v", err)
		}
		displayMetaRedis := redis.NewClient(opt)
		defer func() { _ = displayMetaRedis.Close() }()
		if err := displayMetaRedis.Ping(ctx).Err(); err != nil {
			log.Fatalf("display meta cache redis ping failed: %v", err)
		}
		_ = displaymetacache.New(
			displaymetacache.NewMemoryStore(),
			displaymetacache.NewRedisStore(displayMetaRedis),
		)
	}

	// ローカルモードでは Firestore (game_config) と matchmaking Pub/Sub subscriber は optional。
	// GOOGLE_CLOUD_PROJECT_ID が未設定なら両方スキップする (NPC バトルがメインワークフロー)。
	// FIRESTORE_EMULATOR_HOST が設定されていれば公式クライアントが自動的に
	// エミュレーターへルーティングする。
	if cfg.GoogleCloudProjectID != "" {
		fsClient, err := firestore.NewClient(ctx, cfg.GoogleCloudProjectID)
		if err != nil {
			log.Fatalf("failed to create firestore client: %v", err)
		}
		defer func() { _ = fsClient.Close() }()
		_ = repository.NewFirestoreGameConfigRepository(fsClient)
	} else {
		log.Println("GOOGLE_CLOUD_PROJECT_ID is unset; skipping Firestore client and matchmaking Pub/Sub subscriber")
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
	wsManager := ws.NewManager(battleClient, accountClient, cardClient, matchmakingClient, gamePlayerRepo, matchmakingTimeout, internalSigner)
	wsHandler := ws.NewHandler(wsManager, nil, accountClient, nil)
	handlers := &router.Handlers{
		Auth:     rest.NewAuthHandler(accountClient),
		Spectate: rest.NewSpectateHandler(wsManager),
	}

	r := gin.Default()
	r.Use(middleware.CORS())

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

	api := r.Group("/api/v1")
	api.Use(middleware.DevAuthWithPlayerResolve(accountClient))
	router.RegisterAuthRoutes(api, handlers)
	api.Use(middleware.IssueInternalAuth(internalSigner))
	router.RegisterAPIRoutes(api, handlers)
	if err := router.RegisterForwardRoutes(api, cfg); err != nil {
		log.Fatalf("failed to register forward routes: %v", err)
	}

	srv := &http.Server{
		Addr:    ":9001",
		Handler: r,
	}

	srvCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// matchmaking Pub/Sub subscriber も GOOGLE_CLOUD_PROJECT_ID が設定されたときだけ起動する。
	// 未設定時のスキップログは Firestore 側の分岐で出力済み。
	if cfg.GoogleCloudProjectID != "" {
		stream, err := pubsubadapter.NewStream(srvCtx, cfg.GoogleCloudProjectID, cfg.MatchmakingSubscription)
		if err != nil {
			log.Fatalf("failed to create matchmaking stream: %v", err)
		}
		defer func() { _ = stream.Close() }()
		subscriber, err := pubsubadapter.NewMatchSubscriber(stream, wsManager)
		if err != nil {
			log.Fatalf("failed to create match subscriber: %v", err)
		}
		go func() {
			if err := subscriber.Run(srvCtx); err != nil && srvCtx.Err() == nil {
				log.Fatalf("match subscriber error: %v", err)
			}
		}()
	}

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

package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

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

const serverShutdownTimeout = 10 * time.Second

// subscriberRunner は errgroup で束ねる subscriber の最小契約。
// stream ライフサイクルは caller 側で defer Close しているため、subscriber は
// Run のみを持つ。
type subscriberRunner interface {
	Run(ctx context.Context) error
}

func main() {
	ctx := context.Background()
	cfg := config.Load()

	if cfg.Env == "prod" && len(cfg.AllowedOrigins) == 0 {
		log.Fatal("ALLOWED_ORIGINS must be set in production")
	}
	if cfg.DatabaseConn == "" {
		log.Fatal("DATABASE_CONN must be set")
	}
	if cfg.PubsubProjectID == "" {
		log.Fatal("PUBSUB_PROJECT_ID must be set")
	}
	if cfg.FirestoreProjectID == "" {
		log.Fatal("FIRESTORE_PROJECT_ID must be set (game_config)")
	}
	if cfg.InternalAuthSecret == "" {
		log.Fatal("INTERNAL_AUTH_SECRET must be set")
	}

	if cfg.Env == "prod" {
		gin.SetMode(gin.ReleaseMode)
	}

	// PostgreSQL 接続プール: gateway.game_players（gateway 所有）と
	// newsfeed.news_articles（read-only クロススキーマプロキシ）に使用
	pool, err := pgxpool.New(ctx, cfg.DatabaseConn)
	if err != nil {
		log.Fatalf("failed to create pg pool: %v", err)
	}
	defer pool.Close()

	// Firestore クライアント (game_config)
	fsClient, err := firestore.NewClient(ctx, cfg.FirestoreProjectID)
	if err != nil {
		log.Fatalf("failed to create firestore client: %v", err)
	}
	defer func() { _ = fsClient.Close() }()

	// Firebase Auth クライアント
	authClient, err := middleware.NewFirebaseAuthClient(ctx)
	if err != nil {
		log.Fatalf("failed to create firebase auth client: %v", err)
	}

	// gateway 所有の game_players リポジトリ
	gamePlayerRepo := repository.NewPgGamePlayerRepository(pool)
	// game_config は現在 gateway の runtime パスから参照していない。
	// クライアント到達性は起動時に検証するため、repo を生成だけしておく。
	_ = repository.NewFirestoreGameConfigRepository(fsClient)

	// 外部サービスクライアント
	cardClient := cardclient.New(cfg.CardServiceURL)
	matchmakingClient := matchmakingclient.New(cfg.MatchmakingServiceURL)
	accountClient := accountclient.New(cfg.AccountServiceURL)

	internalSigner := internalauth.NewSigner(
		internalauth.StaticHS256Resolver([]byte(cfg.InternalAuthSecret), internalauth.DefaultKeyID),
		internalauth.DefaultKeyID,
	)

	// Battle クライアント（HTTP → battle server）
	battleClient := service.NewBattleClient(cfg.BattleServerURL)
	matchmakingTimeout := time.Duration(cfg.MatchmakingTimeoutSec) * time.Second
	wsManager := ws.NewManager(battleClient, accountClient, cardClient, matchmakingClient, gamePlayerRepo, matchmakingTimeout, internalSigner)
	wsHandler := ws.NewHandler(wsManager, authClient, accountClient, cfg.AllowedOrigins)

	handlers := &router.Handlers{
		Auth:     rest.NewAuthHandler(accountClient),
		Spectate: rest.NewSpectateHandler(wsManager),
		GameLog:  rest.NewGameLogHandler(battleClient),
		NPC:      rest.NewNPCHandler(battleClient),
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
		staticHandler := rest.NewStaticHandler(cfg)
		pub.GET("/version", staticHandler.GetVersion)
	}

	v1 := r.Group("/api/v1")
	v1.Use(middleware.FirebaseAuth(authClient))

	router.RegisterAuthRoutes(v1, handlers)

	api := v1.Group("")
	api.Use(middleware.PlayerResolve(accountClient))
	api.Use(middleware.IssueInternalAuth(internalSigner))
	router.RegisterAPIRoutes(api, handlers)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	srvCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	matchStream, err := pubsubadapter.NewStream(srvCtx, cfg.PubsubProjectID, cfg.MatchmakingSubscription)
	if err != nil {
		log.Fatalf("failed to create matchmaking stream: %v", err)
	}
	defer func() { _ = matchStream.Close() }()

	matchSub, err := pubsubadapter.NewMatchSubscriber(matchStream, wsManager)
	if err != nil {
		log.Fatalf("failed to create match subscriber: %v", err)
	}

	// Why: graceful shutdown 時に subscriber と HTTP server が確実に停止するまで
	// main を block させるため errgroup で束ねる。どちらかが err を返すと
	// gCtx がキャンセルされ、他方も停止する。
	if err := runServices(srvCtx, cfg, srv, matchSub); err != nil {
		log.Fatalf("server: %v", err)
	}

	log.Println("gateway server exited")
}

// runServices は HTTP server と全 subscriber を errgroup で束ねて起動する。
// ctx キャンセル (SIGINT/SIGTERM) を検知すると HTTP server の Shutdown を呼び、
// subscriber も gCtx 経由で停止する。
func runServices(
	ctx context.Context,
	cfg *config.Config,
	srv *http.Server,
	subscribers ...subscriberRunner,
) error {
	g, gCtx := errgroup.WithContext(ctx)

	for _, sub := range subscribers {
		sub := sub
		g.Go(func() error {
			if err := sub.Run(gCtx); err != nil && gCtx.Err() == nil {
				return fmt.Errorf("subscriber: %w", err)
			}
			return nil
		})
	}

	g.Go(func() error {
		log.Printf("gateway server starting on :%s (env=%s)", cfg.Port, cfg.Env)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		<-gCtx.Done()
		log.Println("shutting down gracefully...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("http shutdown: %w", err)
		}
		return nil
	})

	return g.Wait()
}

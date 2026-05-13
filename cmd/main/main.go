package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"

	pubsubadapter "github.com/kenyamaneko/overload-party-gateway/internal/adapter/pubsub"
	"github.com/kenyamaneko/overload-party-gateway/internal/adapter/secretmanager"
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

// display meta cache 用 Upstash Redis の認証情報を保管している Secret Manager 上の secret ID。
const (
	secretIDDisplayMetaRedisEndpoint = "gateway-upstash-redis-endpoint"
	secretIDDisplayMetaRedisPassword = "gateway-upstash-redis-password"
)

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
	if cfg.GoogleCloudProjectID == "" {
		log.Fatal("GOOGLE_CLOUD_PROJECT_ID must be set")
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
	fsClient, err := firestore.NewClient(ctx, cfg.GoogleCloudProjectID)
	if err != nil {
		log.Fatalf("failed to create firestore client: %v", err)
	}
	defer func() { _ = fsClient.Close() }()

	// Firebase Auth クライアント
	authClient, err := middleware.NewFirebaseAuthClient(ctx)
	if err != nil {
		log.Fatalf("failed to create firebase auth client: %v", err)
	}

	// 起動時に display meta cache 用 Upstash Redis の到達性を検証する。
	// production rollout で接続不能時に fail-fast させるため。
	displayMetaRedis, err := newDisplayMetaRedisClient(ctx, cfg)
	if err != nil {
		log.Fatalf("failed to create display meta cache redis client: %v", err)
	}
	defer func() { _ = displayMetaRedis.Close() }()
	if err := displayMetaRedis.Ping(ctx).Err(); err != nil {
		log.Fatalf("display meta cache redis ping failed: %v", err)
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
	if err := router.RegisterForwardRoutes(api, cfg); err != nil {
		log.Fatalf("failed to register forward routes: %v", err)
	}

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	srvCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	matchStream, err := pubsubadapter.NewStream(srvCtx, cfg.GoogleCloudProjectID, cfg.MatchmakingSubscription)
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

// newDisplayMetaRedisClient は APP_ENV に応じて display meta cache 用の
// Upstash Redis クライアントを構築する。local は URL 直結、production は
// Secret Manager から endpoint / password を取得して TLS 接続する。
func newDisplayMetaRedisClient(ctx context.Context, cfg *config.Config) (*redis.Client, error) {
	switch cfg.AppEnv {
	case config.AppEnvLocal:
		if cfg.RedisURL == "" {
			return nil, fmt.Errorf("config: UPSTASH_REDIS_URL_GATEWAY is required when APP_ENV=%q", config.AppEnvLocal)
		}
		opt, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			return nil, fmt.Errorf("parse redis url: %w", err)
		}
		return redis.NewClient(opt), nil

	case config.AppEnvProduction:
		sm, err := secretmanager.NewClient(ctx, cfg.GoogleCloudProjectID)
		if err != nil {
			return nil, err
		}
		defer func() { _ = sm.Close() }()

		endpoint, err := sm.AccessLatest(ctx, secretIDDisplayMetaRedisEndpoint)
		if err != nil {
			return nil, err
		}
		password, err := sm.AccessLatest(ctx, secretIDDisplayMetaRedisPassword)
		if err != nil {
			return nil, err
		}
		return redis.NewClient(&redis.Options{
			Addr:      endpoint,
			Password:  password,
			TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		}), nil

	default:
		return nil, fmt.Errorf("unsupported APP_ENV: %q (expected %q or %q)", cfg.AppEnv, config.AppEnvLocal, config.AppEnvProduction)
	}
}

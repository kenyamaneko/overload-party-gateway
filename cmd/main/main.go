package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
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

// newCloudLoggingHandler は Cloud Logging に適合するログハンドラを生成する。
func newCloudLoggingHandler() slog.Handler {
	return slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey {
				a.Key = "severity"
				if level, ok := a.Value.Any().(slog.Level); ok {
					switch {
					case level >= slog.LevelError:
						a.Value = slog.StringValue("ERROR")
					case level >= slog.LevelWarn:
						a.Value = slog.StringValue("WARNING")
					case level >= slog.LevelInfo:
						a.Value = slog.StringValue("INFO")
					default:
						a.Value = slog.StringValue("DEBUG")
					}
				}
			}
			if a.Key == slog.MessageKey {
				a.Key = "message"
			}
			return a
		},
	})
}

func main() {
	ctx := context.Background()
	cfg := config.Load()

	// cmd/main は GKE (dev/stg/prod いずれも Cloud Logging) 上で動作するため常に
	// Cloud Logging 互換 JSON で出力する。ローカル実行のテキスト出力は cmd/local が担う。
	slog.SetDefault(slog.New(newCloudLoggingHandler()).With("service", "gateway"))

	if cfg.Env == "prod" && len(cfg.AllowedOrigins) == 0 {
		slog.Error("ALLOWED_ORIGINS must be set in production")
		os.Exit(1)
	}
	if cfg.DatabaseConn == "" {
		slog.Error("DATABASE_CONN must be set")
		os.Exit(1)
	}
	if cfg.GoogleCloudProjectID == "" {
		slog.Error("GOOGLE_CLOUD_PROJECT_ID must be set")
		os.Exit(1)
	}
	if cfg.InternalAuthSecret == "" {
		slog.Error("INTERNAL_AUTH_SECRET must be set")
		os.Exit(1)
	}

	if cfg.Env == "prod" {
		gin.SetMode(gin.ReleaseMode)
	}

	// PostgreSQL 接続プール: gateway.game_players（gateway 所有）と
	// newsfeed.news_articles（read-only クロススキーマプロキシ）に使用
	pool, err := pgxpool.New(ctx, cfg.DatabaseConn)
	if err != nil {
		slog.Error("failed to create pg pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Firestore クライアント (game_config)
	fsClient, err := firestore.NewClient(ctx, cfg.GoogleCloudProjectID)
	if err != nil {
		slog.Error("failed to create firestore client", "error", err)
		os.Exit(1)
	}
	defer func() { _ = fsClient.Close() }()

	// Firebase Auth クライアント
	authClient, err := middleware.NewFirebaseAuthClient(ctx)
	if err != nil {
		slog.Error("failed to create firebase auth client", "error", err)
		os.Exit(1)
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
		Auth: rest.NewAuthHandler(accountClient),
	}

	// ルーター
	r := gin.New()
	r.Use(middleware.UseRequestLogger(), gin.Recovery())
	r.Use(middleware.UseCORS(cfg.AllowedOrigins...))

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
	v1.Use(middleware.UseFirebaseAuth(authClient))

	router.RegisterAuthRoutes(v1, handlers)

	api := v1.Group("")
	api.Use(middleware.ResolvePlayer(accountClient))
	api.Use(middleware.IssueInternalAuth(internalSigner))
	if err := router.RegisterForwardRoutes(api, cfg); err != nil {
		slog.Error("failed to register forward routes", "error", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	srvCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	matchStream, err := pubsubadapter.NewStream(srvCtx, cfg.GoogleCloudProjectID, cfg.MatchmakingSubscription)
	if err != nil {
		slog.Error("failed to create matchmaking stream", "error", err)
		os.Exit(1)
	}
	defer func() { _ = matchStream.Close() }()

	matchSub, err := pubsubadapter.NewMatchSubscriber(matchStream, wsManager)
	if err != nil {
		slog.Error("failed to create match subscriber", "error", err)
		os.Exit(1)
	}

	// Why: graceful shutdown 時に subscriber と HTTP server が確実に停止するまで
	// main を block させるため errgroup で束ねる。どちらかが err を返すと
	// gCtx がキャンセルされ、他方も停止する。
	if err := runServices(srvCtx, cfg, srv, matchSub); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}

	slog.Info("gateway server exited")
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

	for _, subscriber := range subscribers {
		subscriber := subscriber
		g.Go(func() error {
			if err := subscriber.Run(gCtx); err != nil && gCtx.Err() == nil {
				return fmt.Errorf("subscriber: %w", err)
			}
			return nil
		})
	}

	g.Go(func() error {
		slog.Info("gateway server starting", "port", cfg.Port, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		<-gCtx.Done()
		slog.Info("shutting down gracefully")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), serverShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("http shutdown: %w", err)
		}
		return nil
	})

	return g.Wait()
}

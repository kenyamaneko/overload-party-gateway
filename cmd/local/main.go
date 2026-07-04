// Package main はローカルモード gateway のエントリポイントです。
//
// *_SERVICE_URL 環境変数で指定した URL の全下流サービスが起動している必要がある。
// cmd/main との違いは認証 middleware: UseDevAuth により Firebase ID トークンの代わりに
// `dev-token-{uid}` を使用でき、CORS は全オリジン許可となる。
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
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
	// ローカルモードは開発者端末での実行を前提とするため、人間が読みやすい
	// テキスト形式で出力する (GKE 上の cmd/main は Cloud Logging 互換 JSON)。
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})).With("service", "gateway"))
	slog.Info("gateway starting in local mode")

	cfg := config.Load()
	if cfg.DatabaseConn == "" {
		slog.Error("DATABASE_CONN must be set (gateway owns gateway.game_players and reads newsfeed.news_articles)")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DatabaseConn)
	if err != nil {
		slog.Error("failed to create pg pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	gamePlayerRepo := repository.NewPgGamePlayerRepository(pool)

	// ローカルモードでは Firestore (game_config) と matchmaking Pub/Sub subscriber は optional。
	// GOOGLE_CLOUD_PROJECT_ID が未設定なら両方スキップする (NPC バトルがメインワークフロー)。
	// FIRESTORE_EMULATOR_HOST が設定されていれば公式クライアントが自動的に
	// エミュレーターへルーティングする。
	if cfg.GoogleCloudProjectID != "" {
		fsClient, err := firestore.NewClient(ctx, cfg.GoogleCloudProjectID)
		if err != nil {
			slog.Error("failed to create firestore client", "error", err)
			os.Exit(1)
		}
		defer func() { _ = fsClient.Close() }()
		_ = repository.NewFirestoreGameConfigRepository(fsClient)
	} else {
		slog.Info("GOOGLE_CLOUD_PROJECT_ID is unset; skipping Firestore client and matchmaking Pub/Sub subscriber")
	}

	cardClient := cardclient.New(cfg.CardServiceURL)
	matchmakingClient := matchmakingclient.New(cfg.MatchmakingServiceURL)
	accountClient := accountclient.New(cfg.AccountServiceURL)

	if cfg.InternalAuthSecret == "" {
		slog.Error("INTERNAL_AUTH_SECRET must be set")
		os.Exit(1)
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
		Auth: rest.NewAuthHandler(accountClient),
	}

	r := gin.New()
	r.Use(middleware.UseRequestLogger(), gin.Recovery())
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
		slog.Error("failed to register forward routes", "error", err)
		os.Exit(1)
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
			slog.Error("failed to create matchmaking stream", "error", err)
			os.Exit(1)
		}
		defer func() { _ = stream.Close() }()
		subscriber, err := pubsubadapter.NewMatchSubscriber(stream, wsManager)
		if err != nil {
			slog.Error("failed to create match subscriber", "error", err)
			os.Exit(1)
		}
		go func() {
			if err := subscriber.Run(srvCtx); err != nil && srvCtx.Err() == nil {
				slog.Error("match subscriber error", "error", err)
				os.Exit(1)
			}
		}()
	}

	go func() {
		slog.Info("gateway local server starting", "addr", ":9001", "rest_base", "http://localhost:9001/api/v1/")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("listen failed", "error", err)
			os.Exit(1)
		}
	}()

	<-srvCtx.Done()
	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
		os.Exit(1)
	}
	slog.Info("gateway local server exited")
}

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
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	pubsubadapter "github.com/kenyamaneko/overload-party-gateway/internal/adapter/pubsub"
	"github.com/kenyamaneko/overload-party-gateway/internal/adapter/redistimer"
	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/cardclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/matchmakingclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/config"
	"github.com/kenyamaneko/overload-party-gateway/internal/handler/rest"
	ws "github.com/kenyamaneko/overload-party-gateway/internal/handler/ws"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
	"github.com/kenyamaneko/overload-party-gateway/internal/router"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

// invalidatedGameRecoveryTimeout は前回の停止で無効になった対戦を決着させる処理の上限時間。
const invalidatedGameRecoveryTimeout = 60 * time.Second

// exitOnStartupFailure は起動時の失敗を記録してプロセスを終了する。
func exitOnStartupFailure(message string, err error) {
	slog.Error(message, "error", err)
	os.Exit(1)
}

func main() {
	// ローカルモードは開発者端末での実行を前提とするため、人間が読みやすいテキスト形式で
	// 出力する (Cloud Run 上の cmd/main は Cloud Logging 互換 JSON)。
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})).With("service", "gateway"))
	slog.Info("gateway starting in local mode")

	cfg, err := config.FromEnv()
	if err != nil {
		exitOnStartupFailure("failed to load config", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DatabaseConn)
	if err != nil {
		exitOnStartupFailure("failed to create pg pool", err)
	}
	defer pool.Close()

	gamePlayerRepo := repository.NewPgGamePlayerRepository(pool)
	processedMatchRepo := repository.NewPgProcessedMatchRepository(pool)
	invalidatedGameRepo := repository.NewPgInvalidatedGameRepository(pool)

	// ローカルモードでは Firestore (game_config) は optional。GOOGLE_CLOUD_PROJECT_ID が
	// 未設定ならスキップする (NPC バトルがメインワークフロー)。FIRESTORE_EMULATOR_HOST が
	// 設定されていれば公式クライアントが自動的にエミュレーターへルーティングする。
	if cfg.GoogleCloudProjectID != "" {
		fsClient, err := firestore.NewClient(ctx, cfg.GoogleCloudProjectID)
		if err != nil {
			exitOnStartupFailure("failed to create firestore client", err)
		}
		defer func() { _ = fsClient.Close() }()
		_ = repository.NewFirestoreGameConfigRepository(fsClient)
	} else {
		slog.Info("GOOGLE_CLOUD_PROJECT_ID is unset; skipping Firestore client")
	}

	// ローカルの下流は Cloud Run ではなく呼び出し IAM が無いため、ID トークンを付けない。
	localHTTP := &http.Client{}
	cardClient := cardclient.New(cfg.CardServiceURL, localHTTP)
	matchmakingClient := matchmakingclient.New(cfg.MatchmakingServiceURL, uuid.Must(uuid.NewV7()).String(), localHTTP)
	accountClient := accountclient.New(cfg.AccountServiceURL, localHTTP)

	internalAuthKey, err := internalauth.ParsePrivateKeyPEM([]byte(cfg.InternalAuthPrivateKey))
	if err != nil {
		exitOnStartupFailure("INTERNAL_AUTH_PRIVATE_KEY is invalid", err)
	}
	internalSigner := internalauth.NewSigner(
		internalauth.StaticPrivateKeyResolver(internalAuthKey, internalauth.DefaultKeyID),
		internalauth.DefaultKeyID,
	)

	// 対戦ごとの計時の写しは UPSTASH_REDIS_URL が未設定なら行わない
	// (docker-compose の redis サービスを使わないローカル起動でも動く状態を保つ)。
	var timerStore port.TimerStore
	if cfg.UpstashRedisURL != "" {
		redisOpt, err := redis.ParseURL(cfg.UpstashRedisURL)
		if err != nil {
			exitOnStartupFailure("failed to parse UPSTASH_REDIS_URL", err)
		}
		redisClient := redis.NewClient(redisOpt)
		defer func() { _ = redisClient.Close() }()
		timerStore = redistimer.NewStore(redisClient)
	} else {
		slog.Info("UPSTASH_REDIS_URL is unset; skipping timer deadline mirroring")
	}

	battleClient := service.NewBattleClient(cfg.BattleServerURL, localHTTP)
	matchmakingTimeout := time.Duration(cfg.MatchmakingTimeoutSec) * time.Second
	wsManager := ws.NewManager(battleClient, accountClient, cardClient, matchmakingClient, gamePlayerRepo, processedMatchRepo, invalidatedGameRepo, matchmakingTimeout, internalSigner, timerStore, ws.DefaultDisconnectTimeout)
	wsHandler := ws.NewHandler(wsManager, nil, accountClient, nil)

	matchSub, err := pubsubadapter.NewMatchSubscriber(wsManager)
	if err != nil {
		exitOnStartupFailure("failed to create match subscriber", err)
	}

	handlers := &router.Handlers{
		Auth:   rest.NewAuthHandler(accountClient),
		PubSub: rest.NewPubSubPushHandler(matchSub),
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
		exitOnStartupFailure("failed to register forward routes", err)
	}

	srv := &http.Server{
		Addr:    ":9001",
		Handler: r,
	}

	srvCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("gateway local server starting", "rest", "http://localhost:9001/api/v1/")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			exitOnStartupFailure("listen failed", err)
		}
	}()

	go func() {
		recoveryCtx, cancel := context.WithTimeout(context.Background(), invalidatedGameRecoveryTimeout)
		defer cancel()
		wsManager.RecoverInvalidatedGames(recoveryCtx)
	}()

	<-srvCtx.Done()
	slog.Info("shutting down")

	wsShutdownCtx, wsCancel := context.WithTimeout(context.Background(), ws.ShutdownNotifyTimeout)
	wsManager.Shutdown(wsShutdownCtx)
	wsCancel()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		exitOnStartupFailure("server forced to shutdown", err)
	}
	slog.Info("gateway local server exited")
}

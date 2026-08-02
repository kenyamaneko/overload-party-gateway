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
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"

	pubsubadapter "github.com/kenyamaneko/overload-party-gateway/internal/adapter/pubsub"
	"github.com/kenyamaneko/overload-party-gateway/internal/adapter/redistimer"
	"github.com/kenyamaneko/overload-party-gateway/internal/auth/firebaseauth"
	"github.com/kenyamaneko/overload-party-gateway/internal/auth/internalauth"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/cardclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/matchmakingclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/runauth"
	"github.com/kenyamaneko/overload-party-gateway/internal/config"
	"github.com/kenyamaneko/overload-party-gateway/internal/handler/rest"
	ws "github.com/kenyamaneko/overload-party-gateway/internal/handler/ws"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/repository"
	"github.com/kenyamaneko/overload-party-gateway/internal/router"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

const serverShutdownTimeout = 10 * time.Second

// wsShutdownNotifier は errgroup で束ねる WS 終了通知の最小契約。
type wsShutdownNotifier interface {
	Shutdown(ctx context.Context)
}

// newCloudLoggingHandler は Cloud Logging に適合するログハンドラを生成する。
func newCloudLoggingHandler() slog.Handler {
	return slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
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

// exitOnMissingConfig は必須設定の欠落を記録してプロセスを終了する。
func exitOnMissingConfig(message string) {
	slog.Error(message)
	os.Exit(1)
}

// exitOnStartupFailure は起動時の失敗を記録してプロセスを終了する。
func exitOnStartupFailure(message string, err error) {
	slog.Error(message, "error", err)
	os.Exit(1)
}

func main() {
	ctx := context.Background()

	// cmd/main は Cloud Run 上で動作し Cloud Logging に取り込まれるため、常に
	// Cloud Logging 互換 JSON で出力する。ローカル実行のテキスト出力は cmd/local が担う。
	slog.SetDefault(slog.New(newCloudLoggingHandler()).With("service", "gateway"))

	cfg, err := config.FromEnv()
	if err != nil {
		exitOnMissingConfig(err.Error())
	}

	if cfg.Env == "prod" && len(cfg.AllowedOrigins) == 0 {
		exitOnMissingConfig("ALLOWED_ORIGINS must be set in production")
	}
	databaseIAMAuthEnabled := parseDatabaseIAMAuthEnabled(cfg.DatabaseIAMAuthEnabledRaw)
	if databaseIAMAuthEnabled && cfg.CloudSQLConnectionName == "" {
		exitOnMissingConfig("CLOUDSQL_CONNECTION_NAME must be set when DATABASE_IAM_AUTH_ENABLED=true")
	}
	if cfg.GoogleCloudProjectID == "" {
		exitOnMissingConfig("GOOGLE_CLOUD_PROJECT_ID must be set")
	}
	if cfg.PubSubPushServiceAccountEmail == "" {
		exitOnMissingConfig("PUBSUB_PUSH_SERVICE_ACCOUNT_EMAIL must be set")
	}
	if cfg.PubSubPushAudience == "" {
		exitOnMissingConfig("PUBSUB_PUSH_AUDIENCE must be set")
	}
	if cfg.UpstashRedisURL == "" {
		exitOnMissingConfig("UPSTASH_REDIS_URL must be set")
	}

	if cfg.Env == "prod" {
		gin.SetMode(gin.ReleaseMode)
	}

	// PostgreSQL 接続プール: gateway.game_players（gateway 所有）に使用
	pool, closeDatabasePool, err := newDatabasePool(ctx, cfg.DatabaseConn, databaseIAMAuthEnabled, cfg.CloudSQLConnectionName)
	if err != nil {
		exitOnStartupFailure("failed to create pg pool", err)
	}
	defer closeDatabasePool()
	defer pool.Close()

	// Firestore クライアント (game_config)
	fsClient, err := firestore.NewClient(ctx, cfg.GoogleCloudProjectID)
	if err != nil {
		exitOnStartupFailure("failed to create firestore client", err)
	}
	defer func() { _ = fsClient.Close() }()

	// Firebase Auth クライアント
	authClient, err := firebaseauth.NewClient(ctx)
	if err != nil {
		exitOnStartupFailure("failed to create firebase auth client", err)
	}

	// gateway 所有の game_players / processed_matches リポジトリ
	gamePlayerRepo := repository.NewPgGamePlayerRepository(pool)
	processedMatchRepo := repository.NewPgProcessedMatchRepository(pool)
	// game_config は現在 gateway の runtime パスから参照していない。
	// クライアント到達性は起動時に検証するため、repo を生成だけしておく。
	_ = repository.NewFirestoreGameConfigRepository(fsClient)

	// 外部サービスクライアント。Cloud Run の呼び出し IAM は audience ごとの ID トークンを
	// 見るため、呼び出し先ごとに別のクライアントを用意する。
	cardHTTP, err := runauth.NewClient(ctx, cfg.CardServiceURL)
	if err != nil {
		exitOnMissingConfig(err.Error())
	}
	matchmakingHTTP, err := runauth.NewClient(ctx, cfg.MatchmakingServiceURL)
	if err != nil {
		exitOnMissingConfig(err.Error())
	}
	accountHTTP, err := runauth.NewClient(ctx, cfg.AccountServiceURL)
	if err != nil {
		exitOnMissingConfig(err.Error())
	}
	battleHTTP, err := runauth.NewClient(ctx, cfg.BattleServerURL)
	if err != nil {
		exitOnMissingConfig(err.Error())
	}

	cardClient := cardclient.New(cfg.CardServiceURL, cardHTTP)
	matchmakingClient := matchmakingclient.New(cfg.MatchmakingServiceURL, uuid.Must(uuid.NewV7()).String(), matchmakingHTTP)
	accountClient := accountclient.New(cfg.AccountServiceURL, accountHTTP)

	internalAuthKey, err := internalauth.ParsePrivateKeyPEM([]byte(cfg.InternalAuthPrivateKey))
	if err != nil {
		exitOnMissingConfig(fmt.Sprintf("INTERNAL_AUTH_PRIVATE_KEY is invalid: %v", err))
	}
	internalSigner := internalauth.NewSigner(
		internalauth.StaticPrivateKeyResolver(internalAuthKey, internalauth.DefaultKeyID),
		internalauth.DefaultKeyID,
	)

	// 対戦ごとの計時 (切断猶予・ターン) の写しを保持する Redis client。
	// 接続は go-redis が遅延で確立するため、ここでは接続確認 (Ping) を行わない。
	// 到達不能でも各書き込み・読み出しがエラーを返すだけで対戦は継続する。
	redisOpt, err := redis.ParseURL(cfg.UpstashRedisURL)
	if err != nil {
		exitOnStartupFailure("failed to parse UPSTASH_REDIS_URL", err)
	}
	redisClient := redis.NewClient(redisOpt)
	defer func() { _ = redisClient.Close() }()
	timerStore := redistimer.NewStore(redisClient)

	// Battle クライアント（HTTP → battle server）
	battleClient := service.NewBattleClient(cfg.BattleServerURL, battleHTTP)
	matchmakingTimeout := time.Duration(cfg.MatchmakingTimeoutSec) * time.Second
	wsManager := ws.NewManager(battleClient, accountClient, cardClient, matchmakingClient, gamePlayerRepo, processedMatchRepo, matchmakingTimeout, internalSigner, timerStore, ws.DefaultDisconnectTimeout)
	wsHandler := ws.NewHandler(wsManager, authClient, accountClient, cfg.AllowedOrigins)

	matchSub, err := pubsubadapter.NewMatchSubscriber(wsManager)
	if err != nil {
		exitOnStartupFailure("failed to create match subscriber", err)
	}

	handlers := &router.Handlers{
		Auth:   rest.NewAuthHandler(accountClient),
		PubSub: rest.NewPubSubPushHandler(matchSub),
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

	// Pub/Sub push 配信の内部エンドポイント。allUsers に公開されるため、Cloud Run の
	// 呼び出し IAM に頼らずアプリ層で push リクエストの OIDC トークンを検証する。
	internalGroup := r.Group("/internal/v1")
	internalGroup.Use(middleware.UsePubSubPushAuth(
		middleware.NewGoogleIDTokenValidator(),
		cfg.PubSubPushServiceAccountEmail,
		cfg.PubSubPushAudience,
	))
	router.RegisterPubSubRoutes(internalGroup, handlers)

	v1 := r.Group("/api/v1")
	v1.Use(middleware.UseFirebaseAuth(firebaseauth.NewVerifier(authClient)))

	router.RegisterAuthRoutes(v1, handlers)

	api := v1.Group("")
	api.Use(middleware.ResolvePlayer(accountClient))
	api.Use(middleware.IssueInternalAuth(internalSigner))
	if err := router.RegisterForwardRoutes(api, cfg); err != nil {
		exitOnStartupFailure("failed to register forward routes", err)
	}

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	srvCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Why: graceful shutdown 時に HTTP server と WS 接続への終了通知が確実に完了する
	// まで main を block させるため errgroup で束ねる。
	if err := runServices(srvCtx, cfg, srv, wsManager); err != nil {
		exitOnStartupFailure("server", err)
	}

	slog.Info("gateway server exited")
}

// runServices は HTTP server を errgroup で起動する。ctx キャンセル
// (SIGINT/SIGTERM) を検知すると HTTP server の Shutdown を呼ぶ。対戦中を含む WS 接続には
// HTTP server の Shutdown と並行して終了を通知してから閉じる。
func runServices(
	ctx context.Context,
	cfg *config.Config,
	srv *http.Server,
	wsShutdown wsShutdownNotifier,
) error {
	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		slog.Info("gateway server starting", "port", cfg.Port, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		<-gCtx.Done()
		slog.Info("notifying WS connections of shutdown")
		wsCtx, cancel := context.WithTimeout(context.Background(), ws.ShutdownNotifyTimeout)
		defer cancel()
		wsShutdown.Shutdown(wsCtx)
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

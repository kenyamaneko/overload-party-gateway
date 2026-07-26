package config

import (
	"log"
	"os"
	"strconv"
	"strings"
)

// Config は gateway サービスの設定を保持します
type Config struct {
	Port     string
	Env      string
	LogLevel string

	AllowedOrigins []string

	BattleServerURL       string
	CardServiceURL        string
	MatchmakingServiceURL string
	AccountServiceURL     string
	ShopServiceURL        string
	ScenarioServiceURL    string
	NewsServiceURL        string
	SupportServiceURL     string

	// game_players は gateway が直接 Postgres に接続して読み書きする
	DatabaseConn string

	// GoogleCloudProjectID は Firestore (game_config) の対象プロジェクト ID。
	// ローカル/CI では FIRESTORE_EMULATOR_HOST を別途設定することでエミュレーターに接続する。
	GoogleCloudProjectID string

	// matchmaking_start 後のプレイヤー待機タイムアウト（秒）。
	// タイムアウト時に gateway がエラーを push し、上流の enqueue をキャンセルする。
	MatchmakingTimeoutSec int

	AppMinVersion    string
	AppLatestVersion string
	AppForceUpdate   bool

	// InternalAuthSecret は内部認証 JWT (HS256) の共有秘密鍵。
	InternalAuthSecret string

	// PubSubPushServiceAccountEmail は match-made push subscription の OIDC トークンを
	// 署名する Pub/Sub push 用サービスアカウントの email。
	PubSubPushServiceAccountEmail string
	// PubSubPushAudience は match-made push subscription の OIDC トークンに期待する aud クレーム
	// (Terraform 側で明示 audience を設定していないため push endpoint の URL と一致する)。
	PubSubPushAudience string

	// UpstashRedisURL は対戦ごとの計時 (切断猶予・ターン) の写しを保持する
	// Upstash Redis の接続 URL。未設定の場合は写しを行わない。
	UpstashRedisURL string
}

// Load は環境変数からサービス設定を読み込みます
func Load() *Config {
	return &Config{
		Port:     getEnv("PORT", "9001"),
		Env:      getEnv("ENV", "dev"),
		LogLevel: getEnv("LOG_LEVEL", "info"),

		AllowedOrigins: splitCSV(getEnv("ALLOWED_ORIGINS", "")),

		BattleServerURL:       getEnv("BATTLE_SERVER_URL", "http://localhost:9002"),
		CardServiceURL:        getEnv("CARD_SERVICE_URL", "http://localhost:9003"),
		MatchmakingServiceURL: getEnv("MATCHMAKING_SERVICE_URL", "http://localhost:9004"),
		AccountServiceURL:     getEnv("ACCOUNT_SERVICE_URL", "http://localhost:9005"),
		ShopServiceURL:        getEnv("SHOP_SERVICE_URL", "http://localhost:9006"),
		ScenarioServiceURL:    getEnv("SCENARIO_SERVICE_URL", "http://localhost:9007"),
		NewsServiceURL:        getEnv("NEWS_SERVICE_URL", "http://localhost:9008"),
		SupportServiceURL:     getEnv("SUPPORT_SERVICE_URL", "http://localhost:9009"),

		DatabaseConn: getEnv("DATABASE_CONN", ""),

		GoogleCloudProjectID: getEnv("GOOGLE_CLOUD_PROJECT_ID", ""),

		MatchmakingTimeoutSec: getEnvInt("MATCHMAKING_TIMEOUT_SEC", 60),

		AppMinVersion:    getEnv("APP_MIN_VERSION", "0.1.0"),
		AppLatestVersion: getEnv("APP_LATEST_VERSION", "0.1.0"),
		AppForceUpdate:   getEnv("APP_FORCE_UPDATE", "false") == "true",

		InternalAuthSecret: getEnv("INTERNAL_AUTH_SECRET", ""),

		PubSubPushServiceAccountEmail: getEnv("PUBSUB_PUSH_SERVICE_ACCOUNT_EMAIL", ""),
		PubSubPushAudience:            getEnv("PUBSUB_PUSH_AUDIENCE", ""),

		UpstashRedisURL: getEnv("UPSTASH_REDIS_URL", ""),
	}
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getEnvInt は環境変数を int として読み取る。未設定なら fallback、不正値なら fail-fast。
func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Fatalf("config: %s is not a valid int: %q", key, v)
	}
	return n
}

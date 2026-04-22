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

	// game_players / news_articles は gateway が直接 Postgres に接続して読み書きする
	DatabaseURL string

	PubsubProjectID         string
	MatchmakingSubscription string

	// FirestoreProjectID は game_config コレクションの読み取り先プロジェクト ID。
	// ローカル/CI では FIRESTORE_EMULATOR_HOST を別途設定することでエミュレーターに接続する。
	FirestoreProjectID string

	PlayerOnboardedSubscription  string
	FactionPurchasedSubscription string
	PremiumUpdatedSubscription   string

	// matchmaking_start 後のプレイヤー待機タイムアウト（秒）。
	// タイムアウト時に gateway がエラーを push し、上流の enqueue をキャンセルする。
	MatchmakingTimeoutSec int

	AppMinVersion    string
	AppLatestVersion string
	AppForceUpdate   bool
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

		DatabaseURL: getEnv("DATABASE_URL", ""),

		PubsubProjectID:         getEnv("PUBSUB_PROJECT_ID", ""),
		MatchmakingSubscription: getEnv("MATCHMAKING_SUBSCRIPTION", "matchmaking-events-gateway"),

		FirestoreProjectID: getEnv("FIRESTORE_PROJECT_ID", ""),

		PlayerOnboardedSubscription:  getEnv("PLAYER_ONBOARDED_SUBSCRIPTION", "player-onboarded-gateway-sub"),
		FactionPurchasedSubscription: getEnv("FACTION_PURCHASED_SUBSCRIPTION", "faction-purchased-gateway-sub"),
		PremiumUpdatedSubscription:   getEnv("PREMIUM_UPDATED_SUBSCRIPTION", "premium-updated-gateway-sub"),

		MatchmakingTimeoutSec: getEnvInt("MATCHMAKING_TIMEOUT_SEC", 60),

		AppMinVersion:    getEnv("APP_MIN_VERSION", "0.1.0"),
		AppLatestVersion: getEnv("APP_LATEST_VERSION", "0.1.0"),
		AppForceUpdate:   getEnv("APP_FORCE_UPDATE", "false") == "true",
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

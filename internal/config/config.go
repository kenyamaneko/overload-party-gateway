package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	defaultPort                  = "9001"
	defaultEnv                   = "dev"
	defaultAppVersion            = "0.1.0"
	defaultMatchmakingTimeoutSec = 30
)

// Config は gateway サービスの設定を保持します
type Config struct {
	Port string
	Env  string

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

	// DatabaseIAMAuthEnabledRaw は Cloud SQL への接続方式を選ぶ生の環境変数値。cmd/main が検証する。
	DatabaseIAMAuthEnabledRaw string

	// CloudSQLConnectionName は Cloud SQL インスタンスの接続名 (project:region:instance)。
	CloudSQLConnectionName string

	// GoogleCloudProjectID は Firestore (game_config) の対象プロジェクト ID。
	// ローカル/CI では FIRESTORE_EMULATOR_HOST を別途設定することでエミュレーターに接続する。
	GoogleCloudProjectID string

	// matchmaking_start 後のプレイヤー待機タイムアウト（秒）。
	// タイムアウト時に gateway がエラーを push し、上流の enqueue をキャンセルする。
	// 短すぎるとキューが浅い時間帯にプレイヤーが離脱しやすいため、matchmaking のキュー長メトリクスと併せて調整する。
	MatchmakingTimeoutSec int

	AppMinVersion    string
	AppLatestVersion string
	AppForceUpdate   bool
	AppStoreURL      string

	// InternalAuthPrivateKey は内部認証 JWT (RS256) の署名鍵。PEM 形式。
	InternalAuthPrivateKey string

	// PubSubPushServiceAccountEmail は match-made push subscription の OIDC トークンを
	// 署名する Pub/Sub push 用サービスアカウントの email。
	PubSubPushServiceAccountEmail string
	// PubSubPushAudience は match-made push subscription の OIDC トークンに期待する aud クレーム
	// (Terraform 側で明示 audience を設定していないため push endpoint の URL と一致する)。
	PubSubPushAudience string

	// UpstashRedisURL は対戦ごとの計時 (切断猶予・ターン) の写しを保持する
	// Upstash Redis の接続 URL。
	UpstashRedisURL string
}

// FromEnv は環境変数からサービス設定を構築します。
// 未設定の必須環境変数があれば即エラーで返し、デフォルトへの暗黙 fallback は行いません。
func FromEnv() (*Config, error) {
	port, err := getEnv("PORT", defaultPort)
	if err != nil {
		return nil, err
	}
	env, err := getEnv("ENV", defaultEnv)
	if err != nil {
		return nil, err
	}
	appMinVersion, err := getEnv("APP_MIN_VERSION", defaultAppVersion)
	if err != nil {
		return nil, err
	}
	appLatestVersion, err := getEnv("APP_LATEST_VERSION", defaultAppVersion)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Port: port,
		Env:  env,

		AllowedOrigins: splitCSV(os.Getenv("ALLOWED_ORIGINS")),

		DatabaseConn:              os.Getenv("DATABASE_CONN"),
		DatabaseIAMAuthEnabledRaw: os.Getenv("DATABASE_IAM_AUTH_ENABLED"),
		CloudSQLConnectionName:    os.Getenv("CLOUDSQL_CONNECTION_NAME"),

		GoogleCloudProjectID: os.Getenv("GOOGLE_CLOUD_PROJECT_ID"),

		AppMinVersion:    appMinVersion,
		AppLatestVersion: appLatestVersion,
		AppStoreURL:      os.Getenv("APP_STORE_URL"),

		InternalAuthPrivateKey: os.Getenv("INTERNAL_AUTH_PRIVATE_KEY"),

		PubSubPushServiceAccountEmail: os.Getenv("PUBSUB_PUSH_SERVICE_ACCOUNT_EMAIL"),
		PubSubPushAudience:            os.Getenv("PUBSUB_PUSH_AUDIENCE"),

		UpstashRedisURL: os.Getenv("UPSTASH_REDIS_URL"),
	}

	// 下流サービスの URL が欠けたまま起動すると、その宛先への転送が実行時まで
	// 誤りに見えないため、gateway が転送先に持つ 8 本すべてを必須にする。
	downstreamURLs := []struct {
		key   string
		field *string
	}{
		{"BATTLE_SERVER_URL", &cfg.BattleServerURL},
		{"CARD_SERVICE_URL", &cfg.CardServiceURL},
		{"MATCHMAKING_SERVICE_URL", &cfg.MatchmakingServiceURL},
		{"ACCOUNT_SERVICE_URL", &cfg.AccountServiceURL},
		{"SHOP_SERVICE_URL", &cfg.ShopServiceURL},
		{"SCENARIO_SERVICE_URL", &cfg.ScenarioServiceURL},
		{"NEWS_SERVICE_URL", &cfg.NewsServiceURL},
		{"SUPPORT_SERVICE_URL", &cfg.SupportServiceURL},
	}
	for _, u := range downstreamURLs {
		v := os.Getenv(u.key)
		if v == "" {
			return nil, fmt.Errorf("config: %s is required", u.key)
		}
		*u.field = v
	}

	if cfg.DatabaseConn == "" {
		return nil, fmt.Errorf("config: DATABASE_CONN is required (gateway owns gateway.game_players)")
	}
	if cfg.InternalAuthPrivateKey == "" {
		return nil, fmt.Errorf("config: INTERNAL_AUTH_PRIVATE_KEY is required")
	}

	forceUpdate, err := getEnvBool("APP_FORCE_UPDATE", false)
	if err != nil {
		return nil, err
	}
	cfg.AppForceUpdate = forceUpdate

	timeoutSec, err := getEnvInt("MATCHMAKING_TIMEOUT_SEC", defaultMatchmakingTimeoutSec)
	if err != nil {
		return nil, err
	}
	cfg.MatchmakingTimeoutSec = timeoutSec

	return cfg, nil
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

// ParseBool は環境変数の値を "true" / "false" のみ受け付けて解釈します。
func ParseBool(key, value string) (bool, error) {
	switch value {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("config: %s must be %q or %q, got %q", key, "true", "false", value)
	}
}

// lookupEnv は環境変数を読み取り、値と設定済みかどうかを返す。空文字が設定されていればエラーを返す。
// 値を投入する側が空文字を渡した状況は、既定値で動いてよい未設定とは別の設定ミスであるため区別する。
func lookupEnv(key string) (string, bool, error) {
	v, ok := os.LookupEnv(key)
	if !ok {
		return "", false, nil
	}
	if v == "" {
		return "", false, fmt.Errorf("config: %s is set but empty (unset it to use the default)", key)
	}
	return v, true, nil
}

// getEnv は環境変数を読み取る。未設定なら fallback、空文字ならエラーを返す。
func getEnv(key, fallback string) (string, error) {
	v, isSet, err := lookupEnv(key)
	if err != nil {
		return "", err
	}
	if !isSet {
		return fallback, nil
	}
	return v, nil
}

// getEnvInt は環境変数を int として読み取る。未設定なら fallback、空文字と不正値ならエラーを返す。
func getEnvInt(key string, fallback int) (int, error) {
	v, isSet, err := lookupEnv(key)
	if err != nil {
		return 0, err
	}
	if !isSet {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s %q: %w", key, v, err)
	}
	return n, nil
}

// getEnvBool は環境変数を bool として読み取る。未設定なら fallback、空文字と不正値ならエラーを返す。
func getEnvBool(key string, fallback bool) (bool, error) {
	v, isSet, err := lookupEnv(key)
	if err != nil {
		return false, err
	}
	if !isSet {
		return fallback, nil
	}
	return ParseBool(key, v)
}

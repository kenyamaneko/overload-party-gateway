package config

import (
	"os"
	"strings"
)

type Config struct {
	Port        string
	Env         string
	LogLevel    string
	DatabaseURL string // env: DATABASE_URL (PostgreSQL connection string)

	// Apple App Store
	AppleKeyID          string
	AppleIssuerID       string
	AppleBundleID       string
	ApplePrivateKeyPath string
	AppleEnvironment    string // "Production" or "Sandbox"

	// Google Play
	GooglePackageName string

	// CORS
	AllowedOrigins []string

	// Battle server
	BattleServerURL string // env: BATTLE_SERVER_URL

	// App version
	AppMinVersion    string // env: APP_MIN_VERSION
	AppLatestVersion string // env: APP_LATEST_VERSION
	AppForceUpdate   bool   // env: APP_FORCE_UPDATE

	// Story scripts (GCS bucket; empty = local filesystem fallback)
	StoryBucket string
}

func Load() *Config {
	return &Config{
		Port:        getEnv("PORT", "9001"),
		Env:         getEnv("ENV", "dev"),
		LogLevel:    getEnv("LOG_LEVEL", "info"),
		DatabaseURL: getEnv("DATABASE_URL", ""),

		AppleKeyID:          getEnv("APPLE_KEY_ID", ""),
		AppleIssuerID:       getEnv("APPLE_ISSUER_ID", ""),
		AppleBundleID:       getEnv("APPLE_BUNDLE_ID", ""),
		ApplePrivateKeyPath: getEnv("APPLE_PRIVATE_KEY_PATH", ""),
		AppleEnvironment:    getEnv("APPLE_ENVIRONMENT", "Sandbox"),

		GooglePackageName: getEnv("GOOGLE_PACKAGE_NAME", ""),

		AllowedOrigins: splitCSV(getEnv("ALLOWED_ORIGINS", "")),

		BattleServerURL: getEnv("BATTLE_SERVER_URL", "http://localhost:9002"),

		AppMinVersion:    getEnv("APP_MIN_VERSION", "0.1.0"),
		AppLatestVersion: getEnv("APP_LATEST_VERSION", "0.1.0"),
		AppForceUpdate:   getEnv("APP_FORCE_UPDATE", "false") == "true",

		StoryBucket: getEnv("STORY_BUCKET", ""),
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

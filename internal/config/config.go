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

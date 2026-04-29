package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoad_Defaults(t *testing.T) {
	cfg := Load()

	assert.Equal(t, "9001", cfg.Port)
	assert.Equal(t, "dev", cfg.Env)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "", cfg.DatabaseConn)
	assert.Nil(t, cfg.AllowedOrigins)
	assert.Equal(t, "http://localhost:9002", cfg.BattleServerURL)
	assert.Equal(t, "http://localhost:9003", cfg.CardServiceURL)
	assert.Equal(t, "http://localhost:9004", cfg.MatchmakingServiceURL)
	assert.Equal(t, "http://localhost:9005", cfg.AccountServiceURL)
	assert.Equal(t, "http://localhost:9006", cfg.ShopServiceURL)
	assert.Equal(t, "http://localhost:9007", cfg.ScenarioServiceURL)
	assert.Equal(t, "matchmaking-events-gateway", cfg.MatchmakingSubscription)
	assert.Equal(t, 60, cfg.MatchmakingTimeoutSec)
	assert.Equal(t, "0.1.0", cfg.AppMinVersion)
	assert.Equal(t, "0.1.0", cfg.AppLatestVersion)
	assert.False(t, cfg.AppForceUpdate)
}

func TestLoad_CustomValues(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("ENV", "production")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("DATABASE_CONN", "postgres://localhost:5432/mydb")
	t.Setenv("ALLOWED_ORIGINS", "http://localhost:3000")
	t.Setenv("BATTLE_SERVER_URL", "http://battle:9002")
	t.Setenv("CARD_SERVICE_URL", "http://card:9001")
	t.Setenv("ACCOUNT_SERVICE_URL", "http://account:9001")
	t.Setenv("SHOP_SERVICE_URL", "http://shop:9001")
	t.Setenv("SCENARIO_SERVICE_URL", "http://scenario:9001")
	t.Setenv("APP_MIN_VERSION", "1.0.0")
	t.Setenv("APP_LATEST_VERSION", "1.2.0")
	t.Setenv("APP_FORCE_UPDATE", "true")

	cfg := Load()

	assert.Equal(t, "8080", cfg.Port)
	assert.Equal(t, "production", cfg.Env)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, "postgres://localhost:5432/mydb", cfg.DatabaseConn)
	assert.Equal(t, []string{"http://localhost:3000"}, cfg.AllowedOrigins)
	assert.Equal(t, "http://battle:9002", cfg.BattleServerURL)
	assert.Equal(t, "http://card:9001", cfg.CardServiceURL)
	assert.Equal(t, "http://account:9001", cfg.AccountServiceURL)
	assert.Equal(t, "http://shop:9001", cfg.ShopServiceURL)
	assert.Equal(t, "http://scenario:9001", cfg.ScenarioServiceURL)
	assert.Equal(t, "1.0.0", cfg.AppMinVersion)
	assert.Equal(t, "1.2.0", cfg.AppLatestVersion)
	assert.True(t, cfg.AppForceUpdate)
}

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty string", "", nil},
		{"single value", "a", []string{"a"}},
		{"multiple values", "a,b,c", []string{"a", "b", "c"}},
		{"values with whitespace are trimmed", "a, b , c", []string{"a", "b", "c"}},
		{"only whitespace and commas yields empty", " , , ", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitCSV(tt.input)
			if tt.want == nil {
				assert.Empty(t, got)
			} else {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestLoad_AllowedOrigins(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "http://localhost:3000, https://example.com")

	cfg := Load()

	assert.Equal(t, []string{"http://localhost:3000", "https://example.com"}, cfg.AllowedOrigins)
}

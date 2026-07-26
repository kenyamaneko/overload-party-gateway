package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoad(t *testing.T) {
	t.Run("環境変数からの Config 構築", func(t *testing.T) {
		t.Run("env が未設定のとき、既定値で構築される", func(t *testing.T) {
			cfg := Load()

			assert.Equal(t, "9001", cfg.Port)
			assert.Equal(t, "dev", cfg.Env)
			assert.Equal(t, "info", cfg.LogLevel)
			assert.Equal(t, "", cfg.DatabaseConn)
			assert.Equal(t, "", cfg.DatabaseIAMAuthEnabledRaw)
			assert.Equal(t, "", cfg.CloudSQLConnectionName)
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
			assert.Equal(t, "", cfg.InternalAuthSecret)
		})

		t.Run("全 env を指定するとき、その値が反映される", func(t *testing.T) {
			t.Setenv("PORT", "8080")
			t.Setenv("ENV", "production")
			t.Setenv("LOG_LEVEL", "debug")
			t.Setenv("DATABASE_CONN", "postgres://localhost:5432/mydb")
			t.Setenv("DATABASE_IAM_AUTH_ENABLED", "true")
			t.Setenv("CLOUDSQL_CONNECTION_NAME", "overload-party-dev:asia-northeast1:overload-party-db")
			t.Setenv("ALLOWED_ORIGINS", "http://localhost:3000")
			t.Setenv("BATTLE_SERVER_URL", "http://battle:9002")
			t.Setenv("CARD_SERVICE_URL", "http://card:9001")
			t.Setenv("ACCOUNT_SERVICE_URL", "http://account:9001")
			t.Setenv("SHOP_SERVICE_URL", "http://shop:9001")
			t.Setenv("SCENARIO_SERVICE_URL", "http://scenario:9001")
			t.Setenv("APP_MIN_VERSION", "1.0.0")
			t.Setenv("APP_LATEST_VERSION", "1.2.0")
			t.Setenv("APP_FORCE_UPDATE", "true")
			t.Setenv("INTERNAL_AUTH_SECRET", "test-internal-auth-secret-32-bytes-min")

			cfg := Load()

			assert.Equal(t, "8080", cfg.Port)
			assert.Equal(t, "production", cfg.Env)
			assert.Equal(t, "debug", cfg.LogLevel)
			assert.Equal(t, "postgres://localhost:5432/mydb", cfg.DatabaseConn)
			assert.Equal(t, "true", cfg.DatabaseIAMAuthEnabledRaw)
			assert.Equal(t, "overload-party-dev:asia-northeast1:overload-party-db", cfg.CloudSQLConnectionName)
			assert.Equal(t, []string{"http://localhost:3000"}, cfg.AllowedOrigins)
			assert.Equal(t, "http://battle:9002", cfg.BattleServerURL)
			assert.Equal(t, "http://card:9001", cfg.CardServiceURL)
			assert.Equal(t, "http://account:9001", cfg.AccountServiceURL)
			assert.Equal(t, "http://shop:9001", cfg.ShopServiceURL)
			assert.Equal(t, "http://scenario:9001", cfg.ScenarioServiceURL)
			assert.Equal(t, "1.0.0", cfg.AppMinVersion)
			assert.Equal(t, "1.2.0", cfg.AppLatestVersion)
			assert.True(t, cfg.AppForceUpdate)
			assert.Equal(t, "test-internal-auth-secret-32-bytes-min", cfg.InternalAuthSecret)
		})

		t.Run("ALLOWED_ORIGINS を CSV 指定するとき、分割して格納される", func(t *testing.T) {
			t.Setenv("ALLOWED_ORIGINS", "http://localhost:3000, https://example.com")

			cfg := Load()

			assert.Equal(t, []string{"http://localhost:3000", "https://example.com"}, cfg.AllowedOrigins)
		})
	})
}

func TestSplitCSV(t *testing.T) {
	t.Run("CSV 文字列の分割", func(t *testing.T) {
		emptyCases := []struct {
			name  string
			input string
		}{
			{name: `空文字列のとき、空になる`, input: ""},
			{name: `空白とカンマのみ " , , " のとき、空になる`, input: " , , "},
		}
		for _, tc := range emptyCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Empty(t, splitCSV(tc.input))
			})
		}

		splitCases := []struct {
			name  string
			input string
			want  []string
		}{
			{name: `"a" のとき、[a] になる`, input: "a", want: []string{"a"}},
			{name: `"a,b,c" のとき、[a b c] になる`, input: "a,b,c", want: []string{"a", "b", "c"}},
			{name: `"a, b , c" のとき、空白を trim して [a b c] になる`, input: "a, b , c", want: []string{"a", "b", "c"}},
		}
		for _, tc := range splitCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.want, splitCSV(tc.input))
			})
		}
	})
}

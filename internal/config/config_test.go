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
			assert.Equal(t, 30, cfg.MatchmakingTimeoutSec)
			assert.Equal(t, "0.1.0", cfg.AppMinVersion)
			assert.Equal(t, "0.1.0", cfg.AppLatestVersion)
			assert.False(t, cfg.AppForceUpdate)
			assert.Equal(t, "", cfg.AppStoreURL)
			assert.Equal(t, "", cfg.InternalAuthPrivateKey)
			assert.Equal(t, "", cfg.PubSubPushServiceAccountEmail)
			assert.Equal(t, "", cfg.PubSubPushAudience)
		})

		t.Run("PORT を指定するとき、その値が採用される", func(t *testing.T) {
			t.Setenv("PORT", "8080")

			cfg := Load()

			assert.Equal(t, "8080", cfg.Port)
		})

		t.Run("MATCHMAKING_TIMEOUT_SEC に 120 を指定するとき、待機タイムアウトが 120 秒になる", func(t *testing.T) {
			t.Setenv("MATCHMAKING_TIMEOUT_SEC", "120")

			cfg := Load()

			assert.Equal(t, 120, cfg.MatchmakingTimeoutSec)
		})

		t.Run("APP_FORCE_UPDATE に true を指定するとき、強制更新が有効になる", func(t *testing.T) {
			t.Setenv("APP_FORCE_UPDATE", "true")

			cfg := Load()

			assert.True(t, cfg.AppForceUpdate)
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

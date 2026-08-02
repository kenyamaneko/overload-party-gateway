package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPrivateKeyPEM は設定が値をそのまま保持することの確認にだけ使うダミー。
// 鍵としての妥当性は検証しないため、PEM の体裁だけ揃えている。
const testPrivateKeyPEM = "-----BEGIN PRIVATE KEY-----\ndummy-not-a-real-key\n-----END PRIVATE KEY-----\n"

var allEnvKeys = []string{
	"PORT",
	"ENV",
	"ALLOWED_ORIGINS",
	"BATTLE_SERVER_URL",
	"CARD_SERVICE_URL",
	"MATCHMAKING_SERVICE_URL",
	"ACCOUNT_SERVICE_URL",
	"SHOP_SERVICE_URL",
	"SCENARIO_SERVICE_URL",
	"NEWS_SERVICE_URL",
	"SUPPORT_SERVICE_URL",
	"DATABASE_CONN",
	"DATABASE_IAM_AUTH_ENABLED",
	"CLOUDSQL_CONNECTION_NAME",
	"GOOGLE_CLOUD_PROJECT_ID",
	"MATCHMAKING_TIMEOUT_SEC",
	"APP_MIN_VERSION",
	"APP_LATEST_VERSION",
	"APP_FORCE_UPDATE",
	"APP_STORE_URL",
	"INTERNAL_AUTH_PRIVATE_KEY",
	"PUBSUB_PUSH_SERVICE_ACCOUNT_EMAIL",
	"PUBSUB_PUSH_AUDIENCE",
	"UPSTASH_REDIS_URL",
}

// setEnv は os.Getenv が "" と unset を区別しない性質を使い、未指定キーに "" を渡して未設定を再現する。
func setEnv(t *testing.T, envs map[string]string) {
	t.Helper()
	for _, k := range allEnvKeys {
		t.Setenv(k, envs[k])
	}
}

func mergeEnv(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

var validEnv = map[string]string{
	"BATTLE_SERVER_URL":         "http://battle.test",
	"CARD_SERVICE_URL":          "http://card.test",
	"MATCHMAKING_SERVICE_URL":   "http://matchmaking.test",
	"ACCOUNT_SERVICE_URL":       "http://account.test",
	"SHOP_SERVICE_URL":          "http://shop.test",
	"SCENARIO_SERVICE_URL":      "http://scenario.test",
	"NEWS_SERVICE_URL":          "http://news.test",
	"SUPPORT_SERVICE_URL":       "http://support.test",
	"DATABASE_CONN":             "host=localhost port=5432 dbname=gateway user=gateway password=gateway sslmode=disable",
	"INTERNAL_AUTH_PRIVATE_KEY": testPrivateKeyPEM,
}

func TestFromEnv(t *testing.T) {
	t.Run("環境変数からのサービス設定の読み込み", func(t *testing.T) {
		t.Run("必須 env が揃うとき、8 つの下流サービスの宛先・DB 接続文字列・内部認証の署名鍵が設定に入る", func(t *testing.T) {
			setEnv(t, validEnv)

			cfg, err := FromEnv()

			require.NoError(t, err)
			assert.Equal(t, "http://battle.test", cfg.BattleServerURL)
			assert.Equal(t, "http://card.test", cfg.CardServiceURL)
			assert.Equal(t, "http://matchmaking.test", cfg.MatchmakingServiceURL)
			assert.Equal(t, "http://account.test", cfg.AccountServiceURL)
			assert.Equal(t, "http://shop.test", cfg.ShopServiceURL)
			assert.Equal(t, "http://scenario.test", cfg.ScenarioServiceURL)
			assert.Equal(t, "http://news.test", cfg.NewsServiceURL)
			assert.Equal(t, "http://support.test", cfg.SupportServiceURL)
			assert.Equal(t, "host=localhost port=5432 dbname=gateway user=gateway password=gateway sslmode=disable", cfg.DatabaseConn)
			assert.Equal(t, testPrivateKeyPEM, cfg.InternalAuthPrivateKey)
		})

		t.Run("必須 env だけが揃うとき、待受ポートは 9001・動作環境は dev・マッチメイク待ちは 30 秒・アプリの最低/最新バージョンは 0.1.0・強制アップデートは無効になる", func(t *testing.T) {
			setEnv(t, validEnv)

			cfg, err := FromEnv()

			require.NoError(t, err)
			assert.Equal(t, "9001", cfg.Port)
			assert.Equal(t, "dev", cfg.Env)
			assert.Equal(t, 30, cfg.MatchmakingTimeoutSec)
			assert.Equal(t, "0.1.0", cfg.AppMinVersion)
			assert.Equal(t, "0.1.0", cfg.AppLatestVersion)
			assert.False(t, cfg.AppForceUpdate)
		})

		t.Run("Cloud SQL・Firestore・Pub/Sub push・Redis の env を指定するとき、それぞれの値が設定に入る", func(t *testing.T) {
			setEnv(t, mergeEnv(validEnv, map[string]string{
				"DATABASE_IAM_AUTH_ENABLED":         "true",
				"CLOUDSQL_CONNECTION_NAME":          "overload-party-test:asia-northeast1:overload-party-db",
				"GOOGLE_CLOUD_PROJECT_ID":           "overload-party-test",
				"PUBSUB_PUSH_SERVICE_ACCOUNT_EMAIL": "push@overload-party-test.iam.gserviceaccount.com",
				"PUBSUB_PUSH_AUDIENCE":              "https://gateway.test/internal/v1/pubsub/match-made",
				"UPSTASH_REDIS_URL":                 "rediss://default:token@redis.test:6379",
			}))

			cfg, err := FromEnv()

			require.NoError(t, err)
			assert.Equal(t, "true", cfg.DatabaseIAMAuthEnabledRaw)
			assert.Equal(t, "overload-party-test:asia-northeast1:overload-party-db", cfg.CloudSQLConnectionName)
			assert.Equal(t, "overload-party-test", cfg.GoogleCloudProjectID)
			assert.Equal(t, "push@overload-party-test.iam.gserviceaccount.com", cfg.PubSubPushServiceAccountEmail)
			assert.Equal(t, "https://gateway.test/internal/v1/pubsub/match-made", cfg.PubSubPushAudience)
			assert.Equal(t, "rediss://default:token@redis.test:6379", cfg.UpstashRedisURL)
		})

		t.Run("アプリの最低バージョンに 1.0.0・最新バージョンに 1.2.0・ストア URL を指定するとき、その 3 つが設定に入る", func(t *testing.T) {
			setEnv(t, mergeEnv(validEnv, map[string]string{
				"APP_MIN_VERSION":    "1.0.0",
				"APP_LATEST_VERSION": "1.2.0",
				"APP_STORE_URL":      "https://store.test/app",
			}))

			cfg, err := FromEnv()

			require.NoError(t, err)
			assert.Equal(t, "1.0.0", cfg.AppMinVersion)
			assert.Equal(t, "1.2.0", cfg.AppLatestVersion)
			assert.Equal(t, "https://store.test/app", cfg.AppStoreURL)
		})

		t.Run("PORT に 8080 を指定するとき、待受ポートが 8080 になる", func(t *testing.T) {
			setEnv(t, mergeEnv(validEnv, map[string]string{"PORT": "8080"}))

			cfg, err := FromEnv()

			require.NoError(t, err)
			assert.Equal(t, "8080", cfg.Port)
		})

		t.Run("ENV に prod を指定するとき、動作環境が prod になる", func(t *testing.T) {
			setEnv(t, mergeEnv(validEnv, map[string]string{"ENV": "prod"}))

			cfg, err := FromEnv()

			require.NoError(t, err)
			assert.Equal(t, "prod", cfg.Env)
		})

		t.Run("MATCHMAKING_TIMEOUT_SEC に 120 を指定するとき、マッチメイク待ちが 120 秒になる", func(t *testing.T) {
			setEnv(t, mergeEnv(validEnv, map[string]string{"MATCHMAKING_TIMEOUT_SEC": "120"}))

			cfg, err := FromEnv()

			require.NoError(t, err)
			assert.Equal(t, 120, cfg.MatchmakingTimeoutSec)
		})

		t.Run("APP_FORCE_UPDATE に true を指定するとき、強制アップデートが有効になる", func(t *testing.T) {
			setEnv(t, mergeEnv(validEnv, map[string]string{"APP_FORCE_UPDATE": "true"}))

			cfg, err := FromEnv()

			require.NoError(t, err)
			assert.True(t, cfg.AppForceUpdate)
		})

		t.Run("ALLOWED_ORIGINS をカンマ区切りで指定するとき、許可オリジンが分割して入る", func(t *testing.T) {
			setEnv(t, mergeEnv(validEnv, map[string]string{"ALLOWED_ORIGINS": "http://localhost:3000, https://example.com"}))

			cfg, err := FromEnv()

			require.NoError(t, err)
			assert.Equal(t, []string{"http://localhost:3000", "https://example.com"}, cfg.AllowedOrigins)
		})

		invalidCases := []struct {
			name    string
			envs    map[string]string
			wantErr string
		}{
			{
				name:    "BATTLE_SERVER_URL が未設定または空のとき、その変数名を挙げたエラーになる",
				envs:    mergeEnv(validEnv, map[string]string{"BATTLE_SERVER_URL": ""}),
				wantErr: "BATTLE_SERVER_URL is required",
			},
			{
				name:    "CARD_SERVICE_URL が未設定または空のとき、その変数名を挙げたエラーになる",
				envs:    mergeEnv(validEnv, map[string]string{"CARD_SERVICE_URL": ""}),
				wantErr: "CARD_SERVICE_URL is required",
			},
			{
				name:    "MATCHMAKING_SERVICE_URL が未設定または空のとき、その変数名を挙げたエラーになる",
				envs:    mergeEnv(validEnv, map[string]string{"MATCHMAKING_SERVICE_URL": ""}),
				wantErr: "MATCHMAKING_SERVICE_URL is required",
			},
			{
				name:    "ACCOUNT_SERVICE_URL が未設定または空のとき、その変数名を挙げたエラーになる",
				envs:    mergeEnv(validEnv, map[string]string{"ACCOUNT_SERVICE_URL": ""}),
				wantErr: "ACCOUNT_SERVICE_URL is required",
			},
			{
				name:    "SHOP_SERVICE_URL が未設定または空のとき、その変数名を挙げたエラーになる",
				envs:    mergeEnv(validEnv, map[string]string{"SHOP_SERVICE_URL": ""}),
				wantErr: "SHOP_SERVICE_URL is required",
			},
			{
				name:    "SCENARIO_SERVICE_URL が未設定または空のとき、その変数名を挙げたエラーになる",
				envs:    mergeEnv(validEnv, map[string]string{"SCENARIO_SERVICE_URL": ""}),
				wantErr: "SCENARIO_SERVICE_URL is required",
			},
			{
				name:    "NEWS_SERVICE_URL が未設定または空のとき、その変数名を挙げたエラーになる",
				envs:    mergeEnv(validEnv, map[string]string{"NEWS_SERVICE_URL": ""}),
				wantErr: "NEWS_SERVICE_URL is required",
			},
			{
				name:    "SUPPORT_SERVICE_URL が未設定または空のとき、その変数名を挙げたエラーになる",
				envs:    mergeEnv(validEnv, map[string]string{"SUPPORT_SERVICE_URL": ""}),
				wantErr: "SUPPORT_SERVICE_URL is required",
			},
			{
				name:    "DATABASE_CONN が未設定または空のとき、その変数名を挙げたエラーになる",
				envs:    mergeEnv(validEnv, map[string]string{"DATABASE_CONN": ""}),
				wantErr: "DATABASE_CONN is required",
			},
			{
				name:    "INTERNAL_AUTH_PRIVATE_KEY が未設定または空のとき、その変数名を挙げたエラーになる",
				envs:    mergeEnv(validEnv, map[string]string{"INTERNAL_AUTH_PRIVATE_KEY": ""}),
				wantErr: "INTERNAL_AUTH_PRIVATE_KEY is required",
			},
			{
				name:    "MATCHMAKING_TIMEOUT_SEC が数値でない abc のとき、その変数名を挙げたエラーになる",
				envs:    mergeEnv(validEnv, map[string]string{"MATCHMAKING_TIMEOUT_SEC": "abc"}),
				wantErr: `MATCHMAKING_TIMEOUT_SEC "abc"`,
			},
			{
				name:    `APP_FORCE_UPDATE が "true"/"false" 以外の yes のとき、その変数名を挙げたエラーになる`,
				envs:    mergeEnv(validEnv, map[string]string{"APP_FORCE_UPDATE": "yes"}),
				wantErr: "APP_FORCE_UPDATE must be",
			},
		}
		for _, tc := range invalidCases {
			t.Run(tc.name, func(t *testing.T) {
				setEnv(t, tc.envs)

				cfg, err := FromEnv()

				require.Error(t, err)
				assert.Nil(t, cfg)
				assert.Contains(t, err.Error(), tc.wantErr)
			})
		}
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

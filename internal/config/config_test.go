package config

import (
	"os"
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

var requiredKeys = []string{
	"BATTLE_SERVER_URL",
	"CARD_SERVICE_URL",
	"MATCHMAKING_SERVICE_URL",
	"ACCOUNT_SERVICE_URL",
	"SHOP_SERVICE_URL",
	"SCENARIO_SERVICE_URL",
	"NEWS_SERVICE_URL",
	"SUPPORT_SERVICE_URL",
	"DATABASE_CONN",
	"INTERNAL_AUTH_PRIVATE_KEY",
}

var keysWithDefault = []string{
	"PORT",
	"ENV",
	"APP_MIN_VERSION",
	"APP_LATEST_VERSION",
	"MATCHMAKING_TIMEOUT_SEC",
	"APP_FORCE_UPDATE",
}

// setEnv は envs に無いキーを未設定にする。空文字の設定と未設定を区別する仕様を確かめるため、
// t.Setenv が登録する復元処理を残したまま os.Unsetenv で未設定にする。
func setEnv(t *testing.T, envs map[string]string) {
	t.Helper()
	for _, k := range allEnvKeys {
		v, ok := envs[k]
		if !ok {
			t.Setenv(k, "")
			require.NoError(t, os.Unsetenv(k))
			continue
		}
		t.Setenv(k, v)
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

func withoutEnv(base map[string]string, key string) map[string]string {
	out := mergeEnv(base)
	delete(out, key)
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
		t.Run("必須envが揃うとき、8つの下流サービスの宛先・DB接続文字列・内部認証の署名鍵が設定に入る", func(t *testing.T) {
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

		t.Run("Cloud SQL・Firestore・Pub/Sub push・Redisのenvを指定するとき、それぞれの値が設定に入る", func(t *testing.T) {
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

		t.Run("PORTに8080を指定するとき、待受ポートが8080になる", func(t *testing.T) {
			setEnv(t, mergeEnv(validEnv, map[string]string{"PORT": "8080"}))

			cfg, err := FromEnv()

			require.NoError(t, err)
			assert.Equal(t, "8080", cfg.Port)
		})

		t.Run("MATCHMAKING_TIMEOUT_SECに120を指定するとき、マッチメイク待ちが120秒になる", func(t *testing.T) {
			setEnv(t, mergeEnv(validEnv, map[string]string{"MATCHMAKING_TIMEOUT_SEC": "120"}))

			cfg, err := FromEnv()

			require.NoError(t, err)
			assert.Equal(t, 120, cfg.MatchmakingTimeoutSec)
		})

		t.Run("ALLOWED_ORIGINSをカンマ区切りで指定するとき、許可オリジンが分割して入る", func(t *testing.T) {
			setEnv(t, mergeEnv(validEnv, map[string]string{"ALLOWED_ORIGINS": "http://localhost:3000, https://example.com"}))

			cfg, err := FromEnv()

			require.NoError(t, err)
			assert.Equal(t, []string{"http://localhost:3000", "https://example.com"}, cfg.AllowedOrigins)
		})

		for _, key := range requiredKeys {
			t.Run(key+" が未設定のとき、その変数名を挙げたエラーになる", func(t *testing.T) {
				setEnv(t, withoutEnv(validEnv, key))

				cfg, err := FromEnv()

				require.Error(t, err)
				assert.Nil(t, cfg)
				assert.Contains(t, err.Error(), key+" is required")
			})

			t.Run(key+" が空文字のとき、その変数名を挙げたエラーになる", func(t *testing.T) {
				setEnv(t, mergeEnv(validEnv, map[string]string{key: ""}))

				cfg, err := FromEnv()

				require.Error(t, err)
				assert.Nil(t, cfg)
				assert.Contains(t, err.Error(), key+" is required")
			})
		}

		for _, key := range keysWithDefault {
			t.Run(key+" が空文字のとき、既定値を使わずその変数名を挙げたエラーになる", func(t *testing.T) {
				setEnv(t, mergeEnv(validEnv, map[string]string{key: ""}))

				cfg, err := FromEnv()

				require.Error(t, err)
				assert.Nil(t, cfg)
				assert.Contains(t, err.Error(), key+" is set but empty")
			})
		}

		invalidCases := []struct {
			name    string
			envs    map[string]string
			wantErr string
		}{
			{
				name:    "MATCHMAKING_TIMEOUT_SECが数値でないabcのとき、その変数名を挙げたエラーになる",
				envs:    mergeEnv(validEnv, map[string]string{"MATCHMAKING_TIMEOUT_SEC": "abc"}),
				wantErr: `MATCHMAKING_TIMEOUT_SEC "abc"`,
			},
			{
				name:    `APP_FORCE_UPDATEが "true"/"false" 以外のyesのとき、その変数名を挙げたエラーになる`,
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

func TestParseBool(t *testing.T) {
	t.Run("真偽値を要求する環境変数の解釈", func(t *testing.T) {
		t.Run(`"true" のとき、有効になる`, func(t *testing.T) {
			enabled, err := ParseBool("TEST_FLAG", "true")

			require.NoError(t, err)
			assert.True(t, enabled)
		})

		t.Run(`"false" のとき、無効になる`, func(t *testing.T) {
			enabled, err := ParseBool("TEST_FLAG", "false")

			require.NoError(t, err)
			assert.False(t, enabled)
		})

		invalidCases := []struct {
			name  string
			value string
		}{
			{name: `"yes" のとき、変数名と受け付ける値を挙げたエラーになる`, value: "yes"},
			{name: `空文字のとき、変数名と受け付ける値を挙げたエラーになる`, value: ""},
		}
		for _, tc := range invalidCases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := ParseBool("TEST_FLAG", tc.value)

				require.Error(t, err)
				assert.Contains(t, err.Error(), `TEST_FLAG must be "true" or "false"`)
			})
		}
	})
}

func TestSplitCSV(t *testing.T) {
	t.Run("CSV文字列の分割", func(t *testing.T) {
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
			{name: `"a" のとき、[a]になる`, input: "a", want: []string{"a"}},
			{name: `"a,b,c" のとき、[a b c]になる`, input: "a,b,c", want: []string{"a", "b", "c"}},
			{name: `"a, b , c" のとき、空白をtrimして [a b c]になる`, input: "a, b , c", want: []string{"a", "b", "c"}},
			{name: `"a,,b" のとき、値の間の空要素を除いて [a b]になる`, input: "a,,b", want: []string{"a", "b"}},
		}
		for _, tc := range splitCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.want, splitCSV(tc.input))
			})
		}
	})
}
